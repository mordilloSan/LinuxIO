package control

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
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
	InstallScriptURL = "https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh"
)

// --- small helper for clean log lines (no ANSI) ---
var ansiRE = regexp.MustCompile(`\x1B\[[0-9;]*[A-Za-z]`)

func logStream(r io.Reader, prefix string, isInfo bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := ansiRE.ReplaceAllString(sc.Text(), "")
		if isInfo {
			logger.Infof("%s%s", prefix, line)
		} else {
			logger.Errorf("%s%s", prefix, line)
		}
	}
}

func ControlHandlers(shutdownChan chan string) map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"shutdown": func(args []string) (any, error) {
			reason := "unknown"
			if len(args) > 0 {
				reason = args[0]
			}
			logger.Debugf("Received shutdown command: %s", reason)
			select {
			case shutdownChan <- reason:
			default:
			}
			return "Bridge shutting down", nil
		},
		"ping": func(args []string) (any, error) {
			_ = args
			return map[string]string{"type": "pong"}, nil
		},
		"version": func(args []string) (any, error) {
			_ = args
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

	if currentVersion == targetVersion {
		return UpdateResult{
			Success:        true,
			CurrentVersion: currentVersion,
			Message:        fmt.Sprintf("already on version %s", targetVersion),
		}, nil
	}

	logger.Infof("[update] starting update: %s -> %s", currentVersion, targetVersion)

	logger.Infof("[update] running installation script for version %s", targetVersion)
	if err := runInstallScript(targetVersion); err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("installation script failed: %v", err),
		}, nil
	}

	logger.Debugf("[update] reloading systemd daemon")
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		logger.Warnf("[update] daemon-reload failed: %v (continuing anyway)", err)
	}

	logger.Infof("[update] restarting service with new version %s", targetVersion)
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := restartService(); err != nil {
			logger.Errorf("[update] failed to restart service: %v", err)
		}
	}()

	logger.Infof("[update] binaries updated, service restart initiated")
	return UpdateResult{
		Success:        true,
		CurrentVersion: currentVersion,
		NewVersion:     targetVersion,
		Message:        fmt.Sprintf("successfully updated from %s to %s - service restarting", currentVersion, targetVersion),
	}, nil
}

// runInstallScript downloads the installer and runs it in a transient unit
// with stdout/stderr piped back to this process (so logs appear in-order).
func runInstallScript(version string) error {
	// 1) Fetch the script in-process
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", InstallScriptURL, nil)
	if err != nil {
		return fmt.Errorf("build request failed: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	// 2) Run a transient unit with unique name and feed script on STDIN
	unit := fmt.Sprintf("linuxio-updater-%d", time.Now().UnixNano())
	args := []string{
		"--unit=" + unit,
		"--property=Description=LinuxIO updater",
		"--quiet",
		"--collect",
		"--wait",
		"--pipe",
		"--setenv=TERM=dumb",
		"--setenv=NO_COLOR=1",
		"--setenv=CLICOLOR=0",
		"--setenv=LC_ALL=C.UTF-8",
		"-p", "Type=exec",
		"-p", "ProtectSystem=full",
		"-p", "ReadWritePaths=/usr/local/bin",
		"-p", "PrivateTmp=false",
		"-p", "NoNewPrivileges=no",
		"/bin/bash", "-s", "--",
	}
	if version != "" {
		args = append(args, version)
	}

	logger.Infof("[update] systemd-run unit: %s", unit)

	cmd := exec.CommandContext(ctx, "systemd-run", args...)

	// Connect streams BEFORE Start
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	cmd.Stdin = resp.Body // Stream GitHub response directly

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start systemd-run: %w", err)
	}

	// Stream logs in real-time
	go logStream(stdout, "[update] ", true)
	go logStream(stderr, "[update] ", false)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("installer failed: %w", err)
	}

	return nil
}

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

func parseVersionOutput(output string) string {
	output = strings.TrimSpace(output)
	parts := strings.Fields(output)
	for _, part := range parts {
		if strings.HasPrefix(part, "v") && strings.Contains(part, ".") {
			return part
		}
		if strings.Count(part, ".") >= 2 {
			return "v" + part
		}
	}
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

	tagStart := strings.Index(string(body), `"tag_name":"`)
	if tagStart == -1 {
		return "", fmt.Errorf("tag_name not found in response")
	}
	tagStart += len(`"tag_name":"`)
	tagEnd := strings.Index(string(body)[tagStart:], `"`)
	if tagEnd == -1 {
		return "", fmt.Errorf("malformed tag_name in response")
	}
	return string(body)[tagStart : tagStart+tagEnd], nil
}

func restartService() error {
	logger.Infof("[update] restarting linuxio service")
	cmd := exec.Command("systemctl", "restart", "linuxio")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[update] restart failed: %v, output: %s", err, ansiRE.ReplaceAllString(string(output), ""))
		return fmt.Errorf("restart failed: %w", err)
	}
	logger.Infof("[update] service restarted successfully")
	return nil
}
