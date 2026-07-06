package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:     "session",
	Aliases: []string{"sess"},
	Short:   "Manage sessions",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		filterProfile, _ := cmd.Flags().GetString("profile")
		filterExp, _ := cmd.Flags().GetString("experiment")
		filterStatus, _ := cmd.Flags().GetString("status")
		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")

		c := newClient()
		sessions, err := c.ListSessions(SessionListOptions{
			Profile:      filterProfile,
			ExperimentID: filterExp,
			Status:       filterStatus,
			Limit:        limit,
			Cursor:       cursor,
		})
		if err != nil {
			return err
		}

		filtered := filterSessions(sessions, filterProfile, filterExp, filterStatus)

		if flagOutput == "json" {
			printJSON(filtered)
			return nil
		}

		if len(filtered) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		w := newTabWriter()
		if flagOutput == "wide" {
			fmt.Fprintln(w, "ID\tPROFILE\tIMAGE\tPOD\tPOD-IP\tEXPERIMENT\tAGE")
			for _, s := range filtered {
				exp := s.ExperimentID
				if exp == "" {
					exp = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					s.ID, s.Profile, shortImage(s.Image), s.PodName, s.PodIP, exp, age(s.CreatedAt))
			}
		} else {
			fmt.Fprintln(w, "ID\tPROFILE\tIMAGE\tPOD\tEXPERIMENT\tAGE")
			for _, s := range filtered {
				exp := s.ExperimentID
				if exp == "" {
					exp = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					s.ID, s.Profile, shortImage(s.Image), s.PodName, exp, age(s.CreatedAt))
			}
		}
		return w.Flush()
	},
}

func filterSessions(sessions []SessionListItem, profile, experimentID, status string) []SessionListItem {
	filtered := make([]SessionListItem, 0, len(sessions))
	for _, s := range sessions {
		if profile != "" && s.Profile != profile {
			continue
		}
		if experimentID != "" && s.ExperimentID != experimentID {
			continue
		}
		if status != "" && s.Status != status {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

var sessionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a session",
	RunE: func(cmd *cobra.Command, args []string) error {
		image, _ := cmd.Flags().GetString("image")
		profile, _ := cmd.Flags().GetString("profile")
		idleTimeout, _ := cmd.Flags().GetInt("idle-timeout")
		privateContainers, err := privateContainersFromFlags(cmd)
		if err != nil {
			return err
		}

		if image == "" && profile == "" {
			return usageError("--image or --profile is required")
		}

		c := newClient()
		s, err := c.CreateSession(CreateSessionRequest{
			Image:              image,
			Profile:            profile,
			IdleTimeoutSeconds: idleTimeout,
			PrivateContainers:  privateContainers,
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(s)
			return nil
		}

		fmt.Printf("Session %s created.\n", s.ID)
		fmt.Printf("Sandbox:    %s\n", s.SandboxName)
		fmt.Printf("Image:      %s\n", s.Image)
		fmt.Printf("Profile:    %s\n", s.Profile)
		fmt.Printf("Pod:        %s\n", s.PodName)
		fmt.Printf("Pod IP:     %s\n", s.PodIP)
		return nil
	},
}

var sessionGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get session details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		s, err := c.GetSession(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(s)
			return nil
		}

		fmt.Printf("ID:         %s\n", s.ID)
		fmt.Printf("Sandbox:    %s\n", s.SandboxName)
		fmt.Printf("Image:      %s\n", s.Image)
		fmt.Printf("Profile:    %s\n", s.Profile)
		fmt.Printf("Pod:        %s\n", s.PodName)
		fmt.Printf("Pod IP:     %s\n", s.PodIP)
		fmt.Printf("Age:        %s\n", age(s.CreatedAt))
		return nil
	},
}

var sessionDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		if err := c.DeleteSession(args[0]); err != nil {
			return err
		}
		fmt.Printf("Session %s deleted.\n", args[0])
		return nil
	},
}

var sessionHistoryCmd = &cobra.Command{
	Use:   "history <id>",
	Short: "Show execution history",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		records, err := c.GetHistory(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(records)
			return nil
		}

		if len(records) == 0 {
			fmt.Println("No history.")
			return nil
		}

		w := newTabWriter()
		fmt.Fprintln(w, "STEP\tNAME\tEXIT\tDURATION\tTIME")
		for _, r := range records {
			fmt.Fprintf(w, "%d\t%s\t%d\t%dms\t%s\n",
				r.Index, r.Name, r.Output.ExitCode, r.DurationMs,
				r.Timestamp.Format("15:04:05"))
		}
		w.Flush()

		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			fmt.Println()
			for _, r := range records {
				fmt.Printf("--- step %d: %s ---\n", r.Index, r.Name)
				if r.Output.Stdout != "" {
					fmt.Println(r.Output.Stdout)
				}
				if r.Output.Stderr != "" {
					fmt.Fprintf(os.Stderr, "%s\n", r.Output.Stderr)
				}
			}
		}
		return nil
	},
}

var sessionTrajectoryCmd = &cobra.Command{
	Use:   "trajectory <id>",
	Short: "Export JSONL trajectory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outFile, _ := cmd.Flags().GetString("file")

		c := newClient()
		data, err := c.GetTrajectory(args[0])
		if err != nil {
			return err
		}

		if outFile != "" {
			if err := os.WriteFile(outFile, data, 0644); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
			fmt.Printf("Trajectory written to %s (%d bytes).\n", outFile, len(data))
			return nil
		}

		os.Stdout.Write(data)
		return nil
	},
}

var sessionUploadCmd = &cobra.Command{
	Use:   "upload <id> <local-path> <remote-path>",
	Short: "Upload a file into a session workspace",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		localPath := args[1]
		remotePath := args[2]
		expectedSHA256, _ := cmd.Flags().GetString("sha256")
		verify, _ := cmd.Flags().GetBool("verify")

		if verify && expectedSHA256 == "" {
			if localPath == "-" {
				return usageError("--verify with stdin requires --sha256")
			}
			digest, err := computeFileSHA256(localPath)
			if err != nil {
				return uploadLocalFileError(localPath, remotePath, err)
			}
			expectedSHA256 = digest
		}

		var input io.Reader = os.Stdin
		if localPath != "-" {
			file, err := os.Open(localPath)
			if err != nil {
				return uploadLocalFileError(localPath, remotePath, err)
			}
			defer file.Close()
			input = file
		}

		c := newClient()
		upload, err := c.UploadFile(args[0], remotePath, input, expectedSHA256)
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(upload)
			return nil
		}

		if upload.SHA256 != "" {
			fmt.Printf("Uploaded %s (%d bytes, sha256=%s).\n", upload.Path, upload.BytesWritten, upload.SHA256)
		} else {
			fmt.Printf("Uploaded %s (%d bytes).\n", upload.Path, upload.BytesWritten)
		}
		return nil
	},
}

var sessionDownloadCmd = &cobra.Command{
	Use:   "download <id> <remote-path> <local-path>",
	Short: "Download a file from a session workspace",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		remotePath := args[1]
		localPath := args[2]
		if localPath == "-" && flagOutput == "json" {
			return usageError("json output is not supported when downloading to stdout")
		}

		c := newClient()
		resp, err := c.DownloadFile(args[0], remotePath)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if localPath == "-" {
			_, err := io.Copy(os.Stdout, resp.Body)
			return err
		}

		file, err := os.Create(localPath)
		if err != nil {
			return fmt.Errorf("create local file: %w", err)
		}
		bytesWritten, copyErr := io.Copy(file, resp.Body)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("download file: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close local file: %w", closeErr)
		}

		sha := resp.Header.Get("X-ARL-SHA256")
		if sha == "" {
			sha = resp.Trailer.Get("X-ARL-SHA256")
		}
		size := resp.Header.Get("X-ARL-Size-Bytes")
		if size == "" {
			size = resp.Trailer.Get("X-ARL-Size-Bytes")
		}

		if flagOutput == "json" {
			info := map[string]any{
				"path":         localPath,
				"remotePath":   remotePath,
				"bytesWritten": bytesWritten,
			}
			if size != "" {
				info["sizeBytes"] = size
			}
			if sha != "" {
				info["sha256"] = sha
			}
			printJSON(info)
			return nil
		}

		if sha != "" {
			fmt.Printf("Downloaded %s to %s (%d bytes, sha256=%s).\n", remotePath, localPath, bytesWritten, sha)
		} else {
			fmt.Printf("Downloaded %s to %s (%d bytes).\n", remotePath, localPath, bytesWritten)
		}
		return nil
	},
}

var sessionExecCmd = &cobra.Command{
	Use:   "exec <id> -- <command...>",
	Short: "Execute a command in a session",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]
		cmdArgs := args[1:]

		if dash := cmd.ArgsLenAtDash(); dash >= 0 {
			cmdArgs = args[dash:]
		}

		if len(cmdArgs) == 0 {
			return usageError("no command specified; use: arl session exec <id> -- <cmd>")
		}

		c := newClient()
		resp, err := c.Execute(sessionID, ExecuteRequest{
			Steps: []StepRequest{
				{
					Name:    strings.Join(cmdArgs, " "),
					Command: cmdArgs,
				},
			},
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(resp)
			return nil
		}

		return printExecResults(resp.Results)
	},
}

var sessionExecContainerCmd = &cobra.Command{
	Use:   "exec-container <id> <container> -- <command...>",
	Short: "Execute a command in a private container",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]
		container := args[1]
		cmdArgs := args[2:]
		if dash := cmd.ArgsLenAtDash(); dash >= 0 {
			cmdArgs = args[dash:]
		}
		if len(cmdArgs) == 0 {
			return usageError("no command specified; use: arl session exec-container <id> <container> -- <cmd>")
		}

		workDir, _ := cmd.Flags().GetString("workdir")
		timeoutSeconds, _ := cmd.Flags().GetInt32("timeout")
		envValues, _ := cmd.Flags().GetStringArray("env")
		env, err := envMapFromFlags(envValues)
		if err != nil {
			return err
		}

		c := newClient()
		resp, err := c.ExecuteContainer(sessionID, container, ContainerExecuteRequest{
			Steps: []StepRequest{
				{
					Name:           strings.Join(cmdArgs, " "),
					Command:        cmdArgs,
					Env:            env,
					WorkDir:        workDir,
					TimeoutSeconds: timeoutSeconds,
				},
			},
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(resp)
			return nil
		}

		return printExecResults(resp.Results)
	},
}

var sessionRestoreCmd = &cobra.Command{
	Use:   "restore <id> <snapshot-id>",
	Short: "Restore a session to a snapshot",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		if err := c.Restore(args[0], args[1]); err != nil {
			return err
		}
		if flagOutput == "json" {
			printJSON(map[string]string{"status": "restored"})
			return nil
		}
		fmt.Printf("Session %s restored to %s.\n", args[0], args[1])
		return nil
	},
}

var sessionReplayCmd = &cobra.Command{
	Use:   "replay <target-id> --source <source-id>",
	Short: "Replay steps from another session into a target session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceID, _ := cmd.Flags().GetString("source")
		upToStep, _ := cmd.Flags().GetInt("up-to-step")
		if sourceID == "" {
			return usageError("--source is required")
		}

		var upTo *int
		if upToStep >= 0 {
			upTo = &upToStep
		}

		c := newClient()
		resp, err := c.Replay(args[0], ReplayRequest{
			SourceSessionID: sourceID,
			UpToStep:        upTo,
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(resp)
			return nil
		}

		fmt.Printf("Replayed %d step(s) into %s (errors=%d).\n", resp.StepsReplayed, args[0], resp.Errors)
		return nil
	},
}

var sessionShellCmd = &cobra.Command{
	Use:   "shell <id>",
	Short: "Open interactive shell (WebSocket)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShell(args[0])
	},
}

var sessionLogsCmd = &cobra.Command{
	Use:   "logs <id>",
	Short: "Stream session pod logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		tail, _ := cmd.Flags().GetInt("tail")

		c := newClient()
		return streamLogs(c, "/v1/sessions/"+args[0]+"/logs", follow, tail, false)
	},
}

func computeFileSHA256(localPath string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open local file for sha256: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("compute sha256: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func uploadLocalFileError(localPath, remotePath string, err error) error {
	if os.IsNotExist(err) {
		msg := fmt.Sprintf("open local file %q: %v; usage is: arl session upload <id> <local-path> <remote-path>", localPath, err)
		if remotePath != "-" {
			if _, statErr := os.Stat(remotePath); statErr == nil {
				msg += fmt.Sprintf("; %q exists locally, so the arguments may be reversed", remotePath)
			}
		}
		return usageError("%s", msg)
	}
	return fmt.Errorf("open local file %q: %w", localPath, err)
}

func init() {
	sessionListCmd.Flags().String("profile", "", "Filter by profile")
	sessionListCmd.Flags().String("experiment", "", "Filter by experiment ID")
	sessionListCmd.Flags().String("status", "", "Filter by status")
	sessionListCmd.Flags().Int("limit", 0, "Maximum sessions to return")
	sessionListCmd.Flags().String("cursor", "", "Return sessions after this session ID")

	sessionCreateCmd.Flags().String("image", "", "Container image")
	sessionCreateCmd.Flags().String("profile", "default", "Resource profile")
	sessionCreateCmd.Flags().Int("idle-timeout", 0, "Idle timeout in seconds (0 uses gateway default)")
	addPrivateContainerFlags(sessionCreateCmd)

	sessionHistoryCmd.Flags().BoolP("verbose", "v", false, "Show step output")

	sessionTrajectoryCmd.Flags().StringP("file", "f", "", "Write trajectory to file instead of stdout")

	sessionUploadCmd.Flags().String("sha256", "", "Expected SHA256 digest for gateway-side verification")
	sessionUploadCmd.Flags().Bool("verify", false, "Compute local SHA256 and have the gateway verify the upload")

	sessionReplayCmd.Flags().String("source", "", "Source session ID to replay from")
	sessionReplayCmd.Flags().Int("up-to-step", -1, "Replay through this step index (default: all steps)")

	sessionExecContainerCmd.Flags().String("workdir", "", "Working directory inside the private container")
	sessionExecContainerCmd.Flags().Int32("timeout", 0, "Command timeout in seconds (0 means gateway default/no step timeout)")
	sessionExecContainerCmd.Flags().StringArray("env", nil, "Environment variable in KEY=VALUE form; repeatable")

	sessionLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	sessionLogsCmd.Flags().Int("tail", 100, "Number of recent lines to show")

	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionCreateCmd)
	sessionCmd.AddCommand(sessionGetCmd)
	sessionCmd.AddCommand(sessionDeleteCmd)
	sessionCmd.AddCommand(sessionUploadCmd)
	sessionCmd.AddCommand(sessionDownloadCmd)
	sessionCmd.AddCommand(sessionHistoryCmd)
	sessionCmd.AddCommand(sessionTrajectoryCmd)
	sessionCmd.AddCommand(sessionExecCmd)
	sessionCmd.AddCommand(sessionExecContainerCmd)
	sessionCmd.AddCommand(sessionRestoreCmd)
	sessionCmd.AddCommand(sessionReplayCmd)
	sessionCmd.AddCommand(sessionShellCmd)
	sessionCmd.AddCommand(sessionLogsCmd)
}
