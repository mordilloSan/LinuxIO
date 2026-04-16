package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/semver"
	ver "github.com/mordilloSan/LinuxIO/backend/common/version"
)

const GitHubAPI = "https://api.github.com/repos/%s/%s/releases/latest"

const maxGitHubReleaseBodyBytes int64 = 1 << 20

var componentVersionCommandTimeout = time.Second

var runComponentVersionCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

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
		slog.Debug("cannot determine installed version, skipping check")
		return nil
	}

	latest, releaseURL := fetchLatestRelease()
	if latest == "" {
		slog.Debug("could not fetch latest release")
		return nil
	}

	// Compare versions properly - only show update if latest is actually newer
	if semver.IsNewer(latest, current) {
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

func fetchLatestRelease() (version string, releaseURL string) {
	client := &http.Client{Timeout: 5 * time.Second}

	url := fmt.Sprintf(GitHubAPI, ver.RepoOwner, ver.RepoName)
	resp, err := client.Get(url)
	if err != nil {
		slog.Debug("failed to fetch latest release", "error", err)
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("GitHub API returned non-OK status", "status", resp.StatusCode)
		return "", ""
	}

	body, err := readBodyLimited(resp.Body, maxGitHubReleaseBodyBytes)
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

type componentVersionProbe struct {
	component string
	binary    string
	args      []string
}

func getComponentVersions(parent context.Context) map[string]string {
	if parent == nil {
		parent = context.Background()
	}

	components := make(map[string]string, 4)
	if ver.Version != "" {
		components["LinuxIO Web Server"] = ver.Version
	}

	probes := []componentVersionProbe{
		{component: "LinuxIO Bridge", binary: "linuxio-bridge", args: []string{"version"}},
		{component: "LinuxIO Auth", binary: "linuxio-auth", args: []string{"version"}},
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
