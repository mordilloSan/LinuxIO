package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

type stoppedContainerUpdatePolicy struct {
	Allow  bool
	Revive bool
}

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

func updateInspectedContainerWithProgress(
	ctx context.Context,
	cli *client.Client,
	inspect container.InspectResponse,
	report dockerUpdateProgressReporter,
) (apischema.DockerContainerUpdateResult, error) {
	candidate, err := inspectContainerUpdateCandidate(ctx, cli, inspect)
	if err != nil {
		return candidate.result, err
	}
	if !candidate.needsUpdate {
		reportDockerUpdateProgress(report, "current", "The container is already using the current image")
		return candidate.result, nil
	}
	if stateErr := validateContainerUpdateState(inspect); stateErr != nil {
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
	result        apischema.DockerContainerUpdateResult
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
	normalizedRef, _, immutable, err := normalizeUpdateReference(imageRef)
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

func validateComposeServiceScope(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	projectName string,
	service string,
) error {
	return validateComposeServiceScopes(ctx, cli, projectName, []string{service})
}

func validateComposeServiceScopes(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	projectName string,
	services []string,
) error {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list Compose project %q containers: %w", projectName, err)
	}
	selected := make(map[string]struct{}, len(services))
	for _, service := range services {
		selected[service] = struct{}{}
	}
	replicas := make(map[string]int, len(selected))
	for _, summary := range containers.Items {
		service := summary.Labels["com.docker.compose.service"]
		if summary.Labels["com.docker.compose.project"] == projectName {
			if _, ok := selected[service]; ok {
				replicas[service]++
			}
		}
	}
	for _, service := range services {
		if replicas[service] > 1 {
			return fmt.Errorf("Compose service %q has %d replicas and cannot be updated safely as a single container", service, replicas[service])
		}
	}
	return nil
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
	environmentFiles, err := resolveComposeUpdateEnvironmentFiles(
		ctx,
		cli,
		projectName,
		workingDir,
		labels["com.docker.compose.project.environment_file"],
	)
	if err != nil {
		return composeProjectTarget{}, "", true, err
	}

	return composeProjectTarget{
		Name:               projectName,
		ConfigFiles:        configFiles,
		EnvironmentFiles:   environmentFiles,
		IsolateEnvironment: true,
		WorkingDir:         workingDir,
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

func resolveComposeUpdateEnvironmentFiles(
	ctx context.Context,
	cli *client.Client,
	projectName string,
	workingDir string,
	environmentFilesLabel string,
) ([]string, error) {
	rawEnvironmentFiles := parseConfigFiles(environmentFilesLabel)
	for i, environmentFile := range rawEnvironmentFiles {
		if !filepath.IsAbs(environmentFile) {
			rawEnvironmentFiles[i] = filepath.Join(workingDir, environmentFile)
		}
	}
	if len(rawEnvironmentFiles) == 0 {
		return nil, nil
	}
	environmentFiles := translateComposeConfigFiles(ctx, cli, rawEnvironmentFiles)
	if len(environmentFiles) != len(rawEnvironmentFiles) {
		return nil, fmt.Errorf("not all environment files for Compose project %q are accessible", projectName)
	}
	for _, environmentFile := range environmentFiles {
		info, err := os.Stat(environmentFile)
		if err != nil {
			return nil, fmt.Errorf("stat Compose environment file %q: %w", environmentFile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Compose environment file %q is not a regular file", environmentFile)
		}
	}
	return environmentFiles, nil
}

func updateComposeContainerWithProgress(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	target composeProjectTarget,
	service string,
	result apischema.DockerContainerUpdateResult,
	report dockerUpdateProgressReporter,
) (apischema.DockerContainerUpdateResult, error) {
	updated, err := updateComposeContainerWithRunner(ctx, cli, before, target, service, result, func(
		ctx context.Context,
		target composeProjectTarget,
		service string,
		emitter composeLineEmitter,
	) error {
		if err := validateComposeUpdateInputs(ctx, target, emitter); err != nil {
			return err
		}
		reportDockerUpdateProgress(report, "pulling", fmt.Sprintf("Pulling the image for Compose service %s", service))
		err := composePullAndUpServicesValidated(ctx, target, []string{service}, emitter)
		if err == nil {
			reportDockerUpdateProgress(report, "verifying", fmt.Sprintf("Verifying Compose service %s", service))
		}
		return err
	})
	return updated, err
}

func updateComposeContainerWithRunner(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	target composeProjectTarget,
	service string,
	result apischema.DockerContainerUpdateResult,
	run func(context.Context, composeProjectTarget, string, composeLineEmitter) error,
) (apischema.DockerContainerUpdateResult, error) {
	if err := validateComposeServiceScope(ctx, cli, target.Name, service); err != nil {
		return result, err
	}
	collector := &composeMessageCollector{}
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
	after, err := waitForContainerReady(ctx, cli, inspectResult.Container.ID)
	if err != nil {
		return container.InspectResponse{}, err
	}
	return after, nil
}

func validateContainerUpdateState(inspect container.InspectResponse) error {
	return validateContainerUpdateStateForPolicy(inspect, false)
}

func validateContainerUpdateStateForPolicy(inspect container.InspectResponse, allowStopped bool) error {
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

func updateStandaloneContainerWithProgress(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	imageRef string,
	result apischema.DockerContainerUpdateResult,
	journal *standaloneUpdateJournal,
	report dockerUpdateProgressReporter,
) (apischema.DockerContainerUpdateResult, error) {
	return updateStandaloneContainerWithDependenciesAndPolicyAndProgress(ctx, cli, before, imageRef, result, journal, nil, stoppedContainerUpdatePolicy{}, report)
}

func updateStandaloneContainerWithDependenciesAndPolicy(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	imageRef string,
	result apischema.DockerContainerUpdateResult,
	journal *standaloneUpdateJournal,
	dependencies *standaloneDependencyIndex,
	policy stoppedContainerUpdatePolicy,
) (apischema.DockerContainerUpdateResult, error) {
	return updateStandaloneContainerWithDependenciesAndPolicyAndProgress(ctx, cli, before, imageRef, result, journal, dependencies, policy, nil)
}

func updateStandaloneContainerWithDependenciesAndPolicyAndProgress(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	imageRef string,
	result apischema.DockerContainerUpdateResult,
	journal *standaloneUpdateJournal,
	dependencies *standaloneDependencyIndex,
	policy stoppedContainerUpdatePolicy,
	report dockerUpdateProgressReporter,
) (apischema.DockerContainerUpdateResult, error) {
	if err := validateStandaloneUpdatePreconditions(ctx, cli, before, journal, dependencies, policy); err != nil {
		return result, err
	}

	reportDockerUpdateProgress(report, "pulling", fmt.Sprintf("Pulling image %s", imageRef))
	pulled, err := pullAndInspectStandaloneImage(ctx, cli, imageRef)
	if err != nil {
		return result, err
	}
	result.NewImageID = pulled.ID
	if pulled.ID == before.Image {
		markContainerCurrent(ctx, before.ID, before)
		reportDockerUpdateProgress(report, "current", "The container is already using the current image")
		return result, nil
	}

	createOptions, err := standaloneCreateOptions(before, imageRef, result.ContainerName)
	if err != nil {
		return result, err
	}
	backupName := standaloneBackupName(before.ID)
	tx := standaloneUpdateTransaction{
		Phase:           standaloneUpdatePrepared,
		OriginalID:      before.ID,
		OriginalName:    result.ContainerName,
		BackupName:      backupName,
		OriginalRunning: before.State != nil && before.State.Running,
	}
	if tx.OriginalRunning {
		reportDockerUpdateProgress(report, "stopping", fmt.Sprintf("Stopping %s and preparing rollback", result.ContainerName))
	} else {
		reportDockerUpdateProgress(report, "stopping", fmt.Sprintf("Preparing stopped container %s for rollback", result.ContainerName))
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
	reportDockerUpdateProgress(report, "cleanup", "Removing the rollback container")
	if _, err := cli.ContainerRemove(ctx, before.ID, client.ContainerRemoveOptions{}); err != nil {
		return result, fmt.Errorf("remove rollback container %q after successful update: %w", backupName, err)
	}
	if err := clearStandaloneJournal(journal); err != nil {
		return result, err
	}
	return result, nil
}

func validateStandaloneUpdatePreconditions(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	journal *standaloneUpdateJournal,
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

func pullAndInspectStandaloneImage(
	ctx context.Context,
	cli nativeContainerUpdateClient,
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
	cli nativeContainerUpdateClient,
	tx standaloneUpdateTransaction,
	journal *standaloneUpdateJournal,
) error {
	if journal != nil {
		if writeErr := journal.write(tx); writeErr != nil {
			return writeErr
		}
	}
	if tx.OriginalRunning {
		if _, stopErr := cli.ContainerStop(ctx, tx.OriginalID, client.ContainerStopOptions{}); stopErr != nil {
			return errors.Join(fmt.Errorf("stop standalone container %q: %w", tx.OriginalName, stopErr), clearStandaloneJournal(journal))
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
	cli nativeContainerUpdateClient,
	originalID string,
	originalName string,
	createOptions client.ContainerCreateOptions,
	tx standaloneUpdateTransaction,
	journal *standaloneUpdateJournal,
	policy stoppedContainerUpdatePolicy,
	report dockerUpdateProgressReporter,
) (container.InspectResponse, error) {
	reportDockerUpdateProgress(report, "creating", fmt.Sprintf("Creating the replacement for %s", originalName))
	created, createErr := cli.ContainerCreate(ctx, createOptions)
	if createErr != nil {
		reportDockerUpdateProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after replacement creation failed", originalName))
		rollbackErr := restoreOriginalContainer(ctx, cli, originalID, originalName, tx.OriginalRunning)
		return container.InspectResponse{}, errors.Join(fmt.Errorf("create replacement for standalone container %q: %w", originalName, createErr), clearJournalAfterRollback(journal, rollbackErr))
	}
	tx.Phase = standaloneUpdateCreated
	tx.ReplacementID = created.ID
	if writeErr := writeStandaloneJournal(journal, tx); writeErr != nil {
		reportDockerUpdateProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the update journal failed", originalName))
		return container.InspectResponse{}, errors.Join(writeErr, rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
	}
	shouldStart := tx.OriginalRunning || policy.Revive
	var after container.InspectResponse
	if shouldStart {
		reportDockerUpdateProgress(report, "starting", fmt.Sprintf("Starting the replacement for %s", originalName))
		if _, startErr := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); startErr != nil {
			reportDockerUpdateProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the replacement failed to start", originalName))
			return container.InspectResponse{}, errors.Join(fmt.Errorf("start replacement for standalone container %q: %w", originalName, startErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
		}
		tx.ReplacementStarted = true
		reportDockerUpdateProgress(report, "verifying", fmt.Sprintf("Waiting for %s to become ready", originalName))
		var readyErr error
		after, readyErr = waitForContainerReady(ctx, cli, created.ID)
		if readyErr != nil {
			reportDockerUpdateProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after readiness verification failed", originalName))
			return container.InspectResponse{}, errors.Join(fmt.Errorf("verify replacement for standalone container %q: %w", originalName, readyErr), rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
		}
	} else {
		reportDockerUpdateProgress(report, "verifying", fmt.Sprintf("Verifying stopped replacement for %s", originalName))
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
		reportDockerUpdateProgress(report, "rolling_back", fmt.Sprintf("Restoring %s after the update journal failed", originalName))
		return container.InspectResponse{}, errors.Join(writeErr, rollbackAndClearStandalone(ctx, cli, created.ID, originalID, originalName, tx.OriginalRunning, journal))
	}
	return after, nil
}

func writeStandaloneJournal(journal *standaloneUpdateJournal, tx standaloneUpdateTransaction) error {
	if journal == nil {
		return nil
	}
	return journal.write(tx)
}

func rollbackAndClearStandalone(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	replacementID string,
	originalID string,
	originalName string,
	originalRunning bool,
	journal *standaloneUpdateJournal,
) error {
	return clearJournalAfterRollback(journal, rollbackStandaloneContainer(ctx, cli, replacementID, originalID, originalName, originalRunning))
}

func clearJournalAfterRollback(journal *standaloneUpdateJournal, rollbackErr error) error {
	if rollbackErr != nil {
		return rollbackErr
	}
	return clearStandaloneJournal(journal)
}

func clearStandaloneJournal(journal *standaloneUpdateJournal) error {
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
	if err := validateContainerUpdateStateForPolicy(inspect, allowStopped); err != nil {
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

func validateStandaloneDependents(ctx context.Context, cli nativeContainerUpdateClient, inspect container.InspectResponse) error {
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
	cli nativeContainerUpdateClient,
	containers []container.Summary,
) (*standaloneDependencyIndex, error) {
	index := &standaloneDependencyIndex{dependents: make([]standaloneDependent, 0, len(containers))}
	for _, summary := range containers {
		dependentResult, inspectErr := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect potential dependent container %q: %w", primaryContainerName(summary), inspectErr)
		}
		dependent := dependentResult.Container
		name := strings.TrimPrefix(dependent.Name, "/")
		if name == "" {
			name = primaryContainerName(summary)
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

func standaloneBackupName(containerID string) string {
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}
	return "linuxio-update-backup-" + containerID
}

func waitForContainerReady(
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
	cli nativeContainerUpdateClient,
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

func restoreOriginalContainer(ctx context.Context, cli nativeContainerUpdateClient, originalID, originalName string, originalRunning bool) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), standaloneRollbackTimeout)
	defer cancel()
	if _, err := cli.ContainerRename(recoveryCtx, originalID, client.ContainerRenameOptions{NewName: originalName}); err != nil {
		return fmt.Errorf("rollback rename original container: %w", err)
	}
	return restoreOriginalRunningState(recoveryCtx, cli, originalID, originalRunning)
}

func restoreOriginalRunningState(ctx context.Context, cli nativeContainerUpdateClient, originalID string, originalRunning bool) error {
	if !originalRunning {
		return nil
	}
	return startOriginalContainer(ctx, cli, originalID)
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
