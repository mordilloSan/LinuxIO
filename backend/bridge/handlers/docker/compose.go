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
			projects[projectName] = &ComposeProject{
				Name:        projectName,
				Services:    make(map[string]*ComposeService),
				ConfigFiles: parseConfigFiles(configFiles),
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
