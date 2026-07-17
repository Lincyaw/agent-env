package execagent

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Checkpointer captures per-step filesystem diffs using overlayfs.
// Each command is executed inside a new mount namespace with an overlay
// mounted on /. After the command exits, the overlay upper directory
// contains exactly the files that were changed, which are then applied
// back to the real filesystem.
type Checkpointer struct {
	baseDir string // e.g. "/mnt/arl-checkpoint"
	stepNum int
	enabled bool
	mu      sync.Mutex
}

// NewCheckpointer returns a Checkpointer rooted at baseDir.
// It creates the base directory if it does not exist.
func NewCheckpointer(baseDir string) *Checkpointer {
	os.MkdirAll(baseDir, 0755)
	return &Checkpointer{
		baseDir: baseDir,
		enabled: true,
	}
}

// StepDir returns the upper directory path for a given step number.
func (c *Checkpointer) StepDir(step int) string {
	return filepath.Join(c.baseDir, fmt.Sprintf("step-%d", step), "upper")
}

// WrapExec rewrites originalCmd so it runs inside a new mount namespace
// with an overlayfs on /. The returned command should be started by the
// caller; stdout/stderr pipes are not set up here. The step number is
// returned for later use with ApplyStep.
func (c *Checkpointer) WrapExec(ctx context.Context, originalCmd *exec.Cmd) (*exec.Cmd, int, error) {
	c.mu.Lock()
	c.stepNum++
	step := c.stepNum
	c.mu.Unlock()

	stepDir := filepath.Join(c.baseDir, fmt.Sprintf("step-%d", step))
	upperDir := filepath.Join(stepDir, "upper")
	workDir := filepath.Join(stepDir, "work")

	if err := os.MkdirAll(upperDir, 0755); err != nil {
		return originalCmd, 0, fmt.Errorf("create upper dir: %w", err)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return originalCmd, 0, fmt.Errorf("create work dir: %w", err)
	}

	selfExe, err := os.Executable()
	if err != nil {
		return originalCmd, 0, fmt.Errorf("resolve self executable: %w", err)
	}

	// Re-exec self with a sentinel separator, followed by the original command.
	args := append([]string{"--"}, originalCmd.Args...)
	wrapped := exec.CommandContext(ctx, selfExe, args...)
	wrapped.Dir = originalCmd.Dir

	env := originalCmd.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	wrapped.Env = append(env,
		"_ARL_OVERLAY_CHILD=1",
		"_ARL_OVERLAY_UPPER="+upperDir,
		"_ARL_OVERLAY_WORK="+workDir,
	)
	wrapped.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS,
	}

	return wrapped, step, nil
}

// ApplyStep copies changed files from the overlay upper directory back
// to the real filesystem and handles overlayfs whiteout markers (file
// deletions).
func (c *Checkpointer) ApplyStep(step int) error {
	upperDir := filepath.Join(c.baseDir, fmt.Sprintf("step-%d", step), "upper")
	return filepath.Walk(upperDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("[checkpoint] walk error at %s: %v", path, err)
			return nil
		}
		rel, _ := filepath.Rel(upperDir, path)
		if rel == "." {
			return nil
		}
		dst := filepath.Join("/", rel)

		// Overlayfs whiteout: character device with rdev 0,0 means the
		// file was deleted in the overlay.
		if info.Mode()&os.ModeCharDevice != 0 {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if ok && stat.Rdev == 0 {
				os.Remove(dst)
				return nil
			}
		}

		if info.IsDir() {
			os.MkdirAll(dst, info.Mode().Perm())
			return nil
		}

		return copyFile(path, dst, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	os.MkdirAll(filepath.Dir(dst), 0755)
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// RunOverlayChild is called from main() when the process is a re-exec'd
// child that should mount an overlay on / and then exec the real command.
// It never returns on success.
func RunOverlayChild() {
	upperDir := os.Getenv("_ARL_OVERLAY_UPPER")
	workDir := os.Getenv("_ARL_OVERLAY_WORK")
	if upperDir == "" || workDir == "" {
		fmt.Fprintln(os.Stderr, "overlay child: _ARL_OVERLAY_UPPER/_ARL_OVERLAY_WORK not set")
		os.Exit(126)
	}

	opts := fmt.Sprintf("lowerdir=/,upperdir=%s,workdir=%s", upperDir, workDir)
	if err := syscall.Mount("overlay", "/", "overlay", 0, opts); err != nil {
		fmt.Fprintf(os.Stderr, "overlay mount failed: %v\n", err)
		os.Exit(126)
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "overlay child: no command specified")
		os.Exit(126)
	}

	cmdPath, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "command not found: %s: %v\n", args[0], err)
		os.Exit(127)
	}

	// Filter out internal overlay env vars before exec'ing the real command.
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "_ARL_OVERLAY_CHILD=") ||
			strings.HasPrefix(e, "_ARL_OVERLAY_UPPER=") ||
			strings.HasPrefix(e, "_ARL_OVERLAY_WORK=") {
			continue
		}
		env = append(env, e)
	}

	syscall.Exec(cmdPath, args, env)
	os.Exit(127)
}
