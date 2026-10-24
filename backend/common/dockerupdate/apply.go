package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

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

type stoppedContainerUpdatePolicy struct {
	Allow  bool
	Revive bool
}

// ContainerClient is the slice of the Docker client the update and replacement
// flows use; tests substitute a fake.
type ContainerClient interface {
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

// AcquireLock takes the host-wide Docker update lock shared by scheduled and
// durable updates and by standalone container edits.
func AcquireLock(ctx context.Context) (func(), error) {
	return acquireDockerUpdateLockAt(ctx, dockerUpdateLockPath, dockerUpdateLockWait, dockerUpdateLockPoll)
}

func acquireDockerUpdateLockAt(ctx context.Context, path string, wait, poll time.Duration) (func(), error) {
	release, err := filelock.AcquireExclusive(
		ctx,
		path,
		filelock.WithTimeout(wait),
		filelock.WithRetryDelay(poll),
	)
	if errors.Is(err, filelock.ErrTimeout) {
		return nil, errors.New("another Docker update is already in progress")
	}
	if err != nil {
		return nil, fmt.Errorf("acquire Docker update lock: %w", err)
	}
	return func() { _ = release() }, nil
}

// progressReporter receives the phase and a human-readable message of the
// running update. A nil reporter discards progress.
type progressReporter func(phase, message string)

func reportProgress(report progressReporter, phase, message string) {
	if report != nil {
		report(phase, message)
	}
}

func updateContainerWithProgress(
	ctx context.Context,
	containerID string,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	reportProgress(report, "waiting", "Waiting for the Docker update lock")
	release, err := AcquireLock(ctx)
	if err != nil {
		return DockerContainerUpdateResult{}, err
	}
	defer release()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		return DockerContainerUpdateResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()
	if recoveryErr := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); recoveryErr != nil {
		return DockerContainerUpdateResult{}, fmt.Errorf("recover previous standalone Docker update: %w", recoveryErr)
	}

	reportProgress(report, "checking", "Checking the container and registry image")
	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return DockerContainerUpdateResult{}, fmt.Errorf("inspect container: %w", err)
	}
	return updateInspectedContainerWithProgress(ctx, cli, inspectResult.Container, report)
}

func updateInspectedContainerWithProgress(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	candidate, err := inspectContainerUpdateCandidate(ctx, cli, inspect)
	if err != nil {
		return candidate.result, err
	}
	if !candidate.needsUpdate {
		reportProgress(report, "current", "The container is already using the current image")
		return candidate.result, nil
	}
	if stateErr := validateContainerUpdateState(inspect, false); stateErr != nil {
		return candidate.result, stateErr
	}

	target, service, managedByCompose, err := composeTargetForContainer(ctx, cli, inspect)
	if err != nil {
		return candidate.result, err
	}
	if managedByCompose {
		if err := validateComposeServiceScope(ctx, cli, target.Name, service); err != nil {
			return candidate.result, err
		}
		return updateComposeContainerWithProgress(ctx, cli, inspect, target, service, candidate.result, report)
	}
	return updateStandaloneContainerWithProgress(ctx, cli, inspect, candidate.normalizedRef, candidate.result, &defaultStandaloneUpdateJournal, report)
}

type containerUpdateCandidate struct {
	inspect       container.InspectResponse
	result        DockerContainerUpdateResult
	normalizedRef string
	needsUpdate   bool
}

func inspectContainerUpdateCandidate(
	ctx context.Context,
	cli imageUpdateCheckClient,
	inspect container.InspectResponse,
) (containerUpdateCandidate, error) {
	candidate := containerUpdateCandidate{inspect: inspect}
	result, imageRef, err := newContainerUpdateResult(inspect)
	if err != nil {
		candidate.result = result
		return candidate, err
	}
	candidate.result = result
	normalizedRef, _, immutable, err := NormalizeImageReference(imageRef)
	if err != nil {
		return candidate, err
	}
	if immutable {
		candidate.result.NewImageID = inspect.Image
		markContainerCurrent(ctx, inspect.ID, inspect)
		return candidate, nil
	}
	observation, err := inspectImageUpdate(ctx, cli, inspect.Image, normalizedRef)
	if err != nil {
		return candidate, err
	}
	return applyContainerImageObservation(ctx, candidate, normalizedRef, observation)
}

func applyContainerImageObservation(
	ctx context.Context,
	candidate containerUpdateCandidate,
	normalizedRef string,
	observation imageUpdateObservation,
) (containerUpdateCandidate, error) {
	inspect := candidate.inspect
	if observation.err != nil {
		return candidate, observation.err
	}
	if observation.uncheckableReason != "" {
		if markErr := markContainerUncheckable(ctx, inspect, observation.uncheckableReason); markErr != nil {
			return candidate, markErr
		}
		return candidate, nil
	}
	if !observation.updateAvailable {
		candidate.result.NewImageID = inspect.Image
		markContainerCurrent(ctx, inspect.ID, inspect)
		return candidate, nil
	}
	candidate.normalizedRef = normalizedRef
	candidate.needsUpdate = true
	return candidate, nil
}

func newContainerUpdateResult(inspect container.InspectResponse) (DockerContainerUpdateResult, string, error) {
	name := strings.TrimPrefix(inspect.Name, "/")
	result := DockerContainerUpdateResult{
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

func updateComposeContainerWithProgress(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	target ComposeTarget,
	service string,
	result DockerContainerUpdateResult,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	updated, err := updateComposeContainerWithRunner(ctx, cli, before, target, service, result, func(
		ctx context.Context,
		target ComposeTarget,
		service string,
		emitter ComposeLineEmitter,
	) error {
		if err := validateComposeUpdateInputs(ctx, target, emitter); err != nil {
			return err
		}
		reportProgress(report, "pulling", fmt.Sprintf("Pulling the image for Compose service %s", service))
		err := composePullAndUpServicesValidated(ctx, target, []string{service}, emitter)
		if err == nil {
			reportProgress(report, "verifying", fmt.Sprintf("Verifying Compose service %s", service))
		}
		return err
	})
	return updated, err
}

func updateComposeContainerWithRunner(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	target ComposeTarget,
	service string,
	result DockerContainerUpdateResult,
	run func(context.Context, ComposeTarget, string, ComposeLineEmitter) error,
) (DockerContainerUpdateResult, error) {
	if err := validateComposeServiceScope(ctx, cli, target.Name, service); err != nil {
		return result, err
	}
	collector := &ComposeMessageCollector{}
	if err := run(ctx, target, service, collector.Emit); err != nil {
		if output := collector.String(); output != "" {
			return result, fmt.Errorf("update Compose project %q service %q: %w: %s", target.Name, service, err, output)
		}
		return result, fmt.Errorf("update Compose project %q service %q: %w", target.Name, service, err)
	}

	after, err := inspectAndWaitForComposeContainer(ctx, cli, result.ContainerName)
	if err != nil {
		return result, fmt.Errorf("verify updated Compose container %q: %w", result.ContainerName, err)
	}
	result.ContainerID = after.ID
	result.NewImageID = after.Image
	result.Updated = after.Image != before.Image
	markContainerCurrent(ctx, before.ID, after)
	return result, nil
}

func inspectAndWaitForComposeContainer(
	ctx context.Context,
	cli containerReadinessClient,
	name string,
) (container.InspectResponse, error) {
	inspectResult, err := cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("inspect updated Compose container %q: %w", name, err)
	}
	after, err := WaitForContainerReady(ctx, cli, inspectResult.Container.ID)
	if err != nil {
		return container.InspectResponse{}, err
	}
	return after, nil
}

func validateContainerUpdateState(inspect container.InspectResponse, allowStopped bool) error {
	name := strings.TrimPrefix(inspect.Name, "/")
	if inspect.State == nil || inspect.State.Paused || inspect.State.Restarting {
		return fmt.Errorf("container %q must be stable before it can be updated", name)
	}
	if inspect.State.Running {
		return nil
	}
	if allowStopped && isStoppedContainer(inspect) {
		return nil
	}
	return fmt.Errorf("container %q must be running and stable before it can be updated", name)
}
