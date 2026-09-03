package docker

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
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
		Protected:  isDockerDefaultNetwork(summary.Name),
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

type networkRemover interface {
	NetworkInspect(context.Context, string, client.NetworkInspectOptions) (client.NetworkInspectResult, error)
	NetworkRemove(context.Context, string, client.NetworkRemoveOptions) (client.NetworkRemoveResult, error)
}

func isDockerDefaultNetwork(name string) bool {
	switch name {
	case "bridge", "host", "none":
		return true
	default:
		return false
	}
}

func DeleteDockerNetwork(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return deleteDockerNetwork(ctx, cli, id)
}

func deleteDockerNetwork(ctx context.Context, cli networkRemover, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return bridgeipc.NewError("network ID is required", 400)
	}
	inspected, err := cli.NetworkInspect(ctx, id, client.NetworkInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect network before removal: %w", err)
	}
	if isDockerDefaultNetwork(inspected.Network.Name) {
		return bridgeipc.NewError(fmt.Sprintf("Docker default network %q cannot be deleted", inspected.Network.Name), 409)
	}
	if _, err := cli.NetworkRemove(ctx, id, client.NetworkRemoveOptions{}); err != nil {
		if errdefs.IsConflict(err) {
			return bridgeipc.NewError(fmt.Sprintf("network %q is in use: %v", inspected.Network.Name, err), 409)
		}
		return fmt.Errorf("failed to remove network %q: %w", inspected.Network.Name, err)
	}
	return nil
}

func CreateDockerNetwork(ctx context.Context, request apischema.DockerNetworkCreateRequest) error {
	name, options, err := networkCreateOptions(request)
	if err != nil {
		return bridgeipc.NewError(err.Error(), 400)
	}
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.NetworkCreate(ctx, name, options); err != nil {
		return fmt.Errorf("failed to create network %q: %w", name, err)
	}
	return nil
}

func networkCreateOptions(request apischema.DockerNetworkCreateRequest) (string, client.NetworkCreateOptions, error) {
	name := strings.TrimSpace(request.Name)
	if !containerNamePattern.MatchString(name) {
		return "", client.NetworkCreateOptions{}, fmt.Errorf("network name must start with an alphanumeric character and contain only alphanumeric characters, underscores, periods, or hyphens")
	}
	driver := strings.TrimSpace(request.Driver)
	if driver == "" {
		return "", client.NetworkCreateOptions{}, fmt.Errorf("network driver is required")
	}
	options, err := normalizedDockerStringMap(request.Options, "network driver options")
	if err != nil {
		return "", client.NetworkCreateOptions{}, err
	}
	createOptions := client.NetworkCreateOptions{
		Driver: driver, Internal: request.Internal, Attachable: request.Attachable,
		EnableIPv6: new(request.EnableIPv6), Options: options,
	}
	subnetText := strings.TrimSpace(request.Subnet)
	gatewayText := strings.TrimSpace(request.Gateway)
	if subnetText == "" {
		if gatewayText != "" {
			return "", client.NetworkCreateOptions{}, fmt.Errorf("a subnet is required when a gateway is set")
		}
		return name, createOptions, nil
	}
	subnet, err := netip.ParsePrefix(subnetText)
	if err != nil || subnet != subnet.Masked() {
		return "", client.NetworkCreateOptions{}, fmt.Errorf("subnet must be a valid masked CIDR prefix")
	}
	if subnet.Addr().Is6() && !request.EnableIPv6 {
		return "", client.NetworkCreateOptions{}, fmt.Errorf("IPv6 must be enabled for an IPv6 subnet")
	}
	ipamConfig := network.IPAMConfig{Subnet: subnet}
	if gatewayText != "" {
		gateway, err := netip.ParseAddr(gatewayText)
		if err != nil || !subnet.Contains(gateway) {
			return "", client.NetworkCreateOptions{}, fmt.Errorf("gateway must be a valid address inside the subnet")
		}
		ipamConfig.Gateway = gateway
	}
	createOptions.IPAM = &network.IPAM{Driver: "default", Config: []network.IPAMConfig{ipamConfig}}
	return name, createOptions, nil
}

func ConnectDockerNetwork(ctx context.Context, request apischema.DockerNetworkConnectRequest) error {
	networkID, containerID, aliases, err := networkConnectionInput(request.NetworkID, request.ContainerID, request.Aliases)
	if err != nil {
		return bridgeipc.NewError(err.Error(), 400)
	}
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	connectOptions := client.NetworkConnectOptions{Container: containerID}
	if len(aliases) > 0 {
		connectOptions.EndpointConfig = &network.EndpointSettings{Aliases: aliases}
	}
	if _, err := cli.NetworkConnect(ctx, networkID, connectOptions); err != nil {
		return fmt.Errorf("connect container to network: %w", err)
	}
	return nil
}

func DisconnectDockerNetwork(ctx context.Context, request apischema.DockerNetworkDisconnectRequest) error {
	networkID, containerID, _, err := networkConnectionInput(request.NetworkID, request.ContainerID, nil)
	if err != nil {
		return bridgeipc.NewError(err.Error(), 400)
	}
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	if _, err := cli.NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{Container: containerID}); err != nil {
		return fmt.Errorf("disconnect container from network: %w", err)
	}
	return nil
}

func networkConnectionInput(networkID, containerID string, aliases []string) (string, string, []string, error) {
	networkID = strings.TrimSpace(networkID)
	containerID = strings.TrimSpace(containerID)
	if networkID == "" || containerID == "" {
		return "", "", nil, fmt.Errorf("network ID and container ID are required")
	}
	if len(aliases) > maxDockerResourceEntries {
		return "", "", nil, fmt.Errorf("network aliases cannot contain more than %d entries", maxDockerResourceEntries)
	}
	seen := make(map[string]struct{}, len(aliases))
	normalized := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if _, exists := seen[alias]; exists {
			continue
		}
		seen[alias] = struct{}{}
		normalized = append(normalized, alias)
	}
	return networkID, containerID, normalized, nil
}
