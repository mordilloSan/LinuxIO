package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/goccy/go-yaml"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
)

var validNetworkMode = regexp.MustCompile(`^(none|host|bridge|service:.+|container:.+)$`)
var validRestartPolicy = regexp.MustCompile(`^(no|always|unless-stopped|on-failure(:\d+)?)$`)
var validIPCMode = regexp.MustCompile(`^(host|private|shareable|service:.+)$`)
var validPIDMode = regexp.MustCompile(`^(host|service:.+)$`)

// ComposeService represents a service within a compose project
type ComposeService struct {
	Name           string   `json:"name"`
	Image          string   `json:"image"`
	Icon           string   `json:"icon,omitempty"`
	URL            string   `json:"url,omitempty"`
	Status         string   `json:"status"`
	State          string   `json:"state"`
	ContainerCount int      `json:"container_count"`
	ContainerIDs   []string `json:"container_ids"`
	Ports          []string `json:"ports"`
}

// ComposeProject represents a docker compose stack
type ComposeProject struct {
	Name        string                     `json:"name"`
	Icon        string                     `json:"icon,omitempty"`
	Status      string                     `json:"status"` // "running", "partial", "stopped"
	AutoUpdate  bool                       `json:"auto_update"`
	Services    map[string]*ComposeService `json:"services"`
	ConfigFiles []string                   `json:"config_files"`
	WorkingDir  string                     `json:"working_dir"`
}

// ListComposeProjects discovers all compose projects by analyzing container labels and indexer
func ListComposeProjects(username string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	projects := discoverComposeProjectsFromContainers(cli, containers)

	// Query indexer for offline stacks (compose files without running containers)
	if err := discoverOfflineStacks(username, projects); err != nil {
		// Log but don't fail - indexer might be unavailable
		logger.Debugf("failed to discover offline stacks from indexer: %v", err)
	}

	// Load config once to check auto-update preferences.
	cfg, _, _ := config.Load(username)

	return finalizeComposeProjects(projects, cfg), nil
}

func discoverComposeProjectsFromContainers(
	cli *client.Client,
	containers []container.Summary,
) map[string]*ComposeProject {
	projects := make(map[string]*ComposeProject)
	for _, ctr := range containers {
		projectName, ok := ctr.Labels["com.docker.compose.project"]
		if !ok {
			continue
		}
		project := ensureComposeProject(cli, projects, projectName, ctr)
		updateComposeProjectService(project, ctr)
	}
	return projects
}

func ensureComposeProject(
	cli *client.Client,
	projects map[string]*ComposeProject,
	projectName string,
	ctr container.Summary,
) *ComposeProject {
	if project, exists := projects[projectName]; exists {
		return project
	}

	configFiles := resolveComposeConfigFiles(
		cli,
		ctr.Labels["com.docker.compose.project.config_files"],
		ctr.Labels["com.docker.compose.project.working_dir"],
	)
	project := &ComposeProject{
		Name:        projectName,
		Services:    make(map[string]*ComposeService),
		ConfigFiles: configFiles,
		WorkingDir:  ctr.Labels["com.docker.compose.project.working_dir"],
	}
	setComposeProjectIcon(project)
	projects[projectName] = project
	return project
}

func resolveComposeConfigFiles(cli *client.Client, configFilesLabel, workingDir string) []string {
	configFiles := translateComposeConfigFiles(cli, parseConfigFiles(configFilesLabel))
	if len(configFiles) > 0 || workingDir == "" {
		return configFiles
	}
	return inferComposeFilesFromWorkingDir(cli, workingDir)
}

func translateComposeConfigFiles(cli *client.Client, configFiles []string) []string {
	translated := make([]string, 0, len(configFiles))
	for _, configFile := range configFiles {
		if _, err := os.Stat(configFile); err == nil {
			translated = append(translated, configFile)
			continue
		}
		translatedPath := translateContainerPathToHost(cli, configFile)
		if translatedPath != configFile {
			translated = append(translated, translatedPath)
		}
	}
	return translated
}

func inferComposeFilesFromWorkingDir(cli *client.Client, workingDir string) []string {
	translatedWorkingDir := translateContainerPathToHost(cli, workingDir)
	composeFileNames := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
	for _, fileName := range composeFileNames {
		possiblePath := filepath.Join(translatedWorkingDir, fileName)
		if _, err := os.Stat(possiblePath); err == nil {
			return []string{possiblePath}
		}
	}
	return nil
}

func setComposeProjectIcon(project *ComposeProject) {
	if len(project.ConfigFiles) == 0 {
		return
	}
	if icon, err := extractStackIcon(project.ConfigFiles[0]); err == nil && icon != "" {
		project.Icon = icon
	}
}

func updateComposeProjectService(project *ComposeProject, ctr container.Summary) {
	serviceName := ctr.Labels["com.docker.compose.service"]
	service := ensureComposeService(project, serviceName)
	service.ContainerIDs = append(service.ContainerIDs, ctr.ID)
	service.ContainerCount++
	service.Image = ctr.Image
	service.State = ctr.State
	service.Status = ctr.Status
	if service.Icon == "" {
		service.Icon = ResolveIconIdentifier(ctr.Labels["io.linuxio.container.icon"], serviceName)
	}
	if service.URL == "" {
		service.URL = ctr.Labels["io.linuxio.container.url"]
	}
	service.Ports = append(service.Ports, collectComposeServicePorts(ctr)...)
}

func ensureComposeService(project *ComposeProject, serviceName string) *ComposeService {
	if service, exists := project.Services[serviceName]; exists {
		return service
	}
	service := &ComposeService{
		Name:         serviceName,
		ContainerIDs: []string{},
		Ports:        []string{},
	}
	project.Services[serviceName] = service
	return service
}

func collectComposeServicePorts(ctr container.Summary) []string {
	ports := make([]string, 0, len(ctr.Ports))
	for _, port := range ctr.Ports {
		if port.PublicPort > 0 {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", port.PublicPort, port.PrivatePort, port.Type))
		}
	}
	return ports
}

func finalizeComposeProjects(projects map[string]*ComposeProject, cfg *config.Settings) []*ComposeProject {
	result := make([]*ComposeProject, 0, len(projects))
	for _, project := range projects {
		project.Status = calculateProjectStatus(project)
		if cfg != nil {
			project.AutoUpdate = slices.Contains(cfg.Docker.AutoUpdateStacks, project.Name)
		}
		result = append(result, project)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// GetComposeProject returns detailed information about a specific compose project
func GetComposeProject(username, projectName string) (any, error) {
	projects, err := ListComposeProjects(username)
	if err != nil {
		return nil, err
	}

	projectList, ok := projects.([]*ComposeProject)
	if !ok {
		return nil, fmt.Errorf("invalid project list format")
	}

	for _, project := range projectList {
		if project.Name == projectName {
			return project, nil
		}
	}

	return nil, fmt.Errorf("project '%s' not found", projectName)
}

// ComposeUp starts a compose project
func ComposeUp(username, projectName, composePath string) (any, error) {
	logger.Infof("compose up requested: user=%s project=%s compose_path=%s", username, projectName, composePath)
	var configFile string
	var workingDir string

	// If compose path is provided, use it directly
	if composePath != "" {
		configFile = composePath
		workingDir = filepath.Dir(composePath)
	} else {
		// Try to get the project from existing containers first
		project, err := GetComposeProject(username, projectName)
		if err == nil {
			// Project exists with containers
			composeProject, ok := project.(*ComposeProject)
			if !ok {
				return nil, fmt.Errorf("invalid project format")
			}

			if len(composeProject.ConfigFiles) == 0 {
				return nil, fmt.Errorf("no config files found for project '%s'", projectName)
			}

			configFile = composeProject.ConfigFiles[0]
			workingDir = composeProject.WorkingDir
			if workingDir == "" {
				workingDir = filepath.Dir(configFile)
			}
		} else {
			// Project not found in containers - might be a new stack
			// Try to find the compose file in standard locations
			configFile, workingDir, err = findComposeFile(username, projectName)
			if err != nil {
				return nil, fmt.Errorf("project '%s' not found and no compose file found: %w", projectName, err)
			}
		}
	}

	collector := &composeMessageCollector{}
	err := composeUpWithSDK(context.Background(), projectName, configFile, workingDir, false, collector.Emit)
	if err != nil {
		return nil, fmt.Errorf("failed to start project: %w\nOutput: %s", err, collector.String())
	}
	logger.Infof("compose up complete: project=%s config=%s", projectName, configFile)

	return map[string]string{"message": "Project started successfully", "output": collector.String()}, nil
}

// findComposeFile attempts to locate a compose file for a project
func findComposeFile(username, projectName string) (string, string, error) {
	// Common compose file names
	composeFileNames := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

	// Try to get user's docker folder from config
	cfg, _, err := config.Load(username)
	if err == nil && cfg.Docker.Folder != "" {
		sanitized := sanitizeStackName(projectName)
		stackDir := filepath.Join(string(cfg.Docker.Folder), sanitized)

		for _, fileName := range composeFileNames {
			composePath := filepath.Join(stackDir, fileName)
			if _, err := os.Stat(composePath); err == nil {
				return composePath, stackDir, nil
			}
		}
	}

	// Fallback to common paths
	commonBasePaths := []string{
		filepath.Join(os.Getenv("HOME"), "docker"),
		filepath.Join(os.Getenv("HOME"), "Docker"),
		"/opt/docker",
	}

	sanitized := sanitizeStackName(projectName)

	for _, basePath := range commonBasePaths {
		stackDir := filepath.Join(basePath, sanitized)
		for _, fileName := range composeFileNames {
			composePath := filepath.Join(stackDir, fileName)
			if _, err := os.Stat(composePath); err == nil {
				return composePath, stackDir, nil
			}
		}
	}

	return "", "", fmt.Errorf("no compose file found for project '%s'", projectName)
}

// ComposeDown stops and removes a compose project
func ComposeDown(username, projectName string) (any, error) {
	logger.Infof("compose down requested: user=%s project=%s", username, projectName)
	project, err := GetComposeProject(username, projectName)
	if err != nil {
		return nil, err
	}

	composeProject, ok := project.(*ComposeProject)
	if !ok {
		return nil, fmt.Errorf("invalid project format")
	}

	if len(composeProject.ConfigFiles) == 0 {
		return nil, fmt.Errorf("no config files found for project '%s'", projectName)
	}

	configFile := composeProject.ConfigFiles[0]
	workingDir := composeProject.WorkingDir
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}

	collector := &composeMessageCollector{}
	err = composeDownWithSDK(context.Background(), projectName, configFile, workingDir, false, collector.Emit)
	if err != nil {
		return nil, fmt.Errorf("failed to stop project: %w\nOutput: %s", err, collector.String())
	}
	logger.Infof("compose down complete: project=%s config=%s", projectName, configFile)

	return map[string]string{"message": "Project stopped successfully", "output": collector.String()}, nil
}

// DeleteStackOptions defines what to delete when removing a stack
type DeleteStackOptions struct {
	DeleteFile      bool `json:"delete_file"`      // Delete the compose file
	DeleteDirectory bool `json:"delete_directory"` // Delete the entire stack directory
}

// DeleteStack removes a compose stack with options to delete files
func DeleteStack(username, projectName string, options DeleteStackOptions) (any, error) {
	logger.Infof("delete stack requested: user=%s project=%s delete_file=%v delete_directory=%v",
		username, projectName, options.DeleteFile, options.DeleteDirectory)
	// Get project info first
	project, err := GetComposeProject(username, projectName)
	if err != nil {
		// Project might not exist in Docker, but we might still have files to delete
		// Try to find compose file via indexer
		configFile, workingDir, findErr := findComposeFile(username, projectName)
		if findErr != nil {
			return nil, fmt.Errorf("project '%s' not found: %w", projectName, err)
		}

		// No containers, just handle file deletion
		result, delErr := deleteStackFiles(projectName, configFile, workingDir, options)
		if delErr != nil {
			return nil, delErr
		}
		logger.Infof("delete stack complete: project=%s", projectName)
		return result, nil
	}

	composeProject, ok := project.(*ComposeProject)
	if !ok {
		return nil, fmt.Errorf("invalid project format")
	}

	var configFile string
	var workingDir string

	if len(composeProject.ConfigFiles) > 0 {
		configFile = composeProject.ConfigFiles[0]
		workingDir = composeProject.WorkingDir
		if workingDir == "" {
			workingDir = filepath.Dir(configFile)
		}
	}

	// First, run docker compose down to remove containers and networks
	if configFile != "" {
		collector := &composeMessageCollector{}
		cmdErr := composeDownWithSDK(context.Background(), projectName, configFile, workingDir, false, collector.Emit)
		if cmdErr != nil {
			logger.Warnf("docker compose down failed for %s: %v, output: %s", projectName, cmdErr, collector.String())
			// Continue with file deletion even if down fails
		}
	}

	// Handle file deletion
	result, delErr := deleteStackFiles(projectName, configFile, workingDir, options)
	if delErr != nil {
		return nil, delErr
	}
	logger.Infof("delete stack complete: project=%s", projectName)
	return result, nil
}

// deleteStackFiles handles the file/directory deletion part of stack removal
func deleteStackFiles(projectName, configFile, workingDir string, options DeleteStackOptions) (any, error) {
	result := map[string]any{
		"message":       "Stack removed successfully",
		"project":       projectName,
		"files_deleted": false,
		"dir_deleted":   false,
		"deleted_path":  "",
	}

	// Delete entire directory
	if options.DeleteDirectory && workingDir != "" {
		// Safety check: don't delete root or home directories
		if workingDir == "/" || workingDir == os.Getenv("HOME") || workingDir == "/home" {
			return nil, fmt.Errorf("refusing to delete protected directory: %s", workingDir)
		}

		// Check if directory exists
		if _, err := os.Stat(workingDir); err == nil {
			if err := os.RemoveAll(workingDir); err != nil {
				return nil, fmt.Errorf("failed to delete directory %s: %w", workingDir, err)
			}
			result["dir_deleted"] = true
			result["deleted_path"] = workingDir
			logger.InfoKV("deleted stack directory", "project", projectName, "path", workingDir)
		}

		return result, nil
	}

	// Delete only the compose file
	if options.DeleteFile && configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			if err := os.Remove(configFile); err != nil {
				return nil, fmt.Errorf("failed to delete compose file %s: %w", configFile, err)
			}
			result["files_deleted"] = true
			result["deleted_path"] = configFile
			logger.InfoKV("deleted compose file", "project", projectName, "path", configFile)
		}
	}

	return result, nil
}

// ComposeRestart restarts a compose project
func ComposeRestart(username, projectName string) (any, error) {
	logger.Infof("compose restart requested: user=%s project=%s", username, projectName)
	configFile, workingDir, err := resolveComposeRestartTarget(username, projectName)
	if err != nil {
		return nil, err
	}

	// Use up+remove-orphans semantics for restart so compose file changes are applied.
	collector := &composeMessageCollector{}
	err = composeUpWithSDK(context.Background(), projectName, configFile, workingDir, true, collector.Emit)
	if err != nil {
		return nil, fmt.Errorf("failed to restart project: %w\nOutput: %s", err, collector.String())
	}
	logger.Infof("compose restart complete: project=%s config=%s", projectName, configFile)

	return map[string]string{"message": "Project restarted successfully", "output": collector.String()}, nil
}

func resolveComposeRestartTarget(username, projectName string) (string, string, error) {
	project, err := GetComposeProject(username, projectName)
	if err == nil {
		if composeProject, ok := project.(*ComposeProject); ok {
			if configFile, workingDir := resolveComposeRestartTargetFromProject(projectName, composeProject); configFile != "" {
				return configFile, workingDir, nil
			}
		}
	}

	configFile, workingDir, err := findComposeFile(username, projectName)
	if err != nil {
		return "", "", fmt.Errorf("compose file not found: %w", err)
	}
	logger.InfoKV("found compose file via search", "project", projectName, "path", configFile)
	return configFile, workingDir, nil
}

func resolveComposeRestartTargetFromProject(projectName string, composeProject *ComposeProject) (string, string) {
	cli, err := getClient()
	if err != nil {
		logger.WarnKV("failed to get Docker client for path translation", "project", projectName, "error", err.Error())
		return resolveComposeRestartWithoutClient(projectName, composeProject)
	}
	defer releaseClient(cli)

	if len(composeProject.ConfigFiles) > 0 {
		return resolveComposeRestartFromConfigFiles(cli, projectName, composeProject)
	}
	if composeProject.WorkingDir == "" {
		return "", ""
	}
	return resolveComposeRestartFromWorkingDir(cli, projectName, composeProject.WorkingDir)
}

func resolveComposeRestartWithoutClient(projectName string, composeProject *ComposeProject) (string, string) {
	if len(composeProject.ConfigFiles) == 0 {
		return "", ""
	}
	configFile := composeProject.ConfigFiles[0]
	if _, err := os.Stat(configFile); err == nil {
		return configFile, fallbackWorkingDir(composeProject.WorkingDir, configFile)
	}
	logger.WarnKV("compose file from container labels not found",
		"project", projectName,
		"labeled_path", configFile)
	return "", ""
}

func resolveComposeRestartFromConfigFiles(
	cli *client.Client,
	projectName string,
	composeProject *ComposeProject,
) (string, string) {
	configFile := composeProject.ConfigFiles[0]
	workingDir := composeProject.WorkingDir
	if _, err := os.Stat(configFile); err == nil {
		return configFile, fallbackWorkingDir(workingDir, configFile)
	}

	translatedConfigFile := translateContainerPathToHost(cli, configFile)
	if translatedConfigFile == configFile {
		logger.WarnKV("compose file from container labels not found and translation failed",
			"project", projectName,
			"labeled_path", configFile)
		return "", ""
	}
	if _, err := os.Stat(translatedConfigFile); err != nil {
		logger.WarnKV("translated path does not exist", "project", projectName, "translated_path", translatedConfigFile)
		return "", ""
	}

	logger.InfoKV("translated container config path to host path",
		"project", projectName,
		"container_path", configFile,
		"host_path", translatedConfigFile)
	if workingDir != "" {
		workingDir = translateContainerPathToHost(cli, workingDir)
	}
	return translatedConfigFile, fallbackWorkingDir(workingDir, translatedConfigFile)
}

func resolveComposeRestartFromWorkingDir(cli *client.Client, projectName, workingDir string) (string, string) {
	translatedWorkingDir := translateContainerPathToHost(cli, workingDir)
	logger.DebugKV("translating working directory",
		"project", projectName,
		"container_path", workingDir,
		"host_path", translatedWorkingDir)

	for _, fileName := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		possiblePath := filepath.Join(translatedWorkingDir, fileName)
		if _, err := os.Stat(possiblePath); err == nil {
			logger.InfoKV("found compose file via working_dir translation", "project", projectName, "path", possiblePath)
			return possiblePath, translatedWorkingDir
		}
	}
	return "", ""
}

func fallbackWorkingDir(workingDir, configFile string) string {
	if workingDir != "" {
		return workingDir
	}
	return filepath.Dir(configFile)
}

// ComposeStop stops a compose project without removing containers
func ComposeStop(username, projectName string) (any, error) {
	logger.Infof("compose stop requested: user=%s project=%s", username, projectName)
	project, err := GetComposeProject(username, projectName)
	if err != nil {
		return nil, err
	}

	composeProject, ok := project.(*ComposeProject)
	if !ok {
		return nil, fmt.Errorf("invalid project format")
	}

	if len(composeProject.ConfigFiles) == 0 {
		return nil, fmt.Errorf("no config files found for project '%s'", projectName)
	}

	configFile := composeProject.ConfigFiles[0]
	workingDir := composeProject.WorkingDir
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}

	collector := &composeMessageCollector{}
	err = composeStopWithSDK(context.Background(), projectName, configFile, workingDir, collector.Emit)
	if err != nil {
		return nil, fmt.Errorf("failed to stop project: %w\nOutput: %s", err, collector.String())
	}
	logger.Infof("compose stop complete: project=%s config=%s", projectName, configFile)

	return map[string]string{"message": "Project stopped successfully", "output": collector.String()}, nil
}

// Helper functions

// translateContainerPathToHost attempts to translate a container path to a host path
// by inspecting volume mounts across all containers to find the correct mount source
func translateContainerPathToHost(cli *client.Client, containerPath string) string {
	// First, get all running containers to search for the correct mount
	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		logger.Warnf("failed to list containers: %v", err)
		return containerPath
	}

	// Priority containers to check first (e.g., Portainer, Docker-in-Docker)
	priorityNames := []string{"portainer", "portainer-ce", "portainer-ee", "dind"}

	// Check priority containers first
	for _, priorityName := range priorityNames {
		for _, ctr := range containers {
			// Check if container name contains the priority name
			containerName := strings.TrimPrefix(ctr.Names[0], "/")
			if strings.Contains(strings.ToLower(containerName), priorityName) {
				if hostPath := tryTranslatePath(cli, ctr.ID, containerPath, containerName); hostPath != containerPath {
					return hostPath
				}
			}
		}
	}

	// Check all other containers
	for _, ctr := range containers {
		containerName := strings.TrimPrefix(ctr.Names[0], "/")
		if hostPath := tryTranslatePath(cli, ctr.ID, containerPath, containerName); hostPath != containerPath {
			return hostPath
		}
	}

	logger.Debugf("no mount found for container path %s, using original", containerPath)
	return containerPath // Return original path if no mount translation found
}

// tryTranslatePath attempts to translate a path using a specific container's mounts
func tryTranslatePath(cli *client.Client, containerID, containerPath, containerName string) string {
	containerJSON, err := cli.ContainerInspect(context.Background(), containerID)
	if err != nil {
		return containerPath
	}

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
			logger.Debugf("found path in Docker volume %s (path: %s) - may require elevated permissions", mount.Name, hostPath)
		} else if mount.Type == "bind" {
			logger.Debugf("translated container path %s to host path %s via bind mount in container %s", containerPath, hostPath, containerName)
		}
		if stat.IsDir() || stat.Mode().IsRegular() {
			return hostPath, true
		}
		return "", false
	}

	// Check parent directory for file paths that don't exist yet
	parentDir := filepath.Dir(hostPath)
	if parentStat, err := os.Stat(parentDir); err == nil && parentStat.IsDir() {
		logger.Debugf("translated container path %s to host path %s (parent dir verified) via container %s", containerPath, hostPath, containerName)
		return hostPath, true
	}
	return "", false
}

func parseConfigFiles(configFilesStr string) []string {
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

func calculateProjectStatus(project *ComposeProject) string {
	if len(project.Services) == 0 {
		return "stopped"
	}

	runningCount := 0
	stoppedCount := 0

	for _, service := range project.Services {
		if service.State == "running" {
			runningCount++
		} else {
			stoppedCount++
		}
	}

	if runningCount == len(project.Services) {
		return "running"
	} else if stoppedCount == len(project.Services) {
		return "stopped"
	}
	return "partial"
}

// ValidationError represents a validation error with location information
type ValidationError struct {
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Type    string `json:"type"` // "error" or "warning"
}

// ValidationResult represents the result of compose file validation
type ValidationResult struct {
	Valid             bool              `json:"valid"`
	Errors            []ValidationError `json:"errors"`
	NormalizedContent string            `json:"normalized_content,omitempty"` // Auto-normalized content with container_name added
}

// ComposeFilePathInfo represents information about a compose file path
type ComposeFilePathInfo struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Directory string `json:"directory"`
}

// NormalizeComposeFile automatically adds container_name to services that don't have it
// This prevents Docker from using auto-generated names like "project-service-1"
func NormalizeComposeFile(content string) (string, error) {
	var composeData map[string]any
	if err := yaml.Unmarshal([]byte(content), &composeData); err != nil {
		// Return original content if we can't parse it (validation will catch this later)
		return content, nil
	}

	// Get services section
	services, hasServices := composeData["services"]
	if !hasServices {
		return content, nil
	}

	servicesMap, ok := services.(map[string]any)
	if !ok {
		return content, nil
	}

	// Add container_name to each service that doesn't have it
	modified := false
	for serviceName, serviceData := range servicesMap {
		serviceMap, ok := serviceData.(map[string]any)
		if !ok {
			continue
		}

		// Check if container_name is already set
		if _, hasContainerName := serviceMap["container_name"]; !hasContainerName {
			serviceMap["container_name"] = serviceName
			modified = true
		}
	}

	// If nothing was modified, return original content
	if !modified {
		return content, nil
	}

	// Marshal back to YAML
	normalized, err := yaml.Marshal(composeData)
	if err != nil {
		return content, err
	}

	return string(normalized), nil
}

// ValidateComposeFile validates docker-compose YAML syntax and structure
func ValidateComposeFile(content string) (any, error) {
	result := ValidationResult{
		Valid:  true,
		Errors: []ValidationError{},
	}

	composeData, err := parseComposeValidationContent(content, &result)
	if err != nil {
		return result, nil
	}

	if sdkErr := composeValidateContentWithSDK(context.Background(), content); sdkErr != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Message: strings.TrimPrefix(sdkErr.Error(), "load compose project: "),
			Type:    "error",
		})
		return result, nil
	}

	validateComposeServices(composeData, &result)

	if !result.Valid {
		return result, nil
	}

	validateComposeVersionField(composeData, &result)
	populateNormalizedComposeContent(content, &result)

	return result, nil
}

func parseComposeValidationContent(content string, result *ValidationResult) (map[string]any, error) {
	var composeData map[string]any
	if err := yaml.Unmarshal([]byte(content), &composeData); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Message: fmt.Sprintf("Invalid YAML syntax: %v", err),
			Type:    "error",
		})
		return nil, err
	}
	return composeData, nil
}

func validateComposeServices(composeData map[string]any, result *ValidationResult) {
	services, ok := composeData["services"].(map[string]any)
	if !ok {
		return
	}
	containerNames := map[string]string{}
	hostPorts := map[string]string{}
	for svcName, svcData := range services {
		svc, ok := svcData.(map[string]any)
		if !ok {
			continue
		}
		validateComposeServiceModeFields(result, svcName, svc)
		validateComposeServiceContainerName(result, svcName, svc, containerNames)
		validateComposeServicePorts(result, svcName, svc, hostPorts)
	}
}

func validateComposeServiceModeFields(result *ValidationResult, svcName string, svc map[string]any) {
	validateComposePatternField(result, svcName, svc, "network_mode", validNetworkMode,
		"must be none, host, bridge, service:<name>, or container:<name>")
	validateHostNetworkPorts(result, svcName, svc)
	validateComposePatternField(result, svcName, svc, "restart", validRestartPolicy,
		"must be no, always, unless-stopped, or on-failure[:max-retries]")
	validateComposePatternField(result, svcName, svc, "ipc", validIPCMode,
		"must be host, private, shareable, or service:<name>")
	validateComposePatternField(result, svcName, svc, "pid", validPIDMode,
		"must be host or service:<name>")
}

func validateComposePatternField(
	result *ValidationResult,
	svcName string,
	svc map[string]any,
	field string,
	pattern *regexp.Regexp,
	message string,
) {
	value, ok := svc[field].(string)
	if !ok || value == "" || pattern.MatchString(value) {
		return
	}
	result.Valid = false
	result.Errors = append(result.Errors, ValidationError{
		Field:   fmt.Sprintf("services.%s.%s", svcName, field),
		Message: fmt.Sprintf("invalid value %q: %s", value, message),
		Type:    "error",
	})
}

func validateHostNetworkPorts(result *ValidationResult, svcName string, svc map[string]any) {
	if networkMode, _ := svc["network_mode"].(string); networkMode == "host" {
		if portList, _ := svc["ports"].([]any); len(portList) > 0 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("services.%s.ports", svcName),
				Message: "port mappings are ignored when network_mode is 'host'",
				Type:    "warning",
			})
		}
	}
}

func validateComposeServiceContainerName(
	result *ValidationResult,
	svcName string,
	svc map[string]any,
	containerNames map[string]string,
) {
	containerName, ok := svc["container_name"].(string)
	if !ok || containerName == "" {
		return
	}
	if first, seen := containerNames[containerName]; seen {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   fmt.Sprintf("services.%s.container_name", svcName),
			Message: fmt.Sprintf("duplicate container_name %q already used by service %q", containerName, first),
			Type:    "error",
		})
		return
	}
	containerNames[containerName] = svcName
}

func validateComposeServicePorts(
	result *ValidationResult,
	svcName string,
	svc map[string]any,
	hostPorts map[string]string,
) {
	for _, hostPort := range extractHostPorts(svc) {
		if first, seen := hostPorts[hostPort]; seen {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("services.%s.ports", svcName),
				Message: fmt.Sprintf("host port %q already bound by service %q", hostPort, first),
				Type:    "error",
			})
			continue
		}
		hostPorts[hostPort] = svcName
	}
}

func validateComposeVersionField(composeData map[string]any, result *ValidationResult) {
	version, hasVersion := composeData["version"]
	if !hasVersion {
		return
	}
	versionStr, ok := version.(string)
	if !ok {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "version",
			Message: "Version should be a string",
			Type:    "warning",
		})
		return
	}
	if versionStr != "" {
		logger.Debugf("Compose file version: %s", versionStr)
	}
}

func populateNormalizedComposeContent(content string, result *ValidationResult) {
	normalized, err := NormalizeComposeFile(content)
	if err != nil {
		result.NormalizedContent = content
		return
	}
	result.NormalizedContent = normalized
}

// GetDockerFolder returns the configured Docker folder path from user config
func GetDockerFolder(username string) (any, error) {
	cfg, _, err := config.Load(username)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return map[string]string{
		"folder": string(cfg.Docker.Folder),
	}, nil
}

// GetComposeFilePath builds the full path for a compose file
func GetComposeFilePath(username, stackName string) (any, error) {
	cfg, _, err := config.Load(username)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Sanitize stack name
	sanitized := sanitizeStackName(stackName)
	if sanitized == "" {
		return nil, fmt.Errorf("invalid stack name")
	}

	// Build path: {Docker.Folder}/{stack-name}/docker-compose.yml
	stackDir := filepath.Join(string(cfg.Docker.Folder), sanitized)
	composePath := filepath.Join(stackDir, "docker-compose.yml")

	// Check if file exists
	_, err = os.Stat(composePath)
	exists := err == nil

	return ComposeFilePathInfo{
		Path:      composePath,
		Exists:    exists,
		Directory: stackDir,
	}, nil
}

// extractHostPorts returns host-port binding keys for all ports in a service.
func extractHostPorts(svc map[string]any) []string {
	portList, ok := svc["ports"].([]any)
	if !ok || len(portList) == 0 {
		return nil
	}
	var result []string
	for _, p := range portList {
		switch v := p.(type) {
		case string:
			if hp := parseHostPort(v); hp != "" {
				result = append(result, hp)
			}
		case map[string]any:
			// Long syntax: {target: 80, published: "8080", protocol: "tcp"}
			if published, ok := v["published"]; ok && published != nil {
				if s := fmt.Sprintf("%v", published); s != "" && s != "0" {
					result = append(result, s)
				}
			}
		}
	}
	return result
}

// parseHostPort extracts the host-side port key from a short-syntax port string.
// Returns empty string when there is no explicit host binding.
func parseHostPort(port string) string {
	port = strings.SplitN(port, "/", 2)[0] // strip protocol
	parts := strings.Split(port, ":")
	switch len(parts) {
	case 2:
		return parts[0] // "8080:80" → "8080"
	case 3:
		return parts[0] + ":" + parts[1] // "127.0.0.1:8080:80" → "127.0.0.1:8080"
	default:
		return "" // container-only port, no host binding
	}
}

// sanitizeStackName sanitizes a stack name for use in file paths
func sanitizeStackName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace invalid characters with hyphens
	var result strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			result.WriteRune(ch)
		} else {
			result.WriteRune('-')
		}
	}

	// Remove leading/trailing hyphens
	sanitized := strings.Trim(result.String(), "-")

	// Limit to 63 characters (Docker project name limit)
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	return sanitized
}

// DirectoryValidationResult represents the result of directory validation
type DirectoryValidationResult struct {
	Valid       bool   `json:"valid"`
	Exists      bool   `json:"exists"`
	CanCreate   bool   `json:"canCreate"`
	CanWrite    bool   `json:"canWrite"`
	Error       string `json:"error,omitempty"`
	IsDirectory bool   `json:"isDirectory"`
}

// ValidateStackDirectory validates if a directory path is suitable for creating a stack
func ValidateStackDirectory(dirPath string) (any, error) {
	result := DirectoryValidationResult{
		Valid:       false,
		Exists:      false,
		CanCreate:   false,
		CanWrite:    false,
		IsDirectory: false,
	}

	// Check if path is absolute
	if !filepath.IsAbs(dirPath) {
		result.Error = "Path must be absolute"
		return result, nil
	}

	// Clean the path
	dirPath = filepath.Clean(dirPath)

	// Check if path exists
	info, err := os.Stat(dirPath)
	if err == nil {
		// Path exists
		result.Exists = true

		// Check if it's a directory
		if !info.IsDir() {
			result.Error = "Path exists but is not a directory"
			return result, nil
		}

		result.IsDirectory = true

		// Check write permissions by trying to create a temp file
		testFile := filepath.Join(dirPath, ".linuxio-write-test")
		f, createErr := os.Create(testFile)
		if createErr != nil {
			result.Error = "No write permission in directory"
			return result, nil
		}
		f.Close()
		os.Remove(testFile)

		result.CanWrite = true
		result.Valid = true
		return result, nil
	}

	// Path doesn't exist - check if we can create it
	if !os.IsNotExist(err) {
		result.Error = fmt.Sprintf("Error accessing path: %v", err)
		return result, nil
	}

	// Check parent directory
	parentDir := filepath.Dir(dirPath)
	parentInfo, err := os.Stat(parentDir)
	if err != nil {
		if os.IsNotExist(err) {
			result.Error = "Parent directory does not exist"
		} else {
			result.Error = fmt.Sprintf("Error accessing parent directory: %v", err)
		}
		return result, nil
	}

	if !parentInfo.IsDir() {
		result.Error = "Parent path is not a directory"
		return result, nil
	}

	// Try to create the directory to verify permissions
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		result.Error = fmt.Sprintf("Cannot create directory: %v", err)
		return result, nil
	}

	// Successfully created, now check write permissions
	testFile := filepath.Join(dirPath, ".linuxio-write-test")
	f, createErr := os.Create(testFile)
	if createErr != nil {
		result.Error = "Cannot write to created directory"
		// Clean up the directory we created
		os.RemoveAll(dirPath)
		return result, nil
	}
	f.Close()
	os.Remove(testFile)

	// Clean up the test directory - we only wanted to verify permissions
	// The actual directory will be created when the stack is saved
	err = os.RemoveAll(dirPath)
	if err != nil {
		logger.Warnf("Failed to clean up test directory %s: %v", dirPath, err)
	}

	result.CanCreate = true
	result.CanWrite = true
	result.Valid = true

	return result, nil
}

// indexerHTTPClient is a shared HTTP client for communicating with the indexer daemon.
var indexerHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, "unix", "/var/run/indexer.sock")
		},
	},
	Timeout: 10 * time.Second,
}

// indexerSearchResult represents a search result from the indexer
type indexerSearchResult struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"isDir"`
}

const (
	indexerEntriesPageSize = 5000
	indexerEntriesMaxPages = 2000
)

func fetchIndexerEntriesPage(basePath string, limit, offset int) ([]indexerSearchResult, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/entries", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build indexer request: %w", err)
	}

	q := req.URL.Query()
	q.Set("path", basePath)
	q.Set("recursive", "true")
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("offset", fmt.Sprintf("%d", offset))
	req.URL.RawQuery = q.Encode()

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer returned status %s", resp.Status)
	}

	var results []indexerSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode indexer response: %w", err)
	}

	return results, nil
}

// searchIndexerForYAML searches the indexer for YAML files in the specified base path
func searchIndexerForYAML(basePath string) ([]indexerSearchResult, error) {
	normPath := normalizeIndexerPath(basePath)

	var yamlFiles []indexerSearchResult
	seenPaths := make(map[string]struct{})
	offset := 0

	for range indexerEntriesMaxPages {
		results, err := fetchIndexerEntriesPage(normPath, indexerEntriesPageSize, offset)
		if err != nil {
			logger.Debugf("indexer search request failed (indexer may be offline): %v", err)
			return nil, fmt.Errorf("indexer unavailable: %w", err)
		}

		if len(results) == 0 {
			break
		}

		for _, result := range results {
			if isYAMLFile(result) {
				if _, exists := seenPaths[result.Path]; !exists {
					seenPaths[result.Path] = struct{}{}
					yamlFiles = append(yamlFiles, result)
				}
			}
		}

		offset += len(results)
		if len(results) < indexerEntriesPageSize {
			break
		}
	}

	if offset >= indexerEntriesPageSize*indexerEntriesMaxPages {
		logger.Warnf("indexer YAML scan reached max pages for path %s", normPath)
	}

	return yamlFiles, nil
}

func normalizeIndexerPath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	path = strings.TrimRight(path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func isYAMLFile(r indexerSearchResult) bool {
	if r.IsDir {
		return false
	}
	lower := strings.ToLower(r.Name)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

// isValidComposeFile checks if a file is a valid docker-compose file
func isValidComposeFile(filePath string) bool {
	// Check if file exists
	if _, err := os.Stat(filePath); err != nil {
		return false
	}

	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	// Try to parse as YAML
	var content map[string]any
	if err := yaml.Unmarshal(data, &content); err != nil {
		return false
	}

	// Check for compose-specific fields
	// Valid compose files should have at least one of: services, version, or networks/volumes with services implied
	if _, hasServices := content["services"]; hasServices {
		return true
	}

	// Some compose files might have version without services (edge case)
	if version, hasVersion := content["version"]; hasVersion {
		// If it has a version field and it looks like a compose version, consider it valid
		if vStr, ok := version.(string); ok {
			if strings.HasPrefix(vStr, "2") || strings.HasPrefix(vStr, "3") || vStr == "3.8" || vStr == "3.9" {
				return true
			}
		}
		if vFloat, ok := version.(float64); ok {
			if vFloat >= 2.0 && vFloat < 4.0 {
				return true
			}
		}
	}

	return false
}

// getProjectNameFromComposePath extracts the likely project name from a compose file path
// For files in the docker folder structure like /path/to/docker/stackname/docker-compose.yml
// it returns "stackname". Otherwise, it returns the parent directory name.
func getProjectNameFromComposePath(composePath string) string {
	// Get the directory containing the compose file
	dir := filepath.Dir(composePath)
	// Return the base name of that directory
	return filepath.Base(dir)
}

// indexDockerFolder triggers indexing of the user's docker folder in the indexer
func indexDockerFolder(username string) error {
	// Get the user's docker folder from config
	cfg, _, err := config.Load(username)
	if err != nil {
		return fmt.Errorf("failed to load user config: %w", err)
	}

	if cfg.Docker.Folder == "" {
		return fmt.Errorf("docker folder not configured for user")
	}

	dockerFolder := string(cfg.Docker.Folder)

	// Trigger indexing of the docker folder
	indexURL := fmt.Sprintf("http://unix/reindex?path=%s", url.QueryEscape(dockerFolder))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, indexURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build indexer request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("indexer request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indexer returned status %s", resp.Status)
	}

	logger.InfoKV("triggered indexing of docker folder", "path", dockerFolder, "user", username)

	return nil
}

// IndexDockerFolder is the handler function for indexing the docker folder
func IndexDockerFolder(username string) (any, error) {
	if err := indexDockerFolder(username); err != nil {
		return nil, err
	}

	return map[string]any{
		"message": "Indexing started",
		"status":  "running",
	}, nil
}

// extractStackIcon parses a docker-compose file and extracts the stack icon from x-linuxio-stack metadata
func extractStackIcon(composePath string) (string, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return "", err
	}

	var composeData map[string]any
	if err := yaml.Unmarshal(data, &composeData); err != nil {
		return "", err
	}

	// Look for x-linuxio-stack.icon extension field
	if stackMeta, ok := composeData["x-linuxio-stack"].(map[string]any); ok {
		if icon, ok := stackMeta["icon"].(string); ok {
			return icon, nil
		}
	}

	return "", nil
}

// discoverOfflineStacks searches the indexer for compose files and adds them as offline stacks
// It merges with existing projects to handle duplicates
func discoverOfflineStacks(username string, projects map[string]*ComposeProject) error {
	cfg, _, err := config.Load(username)
	if err != nil {
		return fmt.Errorf("failed to load user config: %w", err)
	}

	if cfg.Docker.Folder == "" {
		logger.Debugf("no docker folder configured for user %s, skipping offline stack discovery", username)
		return nil
	}

	yamlFiles, err := searchIndexerForYAML(string(cfg.Docker.Folder))
	if err != nil {
		return fmt.Errorf("failed to search indexer: %w", err)
	}
	logger.Debugf("found %d YAML files in docker folder via indexer", len(yamlFiles))

	for _, yamlFile := range yamlFiles {
		if !isValidComposeFile(yamlFile.Path) {
			continue
		}
		existingProject := findOfflineComposeProjectMatch(projects, yamlFile.Path)
		if existingProject != nil {
			mergeOfflineComposeFile(existingProject, yamlFile.Path)
			continue
		}
		addOfflineComposeProject(projects, yamlFile.Path)
	}

	return nil
}

func findOfflineComposeProjectMatch(projects map[string]*ComposeProject, composePath string) *ComposeProject {
	composeDir := filepath.Dir(composePath)
	for _, project := range projects {
		if slices.Contains(project.ConfigFiles, composePath) {
			return project
		}
		if project.WorkingDir != "" && project.WorkingDir == composeDir {
			return project
		}
	}
	return nil
}

func mergeOfflineComposeFile(project *ComposeProject, composePath string) {
	if !slices.Contains(project.ConfigFiles, composePath) {
		project.ConfigFiles = append(project.ConfigFiles, composePath)
	}
	if project.WorkingDir == "" {
		project.WorkingDir = filepath.Dir(composePath)
	}
	if project.Icon == "" {
		if icon, err := extractStackIcon(composePath); err == nil && icon != "" {
			project.Icon = icon
		}
	}
	logger.DebugKV("matched compose file to existing project", "compose_file", composePath, "project", project.Name)
}

func addOfflineComposeProject(projects map[string]*ComposeProject, composePath string) {
	projectName := getProjectNameFromComposePath(composePath)
	if projectName == "" {
		return
	}
	logger.InfoKV("discovered offline stack via indexer", "project", projectName, "compose_file", composePath)
	projects[projectName] = &ComposeProject{
		Name:        projectName,
		Icon:        extractComposeIcon(composePath),
		Status:      "stopped",
		Services:    make(map[string]*ComposeService),
		ConfigFiles: []string{composePath},
		WorkingDir:  filepath.Dir(composePath),
	}
}

func extractComposeIcon(composePath string) string {
	icon, err := extractStackIcon(composePath)
	if err != nil {
		return ""
	}
	return icon
}

// DeleteComposeStack runs docker compose down and deletes the compose file(s)
func DeleteComposeStack(username, projectName string) error {
	logger.Infof("delete compose stack requested: user=%s project=%s", username, projectName)
	// Get project details to find config files
	projects, err := ListComposeProjects(username)
	if err != nil {
		return fmt.Errorf("failed to list compose projects: %w", err)
	}

	projectsList, ok := projects.([]*ComposeProject)
	if !ok {
		return fmt.Errorf("invalid projects format")
	}

	// Find the project in the slice
	var project *ComposeProject
	for _, p := range projectsList {
		if p.Name == projectName {
			project = p
			break
		}
	}

	if project == nil {
		return fmt.Errorf("project %s not found", projectName)
	}

	// Run docker compose down first to clean up containers/networks
	if len(project.ConfigFiles) > 0 {
		logger.InfoKV("running docker compose down before deleting files",
			"project", projectName,
			"working_dir", project.WorkingDir)

		collector := &composeMessageCollector{}
		err := composeDownWithSDK(
			context.Background(),
			projectName,
			project.ConfigFiles[0],
			project.WorkingDir,
			true,
			collector.Emit,
		)
		if err != nil {
			logger.WarnKV("failed to run docker compose down",
				"project", projectName,
				"error", err.Error(),
				"output", collector.String())
			// Don't fail here - continue with file deletion even if down fails
		}
	}

	deleteComposeFiles(projectName, project.ConfigFiles)
	tryRemoveEmptyDir(project.WorkingDir, projectName)

	logger.InfoKV("compose stack deleted successfully", "project", projectName)
	return nil
}

func deleteComposeFiles(projectName string, files []string) {
	for _, configFile := range files {
		logger.InfoKV("deleting compose file",
			"project", projectName,
			"file", configFile)

		if err := os.Remove(configFile); err != nil {
			if os.IsNotExist(err) {
				logger.WarnKV("compose file already deleted", "file", configFile)
			} else {
				logger.WarnKV("failed to delete compose file",
					"file", configFile,
					"error", err.Error())
			}
		}
	}
}

func tryRemoveEmptyDir(dir, projectName string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	logger.InfoKV("removing empty working directory",
		"project", projectName,
		"dir", dir)

	if err := os.Remove(dir); err != nil {
		logger.WarnKV("failed to remove working directory",
			"dir", dir,
			"error", err.Error())
	}
}
