package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"
)

// Helper to get a docker client
func getClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv)
}

// CheckDockerAvailability verifies that Docker is installed and accessible
func CheckDockerAvailability() (bool, error) {
	cli, err := getClient()
	if err != nil {
		logger.Warnf("docker client error: %v", err)
		return false, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	_, err = cli.Ping(context.Background())
	if err != nil {
		logger.Warnf("docker daemon not accessible: %v", err)
		return false, fmt.Errorf("docker daemon not accessible: %w", err)
	}

	logger.Infof("docker service available")
	return true, nil
}
