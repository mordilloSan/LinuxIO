package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/mordilloSan/go-logger/logger"

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

	var results []map[string]any

	for _, nw := range networks {
		inspect, err := cli.NetworkInspect(context.Background(), nw.ID, network.InspectOptions{})
		if err != nil {
			// Log warning but continue
			logger.Warnf("failed to inspect network %s: %v", nw.Name, err)
			continue
		}

		// Prepare your structure: copy summary + attach containers map
		result := map[string]any{
			"Name":       nw.Name,
			"Id":         nw.ID,
			"Scope":      nw.Scope,
			"Driver":     nw.Driver,
			"EnableIPv4": nw.EnableIPv4,
			"EnableIPv6": nw.EnableIPv6,
			"Internal":   nw.Internal,
			"Attachable": nw.Attachable,
			"Ingress":    nw.Ingress,
			"IPAM":       nw.IPAM,
			"ConfigOnly": nw.ConfigOnly,
			"Labels":     nw.Labels,
			"Options":    nw.Options,
			"Created":    nw.Created,
			"Containers": inspect.Containers, // <-- Now you have the attached containers!
		}
		results = append(results, result)
	}

	// Sort networks by Name alphabetically
	sort.Slice(results, func(i, j int) bool {
		nameI, okI := results[i]["Name"].(string)
		if !okI {
			nameI = ""
		}
		nameJ, okJ := results[j]["Name"].(string)
		if !okJ {
			nameJ = ""
		}
		return nameI < nameJ
	})

	return results, nil
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
