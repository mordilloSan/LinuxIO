package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

const (
	dockerUpdateLockPath  = "/run/linuxio-docker-update.lock"
	dockerUpdateLockWait  = 10 * time.Second
	dockerUpdateLockPoll  = 250 * time.Millisecond
	containerReadyTimeout = 90 * time.Second
	containerReadyPoll    = 500 * time.Millisecond
	// containerRestartLimit is how many restarts by Docker's restart policy
	// end the readiness wait. Docker retries a failing container after a
	// delay that starts at 100 ms and doubles up to a minute, so a container
	// that fails as it starts reaches the limit within seconds, and waiting
	// out the timeout would only watch the same failure more slowly.
	containerRestartLimit = 3
	// containerSettleTime is how long a container without a health check
	// must run since its last start to count as ready: the time after which
	// Docker's restart policy counts a start as successful and resets its
	// delay.
	containerSettleTime       = 10 * time.Second
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
// on-demand updates and by standalone container edits.
func AcquireLock(ctx context.Context) (func(), error) {
	return acquireDockerUpdateLockAt(ctx, dockerUpdateLockPath, dockerUpdateLockWait, dockerUpdateLockPoll)
}

var errUpdateInProgress = errors.New("another Docker update is already in progress")

func acquireDockerUpdateLockAt(ctx context.Context, path string, wait, poll time.Duration) (func(), error) {
	release, err := filelock.AcquireExclusive(
		ctx,
		path,
		filelock.WithTimeout(wait),
		filelock.WithRetryDelay(poll),
	)
	if errors.Is(err, filelock.ErrTimeout) {
		return nil, errUpdateInProgress
	}
	if err != nil {
		return nil, fmt.Errorf("acquire Docker update lock: %w", err)
	}
	return func() { _ = release() }, nil
}

// progressReporter receives the phase and a human-readable message of the
// running update. A nil reporter discards progress.
type progressReporter func(phase, message string)

// ProgressPhaseBuildOutput is the phase of each line a rebuild's build
// writes. It is output to show, not a step of the job: the bridge sends it
// to the page without storing it as the job's progress, which the building
// phase holds.
const ProgressPhaseBuildOutput = "build_output"

func reportProgress(report progressReporter, phase, message string) {
	if report != nil {
		report(phase, message)
	}
}

// ValidateTarget checks the container ID of an update request.
func ValidateTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("container ID is required")
	}
	if target != strings.TrimSpace(target) {
		return errors.New("container ID must not have surrounding whitespace")
	}
	if len(target) > 256 {
		return errors.New("container ID is too long")
	}
	for _, char := range target {
		if char < 0x20 || char == 0x7f {
			return errors.New("container ID contains an invalid control character")
		}
	}
	return nil
}

// UpdateContainerWait updates one container to its registry image, reporting
// each phase to report (nil discards progress). It waits up to lockWait for
// the engine lock another update holds; a lockWait of 0 waits until ctx ends.
func UpdateContainerWait(
	ctx context.Context,
	containerID string,
	lockWait time.Duration,
	report func(phase, message string),
) (DockerContainerUpdateResult, error) {
	return withInspectedContainer(ctx, containerID, lockWait, report, "Checking the container and registry image", updateInspectedContainer)
}

// ErrNotRebuildable marks a rebuild refused because the container does not
// run an image a Compose service builds on this host.
var ErrNotRebuildable = errors.New("not built by a Compose service")

// RebuildContainerWait builds the image of the Compose service that runs one
// container again, with fresh base images, and moves the service to it when
// it changed, waiting for the new container and rolling it back when it
// fails, as UpdateContainerWait does. A container that Compose does not
// manage, or whose service has no build section, is refused with
// ErrNotRebuildable. Only the Containers page asks for it: the scheduled
// pass never builds.
func RebuildContainerWait(
	ctx context.Context,
	containerID string,
	lockWait time.Duration,
	report func(phase, message string),
) (DockerContainerUpdateResult, error) {
	return withInspectedContainer(ctx, containerID, lockWait, report, "Checking the container and its Compose service", rebuildInspectedContainer)
}

// withInspectedContainer takes the engine lock (waiting up to lockWait, 0
// until ctx ends), finishes or undoes a standalone update a crash left
// half-way, reports the checking phase with checking, and hands the inspected
// container to run.
func withInspectedContainer(
	ctx context.Context,
	containerID string,
	lockWait time.Duration,
	report progressReporter,
	checking string,
	run func(context.Context, *client.Client, container.InspectResponse, progressReporter) (DockerContainerUpdateResult, error),
) (DockerContainerUpdateResult, error) {
	// One poll tells whether the lock is free before the waiting phase shows.
	release, err := acquireDockerUpdateLockAt(ctx, dockerUpdateLockPath, dockerUpdateLockPoll, dockerUpdateLockPoll)
	if errors.Is(err, errUpdateInProgress) {
		reportProgress(report, "waiting", "Waiting for another update to finish")
		release, err = acquireDockerUpdateLockAt(ctx, dockerUpdateLockPath, lockWait, dockerUpdateLockPoll)
	}
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

	reportProgress(report, "checking", checking)
	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return DockerContainerUpdateResult{}, fmt.Errorf("inspect container: %w", err)
	}
	return run(ctx, cli, inspectResult.Container, report)
}

func updateInspectedContainer(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	candidate, err := inspectContainerUpdateCandidate(ctx, cli, classifyComposeBuilt(cli), inspect)
	if err != nil {
		return candidate.result, err
	}
	if candidate.built {
		message := "The image is built on this host by its Compose service; Rebuild and update builds it again"
		if candidate.baseMoved {
			message = "A base image of the image this host builds has a newer version; Rebuild and update builds the image again with it"
		}
		reportProgress(report, "current", message)
		return candidate.result, nil
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
		return updateComposeContainer(ctx, cli, inspect, target, service, candidate.result, report)
	}
	return updateStandaloneContainer(ctx, cli, inspect, candidate.normalizedRef, candidate.result, &defaultStandaloneUpdateJournal, nil, stoppedContainerUpdatePolicy{}, report)
}

type containerUpdateCandidate struct {
	inspect       container.InspectResponse
	result        DockerContainerUpdateResult
	normalizedRef string
	needsUpdate   bool
	// built is a container whose image its Compose service builds on this
	// host: there is no registry image to update it to.
	built bool
	// baseMoved is a built container a base image of whose image has a
	// newer version: a rebuild, never an update, takes it.
	baseMoved bool
}

// inspectContainerUpdateCandidate tells whether inspect has an update, and
// records what it found as a check does. A container whose image its Compose
// service builds on this host (classify) has nothing to update: no error, and
// its status stays built, with a newer base image found as the check finds
// it (checkBuiltBases).
func inspectContainerUpdateCandidate(
	ctx context.Context,
	cli imageUpdateCheckClient,
	classify builtClassifier,
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
	if reason := containerBuiltReason(ctx, classify, inspect, observation); reason != "" {
		checks, err := checkBuiltBases(ctx, cli, []containerImageUpdateTarget{inspectedTarget(inspect)}, map[string]string{inspect.ID: reason})
		if err != nil {
			return candidate, err
		}
		check := checks[inspect.ID]
		candidate.built, candidate.baseMoved = true, check.baseMoved
		candidate.result.NewImageID = inspect.Image
		markContainerBuilt(ctx, inspect.ID, inspect, check)
		return candidate, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return candidate, ctxErr
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

func updateComposeContainer(
	ctx context.Context,
	cli *client.Client,
	before container.InspectResponse,
	target ComposeTarget,
	service string,
	result DockerContainerUpdateResult,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	updated, err := updateComposeContainerWithRunner(ctx, cli, newComposeRollback(cli), before, target, service, result, func(
		ctx context.Context,
		target ComposeTarget,
		service string,
		dependents []string,
		emitter ComposeLineEmitter,
	) error {
		if err := validateComposeUpdateInputs(ctx, target, emitter); err != nil {
			return err
		}
		reportProgress(report, "pulling", fmt.Sprintf("Pulling the image for Compose service %s", service))
		err := composePullAndUpServicesValidated(ctx, target, []string{service}, dependents, emitter)
		if err == nil {
			reportProgress(report, "verifying", fmt.Sprintf("Verifying Compose service %s", service))
		}
		return err
	}, report)
	return updated, err
}

// updateComposeContainerWithRunner updates service through run (pull and up)
// as replaceComposeContainer does, and records the new container current.
func updateComposeContainerWithRunner(
	ctx context.Context,
	cli ContainerClient,
	rollback composeRollback,
	before container.InspectResponse,
	target ComposeTarget,
	service string,
	result DockerContainerUpdateResult,
	run func(ctx context.Context, target ComposeTarget, service string, dependents []string, emitter ComposeLineEmitter) error,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	result, after, err := replaceComposeContainer(ctx, cli, rollback, before, target, service, result, run, report)
	if err == nil {
		markContainerCurrent(ctx, before.ID, after)
	}
	return result, err
}

// replaceComposeContainer brings service up on a new image through run and
// waits for its new container to be ready, which it returns. One that failed
// (exited, unhealthy, kept restarting) is rolled back to the image it ran
// before, unless ctx ended first; one still coming up when the wait ended is
// not.
func replaceComposeContainer(
	ctx context.Context,
	cli ContainerClient,
	rollback composeRollback,
	before container.InspectResponse,
	target ComposeTarget,
	service string,
	result DockerContainerUpdateResult,
	run func(ctx context.Context, target ComposeTarget, service string, dependents []string, emitter ComposeLineEmitter) error,
	report progressReporter,
) (DockerContainerUpdateResult, container.InspectResponse, error) {
	if err := validateComposeServiceScope(ctx, cli, target.Name, service); err != nil {
		return result, container.InspectResponse{}, err
	}
	joins, err := composeServiceJoins(ctx, cli, target.Name, []string{service})
	if err != nil {
		return result, container.InspectResponse{}, err
	}
	dependents := joins.of([]string{service})
	collector := &ComposeMessageCollector{}
	if err = run(ctx, target, service, dependents, collector.Emit); err != nil {
		if output := collector.String(); output != "" {
			return result, container.InspectResponse{}, fmt.Errorf("update Compose project %q service %q: %w: %s", target.Name, service, err, output)
		}
		return result, container.InspectResponse{}, fmt.Errorf("update Compose project %q service %q: %w", target.Name, service, err)
	}

	after, err := inspectAndWaitForComposeContainer(ctx, cli, result.ContainerName)
	if err != nil {
		verifyErr := fmt.Errorf("verify updated Compose container %q: %w", result.ContainerName, err)
		if ctx.Err() != nil {
			// Stopped while it waited: how the new container would have
			// done is unknown, so it is not rolled back.
			return result, container.InspectResponse{}, verifyErr
		}
		if refusal := rollbackRefusal(err); refusal != nil {
			return result, container.InspectResponse{}, composeUpdateFailure(verifyErr, before.Image, refusal)
		}
		reportProgress(report, "rolling_back", fmt.Sprintf("Restoring the previous image of Compose service %s", service))
		imageRef := normalizedReference(configImage(before))
		pulledImageID, rollbackErr := rollback.taggedImage(ctx, imageRef)
		if rollbackErr == nil {
			_, rollbackErr = rollback.run(ctx, target, imageRef, before.Image, pulledImageID, []composeRollbackService{{Service: service, Container: result.ContainerName}}, dependents)
		}
		return result, container.InspectResponse{}, composeUpdateFailure(verifyErr, before.Image, rollbackErr)
	}
	result.ContainerID = after.ID
	result.NewImageID = after.Image
	result.Updated = after.Image != before.Image
	return result, after, nil
}

// buildOutputErrorLines is how many of the last lines of a failed build its
// error carries.
const buildOutputErrorLines = 20

// rebuildPreviousTag is the tag, in the repository of the service's image,
// that holds the image a container ran while its service is rebuilt.
const rebuildPreviousTag = "linuxio-rebuild-previous"

// rebuildClient is the slice of the Docker client a rebuild uses; tests
// substitute a fake.
type rebuildClient interface {
	ContainerClient
	ImageTag(context.Context, client.ImageTagOptions) (client.ImageTagResult, error)
	ImageRemove(context.Context, string, client.ImageRemoveOptions) (client.ImageRemoveResult, error)
}

// rebuildInspectedContainer refuses inspect unless it runs and Compose manages
// it, then rebuilds its service (rebuildComposeContainer).
func rebuildInspectedContainer(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	result, _, err := newContainerUpdateResult(inspect)
	if err != nil {
		return result, err
	}
	if stateErr := validateContainerUpdateState(inspect, false); stateErr != nil {
		return result, stateErr
	}
	target, service, managedByCompose, err := composeTargetForContainer(ctx, cli, inspect)
	if err != nil {
		return result, err
	}
	if !managedByCompose {
		return result, fmt.Errorf("rebuild container %q: %w: Docker Compose does not manage it", result.ContainerName, ErrNotRebuildable)
	}
	if err := validateComposeServiceScope(ctx, cli, target.Name, service); err != nil {
		return result, err
	}
	return rebuildComposeContainer(ctx, cli, newComposeRollback(cli), inspect, target, service, result, report)
}

// rebuildComposeContainer builds the image of service, which before runs,
// again with fresh base images (`build --pull`), each line of the build's
// output reported in the building phase. When the tag before was created
// from names the image it runs, nothing changed and the container is left
// as it is. Otherwise the service is brought up on the new image, with
// nothing pulled or built (composeUpFromLocalImages), and replaced as
// replaceComposeContainer does, rolled back when its new container fails. A
// service without a build section is refused with ErrNotRebuildable.
func rebuildComposeContainer(
	ctx context.Context,
	cli rebuildClient,
	rollback composeRollback,
	before container.InspectResponse,
	target ComposeTarget,
	service string,
	result DockerContainerUpdateResult,
	report progressReporter,
) (DockerContainerUpdateResult, error) {
	settings, err := composeServiceSettingsOf(ctx, target, []string{service})
	if err != nil {
		return result, err
	}
	// Reading the settings is the project's validation: `config` fails on an
	// invalid project as `config --quiet` does, and also on a service no
	// longer in its files.
	if !settings[service].Build {
		return result, fmt.Errorf("rebuild container %q: %w: service %q of project %q has no build section", result.ContainerName, ErrNotRebuildable, service, target.Name)
	}

	release, err := holdPreviousImage(ctx, cli, normalizedReference(configImage(before)), before.Image, result.ContainerName)
	if err != nil {
		return result, err
	}
	defer release()

	reportProgress(report, "building", fmt.Sprintf("Building the image of Compose service %s with fresh base images", service))
	var tail []string // the last lines of the build's output, for its error
	var bases buildBaseCollector
	emit := func(_ string, message string, _ *ComposeProgress) {
		reportProgress(report, ProgressPhaseBuildOutput, message)
		bases.add(message)
		if len(tail) == buildOutputErrorLines {
			tail = slices.Delete(tail, 0, 1)
		}
		tail = append(tail, message)
	}
	if buildErr := composeBuildService(ctx, target, service, emit); buildErr != nil {
		if len(tail) > 0 {
			return result, fmt.Errorf("build Compose project %q service %q: %w: %s", target.Name, service, buildErr, strings.Join(tail, "\n"))
		}
		return result, fmt.Errorf("build Compose project %q service %q: %w", target.Name, service, buildErr)
	}

	// The build tags the image the files name now, which is not the one the
	// container was created from when they changed it since.
	imageRef := normalizedReference(configImage(before))
	if named := settings[service].Image; named != "" {
		imageRef = normalizedReference(named)
	}
	rebuilt, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return result, fmt.Errorf("inspect rebuilt image %q: %w", imageRef, err)
	}
	check := recordRebuiltImage(ctx, bases.record(rebuilt.ID, time.Now()), builtReason(target.Name, service), result.ContainerName)
	if rebuilt.ID == before.Image {
		result.NewImageID = before.Image
		markContainerBuilt(ctx, before.ID, before, check)
		reportProgress(report, "current", "The rebuilt image is the one the container already runs")
		return result, nil
	}

	result, after, err := replaceComposeContainer(ctx, cli, rollback, before, target, service, result, func(
		ctx context.Context,
		target ComposeTarget,
		service string,
		dependents []string,
		emitter ComposeLineEmitter,
	) error {
		reportProgress(report, "starting", fmt.Sprintf("Starting Compose service %s on the rebuilt image", service))
		if upErr := composeUpFromLocalImages(ctx, target, append([]string{service}, dependents...), emitter); upErr != nil {
			return upErr
		}
		reportProgress(report, "verifying", fmt.Sprintf("Verifying Compose service %s", service))
		return nil
	}, report)
	if err == nil {
		markContainerBuilt(ctx, before.ID, after, check)
	}
	return result, err
}

// recordRebuiltImage records the base images the rebuild of the image of
// Docker container name resolved, whether or not the image changed: a base
// can move without changing the image built on it, and the record follows
// the base. It returns what a check of the container finds now, for reason
// (builtReason). A record that cannot be written leaves the container as one
// without a record, and the rebuild goes on.
func recordRebuiltImage(ctx context.Context, record builtImageRecord, reason, name string) builtCheck {
	if err := recordBuiltImage(ctx, record); err != nil {
		slog.Warn("failed to record the base images of the rebuilt image of Docker container "+name,
			"component", "docker",
			"subsystem", "update",
			"container", name,
			"image", record.ImageID,
			"error", err)
		return builtCheck{reason: reason + builtWithoutRecord}
	}
	slog.Debug("recorded the base images of the rebuilt image of Docker container "+name,
		"component", "docker",
		"subsystem", "update",
		"container", name,
		"image", record.ImageID,
		"bases", len(record.Bases),
		"bases_unread", record.BasesUnread)
	return rebuiltCheck(reason, record)
}

// holdPreviousImage tags imageID, the image a container runs, as the
// repository of imageRef at rebuildPreviousTag, and returns what removes that
// tag again. The build moves imageRef off imageID, and with Docker's
// containerd image store an image the build leaves with no name is gone at
// once, though the container still runs it (seen with Docker 29), so a
// rollback would have nothing to put the name back on; the tag keeps it
// until the rebuild ends. Removing the tag removes the image when nothing
// else names or runs it, and is refused, leaving the tag, when the tag is
// its last name and a container still runs it. An image already gone is not
// held: the rebuild goes on, and a rollback, should one come, says it cannot
// tag it.
func holdPreviousImage(ctx context.Context, cli rebuildClient, imageRef, imageID, name string) (func(), error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parse image reference %q: %w", imageRef, err)
	}
	held, err := reference.WithTag(reference.TrimNamed(named), rebuildPreviousTag)
	if err != nil {
		return nil, fmt.Errorf("name the tag that holds image %s: %w", shortImageID(imageID), err)
	}
	tagCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), composeTagTimeout)
	defer cancel()
	if _, tagErr := cli.ImageTag(tagCtx, client.ImageTagOptions{Source: imageID, Target: held.String()}); tagErr != nil {
		if tagCtx.Err() == nil && imageNotFound(tagErr) {
			slog.Debug("rebuilding without holding the previous image, already gone, of Docker container "+name,
				"component", "docker",
				"subsystem", "update",
				"container", name,
				"image", imageID,
				"error", tagErr)
			return func() {}, nil
		}
		return nil, tagCallError(tagCtx, fmt.Sprintf("tag image %s as %q", shortImageID(imageID), held.String()), tagErr)
	}
	return func() {
		removeCtx, removeCancel := context.WithTimeout(context.WithoutCancel(ctx), composeTagTimeout)
		defer removeCancel()
		_, removeErr := cli.ImageRemove(removeCtx, held.String(), client.ImageRemoveOptions{})
		switch {
		case removeErr == nil:
		case errdefs.IsConflict(removeErr) || strings.Contains(strings.ToLower(removeErr.Error()), "conflict"):
			// Its last name, and a container still runs it: the tag is what
			// keeps that image.
			slog.Debug("kept the tag that holds the previous image of Docker container "+name,
				"component", "docker",
				"subsystem", "update",
				"container", name,
				"tag", held.String(),
				"error", removeErr)
		default:
			slog.Warn("failed to remove the tag that held the previous image of Docker container "+name,
				"component", "docker",
				"subsystem", "update",
				"container", name,
				"tag", held.String(),
				"error", removeErr)
		}
	}, nil
}

// imageNotFound tells an image the daemon does not have.
func imageNotFound(err error) bool {
	// Errors from the daemon lose their type across HTTP.
	return errdefs.IsNotFound(err) || strings.Contains(strings.ToLower(err.Error()), "no such image")
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
	// A Compose update replaces a running service.
	after, err := waitForReplacementReady(ctx, cli, inspectResult.Container.ID)
	if err != nil {
		return container.InspectResponse{}, err
	}
	return after, nil
}

func validateContainerUpdateState(inspect container.InspectResponse, allowStopped bool) error {
	name := strings.TrimPrefix(inspect.Name, "/")
	if inspect.Config != nil && dockerJobRun(inspect.Config.Labels) {
		return fmt.Errorf("container %q is a run of a Docker job, which Docker replaces at the job's next run", name)
	}
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

// dockerJobLabel marks a container Docker Engine runs for a job (Engine 29.10;
// Compose 5.8 schedules its jobs through it). The engine creates and removes
// each run itself and keeps its exit status for the job's history, so a run is
// not a service: it is never checked, counted as a replica or updated, which
// would run the job again outside its schedule or lose that status.
const dockerJobLabel = "com.docker.job.id"

func dockerJobRun(labels map[string]string) bool {
	_, ok := labels[dockerJobLabel]
	return ok
}
