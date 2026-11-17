package appupdate

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/iolimit"
	ver "github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

// checkForUpdate is a test seam.
var checkForUpdate = CheckForUpdate

const GitHubAPI = "https://api.github.com/repos/%s/%s/releases/latest"

const maxGitHubReleaseBodyBytes int64 = 1 << 20

// readComponentVersions reads the versions the installer recorded; tests
// swap it.
var readComponentVersions = func() (map[string]string, error) {
	return ver.ReadComponentVersions(ver.ComponentsFile)
}

type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	ReleaseURL     string `json:"release_url,omitempty"`
}

// CheckForUpdate queries GitHub for the latest release and compares it with
// the installed version. The request is scoped to ctx so callers can cancel
// an in-flight update check.
func CheckForUpdate(ctx context.Context) *UpdateInfo {
	current := getInstalledVersion()

	if current == "" || current == "unknown" {
		slog.Debug("cannot determine installed version, skipping check")
		return nil
	}

	latest, releaseURL := fetchLatestRelease(ctx)
	if latest == "" {
		slog.Debug("could not fetch latest release")
		return nil
	}

	// Compare versions properly - only show update if latest is actually newer
	if ver.IsNewer(latest, current) {
		slog.Debug("update available", "current_version", current, "latest_version", latest)
		return &UpdateInfo{
			Available:      true,
			CurrentVersion: current,
			LatestVersion:  latest,
			ReleaseURL:     releaseURL,
		}
	}
	slog.Debug("already on latest version", "current_version", current)
	return &UpdateInfo{
		Available:      false,
		CurrentVersion: current,
	}
}

// getInstalledVersion returns the compiled-in version from ver.Version
func getInstalledVersion() string {
	if ver.Version == "" || ver.Version == "untracked" {
		return "unknown"
	}
	return ver.Version
}

func fetchLatestRelease(ctx context.Context) (version string, releaseURL string) {
	client := &http.Client{Timeout: 5 * time.Second}

	url := fmt.Sprintf(GitHubAPI, ver.RepoOwner, ver.RepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Debug("failed to create latest release request", "error", err)
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("failed to fetch latest release", "error", err)
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("GitHub API returned non-OK status", "status", resp.StatusCode)
		return "", ""
	}

	body, err := iolimit.ReadAll(resp.Body, maxGitHubReleaseBodyBytes)
	if err != nil {
		slog.Debug("failed to read latest release response body", "error", err)
		return "", ""
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(body, &release, json.MatchCaseInsensitiveNames(true)); err != nil {
		slog.Debug("failed to parse latest release response", "error", err)
		return "", ""
	}

	return release.TagName, release.HTMLURL
}

// getComponentVersions returns each program's version as the installer
// recorded it, and the webserver's own from this build. It runs no program:
// /api/version is unauthenticated, and several programs are root only.
func getComponentVersions() map[string]string {
	components, err := readComponentVersions()
	if err != nil {
		slog.Debug("failed to read component versions", "path", ver.ComponentsFile, "error", err)
		components = map[string]string{}
	}
	if ver.Version != "" {
		components[webserverComponent] = ver.Version
	}
	return components
}

// webserverComponent is the webserver's name in ver.Components.
const webserverComponent = "LinuxIO Web Server"

// handleVersion returns installed component versions (public endpoint, no auth required).
// Used by frontend to detect when server is back up after updates.
func handleVersion(w http.ResponseWriter, _ *http.Request) {
	web.WriteJSON(w, http.StatusOK, getComponentVersions())
}

// handleUpdateInfo reports whether a newer LinuxIO release is available. Update
// checks are restricted to privileged sessions and are kept out of login so
// authentication is not delayed by the external GitHub request.
func handleUpdateInfo(w http.ResponseWriter, r *http.Request) {
	sess := sessions.SessionFromContext(r.Context())
	if sess == nil || !sess.Privileged {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if updateInfo := checkForUpdate(r.Context()); updateInfo != nil {
		web.WriteJSON(w, http.StatusOK, updateInfo)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
