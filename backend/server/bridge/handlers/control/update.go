package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

const GitHubAPI = "https://api.github.com/repos/%s/%s/releases/latest"

type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	ReleaseURL     string `json:"release_url,omitempty"`
}

// CheckForUpdate queries GitHub for the latest release and compares with installed version.
// Called during login for privileged users only.
func CheckForUpdate() *UpdateInfo {
	current := getInstalledVersion()

	if current == "" || current == "unknown" {
		logger.Debugf("cannot determine installed version, skipping check")
		return nil
	}

	// Skip update check for development versions
	if strings.HasPrefix(current, "dev-") {
		logger.Debugf("running dev version (%s), skipping update check", current)
		return nil
	}

	latest, releaseURL := fetchLatestRelease()
	if latest == "" {
		logger.Debugf("could not fetch latest release")
		return nil
	}

	// Compare versions properly - only show update if latest is actually newer
	if isNewerVersion(latest, current) {
		logger.Infof("update available: %s -> %s", current, latest)
		return &UpdateInfo{
			Available:      true,
			CurrentVersion: current,
			LatestVersion:  latest,
			ReleaseURL:     releaseURL,
		}
	}

	logger.Debugf("already on latest version: %s", current)
	return &UpdateInfo{
		Available:      false,
		CurrentVersion: current,
	}
}

// isNewerVersion returns true if 'latest' is newer than 'current'.
// Handles versions like v0.4.1, v0.4.2, etc.
func isNewerVersion(latest, current string) bool {
	// Normalize versions (remove 'v' prefix if present)
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	// Compare each part numerically
	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		var l, c int
		fmt.Sscanf(latestParts[i], "%d", &l)
		fmt.Sscanf(currentParts[i], "%d", &c)
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}

	// If all compared parts are equal, longer version is newer (e.g., 1.0.1 > 1.0)
	return len(latestParts) > len(currentParts)
}

// getInstalledVersion runs 'linuxio --version' and parses the output
func getInstalledVersion() string {
	cmd := exec.Command(config.BinPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		// Fall back to compiled-in version (useful in dev mode where binary doesn't exist)
		if config.Version != "" && config.Version != "untracked" {
			return config.Version
		}
		logger.Debugf("failed to run linuxio --version: %v", err)
		return "unknown"
	}

	version := parseVersionOutput(string(output))
	logger.Debugf("detected installed version: %s", version)
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

func fetchLatestRelease() (version string, releaseURL string) {
	client := &http.Client{Timeout: 5 * time.Second}

	url := fmt.Sprintf(GitHubAPI, config.RepoOwner, config.RepoName)
	resp, err := client.Get(url)
	if err != nil {
		logger.Debugf("failed to fetch latest release: %v", err)
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("GitHub API returned status %d", resp.StatusCode)
		return "", ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debugf("failed to read response body: %v", err)
		return "", ""
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		logger.Debugf("failed to parse JSON: %v", err)
		return "", ""
	}

	return release.TagName, release.HTMLURL
}
