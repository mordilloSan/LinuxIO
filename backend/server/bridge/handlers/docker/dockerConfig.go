package docker

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mordilloSan/go_logger/logger"
)

// IsDockerInstalled returns true if 'docker' is available in PATH
func isDockerInstalled() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// IsDockerDaemonRunning returns true if 'docker info' runs without error
func isDockerDaemonRunning() bool {
	cmd := exec.Command("docker", "info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warnf("Docker daemon check failed: %v (output: %s)", err, strings.TrimSpace(string(output)))
		return false
	}
	return true
}

// EnsureDockerAvailable logs warnings or errors if Docker is not usable
func EnsureDockerAvailable() error {
	if !isDockerInstalled() {
		logger.Errorf("Docker is not installed or not in PATH")
		return fmt.Errorf("docker not installed")
	}
	if !isDockerDaemonRunning() {
		logger.Errorf("Docker daemon is not running or permission denied")
		return fmt.Errorf("docker daemon not running")
	}
	logger.Infof("Docker is installed and running")
	return nil
}
