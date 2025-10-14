package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
)

const (
	RepoOwner = "mordilloSan"
	RepoName  = "LinuxIO"
	GitHubAPI = "https://api.github.com/repos/%s/%s/releases/latest"
	BinPath   = "/usr/local/bin/linuxio"
)

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
		logger.Debugf("[update] cannot determine installed version, skipping check")
		return nil
	}

	latest, releaseURL := fetchLatestRelease()
	if latest == "" {
		logger.Debugf("[update] could not fetch latest release")
		return nil
	}

	if current != latest {
		logger.Infof("[update] update available: %s -> %s", current, latest)
		return &UpdateInfo{
			Available:      true,
			CurrentVersion: current,
			LatestVersion:  latest,
			ReleaseURL:     releaseURL,
		}
	}

	logger.Debugf("[update] already on latest version: %s", current)
	return &UpdateInfo{
		Available:      false,
		CurrentVersion: current,
	}
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

func fetchLatestRelease() (version string, releaseURL string) {
	client := &http.Client{Timeout: 5 * time.Second}

	url := fmt.Sprintf(GitHubAPI, RepoOwner, RepoName)
	resp, err := client.Get(url)
	if err != nil {
		logger.Debugf("[update] failed to fetch latest release: %v", err)
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("[update] GitHub API returned status %d", resp.StatusCode)
		return "", ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debugf("[update] failed to read response body: %v", err)
		return "", ""
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		logger.Debugf("[update] failed to parse JSON: %v", err)
		return "", ""
	}

	return release.TagName, release.HTMLURL
}
