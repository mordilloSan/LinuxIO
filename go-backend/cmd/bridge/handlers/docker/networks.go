package docker

import (
	"context"
	"fmt"
	"go-backend/internal/logger"

	"github.com/docker/docker/api/types/network"
)

// List all networks
func ListDockerNetworks() (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	networks, err := cli.NetworkList(context.Background(), network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	return networks, nil
}

// Delete a network
func DeleteDockerNetwork(name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.NetworkRemove(context.Background(), name); err != nil {
		return nil, fmt.Errorf("failed to remove network: %w", err)
	}

	return nil, nil
}

// Create a volume
func CreateDockerNetwork(name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	network, err := cli.NetworkCreate(context.Background(), name, network.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	return network, nil
}
