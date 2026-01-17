package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/goccy/go-yaml"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/go-logger/logger"
)

// ComposeService represents a service within a compose project
type ComposeService struct {
	Name           string   `json:"name"`
	Image          string   `json:"image"`
	Status         string   `json:"status"`
	State          string   `json:"state"`
	ContainerCount int      `json:"container_count"`
	ContainerIDs   []string `json:"container_ids"`
	Ports          []string `json:"ports"`
}

// ComposeProject represents a docker compose stack
type ComposeProject struct {
	Name        string                     `json:"name"`
	Status      string                     `json:"status"` // "running", "partial", "stopped"
	Services    map[string]*ComposeService `json:"services"`
	ConfigFiles []string                   `json:"config_files"`
	WorkingDir  string                     `json:"working_dir"`
}

// ListComposeProjects discovers all compose projects by analyzing container labels
func ListComposeProjects() (any, error) {
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

		// Collect port mappings
		for _, port := range ctr.Ports {
			if port.PublicPort > 0 {
				portStr := fmt.Sprintf("%d:%d/%s", port.PublicPort, port.PrivatePort, port.Type)
				service.Ports = append(service.Ports, portStr)
			}
		}
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
func GetComposeProject(projectName string) (any, error) {
	projects, err := ListComposeProjects()
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
func ComposeUp(projectName string) (any, error) {
	project, err := GetComposeProject(projectName)
	if err != nil {
		return nil, err
	}

	composeProject, ok := project.(*ComposeProject)
	if !ok {
		return nil, fmt.Errorf("invalid project format")
	}

	// Get the first config file path
	if len(composeProject.ConfigFiles) == 0 {
		return nil, fmt.Errorf("no config files found for project '%s'", projectName)
	}

	configFile := composeProject.ConfigFiles[0]
	workingDir := composeProject.WorkingDir
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
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

// ComposeDown stops and removes a compose project
func ComposeDown(projectName string) (any, error) {
	project, err := GetComposeProject(projectName)
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

// ComposeRestart restarts a compose project
func ComposeRestart(projectName string) (any, error) {
	project, err := GetComposeProject(projectName)
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

	cmd := exec.Command("docker", "compose", "-f", configFile, "-p", projectName, "restart")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to restart project: %w\nOutput: %s", err, string(output))
	}

	return map[string]string{"message": "Project restarted successfully", "output": string(output)}, nil
}

// ComposeStop stops a compose project without removing containers
func ComposeStop(projectName string) (any, error) {
	project, err := GetComposeProject(projectName)
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
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

// ComposeFilePathInfo represents information about a compose file path
type ComposeFilePathInfo struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Directory string `json:"directory"`
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
		f, err := os.Create(testFile)
		if err != nil {
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
	f, err := os.Create(testFile)
	if err != nil {
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
