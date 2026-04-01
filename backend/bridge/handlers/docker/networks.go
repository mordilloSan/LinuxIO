package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/mordilloSan/go-logger/logger"
)

const linuxIONetworkName = "linuxio-docker"

// EnsureLinuxIONetwork checks that the linuxio-docker bridge network exists and
// creates it if it does not. Failures are logged but never fatal — the bridge
// starts normally even when Docker is unavailable.
func EnsureLinuxIONetwork() {
	cli, err := getClient()
	if err != nil {
		logger.Debugf("cannot ensure %s network: %v", linuxIONetworkName, err)
		return
	}
	defer releaseClient(cli)

	ctx := context.Background()

	networks, err := cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", linuxIONetworkName)),
	})
	if err != nil {
		logger.Warnf("failed to list networks while checking %s: %v", linuxIONetworkName, err)
		return
	}

	// NetworkList filter is a substring match — verify exact name.
	for _, nw := range networks {
		if nw.Name == linuxIONetworkName {
			logger.Debugf("%s network already exists", linuxIONetworkName)
			return
		}
	}

	_, err = cli.NetworkCreate(ctx, linuxIONetworkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"io.linuxio.managed": "true",
		},
	})
	if err != nil {
		logger.Warnf("failed to create %s network: %v", linuxIONetworkName, err)
		return
	}

	logger.Infof("created %s bridge network", linuxIONetworkName)
}

func connectToProxyNetwork(ctx context.Context, containerID string) {
	cli, err := getClient()
	if err != nil {
		logger.Debugf("ConnectToProxyNetwork: client error: %v", err)
		return
	}
	defer releaseClient(cli)

	err = cli.NetworkConnect(ctx, linuxIONetworkName, containerID, nil)
	if err != nil {
		// "already connected" is expected and harmless
		logger.Debugf("ConnectToProxyNetwork %s: %v", containerID[:12], err)
	}
}

// ConnectToProxyNetwork attaches a container to the linuxio-docker bridge so the
// built-in path proxy can reach it. The call is idempotent — Docker returns a
// "already exists" error which is silently ignored.
func ConnectToProxyNetwork(containerID string) {
	connectToProxyNetwork(context.Background(), containerID)
}

// List all networks
func ListDockerNetworks() (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

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
	defer releaseClient(cli)

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
	defer releaseClient(cli)

	network, err := cli.NetworkCreate(context.Background(), name, network.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	return network, nil
}
