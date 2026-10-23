package dockerupdate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func validateComposeServiceScope(
	ctx context.Context,
	cli ContainerClient,
	projectName string,
	service string,
) error {
	return validateComposeServiceScopes(ctx, cli, projectName, []string{service})
}

func validateComposeServiceScopes(
	ctx context.Context,
	cli ContainerClient,
	projectName string,
	services []string,
) error {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list Compose project %q containers: %w", projectName, err)
	}
	selected := make(map[string]struct{}, len(services))
	for _, service := range services {
		selected[service] = struct{}{}
	}
	replicas := make(map[string]int, len(selected))
	for _, summary := range containers.Items {
		service := summary.Labels["com.docker.compose.service"]
		if summary.Labels["com.docker.compose.project"] == projectName {
			if _, ok := selected[service]; ok {
				replicas[service]++
			}
		}
	}
	for _, service := range services {
		if replicas[service] > 1 {
			return fmt.Errorf("compose service %q has %d replicas and cannot be updated safely as a single container", service, replicas[service])
		}
	}
	return nil
}

func composeTargetForContainer(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
) (ComposeTarget, string, bool, error) {
	if inspect.Config == nil {
		return ComposeTarget{}, "", false, nil
	}
	labels := inspect.Config.Labels
	projectName := strings.TrimSpace(labels["com.docker.compose.project"])
	if projectName == "" {
		return ComposeTarget{}, "", false, nil
	}
	service := strings.TrimSpace(labels["com.docker.compose.service"])
	if service == "" {
		return ComposeTarget{}, "", true, fmt.Errorf("compose-managed container %q has no service label", strings.TrimPrefix(inspect.Name, "/"))
	}

	workingDir := strings.TrimSpace(labels["com.docker.compose.project.working_dir"])
	configFiles, err := resolveComposeUpdateConfigFiles(ctx, cli, projectName, workingDir, labels["com.docker.compose.project.config_files"])
	if err != nil {
		return ComposeTarget{}, "", true, err
	}
	workingDir, err = resolveComposeUpdateWorkingDir(ctx, cli, workingDir, configFiles[0])
	if err != nil {
		return ComposeTarget{}, "", true, err
	}
	environmentFiles, err := resolveComposeUpdateEnvironmentFiles(
		ctx,
		cli,
		projectName,
		workingDir,
		labels["com.docker.compose.project.environment_file"],
	)
	if err != nil {
		return ComposeTarget{}, "", true, err
	}

	return ComposeTarget{
		Name:               projectName,
		ConfigFiles:        configFiles,
		EnvironmentFiles:   environmentFiles,
		IsolateEnvironment: true,
		WorkingDir:         workingDir,
	}, service, true, nil
}

func resolveComposeUpdateConfigFiles(
	ctx context.Context,
	cli *client.Client,
	projectName string,
	workingDir string,
	configFilesLabel string,
) ([]string, error) {
	rawConfigFiles := ParseComposeConfigFiles(configFilesLabel)
	for i, configFile := range rawConfigFiles {
		if !filepath.IsAbs(configFile) && workingDir != "" {
			rawConfigFiles[i] = filepath.Join(workingDir, configFile)
		}
	}
	configFiles := TranslateComposeConfigFiles(ctx, cli, rawConfigFiles)
	if len(configFiles) == 0 && workingDir != "" {
		configFiles = InferComposeFilesFromWorkingDir(ctx, cli, workingDir)
	}
	if len(configFiles) == 0 {
		return nil, fmt.Errorf("compose project %q has no accessible config files", projectName)
	}
	if len(rawConfigFiles) > 0 && len(configFiles) != len(rawConfigFiles) {
		return nil, fmt.Errorf("not all config files for Compose project %q are accessible", projectName)
	}
	for _, configFile := range configFiles {
		info, err := os.Stat(configFile)
		if err != nil {
			return nil, fmt.Errorf("stat Compose config %q: %w", configFile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("compose config %q is not a regular file", configFile)
		}
	}
	return configFiles, nil
}

func resolveComposeUpdateWorkingDir(
	ctx context.Context,
	cli *client.Client,
	workingDir string,
	configFile string,
) (string, error) {
	if workingDir != "" {
		if info, err := os.Stat(workingDir); err != nil || !info.IsDir() {
			workingDir = TranslateContainerPathToHost(ctx, cli, workingDir)
		}
	}
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	info, err := os.Stat(workingDir)
	if err != nil {
		return "", fmt.Errorf("stat Compose working directory %q: %w", workingDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("compose working directory %q is not a directory", workingDir)
	}
	return workingDir, nil
}

func resolveComposeUpdateEnvironmentFiles(
	ctx context.Context,
	cli *client.Client,
	projectName string,
	workingDir string,
	environmentFilesLabel string,
) ([]string, error) {
	rawEnvironmentFiles := ParseComposeConfigFiles(environmentFilesLabel)
	for i, environmentFile := range rawEnvironmentFiles {
		if !filepath.IsAbs(environmentFile) {
			rawEnvironmentFiles[i] = filepath.Join(workingDir, environmentFile)
		}
	}
	if len(rawEnvironmentFiles) == 0 {
		return nil, nil
	}
	environmentFiles := TranslateComposeConfigFiles(ctx, cli, rawEnvironmentFiles)
	if len(environmentFiles) != len(rawEnvironmentFiles) {
		return nil, fmt.Errorf("not all environment files for Compose project %q are accessible", projectName)
	}
	for _, environmentFile := range environmentFiles {
		info, err := os.Stat(environmentFile)
		if err != nil {
			return nil, fmt.Errorf("stat Compose environment file %q: %w", environmentFile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("compose environment file %q is not a regular file", environmentFile)
		}
	}
	return environmentFiles, nil
}

// TranslateComposeConfigFiles maps Compose file paths as seen from inside a
// container onto host paths, dropping files that cannot be found.
func TranslateComposeConfigFiles(ctx context.Context, cli *client.Client, configFiles []string) []string {
	translated := make([]string, 0, len(configFiles))
	for _, configFile := range configFiles {
		if ctx.Err() != nil {
			return translated
		}
		if _, err := os.Stat(configFile); err == nil {
			translated = append(translated, configFile)
			continue
		}
		translatedPath := TranslateContainerPathToHost(ctx, cli, configFile)
		if translatedPath != configFile {
			translated = append(translated, translatedPath)
		}
	}
	return translated
}

// InferComposeFilesFromWorkingDir looks for a conventionally named Compose file
// in the translated working directory.
func InferComposeFilesFromWorkingDir(ctx context.Context, cli *client.Client, workingDir string) []string {
	translatedWorkingDir := TranslateContainerPathToHost(ctx, cli, workingDir)
	composeFileNames := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
	for _, fileName := range composeFileNames {
		possiblePath := filepath.Join(translatedWorkingDir, fileName)
		if _, err := os.Stat(possiblePath); err == nil {
			return []string{possiblePath}
		}
	}
	return nil
}

// TranslateContainerPathToHost attempts to translate a container path to a host path
// by inspecting volume mounts across all containers to find the correct mount source
func TranslateContainerPathToHost(ctx context.Context, cli *client.Client, containerPath string) string {
	// First, get all running containers to search for the correct mount
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		slog.Warn("failed to list containers for path translation", "error", err)
		return containerPath
	}

	// Priority containers to check first (e.g., Portainer, Docker-in-Docker)
	priorityNames := []string{"portainer", "portainer-ce", "portainer-ee", "dind"}

	// Check priority containers first
	for _, priorityName := range priorityNames {
		for _, ctr := range containers.Items {
			// Check if container name contains the priority name
			containerName := strings.TrimPrefix(ctr.Names[0], "/")
			if strings.Contains(strings.ToLower(containerName), priorityName) {
				if hostPath := tryTranslatePath(ctx, cli, ctr.ID, containerPath, containerName); hostPath != containerPath {
					return hostPath
				}
			}
		}
	}

	// Check all other containers
	for _, ctr := range containers.Items {
		containerName := strings.TrimPrefix(ctr.Names[0], "/")
		if hostPath := tryTranslatePath(ctx, cli, ctr.ID, containerPath, containerName); hostPath != containerPath {
			return hostPath
		}
	}
	slog.Debug("no mount found for container path, using original", "path", containerPath)
	return containerPath // Return original path if no mount translation found
}

// tryTranslatePath attempts to translate a path using a specific container's mounts
func tryTranslatePath(ctx context.Context, cli *client.Client, containerID, containerPath, containerName string) string {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return containerPath
	}
	containerJSON := inspect.Container

	for _, mount := range containerJSON.Mounts {
		if hostPath, ok := translateMountPath(mount, containerPath, containerName); ok {
			return hostPath
		}
	}

	return containerPath
}

func translateMountPath(mount container.MountPoint, containerPath, containerName string) (string, bool) {
	relPath, found := strings.CutPrefix(containerPath, mount.Destination)
	if !found || mount.Source == "" {
		return "", false
	}

	relPath = strings.TrimPrefix(relPath, "/")
	hostPath := filepath.Join(mount.Source, relPath)

	if stat, err := os.Stat(hostPath); err == nil {
		if mount.Type == "volume" && strings.HasPrefix(mount.Source, "/var/lib/docker/volumes/") {
			slog.Debug("found path in Docker volume", "volume", mount.Name, "path", hostPath)
		} else if mount.Type == "bind" {
			slog.Debug("translated container path via bind mount", "container_path", containerPath, "host_path", hostPath, "container", containerName)
		}
		if stat.IsDir() || stat.Mode().IsRegular() {
			return hostPath, true
		}
		return "", false
	}

	// Check parent directory for file paths that don't exist yet
	parentDir := filepath.Dir(hostPath)
	if parentStat, err := os.Stat(parentDir); err == nil && parentStat.IsDir() {
		slog.Debug("translated container path via parent directory verification", "container_path", containerPath, "host_path", hostPath, "container", containerName)
		return hostPath, true
	}
	return "", false
}

// ParseComposeConfigFiles splits the comma-separated config-files label.
func ParseComposeConfigFiles(configFilesStr string) []string {
	if configFilesStr == "" {
		return []string{}
	}
	// Config files are comma-separated in the label
	files := strings.Split(configFilesStr, ",")
	for i, f := range files {
		files[i] = strings.TrimSpace(f)
	}
	return files
}
