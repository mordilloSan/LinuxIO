package docker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/moby/moby/client"
)

const linuxIONetworkName = "linuxio-docker"

// EnsureLinuxIONetwork checks that the linuxio-docker bridge network exists and
// creates it if it does not. Failures are logged but never fatal — the bridge
// starts normally even when Docker is unavailable.
func EnsureLinuxIONetwork(ctx context.Context) {
	cli, err := getClient()
	if err != nil {
		slog.Debug("cannot ensure docker network", "component", "docker", "subsystem", "network", "network", linuxIONetworkName, "error", err)
		return
	}
	defer releaseClient(cli)

	networks, err := cli.NetworkList(ctx, client.NetworkListOptions{
		Filters: client.Filters{}.Add("name", linuxIONetworkName),
	})
	if err != nil {
		slog.Warn("failed to list docker networks", "component", "docker", "subsystem", "network", "network", linuxIONetworkName, "error", err)
		return
	}

	// NetworkList filter is a substring match — verify exact name.
	for _, nw := range networks.Items {
		if nw.Name == linuxIONetworkName {
			slog.Debug("docker network already exists", "component", "docker", "subsystem", "network", "network", linuxIONetworkName)
			return
		}
	}

	_, err = cli.NetworkCreate(ctx, linuxIONetworkName, client.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"io.linuxio.managed": "true",
		},
	})
	if err != nil {
		slog.Warn("failed to create docker network", "component", "docker", "subsystem", "network", "network", linuxIONetworkName, "error", err)
		return
	}
	slog.Info("created docker bridge network", "component", "docker", "subsystem", "network", "network", linuxIONetworkName)
}

func connectToProxyNetwork(ctx context.Context, containerID string) {
	cli, err := getClient()
	if err != nil {
		slog.Debug("failed to get docker client for proxy network connect", "component", "docker", "subsystem", "network", "container", containerID, "error", err)
		return
	}
	defer releaseClient(cli)

	_, err = cli.NetworkConnect(ctx, linuxIONetworkName, client.NetworkConnectOptions{Container: containerID})
	if err != nil {
		// "already connected" is expected and harmless
		slog.Debug("docker proxy network connect returned error", "component", "docker", "subsystem", "network", "container", containerID, "network", linuxIONetworkName, "error", err)
	}
}

// ConnectToProxyNetwork attaches a container to the linuxio-docker bridge so the
// built-in path proxy can reach it. The call is idempotent — Docker returns a
// "already exists" error which is silently ignored.
func ConnectToProxyNetwork(ctx context.Context, containerID string) {
	connectToProxyNetwork(ctx, containerID)
}

// List all networks
func ListDockerNetworks(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	networks, err := cli.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	results := make([]map[string]any, 0, len(networks.Items))

	for _, nw := range networks.Items {
		inspect, err := cli.NetworkInspect(ctx, nw.ID, client.NetworkInspectOptions{})
		if err != nil {
			slog.
				// Log warning but continue
				Warn("failed to inspect network", "network", nw.Name, "error", err)
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
			"Containers": inspect.Network.Containers, // <-- Now you have the attached containers!
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
func DeleteDockerNetwork(ctx context.Context, name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.NetworkRemove(ctx, name, client.NetworkRemoveOptions{}); err != nil {
		return nil, fmt.Errorf("failed to remove network: %w", err)
	}

	return nil, nil
}

// Create a volume
func CreateDockerNetwork(ctx context.Context, name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	network, err := cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	return network, nil
}
