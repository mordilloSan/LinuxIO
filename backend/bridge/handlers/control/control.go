package control

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
)

const (
	RepoOwner = "mordilloSan"
	RepoName  = "LinuxIO"
	BinDir    = "/usr/local/bin"
	BinPath   = BinDir + "/linuxio"
)

func ControlHandlers(shutdownChan chan string) map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"shutdown": func(args []string) (any, error) {
			reason := "unknown"
			if len(args) > 0 {
				reason = args[0] // "logout" or "forced"
			}
			logger.Debugf("Received shutdown command: %s", reason)
			select {
			case shutdownChan <- reason:
			default:
			}
			return "Bridge shutting down", nil
		},
		"ping": func(args []string) (any, error) {
			_ = args // Acknowledge we're not using this
			return map[string]string{"type": "pong"}, nil
		},
		"update": func(args []string) (any, error) {
			targetVersion := ""
			if len(args) > 0 {
				targetVersion = args[0] // Optional: specific version
			}
			return performUpdate(targetVersion)
		},
	}
}

type UpdateResult struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	CurrentVersion string `json:"current_version"`
	NewVersion     string `json:"new_version,omitempty"`
	Error          string `json:"error,omitempty"`
}

func performUpdate(targetVersion string) (UpdateResult, error) {
	currentVersion := getInstalledVersion()

	if targetVersion == "" {
		logger.Debugf("[update] fetching latest version")
		latest, err := fetchLatestVersion()
		if err != nil {
			return UpdateResult{
				Success:        false,
				CurrentVersion: currentVersion,
				Error:          fmt.Sprintf("failed to fetch latest version: %v", err),
			}, nil
		}
		targetVersion = latest
	}

	// Check if already on target version
	if currentVersion == targetVersion {
		return UpdateResult{
			Success:        true,
			CurrentVersion: currentVersion,
			Message:        fmt.Sprintf("already on version %s", targetVersion),
		}, nil
	}

	logger.Infof("[update] starting update: %s -> %s", currentVersion, targetVersion)

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "linuxio-update-*")
	if err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("failed to create temp directory: %v", err),
		}, nil
	}
	defer os.RemoveAll(tmpDir)

	// Download binaries and checksums
	logger.Debugf("[update] downloading release %s", targetVersion)
	if err := downloadRelease(targetVersion, tmpDir); err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("failed to download release: %v", err),
		}, nil
	}

	// Verify checksums
	logger.Debugf("[update] verifying checksums")
	if err := verifyChecksums(tmpDir); err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("checksum verification failed: %v", err),
		}, nil
	}

	// Stop service before replacing binaries
	logger.Debugf("[update] stopping service")
	if err := stopService(); err != nil {
		logger.Warnf("[update] failed to stop service: %v (continuing anyway)", err)
	}

	// Install new binaries
	logger.Debugf("[update] installing binaries")
	if err := installBinaries(tmpDir); err != nil {
		_ = startService() // Try to restart old service
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("failed to install binaries: %v", err),
		}, nil
	}

	// Start service with new binaries
	logger.Debugf("[update] starting service")
	if err := startService(); err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			NewVersion:     targetVersion,
			Error:          fmt.Sprintf("binaries updated but failed to start service: %v", err),
		}, nil
	}

	logger.Infof("[update] successfully updated to %s", targetVersion)
	return UpdateResult{
		Success:        true,
		CurrentVersion: currentVersion,
		NewVersion:     targetVersion,
		Message:        fmt.Sprintf("successfully updated from %s to %s", currentVersion, targetVersion),
	}, nil
}

// getInstalledVersion runs 'linuxio --version' and parses the output
func getInstalledVersion() string {
	cmd := exec.Command(BinPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		logger.Debugf("[update] failed to run linuxio --version: %v", err)
		return "unknown"
	}

	version := parseVersionOutput(string(output))
	logger.Debugf("[update] detected installed version: %s", version)
	return version
}

// parseVersionOutput extracts version string from binary output
// Handles formats like:
//
//	"linuxio version v0.2.4"
//	"v0.2.4"
//	"0.2.4"
func parseVersionOutput(output string) string {
	output = strings.TrimSpace(output)

	// Split by whitespace and look for version-like string
	parts := strings.Fields(output)
	for _, part := range parts {
		// Look for vX.Y.Z or X.Y.Z pattern
		if strings.HasPrefix(part, "v") && strings.Contains(part, ".") {
			return part
		}
		if strings.Count(part, ".") >= 2 {
			// Assume it's a version without 'v' prefix
			return "v" + part
		}
	}

	// Fallback: return the whole output if it looks like a version
	if strings.Contains(output, ".") {
		if !strings.HasPrefix(output, "v") {
			return "v" + output
		}
		return output
	}

	return "unknown"
}

func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", RepoOwner, RepoName)

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Simple parsing for tag_name (avoiding full JSON decode for minimal deps)
	tagStart := strings.Index(string(body), `"tag_name":"`)
	if tagStart == -1 {
		return "", fmt.Errorf("tag_name not found in response")
	}
	tagStart += len(`"tag_name":"`)
	tagEnd := strings.Index(string(body[tagStart:]), `"`)
	if tagEnd == -1 {
		return "", fmt.Errorf("malformed tag_name in response")
	}

	return string(body[tagStart : tagStart+tagEnd]), nil
}

func downloadRelease(version, destDir string) error {
	baseURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", RepoOwner, RepoName, version)

	files := []string{
		"linuxio",
		"linuxio-bridge",
		"linuxio-auth-helper",
		"SHA256SUMS",
	}

	client := &http.Client{Timeout: 2 * time.Minute}

	for _, file := range files {
		url := fmt.Sprintf("%s/%s", baseURL, file)
		destPath := filepath.Join(destDir, file)

		logger.Debugf("[update] downloading %s", file)

		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", file, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to download %s: status %d", file, resp.StatusCode)
		}

		out, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", file, err)
		}

		_, err = io.Copy(out, resp.Body)
		out.Close()
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", file, err)
		}
	}

	return nil
}

func verifyChecksums(dir string) error {
	checksumFile := filepath.Join(dir, "SHA256SUMS")
	data, err := os.ReadFile(checksumFile)
	if err != nil {
		return fmt.Errorf("failed to read checksums: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		expectedHash := parts[0]
		filename := parts[1]

		// Skip tarball in checksums
		if strings.HasSuffix(filename, ".tar.gz") {
			continue
		}

		filePath := filepath.Join(dir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue // File not downloaded
		}

		actualHash, err := calculateSHA256(filePath)
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", filename, err)
		}

		if actualHash != expectedHash {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", filename, expectedHash, actualHash)
		}

		logger.Debugf("[update] checksum verified for %s", filename)
	}

	return nil
}

func calculateSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func installBinaries(srcDir string) error {
	binaries := map[string]os.FileMode{
		"linuxio":             0755,
		"linuxio-bridge":      0755,
		"linuxio-auth-helper": 04755, // setuid
	}

	for binary, mode := range binaries {
		src := filepath.Join(srcDir, binary)
		dst := filepath.Join(BinDir, binary)

		// Read source
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", binary, err)
		}

		// Write to temporary file first (atomic)
		tmpDst := dst + ".new"
		if err := os.WriteFile(tmpDst, data, mode); err != nil {
			return fmt.Errorf("failed to write %s: %w", binary, err)
		}

		// Atomic rename
		if err := os.Rename(tmpDst, dst); err != nil {
			os.Remove(tmpDst)
			return fmt.Errorf("failed to install %s: %w", binary, err)
		}

		logger.Debugf("[update] installed %s", binary)
	}

	return nil
}

func stopService() error {
	cmd := exec.Command("systemctl", "stop", "linuxio.service")
	return cmd.Run()
}

func startService() error {
	cmd := exec.Command("systemctl", "start", "linuxio.service")
	return cmd.Run()
}
