package appupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	maxGitHubReleaseBodyBytes int64 = 1 << 20
	maxHTTPErrorBodyBytes     int64 = 8 << 10
	maxChecksumBodyBytes      int64 = 64 << 10
	maxInstallScriptBodyBytes int64 = 4 << 20
)

// buildScriptURLs constructs URLs to download install script and checksum from a specific release
func buildScriptURLs(ver string) (scriptURL, checksumURL string) {
	baseURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s",
		version.RepoOwner, version.RepoName, ver)
	return baseURL + "/install-linuxio-binaries.sh",
		baseURL + "/install-linuxio-binaries.sh.sha256"
}

// --- small helper for clean log lines (no ANSI) ---
var ansiRE = regexp.MustCompile(`\x1B\[[0-9;]*[A-Za-z]`)

func getVersionInfo(ctx context.Context) (apischema.VersionResponse, error) {
	currentVersion := getInstalledVersion(ctx)
	info := apischema.VersionResponse{
		CurrentVersion:  currentVersion,
		UpdateAvailable: false,
		CheckedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	latestVersion, err := fetchLatestVersion(ctx)
	if err != nil {
		slog.Debug("failed to fetch latest version", "component", "control", "subsystem", "version", "error", err)
		info.Error = fmt.Sprintf("could not check for updates: %v", err)
	} else {
		info.LatestVersion = latestVersion

		// For dev/untracked/unknown versions, always show update is available
		if strings.HasPrefix(currentVersion, "dev-") || currentVersion == "untracked" || currentVersion == "unknown" {
			info.UpdateAvailable = true
		} else {
			// For release versions, compare semantically
			info.UpdateAvailable = version.IsNewer(latestVersion, currentVersion)
		}
	}
	return info, nil
}

type updaterWritablePath struct {
	path     string
	optional bool
}

func updaterWritablePaths() []updaterWritablePath {
	return []updaterWritablePath{
		{path: version.BinDir},
		{path: "/etc/linuxio"},
		{path: "/etc/pam.d"},
		{path: "/etc/systemd/system"},
		{path: "/etc/motd.d", optional: true},
		{path: "/usr/lib/tmpfiles.d"},
		{path: "/usr/share/linuxio"},
		{path: "/var/lib/linuxIO"},
	}
}

func systemdReadWritePaths() []string {
	paths := make([]string, 0, len(updaterWritablePaths()))
	for _, entry := range updaterWritablePaths() {
		path := entry.path
		if entry.optional {
			path = "-" + path
		}
		paths = append(paths, path)
	}
	return paths
}

func ensureUpdaterWritablePathDirs() error {
	for _, entry := range updaterWritablePaths() {
		if entry.optional {
			continue
		}
		if err := os.MkdirAll(entry.path, 0o755); err != nil {
			return fmt.Errorf("create updater writable path %s: %w", entry.path, err)
		}
	}
	return nil
}

// downloadVerifiedInstallScript downloads and verifies the release installer.
// Execution is owned separately by the transient-unit executor.
func downloadVerifiedInstallScript(ctx context.Context, ver string) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 20 * time.Second}

	// Build URLs for the specific release version
	scriptURL, checksumURL := buildScriptURLs(ver)
	// 1) Download checksum file
	slog.Debug("downloading checksum", "component", "control", "subsystem", "app_update", "path", checksumURL)
	expectedChecksum, err := downloadChecksum(ctx, client, checksumURL)
	if err != nil {
		return nil, fmt.Errorf("download checksum failed: %w", err)
	}
	slog.Info("downloaded expected checksum", "component", "control", "subsystem", "app_update", "checksum", expectedChecksum)

	// 2) Download install script
	slog.Debug("downloading install script", "component", "control", "subsystem", "app_update", "path", scriptURL)
	scriptBytes, err := downloadScript(ctx, client, scriptURL)
	if err != nil {
		return nil, fmt.Errorf("download script failed: %w", err)
	}
	slog.Debug("downloaded install script", "component", "control", "subsystem", "app_update", "bytes", len(scriptBytes))

	// 3) Verify checksum
	actualChecksum := computeSHA256(scriptBytes)
	slog.Debug("computed install script checksum", "component", "control", "subsystem", "app_update", "checksum", actualChecksum)

	if actualChecksum != expectedChecksum {
		slog.Error("install script checksum mismatch", "component", "control", "subsystem", "app_update", "expected_checksum", expectedChecksum, "actual_checksum", actualChecksum)
		return nil, fmt.Errorf("checksum verification failed: script integrity compromised")
	}
	slog.Info("checksum verified successfully")
	return scriptBytes, nil
}

func systemdUnitJournalTail(parent context.Context, unit string) string {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", "80", "--no-pager", "--output=cat").CombinedOutput()
	if err != nil && len(output) == 0 {
		slog.Debug("failed to read updater unit journal", "component", "control", "subsystem", "app_update", "unit", unit, "error", err)
		return ""
	}
	return strings.TrimSpace(ansiRE.ReplaceAllString(string(output), ""))
}

func scriptArgs(ver string) []string {
	args := []string{"--defer-restart"}
	if ver != "" {
		args = append(args, ver)
	}
	return args
}

func getInstalledVersion(parent context.Context) string {
	// Use compiled-in version (most reliable)
	// The binary is compiled with -ldflags to set version.Version
	if version.Version != "" && version.Version != "untracked" {
		return version.Version
	}

	// Fallback: try running linuxio-webserver to get version
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, version.BinDir+"/linuxio-webserver", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug("failed to get version from binary", "component", "control", "subsystem", "version", "error", err)
		return "unknown"
	}
	ver := parseVersionOutput(string(output))
	slog.Debug("detected installed version", "component", "control", "subsystem", "version", "version", ver)
	return ver
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

func fetchLatestVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", version.RepoOwner, version.RepoName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	body, err := utils.ReadAllLimited(resp.Body, maxGitHubReleaseBodyBytes)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	checksumBytes, err := utils.ReadAllLimited(resp.Body, maxChecksumBodyBytes)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	scriptBytes, err := utils.ReadAllLimited(resp.Body, maxInstallScriptBodyBytes)
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

func readErrorBody(r io.Reader) string {
	body, err := utils.ReadAllLimited(r, maxHTTPErrorBodyBytes)
	if err != nil {
		return err.Error()
	}
	return string(body)
}
