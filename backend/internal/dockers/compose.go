package dockers

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/internal/config"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValidProjectName(name string) bool {
	return validProjectName.MatchString(name)
}

func runComposeCommandInDir(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func getComposeProjectDir(project string) (string, error) {
	baseDir, err := config.GetDockerAppsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, project), nil
}

func checkComposeFileExists(dir string) bool {
	return fileExists(filepath.Join(dir, "compose.yaml")) || fileExists(filepath.Join(dir, "docker-compose.yml"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ComposeUp(c *gin.Context) {
	project := c.Param("project")

	if !isValidProjectName(project) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project name"})
		return
	}

	projectDir, err := getComposeProjectDir(project)
	if err != nil {
		logger.Errorf("Failed to resolve project dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project directory"})
		return
	}

	if !checkComposeFileExists(projectDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No compose.yaml found in project directory"})
		return
	}

	output, err := runComposeCommandInDir(projectDir, "up", "-d")
	if err != nil {
		logger.Errorf("Compose up failed for %s: %v", project, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to start compose project",
			"details": string(output),
		})
		return
	}

	logger.Infof("Compose project %s started", project)
	c.JSON(http.StatusOK, gin.H{"message": "Compose project started"})
}

func ComposeDown(c *gin.Context) {
	project := c.Param("project")

	if !isValidProjectName(project) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project name"})
		return
	}

	projectDir, err := getComposeProjectDir(project)
	if err != nil {
		logger.Errorf("Failed to resolve project dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project directory"})
		return
	}

	output, err := runComposeCommandInDir(projectDir, "down")
	if err != nil {
		logger.Errorf("Compose down failed for %s: %v", project, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to stop compose project",
			"details": string(output),
		})
		return
	}

	logger.Infof("Compose project %s stopped", project)
	c.JSON(http.StatusOK, gin.H{"message": "Compose project stopped"})
}

func ComposeRestart(c *gin.Context) {
	project := c.Param("project")

	if !isValidProjectName(project) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project name"})
		return
	}

	projectDir, err := getComposeProjectDir(project)
	if err != nil {
		logger.Errorf("Failed to resolve project dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project directory"})
		return
	}

	output, err := runComposeCommandInDir(projectDir, "restart")
	if err != nil {
		logger.Errorf("Compose restart failed for %s: %v", project, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to restart compose project",
			"details": string(output),
		})
		return
	}

	logger.Infof("Compose project %s restarted", project)
	c.JSON(http.StatusOK, gin.H{"message": "Compose project restarted"})
}

func ComposeStatus(c *gin.Context) {
	project := c.Param("project")

	if !isValidProjectName(project) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid project name"})
		return
	}

	projectDir, err := getComposeProjectDir(project)
	if err != nil {
		logger.Errorf("Failed to resolve project dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project directory"})
		return
	}

	output, err := runComposeCommandInDir(projectDir, "ps")
	if err != nil {
		logger.Errorf("Compose status failed for %s: %v", project, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get compose status",
			"details": string(output),
		})
		return
	}

	logger.Debugf("Compose status for %s:\n%s", project, output)
	lines := strings.Split(string(output), "\n")
	c.JSON(http.StatusOK, gin.H{"status": lines})
}

func ListComposeProjects(c *gin.Context) {
	baseDir, err := config.GetDockerAppsDir()
	if err != nil {
		logger.Errorf("Failed to get base dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get base directory"})
		return
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		logger.Errorf("Failed to list projects in %s: %v", baseDir, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list projects"})
		return
	}

	var projects []string
	for _, entry := range entries {
		if entry.IsDir() {
			projects = append(projects, entry.Name())
		}
	}

	logger.Infof("Listed %d Docker Compose projects", len(projects))
	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

func RegisterDockerComposeRoutes(router *gin.Engine) {
	docker := router.Group("/docker/compose", auth.AuthMiddleware())
	{
		docker.GET("/projects", ListComposeProjects)
		docker.POST("/:project/up", ComposeUp)
		docker.POST("/:project/down", ComposeDown)
		docker.POST("/:project/restart", ComposeRestart)
		docker.GET("/:project/status", ComposeStatus)
	}
}
