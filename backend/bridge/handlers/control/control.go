package control

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/logger"
)

const (
	RepoOwner        = "mordilloSan"
	RepoName         = "LinuxIO"
	BinDir           = "/usr/local/bin"
	BinPath          = BinDir + "/linuxio"
	InstallScriptURL = "https://raw.githubusercontent.com/mordilloSan/LinuxIO/refs/heads/main/packaging/scripts/install-linuxio-binaries.sh"
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
		"version": func(args []string) (any, error) {
			_ = args // Acknowledge we're not using this
			return getVersionInfo()
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

type VersionInfo struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	CheckedAt       string `json:"checked_at"`
	Error           string `json:"error,omitempty"`
}

type UpdateResult struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	CurrentVersion string `json:"current_version"`
	NewVersion     string `json:"new_version,omitempty"`
	Error          string `json:"error,omitempty"`
}

func getVersionInfo() (VersionInfo, error) {
	currentVersion := getInstalledVersion()
	info := VersionInfo{
		CurrentVersion:  currentVersion,
		UpdateAvailable: false,
		CheckedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	// Optionally check for latest version (non-blocking)
	latestVersion, err := fetchLatestVersion()
	if err != nil {
		logger.Debugf("[version] failed to fetch latest version: %v", err)
		info.Error = fmt.Sprintf("could not check for updates: %v", err)
	} else {
		info.LatestVersion = latestVersion
		info.UpdateAvailable = currentVersion != latestVersion && currentVersion != "unknown"
	}

	return info, nil
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

	// Execute the installation script
	logger.Infof("[update] running installation script for version %s", targetVersion)
	if err := runInstallScript(targetVersion); err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("installation script failed: %v", err),
		}, nil
	}

	// Reload systemd daemon to pick up any service changes
	logger.Debugf("[update] reloading systemd daemon")
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		logger.Warnf("[update] daemon-reload failed: %v (continuing anyway)", err)
	}

	// Restart service with new binaries
	// Note: This will terminate the current process, so we spawn it in background
	logger.Infof("[update] restarting service with new version %s", targetVersion)
	go func() {
		// Small delay to allow this function to return response first
		time.Sleep(500 * time.Millisecond)
		if err := restartService(); err != nil {
			logger.Errorf("[update] failed to restart service: %v", err)
		}
	}()

	// Return success immediately - service will restart momentarily
	logger.Infof("[update] binaries updated, service restart initiated")
	return UpdateResult{
		Success:        true,
		CurrentVersion: currentVersion,
		NewVersion:     targetVersion,
		Message:        fmt.Sprintf("successfully updated from %s to %s - service restarting", currentVersion, targetVersion),
	}, nil
}

func runInstallScript(version string) error {
	tmp := "/tmp/linuxio-install.sh"

	// Download script
	if out, err := exec.Command("bash", "-c",
		fmt.Sprintf("curl -fsSL %s -o %s", InstallScriptURL, tmp)).CombinedOutput(); err != nil {
		logger.Errorf("[update] download failed:\n%s", string(out))
		return fmt.Errorf("download failed: %w", err)
	}

	// Execute script
	cmd := exec.Command("sudo", "bash", tmp, version)
	out, err := cmd.CombinedOutput()

	// SANITIZE OUTPUT - strip binary data
	sanitized := strings.ToValidUTF8(string(out), "�")

	if err != nil {
		logger.Errorf("[update] installation failed:\n%s", sanitized)
		return fmt.Errorf("script execution failed: %w", err)
	}

	logger.Infof("[update] installation output:\n%s", sanitized)
	return nil
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

func restartService() error {
	logger.Infof("[update] restarting linuxio service")
	cmd := exec.Command("systemctl", "restart", "linuxio")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[update] restart failed: %v, output: %s", err, string(output))
		return fmt.Errorf("restart failed: %w", err)
	}
	logger.Infof("[update] service restarted successfully")
	return nil
}
