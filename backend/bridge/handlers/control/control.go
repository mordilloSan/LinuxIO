package control

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

// buildScriptURLs constructs URLs to download install script and checksum from a specific release
func buildScriptURLs(version string) (scriptURL, checksumURL string) {
	baseURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s",
		config.RepoOwner, config.RepoName, version)
	return baseURL + "/install-linuxio-binaries.sh",
		baseURL + "/install-linuxio-binaries.sh.sha256"
}

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

		// For dev/untracked/unknown versions, always show update is available
		if strings.HasPrefix(currentVersion, "dev-") || currentVersion == "untracked" || currentVersion == "unknown" {
			info.UpdateAvailable = true
		} else {
			// For release versions, compare semantically
			info.UpdateAvailable = isNewerVersion(latestVersion, currentVersion)
		}
	}
	return info, nil
}

func performUpdate(targetVersion string) (UpdateResult, error) {
	currentVersion := getInstalledVersion()

	if targetVersion == "" {
		logger.Debugf("fetching latest version")
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

	logger.Infof("starting update: %s -> %s", currentVersion, targetVersion)

	logger.Infof("running installation script for version %s", targetVersion)
	if err := runInstallScript(targetVersion); err != nil {
		return UpdateResult{
			Success:        false,
			CurrentVersion: currentVersion,
			Error:          fmt.Sprintf("installation script failed: %v", err),
		}, nil
	}

	logger.Debugf("reloading systemd daemon")
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		logger.Warnf("daemon-reload failed: %v (continuing anyway)", err)
	}

	logger.Infof("restarting service with new version %s", targetVersion)
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := restartService(); err != nil {
			logger.Errorf("failed to restart service: %v", err)
		}
	}()

	logger.Infof("binaries updated, service restart initiated")
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 20 * time.Second}

	// Build URLs for the specific release version
	scriptURL, checksumURL := buildScriptURLs(version)

	// 1) Download checksum file
	logger.Debugf("downloading checksum from %s", checksumURL)
	expectedChecksum, err := downloadChecksum(ctx, client, checksumURL)
	if err != nil {
		return fmt.Errorf("download checksum failed: %w", err)
	}
	logger.Infof("expected checksum: %s", expectedChecksum)

	// 2) Download install script
	logger.Debugf("downloading install script from %s", scriptURL)
	scriptBytes, err := downloadScript(ctx, client, scriptURL)
	if err != nil {
		return fmt.Errorf("download script failed: %w", err)
	}
	logger.Debugf("downloaded %d bytes", len(scriptBytes))

	// 3) Verify checksum
	actualChecksum := computeSHA256(scriptBytes)
	logger.Debugf("computed checksum: %s", actualChecksum)

	if actualChecksum != expectedChecksum {
		logger.Errorf("SECURITY: checksum mismatch! expected=%s actual=%s", expectedChecksum, actualChecksum)
		return fmt.Errorf("checksum verification failed: script integrity compromised")
	}
	logger.Infof("checksum verified successfully")

	// 4) Run a transient unit with unique name and feed script on STDIN
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
		"-p", "ReadWritePaths=" + config.BinDir,
		"-p", "PrivateTmp=false",
		"-p", "NoNewPrivileges=no",
		"/bin/bash", "-s", "--",
	}
	if version != "" {
		args = append(args, version)
	}

	logger.Infof("systemd-run unit: %s", unit)

	cmd := exec.CommandContext(ctx, "systemd-run", args...)

	// Connect streams BEFORE Start
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	cmd.Stdin = bytes.NewReader(scriptBytes) // Feed verified script

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start systemd-run: %w", err)
	}

	// Stream logs in real-time with WaitGroup to ensure completion
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		logStream(stdout, "", true)
	}()
	go func() {
		defer wg.Done()
		logStream(stderr, "", false)
	}()

	// Wait for command to complete
	err = cmd.Wait()

	// Wait for log goroutines to finish processing all output
	wg.Wait()

	if err != nil {
		return fmt.Errorf("installer failed: %w", err)
	}

	return nil
}

func getInstalledVersion() string {
	// Use compiled-in version (most reliable)
	// The binary is compiled with -ldflags to set config.Version
	if config.Version != "" && config.Version != "untracked" {
		return config.Version
	}

	// Fallback: try running linuxio-webserver to get version
	cmd := exec.Command(config.BinDir+"/linuxio-webserver", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Debugf("failed to get version from binary: %v", err)
		return "unknown"
	}
	version := parseVersionOutput(string(output))
	logger.Debugf("detected installed version: %s", version)
	return version
}

func parseVersionOutput(output string) string {
	output = strings.TrimSpace(output)
	parts := strings.FieldsSeq(output)
	for part := range parts {
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
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", config.RepoOwner, config.RepoName)

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
	logger.Infof("restarting linuxio service")
	cmd := exec.Command("systemctl", "restart", "linuxio")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("restart failed: %v, output: %s", err, ansiRE.ReplaceAllString(string(output), ""))
		return fmt.Errorf("restart failed: %w", err)
	}
	logger.Infof("service restarted successfully")
	return nil
}

// isNewerVersion returns true if latest is semantically newer than current.
// Expects versions like "v1.2.3" or "1.2.3".
func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}

	// Strip leading 'v' if present
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	// Compare each numeric part
	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		latestNum, err1 := strconv.Atoi(latestParts[i])
		currentNum, err2 := strconv.Atoi(currentParts[i])
		if err1 != nil || err2 != nil {
			// If either part is not a valid number, compare as strings
			if latestParts[i] > currentParts[i] {
				return true
			}
			if latestParts[i] < currentParts[i] {
				return false
			}
			continue
		}

		if latestNum > currentNum {
			return true
		}
		if latestNum < currentNum {
			return false
		}
	}

	// If all compared parts are equal, longer version is newer (e.g., 1.2.3 > 1.2)
	return len(latestParts) > len(currentParts)
}

// downloadChecksum fetches the SHA256 checksum file from GitHub
func downloadChecksum(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	checksumBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	// Parse checksum (format: "abc123  filename" or just "abc123")
	checksum := strings.Fields(string(checksumBytes))
	if len(checksum) == 0 {
		return "", fmt.Errorf("empty checksum file")
	}

	return strings.TrimSpace(checksum[0]), nil
}

// downloadScript fetches the install script from GitHub
func downloadScript(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	scriptBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return scriptBytes, nil
}

// computeSHA256 computes the SHA256 hash of the given data
func computeSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
