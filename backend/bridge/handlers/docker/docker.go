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

// dockerAvailable verifies that Docker client initialization and daemon ping both work.
func dockerAvailable() (bool, error) {
	cli, err := getClient()
	if err != nil {
		return false, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if _, err := cli.Ping(context.Background()); err != nil {
		return false, fmt.Errorf("docker daemon not accessible: %w", err)
	}

	return true, nil
}

// CheckDockerAvailability verifies that Docker is installed and accessible
func CheckDockerAvailability() (bool, error) {
	ok, err := dockerAvailable()
	if err != nil {
		logger.Infof("docker service not available")
		return false, err
	}

	logger.Infof("docker service available")
	return ok, nil
}
