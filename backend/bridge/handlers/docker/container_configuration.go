package docker

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const maxContainerConfigurationEntries = 256

var containerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func CreateConfiguredContainer(ctx context.Context, request apischema.ContainerCreateRequest) (apischema.ContainerConfigurationResult, error) {
	options, err := containerCreateOptions(request.Configuration)
	if err != nil {
		return apischema.ContainerConfigurationResult{}, bridgeipc.NewError(err.Error(), 400)
	}

	cli, err := getClient()
	if err != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	release, err := acquireDockerUpdateLock(ctx)
	if err != nil {
		return apischema.ContainerConfigurationResult{}, err
	}
	defer release()
	if recoveryErr := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); recoveryErr != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("recover previous standalone Docker update: %w", recoveryErr)
	}

	return createConfiguredContainer(ctx, cli, request, options)
}

func createConfiguredContainer(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	request apischema.ContainerCreateRequest,
	options client.ContainerCreateOptions,
) (apischema.ContainerConfigurationResult, error) {
	if err := ensureContainerImage(ctx, cli, request.Configuration.Image); err != nil {
		return apischema.ContainerConfigurationResult{}, err
	}
	created, err := cli.ContainerCreate(ctx, options)
	if err != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("create container %q: %w", options.Name, err)
	}
	if request.Start {
		if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
			return apischema.ContainerConfigurationResult{}, errors.Join(
				fmt.Errorf("start container %q: %w", options.Name, err),
				removeFailedConfiguredContainer(ctx, cli, created.ID),
			)
		}
		if _, err := waitForContainerReady(ctx, cli, created.ID); err != nil {
			return apischema.ContainerConfigurationResult{}, errors.Join(
				fmt.Errorf("verify container %q: %w", options.Name, err),
				removeFailedConfiguredContainer(ctx, cli, created.ID),
			)
		}
	}
	return apischema.ContainerConfigurationResult{ContainerID: created.ID, Name: options.Name}, nil
}

func EditConfiguredContainer(ctx context.Context, request apischema.ContainerEditRequest) (apischema.ContainerConfigurationResult, error) {
	if _, err := containerCreateOptions(request.Configuration); err != nil {
		return apischema.ContainerConfigurationResult{}, bridgeipc.NewError(err.Error(), 400)
	}

	cli, err := getClient()
	if err != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	release, err := acquireDockerUpdateLock(ctx)
	if err != nil {
		return apischema.ContainerConfigurationResult{}, err
	}
	defer release()
	if recoveryErr := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); recoveryErr != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("recover previous standalone Docker update: %w", recoveryErr)
	}

	return editConfiguredContainer(ctx, cli, request, &defaultStandaloneUpdateJournal)
}

func editConfiguredContainer(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	request apischema.ContainerEditRequest,
	journal *standaloneUpdateJournal,
) (apischema.ContainerConfigurationResult, error) {
	inspectResult, err := cli.ContainerInspect(ctx, request.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("inspect container before edit: %w", err)
	}
	before := inspectResult.Container
	originalName := strings.TrimPrefix(before.Name, "/")
	if before.Config != nil && strings.TrimSpace(before.Config.Labels["com.docker.compose.project"]) != "" {
		return apischema.ContainerConfigurationResult{}, bridgeipc.NewError("Compose-managed containers must be edited through their stack", 409)
	}
	policy := stoppedContainerUpdatePolicy{Allow: true}
	if preflightErr := validateStandaloneUpdatePreconditions(ctx, cli, before, journal, nil, policy); preflightErr != nil {
		return apischema.ContainerConfigurationResult{}, bridgeipc.NewError(preflightErr.Error(), 409)
	}
	if mountErr := validateEditableContainerMounts(before.Mounts); mountErr != nil {
		return apischema.ContainerConfigurationResult{}, bridgeipc.NewError(mountErr.Error(), 409)
	}
	if imageErr := ensureContainerImage(ctx, cli, request.Configuration.Image); imageErr != nil {
		return apischema.ContainerConfigurationResult{}, imageErr
	}

	options, err := standaloneCreateOptions(before, request.Configuration.Image, request.Configuration.Name)
	if err != nil {
		return apischema.ContainerConfigurationResult{}, err
	}
	appendInspectedEditableMounts(options.HostConfig, before.Mounts)
	if applyErr := applyContainerConfiguration(&options, request.Configuration); applyErr != nil {
		return apischema.ContainerConfigurationResult{}, bridgeipc.NewError(applyErr.Error(), 400)
	}

	tx := standaloneUpdateTransaction{
		Phase:           standaloneUpdatePrepared,
		OriginalID:      before.ID,
		OriginalName:    originalName,
		BackupName:      standaloneBackupName(before.ID),
		ReplacementName: options.Name,
		OriginalRunning: before.State != nil && before.State.Running,
	}
	if parkErr := parkStandaloneOriginal(ctx, cli, tx, journal); parkErr != nil {
		return apischema.ContainerConfigurationResult{}, parkErr
	}
	after, replacementErr := createAndVerifyStandaloneReplacementWithProgress(
		ctx,
		cli,
		before.ID,
		originalName,
		options,
		tx,
		journal,
		policy,
		nil,
	)
	if replacementErr != nil {
		return apischema.ContainerConfigurationResult{}, replacementErr
	}
	if _, removeErr := cli.ContainerRemove(ctx, before.ID, client.ContainerRemoveOptions{}); removeErr != nil {
		return apischema.ContainerConfigurationResult{}, fmt.Errorf("remove rollback container %q after successful edit: %w", tx.BackupName, removeErr)
	}
	if clearErr := clearStandaloneJournal(journal); clearErr != nil {
		return apischema.ContainerConfigurationResult{}, clearErr
	}
	return apischema.ContainerConfigurationResult{ContainerID: after.ID, Name: options.Name}, nil
}

func containerCreateOptions(configuration apischema.ContainerConfiguration) (client.ContainerCreateOptions, error) {
	name := strings.TrimSpace(configuration.Name)
	if name == "" || len(name) > 255 || !containerNamePattern.MatchString(name) {
		return client.ContainerCreateOptions{}, errors.New("container name must begin with a letter or number and use only letters, numbers, periods, underscores, or hyphens")
	}
	imageRef := strings.TrimSpace(configuration.Image)
	if _, _, _, err := normalizeUpdateReference(imageRef); err != nil {
		return client.ContainerCreateOptions{}, err
	}
	options := client.ContainerCreateOptions{
		Config:           &container.Config{},
		HostConfig:       &container.HostConfig{},
		NetworkingConfig: &network.NetworkingConfig{},
		Name:             name,
	}
	configuration.Name = name
	configuration.Image = imageRef
	if err := applyContainerConfiguration(&options, configuration); err != nil {
		return client.ContainerCreateOptions{}, err
	}
	return options, nil
}

func applyContainerConfiguration(options *client.ContainerCreateOptions, configuration apischema.ContainerConfiguration) error {
	if options.Config == nil || options.HostConfig == nil {
		return errors.New("container configuration is incomplete")
	}
	if err := validateContainerConfiguration(configuration); err != nil {
		return err
	}

	options.Name = strings.TrimSpace(configuration.Name)
	options.Config.Image = strings.TrimSpace(configuration.Image)
	options.Config.Cmd = append([]string(nil), configuration.Command...)
	options.Config.Entrypoint = append([]string(nil), configuration.Entrypoint...)
	options.Config.Env = make([]string, 0, len(configuration.Environment))
	for _, variable := range configuration.Environment {
		options.Config.Env = append(options.Config.Env, strings.TrimSpace(variable.Name)+"="+variable.Value)
	}
	options.Config.User = strings.TrimSpace(configuration.User)
	options.Config.WorkingDir = strings.TrimSpace(configuration.WorkingDirectory)
	options.HostConfig.RestartPolicy = container.RestartPolicy{
		Name:              container.RestartPolicyMode(configuration.RestartPolicy.Name),
		MaximumRetryCount: configuration.RestartPolicy.MaximumRetryCount,
	}
	applyContainerPorts(options, configuration.Ports)
	applyContainerMounts(options, configuration.Mounts)
	applyContainerNetworks(options, configuration.Networks)
	return nil
}

func applyContainerPorts(options *client.ContainerCreateOptions, ports []apischema.ContainerPortBinding) {
	options.Config.ExposedPorts = make(network.PortSet, len(ports))
	options.HostConfig.PortBindings = make(network.PortMap, len(ports))
	for _, published := range ports {
		port, _ := network.ParsePort(fmt.Sprintf("%d/%s", published.ContainerPort, published.Protocol))
		options.Config.ExposedPorts[port] = struct{}{}
		if published.HostPort == "" {
			continue
		}
		var hostIP netip.Addr
		if published.HostIP != "" {
			hostIP, _ = netip.ParseAddr(published.HostIP)
		}
		options.HostConfig.PortBindings[port] = append(options.HostConfig.PortBindings[port], network.PortBinding{
			HostIP:   hostIP,
			HostPort: published.HostPort,
		})
	}
}

func applyContainerMounts(options *client.ContainerCreateOptions, mounts []apischema.ContainerMountConfiguration) {
	options.Config.Volumes = make(map[string]struct{})
	previousMounts := append([]mount.Mount(nil), options.HostConfig.Mounts...)
	options.HostConfig.Binds = nil
	options.HostConfig.Mounts = slices.DeleteFunc(options.HostConfig.Mounts, func(item mount.Mount) bool {
		return item.Type == mount.TypeBind || item.Type == mount.TypeVolume
	})
	for _, configured := range mounts {
		mountType := mount.Type(configured.Type)
		target := filepath.Clean(strings.TrimSpace(configured.Destination))
		configuredMount := mount.Mount{}
		if index := slices.IndexFunc(previousMounts, func(previous mount.Mount) bool {
			return previous.Type == mountType && previous.Target == target
		}); index >= 0 {
			configuredMount = previousMounts[index]
		}
		configuredMount.Type = mountType
		configuredMount.Source = strings.TrimSpace(configured.Source)
		configuredMount.Target = target
		configuredMount.ReadOnly = configured.ReadOnly
		options.HostConfig.Mounts = append(options.HostConfig.Mounts, configuredMount)
		if mountType == mount.TypeVolume {
			options.Config.Volumes[target] = struct{}{}
		}
	}
}

func applyContainerNetworks(options *client.ContainerCreateOptions, networks []apischema.ContainerNetworkAttachment) {
	previousNetworkMode := options.HostConfig.NetworkMode
	previousEndpoints := map[string]*network.EndpointSettings{}
	if options.NetworkingConfig != nil {
		previousEndpoints = options.NetworkingConfig.EndpointsConfig
	}
	endpoints := make(map[string]*network.EndpointSettings, len(networks))
	for _, attached := range networks {
		name := strings.TrimSpace(attached.Name)
		endpoint := previousEndpoints[name]
		if endpoint == nil {
			endpoint = &network.EndpointSettings{}
		}
		endpoint.Aliases = cleanStrings(attached.Aliases)
		endpoints[name] = endpoint
	}
	options.NetworkingConfig = &network.NetworkingConfig{EndpointsConfig: endpoints}
	if len(networks) == 0 {
		options.HostConfig.NetworkMode = network.NetworkNone
	} else if !configurationContainsNetworkMode(networks, previousNetworkMode) {
		options.HostConfig.NetworkMode = container.NetworkMode(strings.TrimSpace(networks[0].Name))
	}
}

func appendInspectedEditableMounts(hostConfig *container.HostConfig, inspected []container.MountPoint) {
	for _, point := range inspected {
		if point.Type != mount.TypeBind && point.Type != mount.TypeVolume {
			continue
		}
		if slices.ContainsFunc(hostConfig.Mounts, func(configured mount.Mount) bool {
			return configured.Type == point.Type && configured.Target == point.Destination
		}) {
			continue
		}
		configured := mount.Mount{
			Type:     point.Type,
			Source:   point.Name,
			Target:   point.Destination,
			ReadOnly: !point.RW,
		}
		if point.Type == mount.TypeBind {
			configured.Source = point.Source
			if point.Propagation != "" {
				configured.BindOptions = &mount.BindOptions{Propagation: point.Propagation}
			}
		} else if slices.Contains(strings.Split(point.Mode, ","), "nocopy") {
			configured.VolumeOptions = &mount.VolumeOptions{NoCopy: true}
		}
		hostConfig.Mounts = append(hostConfig.Mounts, configured)
	}
}

func validateEditableContainerMounts(mounts []container.MountPoint) error {
	for _, point := range mounts {
		if point.Type == mount.TypeVolume && point.Name == "" {
			return fmt.Errorf("cannot edit a container with an anonymous volume mounted at %q", point.Destination)
		}
		if point.Type == mount.TypeBind && point.Source == "" {
			return fmt.Errorf("cannot edit a container with a bind mount at %q without a host source", point.Destination)
		}
	}
	return nil
}

func configurationContainsNetworkMode(networks []apischema.ContainerNetworkAttachment, mode container.NetworkMode) bool {
	name := string(mode)
	if name == "default" {
		name = "bridge"
	}
	return name != "" && containsNetwork(networks, name)
}

func validateContainerConfiguration(configuration apischema.ContainerConfiguration) error {
	if len(configuration.Command) > maxContainerConfigurationEntries ||
		len(configuration.Entrypoint) > maxContainerConfigurationEntries ||
		len(configuration.Environment) > maxContainerConfigurationEntries ||
		len(configuration.Ports) > maxContainerConfigurationEntries ||
		len(configuration.Mounts) > maxContainerConfigurationEntries ||
		len(configuration.Networks) > maxContainerConfigurationEntries {
		return fmt.Errorf("container configuration sections are limited to %d entries", maxContainerConfigurationEntries)
	}
	if err := validateContainerArgumentsAndEnvironment(configuration); err != nil {
		return err
	}
	if err := validateContainerPorts(configuration); err != nil {
		return err
	}
	if err := validateContainerMounts(configuration); err != nil {
		return err
	}
	if err := validateContainerNetworks(configuration); err != nil {
		return err
	}
	return validateContainerRuntime(configuration)
}

func validateContainerArgumentsAndEnvironment(configuration apischema.ContainerConfiguration) error {
	for _, argument := range append(append([]string(nil), configuration.Command...), configuration.Entrypoint...) {
		if strings.ContainsRune(argument, '\x00') {
			return errors.New("command and entrypoint arguments cannot contain null bytes")
		}
	}
	seenEnvironment := make(map[string]struct{}, len(configuration.Environment))
	for _, variable := range configuration.Environment {
		name := strings.TrimSpace(variable.Name)
		if name == "" || strings.ContainsAny(name, "=\x00") || strings.ContainsRune(variable.Value, '\x00') {
			return errors.New("environment variable names must be non-empty and cannot contain equals signs or null bytes")
		}
		if _, exists := seenEnvironment[name]; exists {
			return fmt.Errorf("environment variable %q is duplicated", name)
		}
		seenEnvironment[name] = struct{}{}
	}
	return nil
}

func validateContainerPorts(configuration apischema.ContainerConfiguration) error {
	seenPorts := make(map[string]struct{}, len(configuration.Ports))
	for _, published := range configuration.Ports {
		if published.Protocol != string(network.TCP) && published.Protocol != string(network.UDP) && published.Protocol != string(network.SCTP) {
			return fmt.Errorf("port protocol %q must be tcp, udp, or sctp", published.Protocol)
		}
		port, portErr := network.ParsePort(fmt.Sprintf("%d/%s", published.ContainerPort, published.Protocol))
		if portErr != nil {
			return portErr
		}
		if published.HostPort != "" {
			if _, hostPortErr := network.ParsePort(published.HostPort + "/tcp"); hostPortErr != nil {
				return fmt.Errorf("invalid published host port %q: %w", published.HostPort, hostPortErr)
			}
		}
		if published.HostIP != "" {
			if _, addressErr := netip.ParseAddr(published.HostIP); addressErr != nil {
				return fmt.Errorf("invalid published host IP %q: %w", published.HostIP, addressErr)
			}
		}
		key := port.String() + "\x00" + published.HostIP + "\x00" + published.HostPort
		if _, exists := seenPorts[key]; exists {
			return fmt.Errorf("published port %s is duplicated", port)
		}
		seenPorts[key] = struct{}{}
	}
	return nil
}

func validateContainerMounts(configuration apischema.ContainerConfiguration) error {
	seenTargets := make(map[string]struct{}, len(configuration.Mounts))
	for _, configured := range configuration.Mounts {
		mountType := mount.Type(configured.Type)
		if mountType != mount.TypeBind && mountType != mount.TypeVolume {
			return fmt.Errorf("mount type %q must be bind or volume", configured.Type)
		}
		source := strings.TrimSpace(configured.Source)
		target := strings.TrimSpace(configured.Destination)
		if strings.ContainsRune(source, '\x00') || strings.ContainsRune(target, '\x00') {
			return errors.New("mount paths cannot contain null bytes")
		}
		if source == "" || !filepath.IsAbs(target) {
			return errors.New("mounts require a source and an absolute container destination")
		}
		if mountType == mount.TypeBind && !filepath.IsAbs(source) {
			return errors.New("bind mount sources must be absolute host paths")
		}
		cleanTarget := filepath.Clean(target)
		if _, exists := seenTargets[cleanTarget]; exists {
			return fmt.Errorf("mount destination %q is duplicated", cleanTarget)
		}
		seenTargets[cleanTarget] = struct{}{}
	}
	return nil
}

func validateContainerNetworks(configuration apischema.ContainerConfiguration) error {
	seenNetworks := make(map[string]struct{}, len(configuration.Networks))
	for _, attached := range configuration.Networks {
		name := strings.TrimSpace(attached.Name)
		if name == "" || strings.ContainsRune(name, '\x00') {
			return errors.New("network names cannot be empty or contain null bytes")
		}
		if _, exists := seenNetworks[name]; exists {
			return fmt.Errorf("network %q is duplicated", name)
		}
		seenNetworks[name] = struct{}{}
		if len(attached.Aliases) > maxContainerConfigurationEntries {
			return fmt.Errorf("network aliases are limited to %d entries", maxContainerConfigurationEntries)
		}
		for _, alias := range attached.Aliases {
			if strings.ContainsRune(alias, '\x00') {
				return errors.New("network aliases cannot contain null bytes")
			}
		}
	}
	if len(configuration.Networks) > 1 && (containsNetwork(configuration.Networks, "host") || containsNetwork(configuration.Networks, "none")) {
		return errors.New("host and none networks cannot be combined with other networks")
	}
	if containsNetwork(configuration.Networks, "host") && len(configuration.Ports) > 0 {
		return errors.New("published ports cannot be used with host networking")
	}
	return nil
}

func validateContainerRuntime(configuration apischema.ContainerConfiguration) error {
	if err := container.ValidateRestartPolicy(container.RestartPolicy{
		Name:              container.RestartPolicyMode(configuration.RestartPolicy.Name),
		MaximumRetryCount: configuration.RestartPolicy.MaximumRetryCount,
	}); err != nil {
		return err
	}
	if strings.ContainsRune(configuration.User, '\x00') || strings.ContainsRune(configuration.WorkingDirectory, '\x00') {
		return errors.New("user and working directory cannot contain null bytes")
	}
	if configuration.WorkingDirectory != "" && !filepath.IsAbs(configuration.WorkingDirectory) {
		return errors.New("working directory must be an absolute container path")
	}
	return nil
}

func containsNetwork(networks []apischema.ContainerNetworkAttachment, name string) bool {
	return slices.ContainsFunc(networks, func(network apischema.ContainerNetworkAttachment) bool {
		return strings.TrimSpace(network.Name) == name
	})
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func ensureContainerImage(ctx context.Context, cli nativeContainerUpdateClient, imageRef string) error {
	imageRef = strings.TrimSpace(imageRef)
	if _, err := cli.ImageInspect(ctx, imageRef); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect image %q: %w", imageRef, err)
	}
	if _, err := pullAndInspectStandaloneImage(ctx, cli, imageRef); err != nil {
		return err
	}
	return nil
}

func removeFailedConfiguredContainer(ctx context.Context, cli nativeContainerUpdateClient, containerID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := cli.ContainerRemove(cleanupCtx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove failed container: %w", err)
	}
	return nil
}
