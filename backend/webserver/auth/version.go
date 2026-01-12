package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
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
// Handles versions like v0.4.1, dev-v0.4.1, etc.
// A release version (v1.2.3) is considered newer than a dev version (dev-v1.2.3) of the same number.
func isNewerVersion(latest, current string) bool {
	// Strip leading 'dev-' prefix for comparison (but remember if current was dev)
	currentIsDev := strings.HasPrefix(current, "dev-")
	latestIsDev := strings.HasPrefix(latest, "dev-")

	latest = strings.TrimPrefix(latest, "dev-")
	current = strings.TrimPrefix(current, "dev-")

	// Normalize versions (remove 'v' prefix if present)
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	// Compare each part numerically
	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		l, err1 := strconv.Atoi(latestParts[i])
		c, err2 := strconv.Atoi(currentParts[i])
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
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}

	// If version numbers are equal, check length
	if len(latestParts) > len(currentParts) {
		return true
	}
	if len(latestParts) < len(currentParts) {
		return false
	}

	// If version numbers are identical, a release is newer than a dev version
	// e.g., v0.6.1 > dev-v0.6.1
	if currentIsDev && !latestIsDev {
		return true
	}

	return false
}

// getInstalledVersion returns the compiled-in version from config.Version
func getInstalledVersion() string {
	if config.Version == "" || config.Version == "untracked" {
		return "unknown"
	}
	return config.Version
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

// getComponentVersions runs 'linuxio version' command and parses the output.
// Returns a map of component names to versions, or nil if the command fails.
func getComponentVersions() map[string]string {
	linuxioCLI := config.BinDir + "/linuxio"
	cmd := exec.Command(linuxioCLI, "version")
	output, err := cmd.Output()
	if err != nil {
		logger.Debugf("failed to run '%s version': %v", linuxioCLI, err)
		return nil
	}

	components := make(map[string]string)
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for lines like: "  LinuxIO Web Server dev-v0.6.9"
		if strings.HasPrefix(line, "LinuxIO ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				// Join all parts except the last one for the component name
				componentName := strings.Join(parts[:len(parts)-1], " ")
				version := parts[len(parts)-1]
				components[componentName] = version
			}
		}
	}

	if len(components) == 0 {
		return nil
	}

	return components
}
