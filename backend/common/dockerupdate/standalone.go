package dockerupdate

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// updateStandaloneContainer pulls imageRef and, when it is newer than the
// image before runs, replaces before with a container from it, restoring the
// original if the replacement fails. A nil dependencies checks the
// container's dependents live through cli (see
// validateStandaloneUpdatePreconditions); a nil report discards progress.
func updateStandaloneContainer(
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
	if dropErr := DropImageDefaults(ctx, cli, &createOptions, before.Image); dropErr != nil {
		return result, fmt.Errorf("prepare the replacement of standalone container %q: %w", result.ContainerName, dropErr)
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
	after, err := createAndVerifyStandaloneReplacement(ctx, cli, before.ID, result.ContainerName, createOptions, tx, journal, policy, report)
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
	after, replacementErr := createAndVerifyStandaloneReplacement(
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

func createAndVerifyStandaloneReplacement(
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
		return container.InspectResponse{}, undoneStandalone(fmt.Errorf("create replacement for standalone container %q: %w", originalName, createErr), clearJournalAfterRollback(journal, rollbackErr))
	}
	tx.Phase = standaloneUpdateCreated
	tx.ReplacementID = created.ID
	if writeErr := writeStandaloneJournal(journal, tx); writeErr != nil {
		reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the update journal failed", originalName))
		return container.InspectResponse{}, undoneStandalone(writeErr, rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
	}
	shouldStart := tx.OriginalRunning || policy.Revive
	var after container.InspectResponse
	if shouldStart {
		reportProgress(report, "starting", fmt.Sprintf("Starting the replacement for %s", originalName))
		if _, startErr := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
			reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the replacement failed to start", originalName))
			return container.InspectResponse{}, undoneStandalone(fmt.Errorf("start replacement for standalone container %q: %w", originalName, startErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
		}
		tx.ReplacementStarted = true
		reportProgress(report, "verifying", fmt.Sprintf("Waiting for %s to become ready", originalName))
		wait := WaitForContainerReady
		if tx.OriginalRunning {
			wait = waitForReplacementReady
		}
		var readyErr error
		after, readyErr = wait(ctx, cli, created.ID)
		if readyErr != nil {
			reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after readiness verification failed", originalName))
			return container.InspectResponse{}, undoneStandalone(fmt.Errorf("verify replacement for standalone container %q: %w", originalName, readyErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
		}
	} else {
		reportProgress(report, "verifying", fmt.Sprintf("Verifying stopped replacement for %s", originalName))
		inspectResult, inspectErr := cli.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return container.InspectResponse{}, undoneStandalone(fmt.Errorf("inspect stopped replacement for standalone container %q: %w", originalName, inspectErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, false, journal))
		}
		after = inspectResult.Container
		if after.State == nil || after.State.Running {
			return container.InspectResponse{}, undoneStandalone(fmt.Errorf("replacement for stopped standalone container %q did not remain stopped", originalName), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, false, journal))
		}
	}
	tx.Phase = standaloneUpdateVerified
	if writeErr := writeStandaloneJournal(journal, tx); writeErr != nil {
		reportProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the update journal failed", originalName))
		return container.InspectResponse{}, undoneStandalone(writeErr, rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
	}
	return after, nil
}

// undoneStandalone is the error of a standalone replacement that failed with
// cause once the original was put back (rollbackErr nil): the update was
// rolled back, which the pass report, its summary and the alert say. A
// rollback that failed is reported beside cause.
func undoneStandalone(cause, rollbackErr error) error {
	if rollbackErr != nil {
		return errors.Join(cause, rollbackErr)
	}
	return rolledBackError{err: cause, to: "the original container"}
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

// DropImageDefaults removes from options, a copy of an inspected container's
// configuration (StandaloneCreateOptions), each value the image imageID set
// in it, so a replacement created from another image gets that image's: an
// inspected configuration is the image's merged with what was set for the
// container. A value equal to the image's goes (a label or an env entry, a
// command, entrypoint or health check as a whole, the working folder, user
// and stop signal, an exposed port or a volume the image declares); one set
// differently stays, and so does an exposed port the container publishes. A
// value set for the container equal to the image's cannot be told apart and
// goes too. An image that is gone leaves options as they are.
func DropImageDefaults(ctx context.Context, cli imageInspector, options *client.ContainerCreateOptions, imageID string) error {
	inspected, err := cli.ImageInspect(ctx, imageID)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect image %s: %w", shortImageID(imageID), err)
	}
	image, config := inspected.Config, options.Config
	if image == nil || config == nil {
		return nil
	}
	for key, value := range image.Labels {
		if current, ok := config.Labels[key]; ok && current == value {
			delete(config.Labels, key)
		}
	}
	config.Env = slices.DeleteFunc(config.Env, func(entry string) bool { return slices.Contains(image.Env, entry) })
	config.Entrypoint, config.Cmd = dropImageCommand(config.Entrypoint, config.Cmd, image.Entrypoint, image.Cmd)
	config.Healthcheck = dropImageHealthcheck(config.Healthcheck, image.Healthcheck)
	if config.WorkingDir == image.WorkingDir {
		config.WorkingDir = ""
	}
	if config.User == image.User {
		config.User = ""
	}
	if config.StopSignal == image.StopSignal {
		config.StopSignal = ""
	}
	for path := range image.Volumes {
		delete(config.Volumes, path)
	}
	var published network.PortMap
	if options.HostConfig != nil {
		published = options.HostConfig.PortBindings
	}
	dropImagePorts(config.ExposedPorts, image.ExposedPorts, published)
	return nil
}

// dropImageCommand drops the image's entrypoint and command from the
// container's, following Docker's rule that the image's Cmd is filled in
// only when the container's Entrypoint is empty. An entrypoint set for the
// container keeps both. With the image's entrypoint, the image's command
// goes with it; a command of the container's own stays, and the entrypoint
// goes only when that command is not empty, since an emptied command with no
// entrypoint would be refilled from the image.
func dropImageCommand(entrypoint, cmd, imageEntrypoint, imageCmd []string) ([]string, []string) {
	switch {
	case !slices.Equal(entrypoint, imageEntrypoint):
		return entrypoint, cmd
	case slices.Equal(cmd, imageCmd):
		return nil, nil
	case len(cmd) > 0:
		return nil, cmd
	default:
		return entrypoint, cmd
	}
}

// dropImageHealthcheck drops from the container's health check each field
// equal to the image's, as the daemon merges the image's into it field by
// field; nil when nothing of the container's own is left. A NONE test the
// container set stays.
func dropImageHealthcheck(check, image *container.HealthConfig) *container.HealthConfig {
	if check == nil || image == nil {
		return check
	}
	own := *check
	if slices.Equal(own.Test, image.Test) && !slices.Equal(own.Test, []string{"NONE"}) {
		own.Test = nil
	}
	if own.Interval == image.Interval {
		own.Interval = 0
	}
	if own.Timeout == image.Timeout {
		own.Timeout = 0
	}
	if own.StartPeriod == image.StartPeriod {
		own.StartPeriod = 0
	}
	if own.StartInterval == image.StartInterval {
		own.StartInterval = 0
	}
	if own.Retries == image.Retries {
		own.Retries = 0
	}
	if reflect.ValueOf(own).IsZero() {
		return nil
	}
	return &own
}

// dropImagePorts removes from exposed the ports the image declares, except
// those the container publishes.
func dropImagePorts(exposed network.PortSet, declared map[string]struct{}, published network.PortMap) {
	for name := range declared {
		port, err := network.ParsePort(name)
		if err != nil {
			continue
		}
		if _, ok := published[port]; !ok {
			delete(exposed, port)
		}
	}
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
// has a health check, healthy. It gives up after containerReadyTimeout, naming
// the last state it read, and sooner when Docker's restart policy has
// restarted the container containerRestartLimit times since the first read.
// A failure of the container itself is a containerFailedError.
func WaitForContainerReady(
	ctx context.Context,
	cli containerReadinessClient,
	containerID string,
) (container.InspectResponse, error) {
	return waitForContainerReadyWithTiming(ctx, cli, containerID, containerReadyTimeout, containerReadyPoll, 0)
}

// waitForReplacementReady is WaitForContainerReady for a container that
// replaces a running one, where a container without a health check must also
// have run containerSettleTime since its last start. Elsewhere, such as a
// stopped container a pass revives, it may be one-shot work that exits within
// seconds.
func waitForReplacementReady(
	ctx context.Context,
	cli containerReadinessClient,
	containerID string,
) (container.InspectResponse, error) {
	return waitForContainerReadyWithTiming(ctx, cli, containerID, containerReadyTimeout, containerReadyPoll, containerSettleTime)
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
	settle time.Duration,
) (container.InspectResponse, error) {
	if poll <= 0 {
		poll = time.Nanosecond
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	lastState := "not read"
	// A Compose container that was not recreated keeps its earlier count, so
	// restarts are counted from the first read.
	firstRestarts := -1
	for {
		inspectResult, err := cli.ContainerInspect(readyCtx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			// Only the wait's own end is reported as one; a daemon that
			// failed at the same moment keeps its error.
			if ended := readyCtx.Err(); ended != nil && errors.Is(err, ended) {
				return container.InspectResponse{}, readinessWaitEnded(ctx, timeout, lastState)
			}
			return container.InspectResponse{}, err
		}
		inspect := inspectResult.Container
		lastState = describeContainerState(inspect)
		if firstRestarts < 0 {
			firstRestarts = inspect.RestartCount
		}
		// Checked before readiness: a container without a health check is
		// running, and so would pass, between two crashes.
		if restarts := inspect.RestartCount - firstRestarts; restarts >= containerRestartLimit {
			return container.InspectResponse{}, containerFailedError{fmt.Errorf("container keeps restarting: %d restarts while waiting, last state %s", restarts, lastState)}
		}
		ready, terminal, err := containerReady(inspect, time.Now(), settle)
		switch {
		case ready:
			return inspect, nil
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
			return container.InspectResponse{}, readinessWaitEnded(ctx, timeout, lastState)
		case <-ticker.C:
		}
	}
}

// readinessWaitEnded is the error of a wait that ended without a verdict. The
// caller's own cancellation or deadline is returned as it is. The wait's
// timeout names the last state read, which is the only clue to why the
// container never became ready, and does not wrap context.DeadlineExceeded:
// the bridge task runner answers that error with "operation timed out" alone,
// and the state would never reach the person who started the update.
func readinessWaitEnded(ctx context.Context, timeout time.Duration, lastState string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return notReadyInTimeError{fmt.Errorf("container was not ready after %s, last state %s", timeout, lastState)}
}

// notReadyInTimeError is a readiness wait that ran out of time with the
// container still coming up: neither ready nor failed.
type notReadyInTimeError struct{ error }

func (e notReadyInTimeError) Unwrap() error { return e.error }

// describeContainerState puts what an inspection says about a container that
// is not ready into one phrase: its status, how its last run ended when Docker
// is restarting it, how often Docker restarted it and what its health check
// reports.
func describeContainerState(inspect container.InspectResponse) string {
	state := inspect.State
	if state == nil {
		return "unavailable"
	}
	var details []string
	// Only a container between two runs has an exit code that says anything:
	// one that never ran and one that runs again both report 0.
	if state.Restarting || strings.EqualFold(string(state.Status), "restarting") {
		details = append(details, fmt.Sprintf("exit code %d", state.ExitCode))
	}
	if state.OOMKilled {
		details = append(details, "out of memory")
	}
	if inspect.RestartCount > 0 {
		details = append(details, fmt.Sprintf("restart count %d", inspect.RestartCount))
	}
	if state.Health != nil {
		details = append(details, fmt.Sprintf("health %q", state.Health.Status))
	}
	if state.Error != "" {
		details = append(details, fmt.Sprintf("error %q", state.Error))
	}
	description := fmt.Sprintf("%q", state.Status)
	if len(details) > 0 {
		description += " (" + strings.Join(details, ", ") + ")"
	}
	return description
}

func containerReady(inspect container.InspectResponse, now time.Time, settle time.Duration) (ready bool, terminal bool, err error) {
	if inspect.State == nil {
		return false, true, errors.New("container state is unavailable")
	}
	if inspect.State.Dead {
		return false, true, containerFailedError{fmt.Errorf("container state is %q", inspect.State.Status)}
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
		return false, true, containerFailedError{fmt.Errorf("container state is %q", inspect.State.Status)}
	case "created", "restarting":
		return false, false, nil
	}
	if !inspect.State.Running {
		return false, false, nil
	}
	if inspect.State.Health == nil {
		if settle <= 0 {
			return true, false, nil
		}
		// Running proves nothing about a container that crashes a few seconds
		// after it starts. Docker resets StartedAt on every restart, so this
		// is "settle with no new restart"; a start time that cannot be read
		// never settles, and the wait times out naming the state.
		started, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
		return err == nil && now.Sub(started) >= settle, false, nil
	}
	switch strings.ToLower(string(inspect.State.Health.Status)) {
	case "healthy":
		return true, false, nil
	case "unhealthy":
		return false, true, containerFailedError{fmt.Errorf("container health is %q", inspect.State.Health.Status)}
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

// containerFailedError is a readiness wait that ended because the container
// failed: it exited or died, turned unhealthy, or kept restarting. A wait
// that ran out of time, or that could not read the container or tell its
// state, did not see it fail.
type containerFailedError struct{ error }

func (e containerFailedError) Unwrap() error { return e.error }
