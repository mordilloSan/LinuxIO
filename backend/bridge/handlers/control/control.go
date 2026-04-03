package control

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/versioncmp"
)

const (
	maxGitHubReleaseBodyBytes int64 = 1 << 20
	maxHTTPErrorBodyBytes     int64 = 8 << 10
	maxChecksumBodyBytes      int64 = 64 << 10
	maxInstallScriptBodyBytes int64 = 4 << 20
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

func logStream(r io.Reader, prefix string, isInfo bool, relay io.Writer) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := ansiRE.ReplaceAllString(sc.Text(), "")
		if isInfo {
			logger.Infof("%s%s", prefix, line)
		} else {
			logger.Errorf("%s%s", prefix, line)
		}
		if relay != nil {
			// Best-effort relay; don't fail the update on write errors
			_, _ = io.WriteString(relay, line+"\n")
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
			info.UpdateAvailable = versioncmp.IsNewer(latestVersion, currentVersion)
		}
	}
	return info, nil
}

func buildInstallCommandArgs(unit string, scriptArgs ...string) []string {
	writablePaths := []string{
		config.BinDir,
		"/etc/linuxio",
		"/etc/pam.d",
		"/etc/pam.d/linuxio",
		"/etc/systemd/system",
		"/etc/motd.d",
		"/usr/lib/tmpfiles.d",
		"/usr/share/linuxio",
		"/var/lib/linuxIO",
	}

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
		"-p", "ReadWritePaths=" + strings.Join(writablePaths, " "),
		"-p", "PrivateTmp=false",
		"-p", "NoNewPrivileges=no",
		"/bin/bash", "-s", "--",
	}
	if len(scriptArgs) > 0 {
		args = append(args, scriptArgs...)
	}
	return args
}

// runInstallScript downloads the installer and runs it in a transient unit
// with stdout/stderr piped back to this process (so logs appear in-order).
// If relay is non-nil, output lines are also written to it.
func runInstallScript(version string, relay io.Writer) error {
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

	logger.Infof("systemd-run unit: %s", unit)

	var scriptArgs []string
	if version != "" {
		scriptArgs = append(scriptArgs, version)
	}
	cmd := exec.CommandContext(ctx, "systemd-run", buildInstallCommandArgs(unit, scriptArgs...)...)

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
	wg.Go(func() {
		logStream(stdout, "", true, relay)
	})
	wg.Go(func() {
		logStream(stderr, "", false, relay)
	})

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

	body, err := readBodyLimited(resp.Body, maxGitHubReleaseBodyBytes)
	if err != nil {
		return "", err
	}

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("tag_name not found in response")
	}
	return release.TagName, nil
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
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	checksumBytes, err := readBodyLimited(resp.Body, maxChecksumBodyBytes)
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
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	scriptBytes, err := readBodyLimited(resp.Body, maxInstallScriptBodyBytes)
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

func readBodyLimited(r io.Reader, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("response body exceeds %d bytes", max)
	}
	return body, nil
}

func readErrorBody(r io.Reader) string {
	body, err := readBodyLimited(r, maxHTTPErrorBodyBytes)
	if err != nil {
		return err.Error()
	}
	return string(body)
}

func restartService() error {
	logger.Infof("restarting linuxio service")
	var lastErr error
	for _, unit := range []string{"linuxio.service", "linuxio.target"} {
		if err := systemdapi.RestartUnit(unit); err == nil {
			logger.Infof("service restarted successfully via %s", unit)
			return nil
		} else {
			lastErr = err
			logger.Debugf("restart via %s failed: %v", unit, err)
		}
	}
	return fmt.Errorf("restart failed: %w", lastErr)
}
