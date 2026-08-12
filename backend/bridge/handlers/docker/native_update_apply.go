package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

const (
	dockerUpdateLockPath      = "/run/linuxio-docker-update.lock"
	dockerUpdateLockWait      = 10 * time.Second
	dockerUpdateLockPoll      = 250 * time.Millisecond
	containerReadyTimeout     = 90 * time.Second
	containerReadyPoll        = 500 * time.Millisecond
	standaloneRollbackTimeout = 30 * time.Second
)

type nativeContainerUpdateClient interface {
	imageUpdateCheckClient
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ImagePull(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error)
	ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRename(context.Context, string, client.ContainerRenameOptions) (client.ContainerRenameResult, error)
	ContainerCreate(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
}

func acquireDockerUpdateLock(ctx context.Context) (func(), error) {
	release, err := filelock.AcquireExclusive(
		ctx,
		dockerUpdateLockPath,
		filelock.WithTimeout(dockerUpdateLockWait),
		filelock.WithRetryDelay(dockerUpdateLockPoll),
	)
	if errors.Is(err, filelock.ErrTimeout) {
		return nil, errors.New("another Docker update is already in progress")
	}
	if err != nil {
		return nil, fmt.Errorf("acquire Docker update lock: %w", err)
	}
	return func() { _ = release() }, nil
}

func updateInspectedContainer(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
) (apischema.DockerContainerUpdateResult, error) {
	result, imageRef, err := newContainerUpdateResult(inspect)
	if err != nil {
		return result, err
	}

	normalizedRef, _, immutable, err := normalizeUpdateReference(imageRef)
	if err != nil {
		return result, err
	}
	if immutable {
		result.NewImageID = inspect.Image
		markContainerCurrent(ctx, inspect.ID, inspect)
		return result, nil
	}

	observation, err := inspectImageUpdate(ctx, cli, inspect.Image, normalizedRef)
	if err != nil {
		return result, err
	}
	if observation.err != nil {
		return result, observation.err
	}
	if !observation.updateAvailable {
		result.NewImageID = inspect.Image
		markContainerCurrent(ctx, inspect.ID, inspect)
		return result, nil
	}

	target, service, managedByCompose, err := composeTargetForContainer(ctx, cli, inspect)
	if err != nil {
		return result, err
	}
	if managedByCompose {
		return updateComposeContainer(ctx, cli, inspect, target, service, result)
	}
	return updateStandaloneContainer(ctx, cli, inspect, normalizedRef, result)
}

func newContainerUpdateResult(inspect container.InspectResponse) (apischema.DockerContainerUpdateResult, string, error) {
	name := strings.TrimPrefix(inspect.Name, "/")
	result := apischema.DockerContainerUpdateResult{
		ContainerID:     inspect.ID,
		ContainerName:   name,
		PreviousImageID: inspect.Image,
	}
	if inspect.Config != nil {
		result.Image = inspect.Config.Image
	}
	if name == "" {
		return result, "", fmt.Errorf("container %s has no name", inspect.ID)
	}
	if inspect.Config == nil {
		return result, "", fmt.Errorf("container %q has no configuration", name)
	}
	return result, inspect.Config.Image, nil
}

func composeTargetForContainer(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
) (composeProjectTarget, string, bool, error) {
	if inspect.Config == nil {
		return composeProjectTarget{}, "", false, nil
	}
	labels := inspect.Config.Labels
	projectName := strings.TrimSpace(labels["com.docker.compose.project"])
	if projectName == "" {
		return composeProjectTarget{}, "", false, nil
	}
	service := strings.TrimSpace(labels["com.docker.compose.service"])
	if service == "" {
		return composeProjectTarget{}, "", true, fmt.Errorf("Compose-managed container %q has no service label", strings.TrimPrefix(inspect.Name, "/"))
	}

	workingDir := strings.TrimSpace(labels["com.docker.compose.project.working_dir"])
	configFiles, err := resolveComposeUpdateConfigFiles(ctx, cli, projectName, workingDir, labels["com.docker.compose.project.config_files"])
	if err != nil {
		return composeProjectTarget{}, "", true, err
	}
	workingDir, err = resolveComposeUpdateWorkingDir(ctx, cli, workingDir, configFiles[0])
	if err != nil {
		return composeProjectTarget{}, "", true, err
	}

	return composeProjectTarget{
		Name:        projectName,
		ConfigFiles: configFiles,
		WorkingDir:  workingDir,
	}, service, true, nil
}

func resolveComposeUpdateConfigFiles(
	ctx context.Context,
	cli *client.Client,
	projectName string,
	workingDir string,
	configFilesLabel string,
) ([]string, error) {
	rawConfigFiles := parseConfigFiles(configFilesLabel)
	for i, configFile := range rawConfigFiles {
		if !filepath.IsAbs(configFile) && workingDir != "" {
			rawConfigFiles[i] = filepath.Join(workingDir, configFile)
		}
	}
	configFiles := translateComposeConfigFiles(ctx, cli, rawConfigFiles)
	if len(configFiles) == 0 && workingDir != "" {
		configFiles = inferComposeFilesFromWorkingDir(ctx, cli, workingDir)
	}
	if len(configFiles) == 0 {
		return nil, fmt.Errorf("Compose project %q has no accessible config files", projectName)
	}
	if len(rawConfigFiles) > 0 && len(configFiles) != len(rawConfigFiles) {
		return nil, fmt.Errorf("not all config files for Compose project %q are accessible", projectName)
	}
	for _, configFile := range configFiles {
		info, err := os.Stat(configFile)
		if err != nil {
			return nil, fmt.Errorf("stat Compose config %q: %w", configFile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Compose config %q is not a regular file", configFile)
		}
	}
	return configFiles, nil
}

func resolveComposeUpdateWorkingDir(
	ctx context.Context,
	cli *client.Client,
	workingDir string,
	configFile string,
) (string, error) {
	if workingDir != "" {
		if info, err := os.Stat(workingDir); err != nil || !info.IsDir() {
			workingDir = translateContainerPathToHost(ctx, cli, workingDir)
		}
	}
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	info, err := os.Stat(workingDir)
	if err != nil {
		return "", fmt.Errorf("stat Compose working directory %q: %w", workingDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Compose working directory %q is not a directory", workingDir)
	}
	return workingDir, nil
}

func updateComposeContainer(
	ctx context.Context,
	cli *client.Client,
	before container.InspectResponse,
	target composeProjectTarget,
	service string,
	result apischema.DockerContainerUpdateResult,
) (apischema.DockerContainerUpdateResult, error) {
	collector := &composeMessageCollector{}
	if err := composePullAndUp(ctx, target, service, collector.Emit); err != nil {
		if output := collector.String(); output != "" {
			return result, fmt.Errorf("update Compose project %q service %q: %w: %s", target.Name, service, err, output)
		}
		return result, fmt.Errorf("update Compose project %q service %q: %w", target.Name, service, err)
	}

	afterResult, err := cli.ContainerInspect(ctx, result.ContainerName, client.ContainerInspectOptions{})
	if err != nil {
		return result, fmt.Errorf("inspect updated Compose container %q: %w", result.ContainerName, err)
	}
	after, err := waitForContainerReady(ctx, cli, afterResult.Container.ID)
	if err != nil {
		return result, fmt.Errorf("verify updated Compose container %q: %w", result.ContainerName, err)
	}
	result.ContainerID = after.ID
	result.NewImageID = after.Image
	result.Updated = after.Image != before.Image
	if !result.Updated {
		return result, fmt.Errorf("Compose service %q completed without activating the pulled image", service)
	}
	markContainerCurrent(ctx, before.ID, after)
	return result, nil
}

func updateStandaloneContainer(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	imageRef string,
	result apischema.DockerContainerUpdateResult,
) (apischema.DockerContainerUpdateResult, error) {
	if err := validateStandaloneUpdate(before); err != nil {
		return result, err
	}
	if err := validateStandaloneDependents(ctx, cli, before); err != nil {
		return result, err
	}

	pull, err := cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return result, fmt.Errorf("pull image %q: %w", imageRef, err)
	}
	if waitErr := pull.Wait(ctx); waitErr != nil {
		_ = pull.Close()
		return result, fmt.Errorf("pull image %q: %w", imageRef, waitErr)
	}
	if closeErr := pull.Close(); closeErr != nil {
		return result, fmt.Errorf("close image pull response for %q: %w", imageRef, closeErr)
	}

	pulled, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return result, fmt.Errorf("inspect pulled image %q: %w", imageRef, err)
	}
	result.NewImageID = pulled.ID
	if pulled.ID == before.Image {
		markContainerCurrent(ctx, before.ID, before)
		return result, nil
	}

	createOptions, err := standaloneCreateOptions(before, imageRef, result.ContainerName)
	if err != nil {
		return result, err
	}
	backupName := standaloneBackupName(before.ID)
	if _, stopErr := cli.ContainerStop(ctx, before.ID, client.ContainerStopOptions{}); stopErr != nil {
		return result, fmt.Errorf("stop standalone container %q: %w", result.ContainerName, stopErr)
	}
	if _, renameErr := cli.ContainerRename(ctx, before.ID, client.ContainerRenameOptions{NewName: backupName}); renameErr != nil {
		rollbackErr := startOriginalContainer(ctx, cli, before.ID)
		return result, errors.Join(fmt.Errorf("rename standalone container %q for rollback: %w", result.ContainerName, renameErr), rollbackErr)
	}

	created, err := cli.ContainerCreate(ctx, createOptions)
	if err != nil {
		rollbackErr := restoreOriginalContainer(ctx, cli, before.ID, result.ContainerName)
		return result, errors.Join(fmt.Errorf("create replacement for standalone container %q: %w", result.ContainerName, err), rollbackErr)
	}
	if _, startErr := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
		rollbackErr := rollbackStandaloneContainer(ctx, cli, created.ID, before.ID, result.ContainerName)
		return result, errors.Join(fmt.Errorf("start replacement for standalone container %q: %w", result.ContainerName, startErr), rollbackErr)
	}
	after, err := waitForContainerReady(ctx, cli, created.ID)
	if err != nil {
		rollbackErr := rollbackStandaloneContainer(ctx, cli, created.ID, before.ID, result.ContainerName)
		return result, errors.Join(fmt.Errorf("verify replacement for standalone container %q: %w", result.ContainerName, err), rollbackErr)
	}

	result.ContainerID = after.ID
	result.NewImageID = after.Image
	result.Updated = true
	markContainerCurrent(ctx, before.ID, after)
	if _, err := cli.ContainerRemove(ctx, before.ID, client.ContainerRemoveOptions{}); err != nil {
		return result, fmt.Errorf("remove rollback container %q after successful update: %w", backupName, err)
	}
	return result, nil
}

func validateStandaloneUpdate(inspect container.InspectResponse) error {
	name := strings.TrimPrefix(inspect.Name, "/")
	if inspect.Config == nil || inspect.HostConfig == nil {
		return fmt.Errorf("standalone container %q does not expose complete recreation configuration", name)
	}
	if inspect.State == nil || !inspect.State.Running || inspect.State.Paused || inspect.State.Restarting {
		return fmt.Errorf("standalone container %q must be running and stable before it can be updated", name)
	}
	if inspect.HostConfig.AutoRemove {
		return fmt.Errorf("standalone container %q uses auto-remove and cannot be updated with rollback", name)
	}
	if inspect.HostConfig.ContainerIDFile != "" {
		return fmt.Errorf("standalone container %q uses a container ID file and cannot be recreated safely", name)
	}
	for label := range inspect.Config.Labels {
		if strings.HasPrefix(label, "com.docker.swarm.") {
			return fmt.Errorf("container %q is managed by Docker Swarm and must be updated through its service", name)
		}
	}
	if inspect.NetworkSettings != nil {
		for networkName, endpoint := range inspect.NetworkSettings.Networks {
			if endpoint == nil || endpoint.IPAMConfig == nil {
				continue
			}
			if endpoint.IPAMConfig.IPv4Address.IsValid() || endpoint.IPAMConfig.IPv6Address.IsValid() || len(endpoint.IPAMConfig.LinkLocalIPs) > 0 {
				return fmt.Errorf("standalone container %q uses static addressing on network %q and cannot be recreated with rollback", name, networkName)
			}
		}
	}
	return nil
}

func validateStandaloneDependents(ctx context.Context, cli nativeContainerUpdateClient, inspect container.InspectResponse) error {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers before standalone update: %w", err)
	}
	name := strings.TrimPrefix(inspect.Name, "/")
	for _, summary := range containers.Items {
		if summary.ID == inspect.ID {
			continue
		}
		dependentResult, inspectErr := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return fmt.Errorf("inspect potential dependent container %q: %w", primaryContainerName(summary), inspectErr)
		}
		dependent := dependentResult.Container
		if dependent.HostConfig == nil {
			continue
		}
		if target, ok := strings.CutPrefix(string(dependent.HostConfig.NetworkMode), "container:"); ok && matchesContainerReference(target, inspect.ID, name) {
			return fmt.Errorf("standalone container %q provides the network namespace used by container %q", name, strings.TrimPrefix(dependent.Name, "/"))
		}
		for _, volumeFrom := range dependent.HostConfig.VolumesFrom {
			target, _, _ := strings.Cut(volumeFrom, ":")
			if matchesContainerReference(target, inspect.ID, name) {
				return fmt.Errorf("standalone container %q provides volumes used by container %q", name, strings.TrimPrefix(dependent.Name, "/"))
			}
		}
	}
	return nil
}

func matchesContainerReference(value, containerID, name string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "/")
	return value == name || value == containerID || (len(value) >= 12 && strings.HasPrefix(containerID, value))
}

func standaloneCreateOptions(
	inspect container.InspectResponse,
	imageRef string,
	name string,
) (client.ContainerCreateOptions, error) {
	configCopy, err := cloneJSON(inspect.Config)
	if err != nil {
		return client.ContainerCreateOptions{}, fmt.Errorf("copy standalone container configuration: %w", err)
	}
	hostConfigCopy, err := cloneJSON(inspect.HostConfig)
	if err != nil {
		return client.ContainerCreateOptions{}, fmt.Errorf("copy standalone host configuration: %w", err)
	}
	configCopy.Image = imageRef
	if err := preserveInspectedMounts(hostConfigCopy, inspect.Mounts); err != nil {
		return client.ContainerCreateOptions{}, err
	}

	endpoints := make(map[string]*network.EndpointSettings)
	if inspect.NetworkSettings != nil {
		for networkName, endpoint := range inspect.NetworkSettings.Networks {
			if endpoint == nil {
				continue
			}
			endpoints[networkName] = &network.EndpointSettings{
				IPAMConfig: endpoint.IPAMConfig.Copy(),
				Links:      append([]string(nil), endpoint.Links...),
				Aliases:    append([]string(nil), endpoint.Aliases...),
				DriverOpts: cloneEndpointStringMap(endpoint.DriverOpts),
				GwPriority: endpoint.GwPriority,
			}
		}
	}

	return client.ContainerCreateOptions{
		Config:           configCopy,
		HostConfig:       hostConfigCopy,
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: endpoints},
		Name:             name,
	}, nil
}

func preserveInspectedMounts(hostConfig *container.HostConfig, mounts []container.MountPoint) error {
	for _, point := range mounts {
		if mountTargetConfigured(hostConfig, point.Destination) {
			continue
		}
		switch point.Type {
		case mount.TypeVolume:
			if point.Name == "" {
				return fmt.Errorf("cannot preserve anonymous volume mounted at %q", point.Destination)
			}
			hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   point.Name,
				Target:   point.Destination,
				ReadOnly: !point.RW,
			})
		case mount.TypeBind:
			if point.Source == "" {
				return fmt.Errorf("cannot preserve bind mount at %q without a source", point.Destination)
			}
			hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
				Type:        mount.TypeBind,
				Source:      point.Source,
				Target:      point.Destination,
				ReadOnly:    !point.RW,
				BindOptions: &mount.BindOptions{Propagation: point.Propagation},
			})
		default:
			return fmt.Errorf("cannot preserve %s mount at %q during standalone update", point.Type, point.Destination)
		}
	}
	return nil
}

func mountTargetConfigured(hostConfig *container.HostConfig, target string) bool {
	if _, ok := hostConfig.Tmpfs[target]; ok {
		return true
	}
	for _, configured := range hostConfig.Mounts {
		if configured.Target == target {
			return true
		}
	}
	for _, bind := range hostConfig.Binds {
		if bind == target {
			return true
		}
		parts := strings.Split(bind, ":")
		if slices.Contains(parts[1:], target) {
			return true
		}
	}
	return false
}

func cloneJSON[T any](value *T) (*T, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var clone T
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func cloneEndpointStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	maps.Copy(clone, values)
	return clone
}

func standaloneBackupName(containerID string) string {
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}
	return "linuxio-update-backup-" + containerID
}

func waitForContainerReady(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	containerID string,
) (container.InspectResponse, error) {
	readyCtx, cancel := context.WithTimeout(ctx, containerReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(containerReadyPoll)
	defer ticker.Stop()
	for {
		inspectResult, err := cli.ContainerInspect(readyCtx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return container.InspectResponse{}, err
		}
		if err := containerReady(inspectResult.Container); err == nil {
			return inspectResult.Container, nil
		} else if inspectResult.Container.State == nil || !inspectResult.Container.State.Running || inspectResult.Container.State.Dead || inspectResult.Container.State.Health == nil || inspectResult.Container.State.Health.Status == "unhealthy" {
			return container.InspectResponse{}, err
		}

		select {
		case <-readyCtx.Done():
			return container.InspectResponse{}, readyCtx.Err()
		case <-ticker.C:
		}
	}
}

func containerReady(inspect container.InspectResponse) error {
	if inspect.State == nil {
		return errors.New("container state is unavailable")
	}
	if !inspect.State.Running || inspect.State.Dead {
		return fmt.Errorf("container state is %q", inspect.State.Status)
	}
	if inspect.State.Health != nil && inspect.State.Health.Status != "healthy" {
		return fmt.Errorf("container health is %q", inspect.State.Health.Status)
	}
	return nil
}

func rollbackStandaloneContainer(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	replacementID string,
	originalID string,
	originalName string,
) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), standaloneRollbackTimeout)
	defer cancel()
	_, removeErr := cli.ContainerRemove(recoveryCtx, replacementID, client.ContainerRemoveOptions{Force: true})
	restoreErr := restoreOriginalContainer(recoveryCtx, cli, originalID, originalName)
	return errors.Join(wrapRollbackError("remove failed replacement", removeErr), restoreErr)
}

func restoreOriginalContainer(ctx context.Context, cli nativeContainerUpdateClient, originalID, originalName string) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), standaloneRollbackTimeout)
	defer cancel()
	if _, err := cli.ContainerRename(recoveryCtx, originalID, client.ContainerRenameOptions{NewName: originalName}); err != nil {
		return fmt.Errorf("rollback rename original container: %w", err)
	}
	return startOriginalContainer(recoveryCtx, cli, originalID)
}

func startOriginalContainer(ctx context.Context, cli nativeContainerUpdateClient, originalID string) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), standaloneRollbackTimeout)
	defer cancel()
	if _, err := cli.ContainerStart(recoveryCtx, originalID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("rollback start original container: %w", err)
	}
	return nil
}

func wrapRollbackError(operation string, err error) error {
	if err == nil {
		return nil
	}
	slog.Error("standalone Docker update rollback step failed", "operation", operation, "error", err)
	return fmt.Errorf("rollback %s: %w", operation, err)
}
