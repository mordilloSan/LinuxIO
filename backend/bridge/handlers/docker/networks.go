package docker

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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

// connectToProxyNetwork attaches a container to the linuxio-docker bridge so the
// built-in path proxy can reach it. The call is idempotent — Docker returns a
// "already exists" error which is silently ignored.
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

// List all networks
func ListDockerNetworks(ctx context.Context) ([]apischema.DockerNetwork, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	networks, err := cli.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	results := make([]apischema.DockerNetwork, 0, len(networks.Items))

	for _, nw := range networks.Items {
		inspect, err := cli.NetworkInspect(ctx, nw.ID, client.NetworkInspectOptions{})
		if err != nil {
			slog.
				// Log warning but continue
				Warn("failed to inspect network", "network", nw.Name, "error", err)
			continue
		}

		results = append(results, dockerNetworkFromSDK(nw, inspect.Network))
	}

	// Sort networks by Name alphabetically
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results, nil
}

func dockerNetworkFromSDK(summary network.Summary, inspect network.Inspect) apischema.DockerNetwork {
	result := apischema.DockerNetwork{
		Attachable: summary.Attachable,
		ConfigOnly: summary.ConfigOnly,
		Containers: dockerNetworkContainersFromSDK(inspect.Containers),
		Driver:     summary.Driver,
		EnableIPv4: new(summary.EnableIPv4),
		EnableIPv6: new(summary.EnableIPv6),
		ID:         summary.ID,
		Ingress:    summary.Ingress,
		Internal:   new(summary.Internal),
		IPAM:       dockerNetworkIPAMFromSDK(summary.IPAM),
		Labels:     cloneStringMap(summary.Labels),
		Name:       summary.Name,
		Options:    cloneStringMap(summary.Options),
		Scope:      summary.Scope,
	}
	if !summary.Created.IsZero() {
		result.Created = new(summary.Created.Format(time.RFC3339Nano))
	}
	return result
}

func dockerNetworkContainersFromSDK(containers map[string]network.EndpointResource) map[string]apischema.DockerNetworkContainer {
	if len(containers) == 0 {
		return nil
	}
	result := make(map[string]apischema.DockerNetworkContainer, len(containers))
	for id, container := range containers {
		result[id] = apischema.DockerNetworkContainer{
			EndpointID:  container.EndpointID,
			IPv4Address: prefixPointer(container.IPv4Address),
			IPv6Address: prefixPointer(container.IPv6Address),
			MACAddress:  hardwareAddrPointer(container.MacAddress),
			Name:        container.Name,
		}
	}
	return result
}

func dockerNetworkIPAMFromSDK(ipam network.IPAM) *apischema.DockerNetworkIPAM {
	if ipam.Driver == "" && len(ipam.Options) == 0 && len(ipam.Config) == 0 {
		return nil
	}
	result := &apischema.DockerNetworkIPAM{
		Driver:  ipam.Driver,
		Options: cloneStringMap(ipam.Options),
	}
	if len(ipam.Config) > 0 {
		result.Config = make([]apischema.DockerNetworkIPAMConfig, 0, len(ipam.Config))
		for _, config := range ipam.Config {
			result.Config = append(result.Config, apischema.DockerNetworkIPAMConfig{
				AuxiliaryAddresses: netipMapToStrings(config.AuxAddress),
				Gateway:            networkAddrString(config.Gateway),
				IPRange:            prefixString(config.IPRange),
				Subnet:             prefixString(config.Subnet),
			})
		}
	}
	return result
}

func hardwareAddrPointer(value network.HardwareAddr) *string {
	if len(value) == 0 {
		return nil
	}
	return new(value.String())
}

func prefixPointer(value netip.Prefix) *string {
	if !value.IsValid() {
		return nil
	}
	return new(value.String())
}

func prefixString(value netip.Prefix) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}

func networkAddrString(value netip.Addr) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}

func netipMapToStrings(values map[string]netip.Addr) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		if value.IsValid() {
			result[key] = value.String()
		}
	}
	return result
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
