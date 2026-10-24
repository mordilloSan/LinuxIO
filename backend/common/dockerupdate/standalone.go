package dockerupdate

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func updateStandaloneContainerWithProgress(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	imageRef string,
	result DockerContainerUpdateResult,
	journal *Journal,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	return updateStandaloneContainerWithDependenciesAndPolicyAndProgress(ctx, cli, before, imageRef, result, journal, nil, stoppedContainerUpdatePolicy{}, report)
}

func updateStandaloneContainerWithDependenciesAndPolicy(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	imageRef string,
	result DockerContainerUpdateResult,
	journal *Journal,
	dependencies *standaloneDependencyIndex,
	policy stoppedContainerUpdatePolicy,
) (DockerContainerUpdateResult, error) {
	return updateStandaloneContainerWithDependenciesAndPolicyAndProgress(ctx, cli, before, imageRef, result, journal, dependencies, policy, nil)
}

func updateStandaloneContainerWithDependenciesAndPolicyAndProgress(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	imageRef string,
	result DockerContainerUpdateResult,
	journal *Journal,
	dependencies *standaloneDependencyIndex,
	policy stoppedContainerUpdatePolicy,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	if err := validateStandaloneUpdatePreconditions(ctx, cli, before, journal, dependencies, policy); err != nil {
		return result, err
	}

	reportProgress(report, "pulling", fmt.Sprintf("Pulling image %s", imageRef))
	pulled, err := PullAndInspectImage(ctx, cli, imageRef)
	if err != nil {
		return result, err
	}
	result.NewImageID = pulled.ID
	if pulled.ID == before.Image {
		markContainerCurrent(ctx, before.ID, before)
		reportProgress(report, "current", "The container is already using the current image")
		return result, nil
	}

	createOptions, err := StandaloneCreateOptions(before, imageRef, result.ContainerName)
	if err != nil {
		return result, err
	}
	backupName := standaloneBackupName(before.ID)
	tx := standaloneUpdateTransaction{
		Phase:           standaloneUpdatePrepared,
		OriginalID:      before.ID,
		OriginalName:    result.ContainerName,
		BackupName:      backupName,
		ReplacementName: result.ContainerName,
		OriginalRunning: before.State != nil && before.State.Running,
	}
	if tx.OriginalRunning {
		reportProgress(report, "stopping", fmt.Sprintf("Stopping %s and preparing rollback", result.ContainerName))
	} else {
		reportProgress(report, "stopping", fmt.Sprintf("Preparing stopped container %s for rollback", result.ContainerName))
	}
	if parkErr := parkStandaloneOriginal(ctx, cli, tx, journal); parkErr != nil {
		return result, parkErr
	}
	after, err := createAndVerifyStandaloneReplacementWithProgress(ctx, cli, before.ID, result.ContainerName, createOptions, tx, journal, policy, report)
	if err != nil {
		return result, err
	}

	result.ContainerID = after.ID
	result.NewImageID = after.Image
	result.Updated = true
	markContainerCurrent(ctx, before.ID, after)
	reportProgress(report, "cleanup", "Removing the rollback container")
	if _, err := cli.ContainerRemove(ctx, before.ID, client.ContainerRemoveOptions{}); err != nil {
		return result, fmt.Errorf("remove rollback container %q after successful update: %w", backupName, err)
	}
	if err := clearStandaloneJournal(journal); err != nil {
		return result, err
	}
	return result, nil
}

// ValidateStandaloneEdit checks that before can be replaced in place by an edit:
// no interrupted update is journaled, and the container is a stable standalone
// container with no dependents. Stopped containers are allowed.
func ValidateStandaloneEdit(ctx context.Context, cli ContainerClient, before container.InspectResponse, journal *Journal) error {
	return validateStandaloneUpdatePreconditions(ctx, cli, before, journal, nil, stoppedContainerUpdatePolicy{Allow: true})
}

// ReplaceStandalone parks before under a rollback name, creates and verifies the
// replacement described by options, then removes the rollback container. Any
// failure before that point restores the original. It returns the verified
// replacement.
func ReplaceStandalone(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	options client.ContainerCreateOptions,
	journal *Journal,
) (container.InspectResponse, error) {
	originalName := strings.TrimPrefix(before.Name, "/")
	tx := standaloneUpdateTransaction{
		Phase:           standaloneUpdatePrepared,
		OriginalID:      before.ID,
		OriginalName:    originalName,
		BackupName:      standaloneBackupName(before.ID),
		ReplacementName: options.Name,
		OriginalRunning: before.State != nil && before.State.Running,
	}
	if parkErr := parkStandaloneOriginal(ctx, cli, tx, journal); parkErr != nil {
		return container.InspectResponse{}, parkErr
	}
	after, replacementErr := createAndVerifyStandaloneReplacementWithProgress(
		ctx,
		cli,
		before.ID,
		originalName,
		options,
		tx,
		journal,
		stoppedContainerUpdatePolicy{Allow: true},
		nil,
	)
	if replacementErr != nil {
		return container.InspectResponse{}, replacementErr
	}
	if _, removeErr := cli.ContainerRemove(ctx, before.ID, client.ContainerRemoveOptions{}); removeErr != nil {
		return container.InspectResponse{}, fmt.Errorf("remove rollback container %q after successful edit: %w", tx.BackupName, removeErr)
	}
	if clearErr := clearStandaloneJournal(journal); clearErr != nil {
		return container.InspectResponse{}, clearErr
	}
	return after, nil
}

func validateStandaloneUpdatePreconditions(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	journal *Journal,
	dependencies *standaloneDependencyIndex,
	policy stoppedContainerUpdatePolicy,
) error {
	if journal != nil {
		if _, exists, err := journal.read(); err != nil {
			return err
		} else if exists {
			return errors.New("a previous standalone Docker update requires recovery")
		}
	}
	if policy.Revive && !policy.Allow {
		return errors.New("reviving a stopped container requires stopped-container updates")
	}
	if err := validateStandaloneUpdate(before, policy.Allow); err != nil {
		return err
	}
	if dependencies == nil {
		return validateStandaloneDependents(ctx, cli, before)
	}
	return dependencies.validate(before)
}

// PullAndInspectImage pulls imageRef and returns the inspected result.
func PullAndInspectImage(
	ctx context.Context,
	cli ContainerClient,
	imageRef string,
) (client.ImageInspectResult, error) {
	pull, err := cli.ImagePull(ctx, imageRef, client.ImagePullOptions{})
	if err != nil {
		return client.ImageInspectResult{}, fmt.Errorf("pull image %q: %w", imageRef, err)
	}
	if waitErr := pull.Wait(ctx); waitErr != nil {
		_ = pull.Close()
		return client.ImageInspectResult{}, fmt.Errorf("pull image %q: %w", imageRef, waitErr)
	}
	if closeErr := pull.Close(); closeErr != nil {
		return client.ImageInspectResult{}, fmt.Errorf("close image pull response for %q: %w", imageRef, closeErr)
	}
	pulled, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return client.ImageInspectResult{}, fmt.Errorf("inspect pulled image %q: %w", imageRef, err)
	}
	return pulled, nil
}

func parkStandaloneOriginal(
	ctx context.Context,
	cli ContainerClient,
	tx standaloneUpdateTransaction,
	journal *Journal,
) error {
	if journal != nil {
		if writeErr := journal.write(tx); writeErr != nil {
			return writeErr
		}
	}
	if tx.OriginalRunning {
		if _, stopErr := cli.ContainerStop(ctx, tx.OriginalID, client.ContainerStopOptions{}); stopErr != nil {
			rollbackErr := restoreOriginalRunningState(ctx, cli, tx.OriginalID, tx.OriginalRunning)
			return errors.Join(fmt.Errorf("stop standalone container %q: %w", tx.OriginalName, stopErr), clearJournalAfterRollback(journal, rollbackErr))
		}
	}
	if _, renameErr := cli.ContainerRename(ctx, tx.OriginalID, client.ContainerRenameOptions{NewName: tx.BackupName}); renameErr != nil {
		rollbackErr := restoreOriginalRunningState(ctx, cli, tx.OriginalID, tx.OriginalRunning)
		return errors.Join(fmt.Errorf("rename standalone container %q for rollback: %w", tx.OriginalName, renameErr), clearJournalAfterRollback(journal, rollbackErr))
	}
	return nil
}

func createAndVerifyStandaloneReplacementWithProgress(
	ctx context.Context,
	cli ContainerClient,
	originalID string,
	originalName string,
	createOptions client.ContainerCreateOptions,
	tx standaloneUpdateTransaction,
	journal *Journal,
	policy stoppedContainerUpdatePolicy,
	report progressReporter,
) (container.InspectResponse, error) {
	reportProgress(report, "creating", fmt.Sprintf("Creating the replacement for %s", originalName))
	created, createErr := cli.ContainerCreate(ctx, createOptions)
	if createErr != nil {
		reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after replacement creation failed", originalName))
		rollbackErr := restoreOriginalContainer(ctx, cli, originalID, originalName, tx.OriginalRunning)
		return container.InspectResponse{}, errors.Join(fmt.Errorf("create replacement for standalone container %q: %w", originalName, createErr), clearJournalAfterRollback(journal, rollbackErr))
	}
	tx.Phase = standaloneUpdateCreated
	tx.ReplacementID = created.ID
	if writeErr := writeStandaloneJournal(journal, tx); writeErr != nil {
		reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the update journal failed", originalName))
		return container.InspectResponse{}, errors.Join(writeErr, rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
	}
	shouldStart := tx.OriginalRunning || policy.Revive
	var after container.InspectResponse
	if shouldStart {
		reportProgress(report, "starting", fmt.Sprintf("Starting the replacement for %s", originalName))
		if _, startErr := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
			reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the replacement failed to start", originalName))
			return container.InspectResponse{}, errors.Join(fmt.Errorf("start replacement for standalone container %q: %w", originalName, startErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
		}
		tx.ReplacementStarted = true
		reportProgress(report, "verifying", fmt.Sprintf("Waiting for %s to become ready", originalName))
		var readyErr error
		after, readyErr = WaitForContainerReady(ctx, cli, created.ID)
		if readyErr != nil {
			reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after readiness verification failed", originalName))
			return container.InspectResponse{}, errors.Join(fmt.Errorf("verify replacement for standalone container %q: %w", originalName, readyErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
		}
	} else {
		reportProgress(report, "verifying", fmt.Sprintf("Verifying stopped replacement for %s", originalName))
		inspectResult, inspectErr := cli.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return container.InspectResponse{}, errors.Join(fmt.Errorf("inspect stopped replacement for standalone container %q: %w", originalName, inspectErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, false, journal))
		}
		after = inspectResult.Container
		if after.State == nil || after.State.Running {
			return container.InspectResponse{}, errors.Join(fmt.Errorf("replacement for stopped standalone container %q did not remain stopped", originalName), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, false, journal))
		}
	}
	tx.Phase = standaloneUpdateVerified
	if writeErr := writeStandaloneJournal(journal, tx); writeErr != nil {
		reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the update journal failed", originalName))
		return container.InspectResponse{}, errors.Join(writeErr, rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
	}
	return after, nil
}

func writeStandaloneJournal(journal *Journal, tx standaloneUpdateTransaction) error {
	if journal == nil {
		return nil
	}
	return journal.write(tx)
}

func rollbackAndClearStandalone(
	ctx context.Context,
	cli ContainerClient,
	replacementID string,
	originalID string,
	originalName string,
	originalRunning bool,
	journal *Journal,
) error {
	return clearJournalAfterRollback(journal, rollbackStandaloneContainer(ctx, cli, replacementID, originalID, originalName, originalRunning))
}

func clearJournalAfterRollback(journal *Journal, rollbackErr error) error {
	if rollbackErr != nil {
		return rollbackErr
	}
	return clearStandaloneJournal(journal)
}

func clearStandaloneJournal(journal *Journal) error {
	if journal == nil {
		return nil
	}
	return journal.clear()
}

func validateStandaloneUpdate(inspect container.InspectResponse, allowStopped bool) error {
	name := strings.TrimPrefix(inspect.Name, "/")
	if inspect.Config == nil || inspect.HostConfig == nil {
		return fmt.Errorf("standalone container %q does not expose complete recreation configuration", name)
	}
	if err := validateContainerUpdateState(inspect, allowStopped); err != nil {
		return err
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

func validateStandaloneDependents(ctx context.Context, cli ContainerClient, inspect container.InspectResponse) error {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers before standalone update: %w", err)
	}
	potentialDependents := make([]container.Summary, 0, len(containers.Items))
	for _, summary := range containers.Items {
		if summary.ID != inspect.ID {
			potentialDependents = append(potentialDependents, summary)
		}
	}
	dependencies, err := buildStandaloneDependencyIndex(ctx, cli, potentialDependents)
	if err != nil {
		return err
	}
	return dependencies.validate(inspect)
}

type standaloneDependent struct {
	id         string
	name       string
	hostConfig *container.HostConfig
}

type standaloneDependencyIndex struct {
	dependents []standaloneDependent
}

func buildStandaloneDependencyIndex(
	ctx context.Context,
	cli ContainerClient,
	containers []container.Summary,
) (*standaloneDependencyIndex, error) {
	index := &standaloneDependencyIndex{dependents: make([]standaloneDependent, 0, len(containers))}
	for _, summary := range containers {
		dependentResult, inspectErr := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect potential dependent container %q: %w", PrimaryContainerName(summary), inspectErr)
		}
		dependent := dependentResult.Container
		name := strings.TrimPrefix(dependent.Name, "/")
		if name == "" {
			name = PrimaryContainerName(summary)
		}
		index.dependents = append(index.dependents, standaloneDependent{
			id:         dependent.ID,
			name:       name,
			hostConfig: dependent.HostConfig,
		})
	}
	return index, nil
}

func (i *standaloneDependencyIndex) validate(inspect container.InspectResponse) error {
	name := strings.TrimPrefix(inspect.Name, "/")
	for _, dependent := range i.dependents {
		if dependent.id == inspect.ID || dependent.hostConfig == nil {
			continue
		}
		if dependency := standaloneNamespaceDependency(dependent.hostConfig, inspect.ID, name); dependency != "" {
			return fmt.Errorf("standalone container %q provides the %s used by container %q", name, dependency, dependent.name)
		}
		for _, volumeFrom := range dependent.hostConfig.VolumesFrom {
			target, _, _ := strings.Cut(volumeFrom, ":")
			if matchesContainerReference(target, inspect.ID, name) {
				return fmt.Errorf("standalone container %q provides volumes used by container %q", name, dependent.name)
			}
		}
	}
	return nil
}

func standaloneNamespaceDependency(hostConfig *container.HostConfig, containerID, name string) string {
	if hostConfig.NetworkMode.IsContainer() && matchesContainerReference(hostConfig.NetworkMode.ConnectedContainer(), containerID, name) {
		return "network namespace"
	}
	if hostConfig.IpcMode.IsContainer() && matchesContainerReference(hostConfig.IpcMode.Container(), containerID, name) {
		return "IPC namespace"
	}
	if hostConfig.PidMode.IsContainer() && matchesContainerReference(hostConfig.PidMode.Container(), containerID, name) {
		return "PID namespace"
	}
	if hostConfig.Cgroup.IsContainer() && matchesContainerReference(hostConfig.Cgroup.Container(), containerID, name) {
		return "cgroup namespace"
	}
	return ""
}

func matchesContainerReference(value, containerID, name string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "/")
	return value == name || value == containerID || (len(value) >= 12 && strings.HasPrefix(containerID, value))
}

// StandaloneCreateOptions copies the inspected container's recreation
// configuration into create options for a replacement running imageRef as name.
func StandaloneCreateOptions(
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
	// Docker materializes an unset hostname as the container's short ID in
	// Inspect. Clear that recognizable default so the replacement gets its own.
	if len(inspect.ID) >= 12 &&
		!hostConfigCopy.NetworkMode.IsHost() &&
		configCopy.Hostname == inspect.ID[:12] {
		configCopy.Hostname = ""
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
				DriverOpts: cloneStringMap(endpoint.DriverOpts),
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

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	maps.Copy(clone, values)
	return clone
}

// durationAsNanos keeps time.Duration (container Healthcheck intervals)
// round-trippable: JSON v2 has no default representation for it.
var durationAsNanos = json.JoinOptions(
	json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
		return json.MarshalEncode(enc, int64(d))
	})),
	json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, d *time.Duration) error {
		var nanos int64
		if err := json.UnmarshalDecode(dec, &nanos); err != nil {
			return err
		}
		*d = time.Duration(nanos)
		return nil
	})),
)

func cloneJSON[T any](value *T) (*T, error) {
	data, err := json.Marshal(value, durationAsNanos)
	if err != nil {
		return nil, err
	}
	var clone T
	if err := json.Unmarshal(data, &clone, durationAsNanos); err != nil {
		return nil, err
	}
	return &clone, nil
}

func standaloneBackupName(containerID string) string {
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}
	return "linuxio-update-backup-" + containerID
}

// WaitForContainerReady polls the container until it is running and, when it
// has a health check, healthy.
func WaitForContainerReady(
	ctx context.Context,
	cli containerReadinessClient,
	containerID string,
) (container.InspectResponse, error) {
	return waitForContainerReadyWithTiming(ctx, cli, containerID, containerReadyTimeout, containerReadyPoll)
}

type containerReadinessClient interface {
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

func waitForContainerReadyWithTiming(
	ctx context.Context,
	cli containerReadinessClient,
	containerID string,
	timeout time.Duration,
	poll time.Duration,
) (container.InspectResponse, error) {
	if poll <= 0 {
		poll = time.Nanosecond
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		inspectResult, err := cli.ContainerInspect(readyCtx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return container.InspectResponse{}, err
		}
		ready, terminal, err := containerReady(inspectResult.Container)
		switch {
		case ready:
			return inspectResult.Container, nil
		case terminal:
			if err == nil {
				err = errors.New("container is not ready")
			}
			return container.InspectResponse{}, err
		case err != nil:
			return container.InspectResponse{}, err
		}

		select {
		case <-readyCtx.Done():
			return container.InspectResponse{}, readyCtx.Err()
		case <-ticker.C:
		}
	}
}

func containerReady(inspect container.InspectResponse) (ready bool, terminal bool, err error) {
	if inspect.State == nil {
		return false, true, errors.New("container state is unavailable")
	}
	if inspect.State.Dead {
		return false, true, fmt.Errorf("container state is %q", inspect.State.Status)
	}
	if inspect.State.Paused {
		return false, true, errors.New("container is paused")
	}
	if inspect.State.Restarting {
		return false, false, nil
	}
	status := strings.ToLower(string(inspect.State.Status))
	switch status {
	case "exited", "removing", "dead":
		return false, true, fmt.Errorf("container state is %q", inspect.State.Status)
	case "created", "restarting":
		return false, false, nil
	}
	if !inspect.State.Running {
		return false, false, nil
	}
	if inspect.State.Health == nil {
		return true, false, nil
	}
	switch strings.ToLower(string(inspect.State.Health.Status)) {
	case "healthy":
		return true, false, nil
	case "unhealthy":
		return false, true, fmt.Errorf("container health is %q", inspect.State.Health.Status)
	default:
		return false, false, nil
	}
}

func rollbackStandaloneContainer(
	ctx context.Context,
	cli ContainerClient,
	replacementID string,
	originalID string,
	originalName string,
	originalRunning bool,
) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), standaloneRollbackTimeout)
	defer cancel()
	_, removeErr := cli.ContainerRemove(recoveryCtx, replacementID, client.ContainerRemoveOptions{Force: true})
	restoreErr := restoreOriginalContainer(recoveryCtx, cli, originalID, originalName, originalRunning)
	return errors.Join(wrapRollbackError("remove failed replacement", removeErr), restoreErr)
}

func restoreOriginalContainer(ctx context.Context, cli ContainerClient, originalID, originalName string, originalRunning bool) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), standaloneRollbackTimeout)
	defer cancel()
	if _, err := cli.ContainerRename(recoveryCtx, originalID, client.ContainerRenameOptions{NewName: originalName}); err != nil {
		return fmt.Errorf("rollback rename original container: %w", err)
	}
	return restoreOriginalRunningState(recoveryCtx, cli, originalID, originalRunning)
}

func restoreOriginalRunningState(ctx context.Context, cli ContainerClient, originalID string, originalRunning bool) error {
	if !originalRunning {
		return nil
	}
	return startOriginalContainer(ctx, cli, originalID)
}

func startOriginalContainer(ctx context.Context, cli ContainerClient, originalID string) error {
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
	return fmt.Errorf("rollback %s: %w", operation, err)
}
