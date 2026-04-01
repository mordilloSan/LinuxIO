package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/versioncmp"
)

const GitHubAPI = "https://api.github.com/repos/%s/%s/releases/latest"

const maxGitHubReleaseBodyBytes int64 = 1 << 20

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
	if versioncmp.IsNewer(latest, current) {
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

	body, err := readBodyLimited(resp.Body, maxGitHubReleaseBodyBytes)
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
	lines := strings.SplitSeq(string(output), "\n")

	for line := range lines {
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
