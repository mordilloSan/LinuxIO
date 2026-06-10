package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const containerUpdateHealthTimeout = 5 * time.Minute

func UpdateContainer(ctx context.Context, containerID string) (apischema.DockerContainerUpdateResult, error) {
	result, err := updateContainer(ctx, containerID)
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}

func updateContainer(ctx context.Context, containerID string) (apischema.DockerContainerUpdateResult, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return updateContainerWithClient(ctx, cli, containerID)
}

func updateContainerWithClient(ctx context.Context, cli *client.Client, containerID string) (apischema.DockerContainerUpdateResult, error) {
	inspect, result, normalizedRef, oldImage, prepareErr := prepareContainerUpdate(ctx, cli, containerID)
	if prepareErr != nil {
		return result, prepareErr
	}

	newImage, pullErr := pullUpdatedContainerImage(ctx, cli, normalizedRef)
	if pullErr != nil {
		return result, pullErr
	}
	result.NewImageID = newImage.ID
	if newImage.ID == inspect.Image {
		refreshUpdatedContainerCache(ctx, cli, inspect.ID)
		return result, nil
	}

	if replaceErr := replaceContainerForUpdate(ctx, cli, inspect, oldImage.InspectResponse, normalizedRef, &result); replaceErr != nil {
		return result, replaceErr
	}
	return result, nil
}

func prepareContainerUpdate(ctx context.Context, cli *client.Client, containerID string) (container.InspectResponse, apischema.DockerContainerUpdateResult, string, client.ImageInspectResult, error) {
	inspectResult, inspectErr := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if inspectErr != nil {
		return container.InspectResponse{}, apischema.DockerContainerUpdateResult{}, "", client.ImageInspectResult{}, fmt.Errorf("inspect container: %w", inspectErr)
	}
	inspect := inspectResult.Container
	name := strings.TrimPrefix(inspect.Name, "/")
	imageRef := ""
	if inspect.Config != nil {
		imageRef = inspect.Config.Image
	}

	normalizedRef, _, normalizeErr := normalizeTaggedImageRef(imageRef)
	result := apischema.DockerContainerUpdateResult{
		ContainerID:     inspect.ID,
		ContainerName:   name,
		Image:           imageRef,
		PreviousImageID: inspect.Image,
	}
	if normalizeErr != nil {
		return inspect, result, "", client.ImageInspectResult{}, normalizeErr
	}
	result.Image = normalizedRef

	oldImage, _ := cli.ImageInspect(ctx, inspect.Image)
	return inspect, result, normalizedRef, oldImage, nil
}

func pullUpdatedContainerImage(ctx context.Context, cli *client.Client, normalizedRef string) (client.ImageInspectResult, error) {
	if pullErr := pullImageForUpdate(ctx, cli, normalizedRef); pullErr != nil {
		return client.ImageInspectResult{}, pullErr
	}
	newImage, inspectErr := cli.ImageInspect(ctx, normalizedRef)
	if inspectErr != nil {
		return client.ImageInspectResult{}, fmt.Errorf("inspect pulled image: %w", inspectErr)
	}
	return newImage, nil
}

func replaceContainerForUpdate(ctx context.Context, cli *client.Client, inspect container.InspectResponse, oldImage image.InspectResponse, normalizedRef string, result *apischema.DockerContainerUpdateResult) error {
	name := result.ContainerName
	wasRunning := inspect.State != nil && inspect.State.Running
	backupName := backupContainerName(name)
	renamed := false
	newContainerID := ""
	rollbackFailure := func(updateErr error) error {
		rollbackContainerUpdate(ctx, cli, inspect.ID, newContainerID, name, backupName, renamed, wasRunning)
		result.RolledBack = true
		return updateErr
	}

	_, renameErr := cli.ContainerRename(ctx, inspect.ID, client.ContainerRenameOptions{NewName: backupName})
	if renameErr != nil {
		return fmt.Errorf("rename old container: %w", renameErr)
	}
	renamed = true

	if wasRunning {
		_, stopErr := cli.ContainerStop(ctx, inspect.ID, stopOptionsForContainer(inspect.Config))
		if stopErr != nil {
			return rollbackFailure(fmt.Errorf("stop old container: %w", stopErr))
		}
	}

	createConfig, configErr := createConfigForUpdate(inspect.Config, oldImage, normalizedRef)
	if configErr != nil {
		return rollbackFailure(configErr)
	}
	hostConfig, hostErr := hostConfigForUpdate(ctx, cli, inspect)
	if hostErr != nil {
		return rollbackFailure(hostErr)
	}
	networkConfig := networkConfigForUpdate(inspect)
	initialNetwork, initialNetworkConfig := firstNetworkConfig(networkConfig)

	created, createErr := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           createConfig,
		HostConfig:       hostConfig,
		NetworkingConfig: initialNetworkConfig,
		Name:             name,
	})
	if createErr != nil {
		return rollbackFailure(fmt.Errorf("create replacement container: %w", createErr))
	}
	newContainerID = created.ID

	if connectErr := connectAdditionalNetworks(ctx, cli, newContainerID, networkConfig, initialNetwork); connectErr != nil {
		return rollbackFailure(connectErr)
	}

	if startErr := startUpdatedContainerIfNeeded(ctx, cli, newContainerID, wasRunning); startErr != nil {
		return rollbackFailure(startErr)
	}

	if _, removeErr := cli.ContainerRemove(ctx, inspect.ID, client.ContainerRemoveOptions{Force: true}); removeErr != nil {
		slog.Warn("failed to remove old container backup", "component", "docker", "subsystem", "updates", "container", backupName, "error", removeErr)
	}
	result.Updated = true
	refreshUpdatedContainerCache(ctx, cli, newContainerID)
	return nil
}

func startUpdatedContainerIfNeeded(ctx context.Context, cli *client.Client, containerID string, wasRunning bool) error {
	if !wasRunning {
		return nil
	}
	if _, startErr := cli.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); startErr != nil {
		return fmt.Errorf("start replacement container: %w", startErr)
	}
	return waitForContainerHealthy(ctx, cli, containerID, containerUpdateHealthTimeout)
}

func pullImageForUpdate(ctx context.Context, cli *client.Client, imageRef string) error {
	auth, err := resolveRegistryAuth(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("load registry auth: %w", err)
	}
	resp, err := cli.ImagePull(ctx, imageRef, client.ImagePullOptions{
		RegistryAuth: auth,
		PrivilegeFunc: func(context.Context) (string, error) {
			return "", nil
		},
	})
	if err != nil && auth != "" {
		resp, err = cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	}
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	defer resp.Close()
	if _, err := io.Copy(io.Discard, resp); err != nil {
		return fmt.Errorf("read image pull response: %w", err)
	}
	return nil
}

func createConfigForUpdate(src *container.Config, oldImage image.InspectResponse, imageRef string) (*container.Config, error) {
	if src == nil {
		return nil, errors.New("container config is missing")
	}
	var cfg container.Config
	if err := deepCopy(src, &cfg); err != nil {
		return nil, fmt.Errorf("copy container config: %w", err)
	}
	if oldImage.Config != nil {
		if cfg.WorkingDir == oldImage.Config.WorkingDir {
			cfg.WorkingDir = ""
		}
		if cfg.User == oldImage.Config.User {
			cfg.User = ""
		}
		if slices.Equal(cfg.Entrypoint, oldImage.Config.Entrypoint) {
			cfg.Entrypoint = nil
			if slices.Equal(cfg.Cmd, oldImage.Config.Cmd) {
				cfg.Cmd = nil
			}
		}
		cfg.Env = subtractStrings(cfg.Env, oldImage.Config.Env)
		cfg.Labels = subtractStringMap(cfg.Labels, oldImage.Config.Labels)
		cfg.Volumes = subtractStructMap(cfg.Volumes, oldImage.Config.Volumes)
		if cfg.Healthcheck != nil && oldImage.Config.Healthcheck != nil && slices.Equal(cfg.Healthcheck.Test, oldImage.Config.Healthcheck.Test) {
			cfg.Healthcheck.Test = nil
		}
	}
	cfg.Image = imageRef
	return &cfg, nil
}

func hostConfigForUpdate(ctx context.Context, cli *client.Client, inspect container.InspectResponse) (*container.HostConfig, error) {
	if inspect.HostConfig == nil {
		return &container.HostConfig{}, nil
	}
	var hostConfig container.HostConfig
	if err := deepCopy(inspect.HostConfig, &hostConfig); err != nil {
		return nil, fmt.Errorf("copy host config: %w", err)
	}
	if hostConfig.NetworkMode.IsContainer() {
		parentRef := hostConfig.NetworkMode.ConnectedContainer()
		if parentRef != "" {
			parentResult, err := cli.ContainerInspect(ctx, parentRef, client.ContainerInspectOptions{})
			if err == nil && parentResult.Container.Name != "" {
				hostConfig.NetworkMode = container.NetworkMode("container:" + strings.TrimPrefix(parentResult.Container.Name, "/"))
			}
		}
	}
	hostConfig.Links = normalizeLinks(hostConfig.Links)
	return &hostConfig, nil
}

func networkConfigForUpdate(inspect container.InspectResponse) *network.NetworkingConfig {
	cfg := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
	if inspect.NetworkSettings == nil {
		return cfg
	}
	for name, endpoint := range inspect.NetworkSettings.Networks {
		if endpoint == nil {
			continue
		}
		clone := endpoint.Copy()
		clone.NetworkID = ""
		clone.EndpointID = ""
		clone.Gateway = netip.Addr{}
		clone.IPAddress = netip.Addr{}
		clone.IPPrefixLen = 0
		clone.IPv6Gateway = netip.Addr{}
		clone.GlobalIPv6Address = netip.Addr{}
		clone.GlobalIPv6PrefixLen = 0
		clone.DNSNames = filterContainerAliases(clone.DNSNames, inspect)
		clone.Aliases = filterContainerAliases(clone.Aliases, inspect)
		cfg.EndpointsConfig[name] = clone
	}
	return cfg
}

func firstNetworkConfig(full *network.NetworkingConfig) (string, *network.NetworkingConfig) {
	cfg := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
	if full == nil || len(full.EndpointsConfig) == 0 {
		return "", cfg
	}
	names := make([]string, 0, len(full.EndpointsConfig))
	for name := range full.EndpointsConfig {
		names = append(names, name)
	}
	slices.Sort(names)
	first := names[0]
	cfg.EndpointsConfig[first] = full.EndpointsConfig[first]
	return first, cfg
}

func connectAdditionalNetworks(ctx context.Context, cli *client.Client, containerID string, full *network.NetworkingConfig, initialNetwork string) error {
	if full == nil {
		return nil
	}
	names := make([]string, 0, len(full.EndpointsConfig))
	for name := range full.EndpointsConfig {
		if name != initialNetwork {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if _, err := cli.NetworkConnect(ctx, name, client.NetworkConnectOptions{Container: containerID, EndpointConfig: full.EndpointsConfig[name]}); err != nil {
			return fmt.Errorf("connect network %s: %w", name, err)
		}
	}
	return nil
}

func rollbackContainerUpdate(ctx context.Context, cli *client.Client, oldContainerID, newContainerID, originalName, backupName string, renamed, wasRunning bool) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if newContainerID != "" {
		_, _ = cli.ContainerRemove(cleanupCtx, newContainerID, client.ContainerRemoveOptions{Force: true})
	}
	if renamed {
		_, _ = cli.ContainerRename(cleanupCtx, oldContainerID, client.ContainerRenameOptions{NewName: originalName})
	}
	if wasRunning {
		_, _ = cli.ContainerStart(cleanupCtx, oldContainerID, client.ContainerStartOptions{})
	}
	_ = backupName
}

func waitForContainerHealthy(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspect replacement health: %w", err)
		}
		inspect := inspectResult.Container
		if inspect.State == nil || inspect.State.Health == nil {
			return nil
		}
		switch inspect.State.Health.Status {
		case container.Healthy:
			return nil
		case container.Unhealthy:
			return fmt.Errorf("replacement container became unhealthy")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("replacement container health check timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func refreshUpdatedContainerCache(ctx context.Context, cli *client.Client, containerID string) {
	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return
	}
	ctr := inspectResult.Container
	status := imageUpdateStatus{
		ContainerID:     ctr.ID,
		ContainerName:   strings.TrimPrefix(ctr.Name, "/"),
		ImageID:         ctr.Image,
		ImageRef:        "",
		UpdateAvailable: false,
		CheckedAt:       time.Now(),
	}
	if ctr.Config != nil {
		status.ImageRef = ctr.Config.Image
	}
	imageUpdateCache.Lock()
	imageUpdateCache.byContainerID[ctr.ID] = status
	imageUpdateCache.byContainerName[status.ContainerName] = status
	imageUpdateCache.byImageID[ctr.Image] = status
	imageUpdateCache.Unlock()
}

func backupContainerName(name string) string {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" {
		name = "container"
	}
	return fmt.Sprintf("%s-linuxio-backup-%d", name, time.Now().Unix())
}

func stopOptionsForContainer(cfg *container.Config) client.ContainerStopOptions {
	if cfg == nil {
		return client.ContainerStopOptions{}
	}
	return client.ContainerStopOptions{Signal: cfg.StopSignal, Timeout: cfg.StopTimeout}
}

func normalizeLinks(links []string) []string {
	out := make([]string, 0, len(links))
	for _, link := range links {
		left, right, ok := strings.Cut(link, ":")
		if !ok {
			continue
		}
		out = append(out, strings.TrimPrefix(left, "/")+":"+right)
	}
	return out
}

func filterContainerAliases(values []string, inspect container.InspectResponse) []string {
	name := strings.TrimPrefix(inspect.Name, "/")
	shortID := inspect.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	return slices.DeleteFunc(slices.Clone(values), func(value string) bool {
		return value == "" || value == inspect.ID || value == shortID || value == name
	})
}

func subtractStrings(values, defaults []string) []string {
	if len(values) == 0 || len(defaults) == 0 {
		return values
	}
	return slices.DeleteFunc(slices.Clone(values), func(value string) bool {
		return slices.Contains(defaults, value)
	})
}

func subtractStringMap(values, defaults map[string]string) map[string]string {
	if len(values) == 0 || len(defaults) == 0 {
		return values
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if defaultValue, ok := defaults[key]; ok && defaultValue == value {
			continue
		}
		out[key] = value
	}
	return out
}

func subtractStructMap(values, defaults map[string]struct{}) map[string]struct{} {
	if len(values) == 0 || len(defaults) == 0 {
		return values
	}
	out := make(map[string]struct{}, len(values))
	for key, value := range values {
		if _, ok := defaults[key]; ok {
			continue
		}
		out[key] = value
	}
	return out
}

func deepCopy(src, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
