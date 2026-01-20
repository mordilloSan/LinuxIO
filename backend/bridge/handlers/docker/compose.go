package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/goccy/go-yaml"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
)

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
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Map to collect projects
	projects := make(map[string]*ComposeProject)

	for _, ctr := range containers {
		// Check if this container is part of a compose project
		projectName, ok := ctr.Labels["com.docker.compose.project"]
		if !ok {
			continue // Skip standalone containers
		}

		serviceName := ctr.Labels["com.docker.compose.service"]
		configFiles := ctr.Labels["com.docker.compose.project.config_files"]
		workingDir := ctr.Labels["com.docker.compose.project.working_dir"]
		containerIcon := ctr.Labels["io.linuxio.container.icon"]
		containerURL := ctr.Labels["io.linuxio.container.url"]

		// Initialize project if not exists
		if _, exists := projects[projectName]; !exists {
			parsedConfigFiles := parseConfigFiles(configFiles)

			// Translate container paths to host paths for config files
			translatedConfigFiles := make([]string, 0, len(parsedConfigFiles))
			for _, configFile := range parsedConfigFiles {
				// Check if the config file path exists on the host as-is
				if _, err := os.Stat(configFile); err == nil {
					// Path exists on host, use it directly
					translatedConfigFiles = append(translatedConfigFiles, configFile)
				} else {
					// Path doesn't exist, try to translate from container path to host path
					translatedPath := translateContainerPathToHost(cli, configFile)
					if translatedPath != configFile {
						// Translation succeeded, use translated path
						translatedConfigFiles = append(translatedConfigFiles, translatedPath)
					}
				}
			}
			parsedConfigFiles = translatedConfigFiles

			// Fallback: if config_files label is empty, infer from working_dir
			if len(parsedConfigFiles) == 0 && workingDir != "" {
				// Try to translate container paths to host paths
				translatedWorkingDir := translateContainerPathToHost(cli, workingDir)

				// Check common compose file names in the working directory
				composeFileNames := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
				for _, fileName := range composeFileNames {
					possiblePath := filepath.Join(translatedWorkingDir, fileName)
					if _, err := os.Stat(possiblePath); err == nil {
						parsedConfigFiles = []string{possiblePath}
						break
					}
				}
			}

			projects[projectName] = &ComposeProject{
				Name:        projectName,
				Services:    make(map[string]*ComposeService),
				ConfigFiles: parsedConfigFiles,
				WorkingDir:  workingDir,
			}

			// Extract stack icon from compose file if available
			if len(parsedConfigFiles) > 0 {
				if icon, err := extractStackIcon(parsedConfigFiles[0]); err == nil && icon != "" {
					projects[projectName].Icon = icon
				}
			}
		}

		project := projects[projectName]

		// Initialize service if not exists
		if _, exists := project.Services[serviceName]; !exists {
			project.Services[serviceName] = &ComposeService{
				Name:         serviceName,
				ContainerIDs: []string{},
				Ports:        []string{},
			}
		}

		service := project.Services[serviceName]

		// Add container to service
		service.ContainerIDs = append(service.ContainerIDs, ctr.ID)
		service.ContainerCount++
		service.Image = ctr.Image
		service.State = ctr.State
		service.Status = ctr.Status

		// Set icon and URL if not already set (use first container's values)
		if service.Icon == "" {
			// Resolve icon with fallback to service name, then image name
			service.Icon = ResolveIconIdentifier(containerIcon, serviceName)
		}
		if service.URL == "" && containerURL != "" {
			service.URL = containerURL
		}

		// Collect port mappings
		for _, port := range ctr.Ports {
			if port.PublicPort > 0 {
				portStr := fmt.Sprintf("%d:%d/%s", port.PublicPort, port.PrivatePort, port.Type)
				service.Ports = append(service.Ports, portStr)
			}
		}
	}

	// Query indexer for offline stacks (compose files without running containers)
	if err := discoverOfflineStacks(username, projects); err != nil {
		// Log but don't fail - indexer might be unavailable
		logger.Debugf("failed to discover offline stacks from indexer: %v", err)
	}

	// Calculate overall project status
	for _, project := range projects {
		project.Status = calculateProjectStatus(project)
	}

	// Convert map to sorted slice for consistent output
	var result []*ComposeProject
	for _, project := range projects {
		result = append(result, project)
	}

	// Sort by project name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
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

	// Execute docker compose up -d
	cmd := exec.Command("docker", "compose", "-f", configFile, "-p", projectName, "up", "-d")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to start project: %w\nOutput: %s", err, string(output))
	}

	return map[string]string{"message": "Project started successfully", "output": string(output)}, nil
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

	cmd := exec.Command("docker", "compose", "-f", configFile, "-p", projectName, "down")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to stop project: %w\nOutput: %s", err, string(output))
	}

	return map[string]string{"message": "Project stopped successfully", "output": string(output)}, nil
}

// DeleteStackOptions defines what to delete when removing a stack
type DeleteStackOptions struct {
	DeleteFile      bool `json:"delete_file"`      // Delete the compose file
	DeleteDirectory bool `json:"delete_directory"` // Delete the entire stack directory
}

// DeleteStack removes a compose stack with options to delete files
func DeleteStack(username, projectName string, options DeleteStackOptions) (any, error) {
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
		return deleteStackFiles(projectName, configFile, workingDir, options)
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
		cmd := exec.Command("docker", "compose", "-f", configFile, "-p", projectName, "down")
		cmd.Dir = workingDir
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			logger.Warnf("docker compose down failed for %s: %v, output: %s", projectName, cmdErr, string(output))
			// Continue with file deletion even if down fails
		}
	}

	// Handle file deletion
	return deleteStackFiles(projectName, configFile, workingDir, options)
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
	var configFile string
	var workingDir string

	// Try to get the project from existing containers first
	project, err := GetComposeProject(username, projectName)
	if err == nil {
		composeProject, ok := project.(*ComposeProject)
		if ok && len(composeProject.ConfigFiles) > 0 {
			configFile = composeProject.ConfigFiles[0]
			workingDir = composeProject.WorkingDir
			if workingDir == "" {
				workingDir = filepath.Dir(configFile)
			}

			// Verify the config file still exists at the labeled path
			if _, statErr := os.Stat(configFile); statErr != nil {
				logger.WarnKV("compose file from container labels not found, will search for it",
					"project", projectName,
					"labeled_path", configFile,
					"error", statErr.Error())
				configFile = ""
			}
		} else if ok && composeProject.WorkingDir != "" {
			// Config files list is empty but we have a working directory
			// This can happen with Portainer where config_files label is empty
			// Try to translate container path to host path and find compose file
			cli, cliErr := getClient()
			if cliErr == nil {
				translatedWorkingDir := translateContainerPathToHost(cli, composeProject.WorkingDir)
				cli.Close()

				logger.DebugKV("translating working directory",
					"project", projectName,
					"container_path", composeProject.WorkingDir,
					"host_path", translatedWorkingDir)

				// Try common compose file names in the translated working directory
				composeFileNames := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
				for _, fileName := range composeFileNames {
					possiblePath := filepath.Join(translatedWorkingDir, fileName)
					if _, statErr := os.Stat(possiblePath); statErr == nil {
						configFile = possiblePath
						workingDir = translatedWorkingDir
						logger.InfoKV("found compose file via working_dir translation",
							"project", projectName,
							"path", configFile)
						break
					}
				}
			}
		}
	}

	// If we couldn't get a valid config file from container labels, search for it
	if configFile == "" {
		var findErr error
		configFile, workingDir, findErr = findComposeFile(username, projectName)
		if findErr != nil {
			return nil, fmt.Errorf("compose file not found: %w", findErr)
		}
		logger.InfoKV("found compose file via search",
			"project", projectName,
			"path", configFile)
	}

	// Use 'docker compose up -d' instead of 'restart' to recreate containers with updated config
	// This ensures that changes to the compose file (like service name changes) are applied
	cmd := exec.Command("docker", "compose", "-f", configFile, "-p", projectName, "up", "-d", "--remove-orphans")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to restart project: %w\nOutput: %s", err, string(output))
	}

	return map[string]string{"message": "Project restarted successfully", "output": string(output)}, nil
}

// ComposeStop stops a compose project without removing containers
func ComposeStop(username, projectName string) (any, error) {
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

	cmd := exec.Command("docker", "compose", "-f", configFile, "-p", projectName, "stop")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to stop project: %w\nOutput: %s", err, string(output))
	}

	return map[string]string{"message": "Project stopped successfully", "output": string(output)}, nil
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

	// Check each mount to see if the container path is within it
	for _, mount := range containerJSON.Mounts {
		// Check if the container path starts with this mount's destination
		if relPath, found := strings.CutPrefix(containerPath, mount.Destination); found {
			// Remove leading slash if any
			relPath = strings.TrimPrefix(relPath, "/")

			// Construct the host path
			if mount.Source != "" {
				hostPath := filepath.Join(mount.Source, relPath)

				// Check if this is a bind mount (user-accessible) or volume (Docker-managed)
				// For bind mounts, the source is a regular filesystem path
				// For volumes, the source is typically /var/lib/docker/volumes/...
				isVolume := mount.Type == "volume"
				isBindMount := mount.Type == "bind"

				// Verify the path exists and is accessible
				if stat, err := os.Stat(hostPath); err == nil {
					// Check if it's a Docker volume path
					if isVolume && strings.HasPrefix(mount.Source, "/var/lib/docker/volumes/") {
						logger.Debugf("found path in Docker volume %s (path: %s) - may require elevated permissions", mount.Name, hostPath)
						// Still return it - it exists and might be accessible
					} else if isBindMount {
						logger.Debugf("translated container path %s to host path %s via bind mount in container %s", containerPath, hostPath, containerName)
					}

					// Only return if it's a regular file or directory
					if stat.IsDir() || stat.Mode().IsRegular() {
						return hostPath
					}
				} else {
					// If the exact path doesn't exist, try checking parent directory + filename
					// This handles cases where we're translating a full file path
					parentDir := filepath.Dir(hostPath)
					if parentStat, err := os.Stat(parentDir); err == nil && parentStat.IsDir() {
						// Parent directory exists, return the path even if file doesn't exist yet
						logger.Debugf("translated container path %s to host path %s (parent dir verified) via container %s", containerPath, hostPath, containerName)
						return hostPath
					}
				}
			}
		}
	}

	return containerPath
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
	var composeData map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &composeData); err != nil {
		// Return original content if we can't parse it (validation will catch this later)
		return content, nil
	}

	// Get services section
	services, hasServices := composeData["services"]
	if !hasServices {
		return content, nil
	}

	servicesMap, ok := services.(map[string]interface{})
	if !ok {
		return content, nil
	}

	// Add container_name to each service that doesn't have it
	modified := false
	for serviceName, serviceData := range servicesMap {
		serviceMap, ok := serviceData.(map[string]interface{})
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

	// Parse YAML to verify syntax
	var composeData map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &composeData); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Message: fmt.Sprintf("Invalid YAML syntax: %v", err),
			Type:    "error",
		})
		return result, nil
	}

	// Check for services section
	services, hasServices := composeData["services"]
	if !hasServices {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "services",
			Message: "Missing required 'services' section",
			Type:    "error",
		})
	} else {
		// Validate services is a map
		servicesMap, ok := services.(map[string]interface{})
		if !ok {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   "services",
				Message: "'services' must be a mapping of service names to service definitions",
				Type:    "error",
			})
		} else if len(servicesMap) == 0 {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   "services",
				Message: "At least one service must be defined",
				Type:    "error",
			})
		} else {
			// Validate each service
			for serviceName, serviceData := range servicesMap {
				serviceMap, ok := serviceData.(map[string]interface{})
				if !ok {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Field:   fmt.Sprintf("services.%s", serviceName),
						Message: "Service definition must be a mapping",
						Type:    "error",
					})
					continue
				}

				// Check that service has either image or build
				_, hasImage := serviceMap["image"]
				_, hasBuild := serviceMap["build"]
				if !hasImage && !hasBuild {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Field:   fmt.Sprintf("services.%s", serviceName),
						Message: "Service must have either 'image' or 'build' defined",
						Type:    "error",
					})
				}
			}
		}
	}

	// Optional: Check for version (deprecated in v3 but still common)
	if version, hasVersion := composeData["version"]; hasVersion {
		versionStr, ok := version.(string)
		if !ok {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "version",
				Message: "Version should be a string",
				Type:    "warning",
			})
		} else if versionStr != "" {
			// Just a warning for information
			logger.Debugf("Compose file version: %s", versionStr)
		}
	}

	// Normalize the compose file (add container_name where missing)
	if result.Valid {
		if normalized, normErr := NormalizeComposeFile(content); normErr == nil {
			result.NormalizedContent = normalized
		} else {
			// Normalization failed, but validation passed - just use original content
			result.NormalizedContent = content
		}
	}

	return result, nil
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

// searchIndexerForYAML searches the indexer for YAML files in the specified base path
func searchIndexerForYAML(basePath string) ([]indexerSearchResult, error) {
	// Normalize the base path
	normPath := basePath
	if normPath == "" || normPath == "/" {
		normPath = "/"
	} else {
		normPath = strings.TrimRight(normPath, "/")
		if !strings.HasPrefix(normPath, "/") {
			normPath = "/" + normPath
		}
	}

	// Search for .yml and .yaml files in the base path
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/entries", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build indexer request: %w", err)
	}

	q := req.URL.Query()
	q.Set("path", normPath)
	q.Set("recursive", "true")
	q.Set("limit", "1000")
	req.URL.RawQuery = q.Encode()

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		logger.Debugf("indexer search request failed (indexer may be offline): %v", err)
		return nil, fmt.Errorf("indexer unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("indexer returned non-OK status: %s", resp.Status)
		return nil, fmt.Errorf("indexer returned status %s", resp.Status)
	}

	var results []indexerSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode indexer response: %w", err)
	}

	// Filter for YAML files only
	var yamlFiles []indexerSearchResult
	for _, result := range results {
		if result.IsDir {
			continue
		}
		lowerName := strings.ToLower(result.Name)
		if strings.HasSuffix(lowerName, ".yml") || strings.HasSuffix(lowerName, ".yaml") {
			yamlFiles = append(yamlFiles, result)
		}
	}

	return yamlFiles, nil
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
	var content map[string]interface{}
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

// reindexDockerFolder triggers a reindex of the user's docker folder in the indexer
func reindexDockerFolder(username string) error {
	// Get the user's docker folder from config
	cfg, _, err := config.Load(username)
	if err != nil {
		return fmt.Errorf("failed to load user config: %w", err)
	}

	if cfg.Docker.Folder == "" {
		return fmt.Errorf("docker folder not configured for user")
	}

	dockerFolder := string(cfg.Docker.Folder)

	// Trigger reindex of the docker folder
	reindexURL := fmt.Sprintf("http://unix/reindex?path=%s", url.QueryEscape(dockerFolder))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, reindexURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build reindex request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("indexer reindex request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indexer reindex returned status %s", resp.Status)
	}

	logger.InfoKV("triggered reindex of docker folder", "path", dockerFolder, "user", username)

	return nil
}

// ReindexDockerFolder is the handler function for reindexing the docker folder
func ReindexDockerFolder(username string) (any, error) {
	if err := reindexDockerFolder(username); err != nil {
		return nil, err
	}

	return map[string]any{
		"message": "Reindex started",
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
	// Get the user's docker folder from config
	cfg, _, err := config.Load(username)
	if err != nil {
		return fmt.Errorf("failed to load user config: %w", err)
	}

	// If no docker folder is configured, skip indexer search
	if cfg.Docker.Folder == "" {
		logger.Debugf("no docker folder configured for user %s, skipping offline stack discovery", username)
		return nil
	}

	dockerFolder := string(cfg.Docker.Folder)

	// Search indexer for YAML files in the docker folder
	yamlFiles, err := searchIndexerForYAML(dockerFolder)
	if err != nil {
		return fmt.Errorf("failed to search indexer: %w", err)
	}

	logger.Debugf("found %d YAML files in docker folder via indexer", len(yamlFiles))

	// Check each YAML file to see if it's a valid compose file
	for _, yamlFile := range yamlFiles {
		// Check if this is a valid docker compose file
		if !isValidComposeFile(yamlFile.Path) {
			continue
		}

		composeDir := filepath.Dir(yamlFile.Path)

		// Check if this compose file is already associated with an existing project
		// Match by: 1) config file path, or 2) working directory
		var existingProject *ComposeProject
		for _, project := range projects {
			// Check if config file already listed
			for _, cf := range project.ConfigFiles {
				if cf == yamlFile.Path {
					existingProject = project
					break
				}
			}
			if existingProject != nil {
				break
			}

			// Check if working directory matches
			if project.WorkingDir != "" && project.WorkingDir == composeDir {
				existingProject = project
				break
			}
		}

		if existingProject != nil {
			// Project exists with containers, just ensure config file is listed
			configFileExists := false
			for _, cf := range existingProject.ConfigFiles {
				if cf == yamlFile.Path {
					configFileExists = true
					break
				}
			}
			if !configFileExists {
				existingProject.ConfigFiles = append(existingProject.ConfigFiles, yamlFile.Path)
			}
			// Update working dir if not set
			if existingProject.WorkingDir == "" {
				existingProject.WorkingDir = composeDir
			}
			// Extract stack icon if not already set
			if existingProject.Icon == "" {
				if icon, err := extractStackIcon(yamlFile.Path); err == nil && icon != "" {
					existingProject.Icon = icon
				}
			}
			logger.DebugKV("matched compose file to existing project",
				"compose_file", yamlFile.Path,
				"project", existingProject.Name)
			continue
		}

		// Extract project name from the file path for new offline projects
		projectName := getProjectNameFromComposePath(yamlFile.Path)
		if projectName == "" {
			continue
		}

		// Create new offline project
		logger.InfoKV("discovered offline stack via indexer",
			"project", projectName,
			"compose_file", yamlFile.Path)

		// Extract stack icon from compose file
		stackIcon := ""
		if icon, err := extractStackIcon(yamlFile.Path); err == nil && icon != "" {
			stackIcon = icon
		}

		projects[projectName] = &ComposeProject{
			Name:        projectName,
			Icon:        stackIcon,
			Status:      "stopped", // No containers, so it's stopped
			Services:    make(map[string]*ComposeService),
			ConfigFiles: []string{yamlFile.Path},
			WorkingDir:  composeDir,
		}
	}

	return nil
}

// DeleteComposeStack runs docker compose down and deletes the compose file(s)
func DeleteComposeStack(username, projectName string) error {
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
	if len(project.ConfigFiles) > 0 && project.WorkingDir != "" {
		logger.InfoKV("running docker compose down before deleting files",
			"project", projectName,
			"working_dir", project.WorkingDir)

		cmd := exec.Command("docker", "compose", "down", "--remove-orphans")
		cmd.Dir = project.WorkingDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.WarnKV("failed to run docker compose down",
				"project", projectName,
				"error", err.Error(),
				"output", string(output))
			// Don't fail here - continue with file deletion even if down fails
		}
	}

	// Delete all config files
	for _, configFile := range project.ConfigFiles {
		logger.InfoKV("deleting compose file",
			"project", projectName,
			"file", configFile)

		if err := os.Remove(configFile); err != nil {
			if os.IsNotExist(err) {
				logger.WarnKV("compose file already deleted", "file", configFile)
			} else {
				return fmt.Errorf("failed to delete compose file %s: %w", configFile, err)
			}
		}
	}

	// Try to delete working directory if it's empty
	if project.WorkingDir != "" {
		entries, err := os.ReadDir(project.WorkingDir)
		if err == nil && len(entries) == 0 {
			logger.InfoKV("removing empty working directory",
				"project", projectName,
				"dir", project.WorkingDir)

			if err := os.Remove(project.WorkingDir); err != nil {
				logger.WarnKV("failed to remove working directory",
					"dir", project.WorkingDir,
					"error", err.Error())
				// Don't fail - directory removal is optional
			}
		}
	}

	logger.InfoKV("compose stack deleted successfully", "project", projectName)
	return nil
}
