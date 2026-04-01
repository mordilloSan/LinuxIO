package docker

import (
	"context"
	"fmt"
	"sync"

	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"
)

var (
	dockerClientMu sync.Mutex
	dockerClient   *client.Client
)

// getClient returns a shared Docker client for the process lifetime.
func getClient() (*client.Client, error) {
	dockerClientMu.Lock()
	defer dockerClientMu.Unlock()

	if dockerClient != nil {
		return dockerClient, nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	dockerClient = cli
	return dockerClient, nil
}

// releaseClient is a no-op because Docker handlers share a process-wide client.
func releaseClient(*client.Client) {
}

// dockerAvailable verifies that Docker client initialization and daemon ping both work.
func dockerAvailable() (bool, error) {
	cli, err := getClient()
	if err != nil {
		return false, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

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
