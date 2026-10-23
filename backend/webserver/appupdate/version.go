package appupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

var componentVersionCommandTimeout = time.Second

// componentVersionCacheTTL bounds how often the unauthenticated /api/version
// endpoint can fork the component probes. A restart empties the cache.
const componentVersionCacheTTL = 5 * time.Second

var (
	versionCacheMu  sync.Mutex
	versionCacheAt  time.Time
	versionCacheVal map[string]string
)

var runComponentVersionCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
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
		slog.Info("update available", "current_version", current, "latest_version", latest)
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

	if err := json.Unmarshal(body, &release); err != nil {
		slog.Debug("failed to parse latest release response", "error", err)
		return "", ""
	}

	return release.TagName, release.HTMLURL
}

type componentVersionProbe struct {
	component string
	binary    string
	args      []string
}

func getComponentVersions(parent context.Context) map[string]string {
	if parent == nil {
		return map[string]string{}
	}

	components := make(map[string]string, 5)
	if ver.Version != "" {
		components["LinuxIO Web Server"] = ver.Version
	}

	probes := []componentVersionProbe{
		{component: "LinuxIO Bridge", binary: "linuxio-bridge", args: []string{"version"}},
		{component: "LinuxIO Auth", binary: "linuxio-auth", args: []string{"version"}},
		{component: "LinuxIO Indexer", binary: "linuxio-indexer", args: []string{"--version"}},
		{component: "LinuxIO CLI", binary: "linuxio", args: []string{"version", "--self"}},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, probe := range probes {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(parent, componentVersionCommandTimeout)
			defer cancel()

			binaryPath := filepath.Join(ver.BinDir, probe.binary)
			output, err := runComponentVersionCommand(ctx, binaryPath, probe.args...)
			if err != nil {
				slog.Debug("failed to run component version command",
					"component", probe.component,
					"path", binaryPath,
					"args", strings.Join(probe.args, " "),
					"error", err)
				return
			}

			version, ok := parseComponentVersionOutput(probe.component, output)
			if !ok {
				slog.Debug("failed to parse component version output",
					"component", probe.component,
					"output", strings.TrimSpace(string(output)))
				return
			}

			mu.Lock()
			components[probe.component] = version
			mu.Unlock()
		})
	}

	wg.Wait()
	return components
}

// cachedComponentVersions serves getComponentVersions from a short-lived cache
// so anonymous callers cannot multiply process forks. Concurrent callers wait
// on the lock and reuse the fresh result. The returned map is read-only.
func cachedComponentVersions(ctx context.Context) map[string]string {
	versionCacheMu.Lock()
	defer versionCacheMu.Unlock()
	if versionCacheVal != nil && time.Since(versionCacheAt) < componentVersionCacheTTL {
		return versionCacheVal
	}
	components := getComponentVersions(ctx)
	if ctx.Err() == nil { // a cancelled request yields partial probes; do not cache them
		versionCacheVal, versionCacheAt = components, time.Now()
	}
	return components
}

func parseComponentVersionOutput(component string, output []byte) (string, bool) {
	for line := range strings.SplitSeq(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, component+" ") {
			return "", false
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", false
		}

		version := fields[len(fields)-1]
		if version == "" {
			return "", false
		}
		return version, true
	}
	return "", false
}

// handleVersion returns installed component versions (public endpoint, no auth required).
// Used by frontend to detect when server is back up after updates.
func handleVersion(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, cachedComponentVersions(r.Context()))
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
