package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
)

const (
	watchtowerProjectName = "linuxio-watchtower"
	watchtowerGlobalDir   = "/var/lib/linuxIO/watchtower"
	watchtowerComposePath = "/var/lib/linuxIO/watchtower/docker-compose.yml"
)

// SyncWatchtowerStack regenerates the global Watchtower compose file from the
// current user's AutoUpdateStacks config and starts/restarts (or stops)
// Watchtower accordingly. Called after every auto-update toggle and on login.
// Errors are logged but not returned — the toggle saves the config regardless.
func SyncWatchtowerStack(username string) {
	cfg, _, err := config.Load(username)
	if err != nil {
		slog.Warn("failed to load docker config for watchtower", "component", "docker", "subsystem", "watchtower", "user", username, "error", err)
		return
	}

	// When no stacks have auto-update enabled, stop Watchtower entirely.
	if len(cfg.Docker.AutoUpdateStacks) == 0 {
		stopWatchtower()
		slog.Info("no auto-update stacks — watchtower stopped")
		return
	}

	containerNames := collectContainerNames(cfg.Docker.AutoUpdateStacks)

	// Write the generated compose file.
	content := generateWatchtowerCompose(containerNames)
	if err := os.WriteFile(watchtowerComposePath, []byte(content), 0o644); err != nil {
		slog.Warn("failed to write watchtower compose file", "component", "docker", "subsystem", "watchtower", "path", watchtowerComposePath, "error", err)
		return
	}

	// Start or recreate Watchtower with the new config.
	if err := composeUpWithSDK(context.Background(), watchtowerProjectName, watchtowerComposePath, watchtowerGlobalDir, true, nil); err != nil {
		slog.Warn("watchtower compose up failed", "component", "docker", "subsystem", "watchtower", "path", watchtowerComposePath, "error", err)
	} else {
		slog.Info("synced watchtower with containers", "component", "docker", "subsystem", "watchtower", "container_count", len(containerNames))
	}
}

// stopWatchtower brings down the Watchtower stack and removes its compose file.
// The global directory itself is preserved.
func stopWatchtower() {
	if _, err := os.Stat(watchtowerComposePath); os.IsNotExist(err) {
		return // Nothing to stop.
	}

	if err := composeDownWithSDK(context.Background(), watchtowerProjectName, watchtowerComposePath, watchtowerGlobalDir, false, nil); err != nil {
		slog.Warn("watchtower compose down failed", "component", "docker", "subsystem", "watchtower", "path", watchtowerComposePath, "error", err)
	}

	if err := os.Remove(watchtowerComposePath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove watchtower compose file", "component", "docker", "subsystem", "watchtower", "path", watchtowerComposePath, "error", err)
	}
}

// collectContainerNames filters autoUpdateContainers to only include currently
// running containers. Falls back to the full list if Docker is unavailable,
// so Watchtower is ready once containers start.
func collectContainerNames(autoUpdateContainers []string) []string {
	cli, err := getClient()
	if err != nil {
		slog.Debug("docker client unavailable for watchtower sync; using fallback container names", "component", "docker", "subsystem", "watchtower", "error", err)
		return autoUpdateContainers
	}
	defer releaseClient(cli)

	running, err := cli.ContainerList(context.Background(), container.ListOptions{All: false})
	if err != nil {
		slog.Warn("failed to list running containers for watchtower sync", "component", "docker", "subsystem", "watchtower", "error", err)
		return autoUpdateContainers
	}

	runningNames := make(map[string]bool, len(running))
	for _, ctr := range running {
		if len(ctr.Names) > 0 {
			runningNames[strings.TrimPrefix(ctr.Names[0], "/")] = true
		}
	}

	var names []string
	for _, name := range autoUpdateContainers {
		if runningNames[name] {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		// None running yet — pass the full list so Watchtower is configured
		// and ready as soon as the containers start.
		return autoUpdateContainers
	}

	return names
}

// generateWatchtowerCompose returns a docker-compose YAML string for Watchtower
// configured to watch the given container names.
func generateWatchtowerCompose(containerNames []string) string {
	return fmt.Sprintf(`name: linuxio-watchtower

x-linuxio-stack:
  icon: "di:watchtower"

services:
  linuxio-watchtower:
    image: ghcr.io/nicholas-fedor/watchtower:1.15.0
    container_name: linuxio-watchtower
    hostname: linuxio-watchtower
    restart: unless-stopped
    mem_limit: 32m
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /etc/localtime:/etc/localtime:ro
    environment:
      WATCHTOWER_CLEANUP: "true"
      WATCHTOWER_SCHEDULE: "0 0 4 * * *"
      WATCHTOWER_CONTAINER_NAMES: "%s"
    networks:
      - linuxio-docker
    labels:
      - "io.linuxio.container.icon=di:watchtower"

networks:
  linuxio-docker:
    external: true
`, strings.Join(containerNames, ","))
}
