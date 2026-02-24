package docker

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/mordilloSan/go-logger/logger"

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
		logger.Warnf("[watchtower] failed to load config: %v", err)
		return
	}

	// When no stacks have auto-update enabled, stop Watchtower entirely.
	if len(cfg.Docker.AutoUpdateStacks) == 0 {
		stopWatchtower()
		logger.Infof("[watchtower] no auto-update stacks — watchtower stopped")
		return
	}

	containerNames := collectContainerNames(cfg.Docker.AutoUpdateStacks)

	// Write the generated compose file.
	content := generateWatchtowerCompose(containerNames)
	if err := os.WriteFile(watchtowerComposePath, []byte(content), 0o644); err != nil {
		logger.Warnf("[watchtower] failed to write compose file: %v", err)
		return
	}

	// Start or recreate Watchtower with the new config.
	if err := composeUpWithSDK(context.Background(), watchtowerProjectName, watchtowerComposePath, watchtowerGlobalDir, true, nil); err != nil {
		logger.Warnf("[watchtower] compose up failed: %v", err)
	} else {
		logger.Infof("[watchtower] synced with containers: %s", strings.Join(containerNames, ", "))
	}
}

// stopWatchtower brings down the Watchtower stack and removes its compose file.
// The global directory itself is preserved.
func stopWatchtower() {
	if _, err := os.Stat(watchtowerComposePath); os.IsNotExist(err) {
		return // Nothing to stop.
	}

	if err := composeDownWithSDK(context.Background(), watchtowerProjectName, watchtowerComposePath, watchtowerGlobalDir, false, nil); err != nil {
		logger.Warnf("[watchtower] compose down failed: %v", err)
	}

	if err := os.Remove(watchtowerComposePath); err != nil && !os.IsNotExist(err) {
		logger.Warnf("[watchtower] failed to remove compose file: %v", err)
	}
}

// collectContainerNames returns the running container names belonging to any of
// the given compose projects. Falls back to project names if Docker is unavailable.
func collectContainerNames(autoUpdateStacks []string) []string {
	cli, err := getClient()
	if err != nil {
		logger.Debugf("[watchtower] docker client unavailable, using project names as fallback: %v", err)
		return autoUpdateStacks
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("[watchtower] failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: false})
	if err != nil {
		logger.Warnf("[watchtower] failed to list containers: %v", err)
		return autoUpdateStacks
	}

	var names []string
	for _, ctr := range containers {
		project := ctr.Labels["com.docker.compose.project"]
		if !slices.Contains(autoUpdateStacks, project) {
			continue
		}
		if len(ctr.Names) > 0 {
			names = append(names, strings.TrimPrefix(ctr.Names[0], "/"))
		}
	}

	if len(names) == 0 {
		// No running containers found — fall back to project names so Watchtower
		// at least has something to watch once the stacks are started.
		return autoUpdateStacks
	}

	return names
}

// generateWatchtowerCompose returns a docker-compose YAML string for Watchtower
// configured to watch the given container names.
func generateWatchtowerCompose(containerNames []string) string {
	return fmt.Sprintf(`services:
  watchtower:
    image: ghcr.io/nicholas-fedor/watchtower:1.14.2
    container_name: watchtower
    hostname: watchtower
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

networks:
  linuxio-docker:
    external: true
`, strings.Join(containerNames, ","))
}
