package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	releaseOwner = "Lincyaw"
	releaseRepo  = "agent-env"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Self-update to the latest release",
	Long:  "Download the latest arl binary from GitHub releases and replace the current binary.",
	RunE:  runUpdate,
}

var flagUpdateCheck bool

func init() {
	updateCmd.Flags().BoolVar(&flagUpdateCheck, "check", false, "Check for updates without installing")
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	current := version
	fmt.Fprintf(os.Stderr, "current version: %s\n", current)

	rel, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}

	fmt.Fprintf(os.Stderr, "latest version:  %s\n", rel.TagName)

	if current == rel.TagName || "v"+current == rel.TagName {
		fmt.Fprintln(os.Stderr, "already up to date")
		return nil
	}

	if flagUpdateCheck {
		fmt.Fprintf(os.Stderr, "update available: %s → %s\n", current, rel.TagName)
		return nil
	}

	binaryName := fmt.Sprintf("arl-%s-%s", runtime.GOOS, runtime.GOARCH)
	var binaryAsset, checksumAsset *ghAsset
	for i := range rel.Assets {
		switch rel.Assets[i].Name {
		case binaryName:
			binaryAsset = &rel.Assets[i]
		case binaryName + ".sha256":
			checksumAsset = &rel.Assets[i]
		}
	}
	if binaryAsset == nil {
		return fmt.Errorf("no binary for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	fmt.Fprintf(os.Stderr, "downloading %s ...\n", binaryAsset.Name)

	data, err := downloadAsset(binaryAsset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}

	if checksumAsset != nil {
		csData, err := downloadAsset(checksumAsset.BrowserDownloadURL)
		if err != nil {
			return fmt.Errorf("download checksum: %w", err)
		}
		expectedHash := strings.Fields(strings.TrimSpace(string(csData)))[0]
		actualHash := sha256hex(data)
		if actualHash != expectedHash {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
		}
		fmt.Fprintln(os.Stderr, "checksum verified")
	}

	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current binary: %w", err)
	}

	info, err := os.Stat(selfPath)
	if err != nil {
		return fmt.Errorf("stat current binary: %w", err)
	}

	oldPath := selfPath + ".old"
	if err := os.Rename(selfPath, oldPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	if err := os.WriteFile(selfPath, data, info.Mode()); err != nil {
		os.Rename(oldPath, selfPath)
		return fmt.Errorf("write new binary: %w", err)
	}

	os.Remove(oldPath)

	fmt.Fprintf(os.Stderr, "updated to %s\n", rel.TagName)
	return nil
}

func fetchLatestRelease() (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", releaseOwner, releaseRepo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadAsset(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
