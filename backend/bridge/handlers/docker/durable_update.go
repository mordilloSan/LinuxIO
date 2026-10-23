package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/transientunit"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
)

const dockerUpdateRoute = dockerupdate.UpdateRoute

var (
	dockerUpdateStoreRoot    = durabletask.DefaultRoot
	dockerUpdatePollInterval = 500 * time.Millisecond
	newDockerUpdateExecutor  = func() dockerUpdateExecutor { return systemdDockerUpdateExecutor{} }
)

type dockerUpdateRequest struct {
	operationID string
	containerID string
	fingerprint string
}

type dockerUpdateExecutor interface {
	Start(context.Context, dockerUpdateLaunch) error
	Inspect(context.Context, string, string) (transientunit.State, error)
	Stop(context.Context, string) error
	Collect(context.Context, string)
}

type dockerUpdateLaunch struct {
	OperationID string
	UID         uint32
	Unit        string
	Description string
}

var (
	errDockerUpdateUnitNotFound     = transientunit.ErrNotFound
	errDockerUpdateAlreadyLaunching = errors.New("docker update executor launch already claimed")
)

func dockerUpdateTaskIdentity(payload apischema.DockerContainerUpdateRequest) (bridgeipc.TaskIdentity, error) {
	req, err := parseDockerUpdateRequest(payload)
	if err != nil {
		return bridgeipc.TaskIdentity{}, err
	}
	return bridgeipc.TaskIdentity{ID: req.operationID, Fingerprint: req.fingerprint}, nil
}

func parseDockerUpdateRequest(payload apischema.DockerContainerUpdateRequest) (dockerUpdateRequest, error) {
	if err := durabletask.ValidateID(payload.RunID); err != nil {
		return dockerUpdateRequest{}, bridgeipc.NewError(err.Error(), 400)
	}
	containerID := strings.TrimSpace(payload.ContainerID)
	if containerID == "" {
		return dockerUpdateRequest{}, bridgeipc.NewError("container ID is required", 400)
	}
	if err := dockerupdate.ValidateTarget(containerID); err != nil {
		return dockerUpdateRequest{}, bridgeipc.NewError(err.Error(), 400)
	}
	return dockerUpdateRequest{
		operationID: payload.RunID,
		containerID: containerID,
		fingerprint: dockerupdate.RequestFingerprint(containerID),
	}, nil
}

func runDockerUpdateTask(ctx context.Context, task *bridgeipc.Task, payload apischema.DockerContainerUpdateRequest) (apischema.DockerContainerUpdateResult, error) {
	req, err := parseDockerUpdateRequest(payload)
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	owner := task.Owner()
	if owner.Username == "" || owner.SessionID == "" {
		return apischema.DockerContainerUpdateResult{}, bridgeipc.NewError("durable Docker update requires an authenticated owner", 403)
	}
	store := durabletask.NewStore(dockerUpdateStoreRoot)
	if ctx.Err() != nil {
		return cancelDockerUpdate(store, newDockerUpdateExecutor(), req.operationID, owner.UID)
	}
	record, err := claimDockerUpdate(ctx, store, req, owner.UID)
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}

	slog.Info("Docker update task claimed", "component", "docker", "subsystem", "update", "operation_id", req.operationID, "uid", owner.UID, "state", record.State)
	if ctx.Err() != nil {
		return cancelDockerUpdate(store, newDockerUpdateExecutor(), record.ID, record.UID)
	}
	if record.Terminal() {
		return dockerUpdateResultFromRecord(record)
	}
	if record, err = validateDockerUpdateRecord(ctx, store, record); err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	if record.State == durabletask.StateUnknown {
		return dockerUpdateResultFromRecord(record)
	}

	executor := newDockerUpdateExecutor()
	if record.State == durabletask.StateQueued {
		record, err = prepareAndLaunchDockerUpdate(ctx, task, store, executor, record)
		if err != nil {
			if ctx.Err() != nil {
				return cancelDockerUpdate(store, executor, req.operationID, owner.UID)
			}
			return apischema.DockerContainerUpdateResult{}, err
		}
	}
	return observeDockerUpdate(ctx, task, store, executor, record)
}

func claimDockerUpdate(ctx context.Context, store *durabletask.Store, req dockerUpdateRequest, uid uint32) (durabletask.Record, error) {
	record, _, err := store.Claim(ctx, durabletask.Claim{
		ID:                 req.operationID,
		Route:              dockerUpdateRoute,
		UID:                uid,
		RequestFingerprint: req.fingerprint,
		Target:             req.containerID,
		ExclusiveRoute:     true,
	})
	if errors.Is(err, durabletask.ErrConflict) {
		return durabletask.Record{}, bridgeipc.NewError("operation ID was already used for another Docker update request", 409)
	}
	if errors.Is(err, durabletask.ErrActive) {
		return durabletask.Record{}, bridgeipc.NewError("another Docker update is already active", 409)
	}
	if err != nil {
		return durabletask.Record{}, fmt.Errorf("claim durable Docker update: %w", err)
	}
	return record, nil
}

func validateDockerUpdateRecord(ctx context.Context, store *durabletask.Store, record durabletask.Record) (durabletask.Record, error) {
	if validationErr := dockerupdate.ValidatePersisted(record); validationErr != nil {
		unknown, updateErr := unknownDockerUpdateRecord(ctx, store, record, validationErr.Error())
		if updateErr != nil {
			return durabletask.Record{}, updateErr
		}
		return unknown, nil
	}
	if record.State == durabletask.StateQueued {
		return record, nil
	}
	if validationErr := dockerupdate.ValidateExecutor(record); validationErr != nil {
		unknown, updateErr := unknownDockerUpdateRecord(ctx, store, record, validationErr.Error())
		if updateErr != nil {
			return durabletask.Record{}, updateErr
		}
		return unknown, nil
	}
	return record, nil
}

func prepareAndLaunchDockerUpdate(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (durabletask.Record, error) {
	unitName := dockerupdate.UnitName(record.ID)
	description := dockerupdate.UnitDescription(record.ID, record.UID)
	launching, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		if current.State != durabletask.StateQueued {
			return errDockerUpdateAlreadyLaunching
		}
		current.State = durabletask.StateLaunching
		current.Executor = durabletask.Executor{Kind: dockerupdate.ExecutorKind, Handle: unitName, Identity: dockerupdate.ExecutorIdentity}
		now := time.Now().UTC()
		current.AppendProgress(now, "launching", "Starting the systemd Docker update worker")
		return nil
	})
	if errors.Is(err, errDockerUpdateAlreadyLaunching) {
		return store.Get(ctx, record.ID, record.UID)
	}
	if err != nil {
		return durabletask.Record{}, err
	}
	if task != nil {
		task.ReportProgress(apischema.DockerContainerUpdateProgress{Phase: "launching", Message: "Starting the systemd Docker update worker"})
	}

	startErr := executor.Start(ctx, dockerUpdateLaunch{OperationID: launching.ID, UID: launching.UID, Unit: unitName, Description: description})
	if ctx.Err() != nil {
		return launching, ctx.Err()
	}
	if startErr != nil {
		return handleDockerUpdateStartFailure(ctx, task, store, executor, launching, unitName, description, startErr)
	}
	return markDockerUpdateRunning(ctx, task, store, launching, "Docker update worker accepted by systemd")
}

func handleDockerUpdateStartFailure(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, executor dockerUpdateExecutor, launching durabletask.Record, unitName, description string, startErr error) (durabletask.Record, error) {
	state, inspectErr := executor.Inspect(ctx, unitName, description)
	if artifact, artifactErr := store.ReadExecutorResult(launching.ID); artifactErr == nil {
		terminal, applyErr := applyDockerExecutorResult(ctx, store, launching, artifact)
		if applyErr != nil {
			return durabletask.Record{}, applyErr
		}
		return terminal, nil
	}
	if inspectErr == nil && transientunit.IsActive(state) {
		return markDockerUpdateRunning(ctx, task, store, launching, "Adopted the active Docker update worker after an uncertain start")
	}
	if errors.Is(inspectErr, errDockerUpdateUnitNotFound) {
		failed, failErr := failDockerUpdateRecord(ctx, store, launching, 500, startErr.Error())
		if failErr != nil {
			return durabletask.Record{}, failErr
		}
		return failed, bridgeipc.NewError(dockerupdate.BoundedMessage(startErr.Error()), 500)
	}
	unknown, unknownErr := unknownDockerUpdateRecord(ctx, store, launching, fmt.Sprintf("transient-unit start outcome is unknown: %v", startErr))
	if unknownErr != nil {
		return durabletask.Record{}, errors.Join(startErr, unknownErr)
	}
	return unknown, bridgeipc.NewError(unknown.Error.Message, unknown.Error.Code)
}

func markDockerUpdateRunning(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, record durabletask.Record, message string) (durabletask.Record, error) {
	now := time.Now().UTC()
	updated, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateRunning
		if current.StartedAt == nil {
			current.StartedAt = &now
		}
		current.AppendProgress(now, "running", message)
		return nil
	})
	if err == nil && task != nil {
		task.ReportProgress(apischema.DockerContainerUpdateProgress{Phase: "running", Message: message})
	}
	return updated, err
}

func observeDockerUpdate(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (apischema.DockerContainerUpdateResult, error) {
	ticker := time.NewTicker(dockerUpdatePollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return cancelDockerUpdate(store, executor, record.ID, record.UID)
		}
		current, getErr := store.Get(ctx, record.ID, record.UID)
		if getErr != nil {
			return apischema.DockerContainerUpdateResult{}, getErr
		}
		reportNewDockerUpdateProgress(task, record, current)
		record = current
		result, done, err := reconcileDockerUpdate(ctx, task, store, executor, record)
		if done || err != nil {
			return result, err
		}
		select {
		case <-ctx.Done():
			return cancelDockerUpdate(store, executor, record.ID, record.UID)
		case <-ticker.C:
		}
	}
}

func reportNewDockerUpdateProgress(task *bridgeipc.Task, previous, current durabletask.Record) {
	if task == nil || len(current.Progress) == 0 {
		return
	}
	start := 0
	if len(previous.Progress) > 0 {
		prior := previous.Progress[len(previous.Progress)-1]
		for index, candidate := range slices.Backward(current.Progress) {

			if prior.At.Equal(candidate.At) && prior.Phase == candidate.Phase && prior.Message == candidate.Message {
				start = index + 1
				break
			}
		}
	}
	for _, progress := range current.Progress[start:] {
		task.ReportProgress(apischema.DockerContainerUpdateProgress{
			Phase:   progress.Phase,
			Message: progress.Message,
		})
	}
}

func reconcileDockerUpdate(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (apischema.DockerContainerUpdateResult, bool, error) {
	if result, handled, err := reconcileDockerExecutorResult(ctx, store, executor, record); handled {
		return result, true, err
	}
	state, err := executor.Inspect(ctx, record.Executor.Handle, dockerupdate.UnitDescription(record.ID, record.UID))
	if err != nil {
		if !errors.Is(err, errDockerUpdateUnitNotFound) {
			return apischema.DockerContainerUpdateResult{}, false, err
		}
		if record.State == durabletask.StateLaunching {
			failed, failErr := failDockerUpdateRecord(ctx, store, record, 500, "systemd did not retain the Docker update worker launch")
			if failErr != nil {
				return apischema.DockerContainerUpdateResult{}, true, failErr
			}
			result, resultErr := dockerUpdateResultFromRecord(failed)
			return result, true, resultErr
		}
		unknown, unknownErr := unknownDockerUpdateRecord(ctx, store, record, "Docker update worker disappeared before a typed result was recorded")
		if unknownErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, unknownErr
		}
		result, resultErr := dockerUpdateResultFromRecord(unknown)
		return result, true, resultErr
	}
	if transientunit.IsActive(state) {
		if record.State == durabletask.StateLaunching {
			_, updateErr := markDockerUpdateRunning(ctx, task, store, record, "Recovered the active systemd Docker update worker")
			if updateErr != nil {
				return apischema.DockerContainerUpdateResult{}, false, updateErr
			}
		}
		return apischema.DockerContainerUpdateResult{}, false, nil
	}
	return reconcileStoppedDockerUpdate(ctx, task, store, executor, record, state)
}

func reconcileDockerExecutorResult(ctx context.Context, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (apischema.DockerContainerUpdateResult, bool, error) {
	result, err := store.ReadExecutorResult(record.ID)
	if errors.Is(err, durabletask.ErrNotFound) {
		return apischema.DockerContainerUpdateResult{}, false, nil
	}
	if err != nil {
		unknown, unknownErr := unknownDockerUpdateRecord(ctx, store, record, err.Error())
		if unknownErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, unknownErr
		}
		terminal, terminalErr := dockerUpdateResultFromRecord(unknown)
		return terminal, true, terminalErr
	}
	terminal, applyErr := applyDockerExecutorResult(ctx, store, record, result)
	if applyErr != nil {
		return apischema.DockerContainerUpdateResult{}, true, applyErr
	}
	executor.Collect(ctx, record.Executor.Handle)
	value, valueErr := dockerUpdateResultFromRecord(terminal)
	return value, true, valueErr
}

func reconcileStoppedDockerUpdate(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record, state transientunit.State) (apischema.DockerContainerUpdateResult, bool, error) {
	if result, err := store.ReadExecutorResult(record.ID); err == nil {
		terminal, applyErr := applyDockerExecutorResult(ctx, store, record, result)
		if applyErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, applyErr
		}
		executor.Collect(ctx, record.Executor.Handle)
		value, valueErr := dockerUpdateResultFromRecord(terminal)
		return value, true, valueErr
	} else if !errors.Is(err, durabletask.ErrNotFound) {
		unknown, unknownErr := unknownDockerUpdateRecord(ctx, store, record, err.Error())
		if unknownErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, unknownErr
		}
		value, valueErr := dockerUpdateResultFromRecord(unknown)
		return value, true, valueErr
	}

	message := fmt.Sprintf("Docker update worker stopped without a typed result (systemd result=%s, exit=%d)", state.ServiceResult, state.ExitCode)
	if state.ServiceResult == "success" && state.ExitCode == 0 {
		unknown, unknownErr := unknownDockerUpdateRecord(ctx, store, record, message)
		if unknownErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, unknownErr
		}
		value, valueErr := dockerUpdateResultFromRecord(unknown)
		return value, true, valueErr
	}
	failed, failErr := failDockerUpdateRecord(ctx, store, record, state.ExitCode, message)
	if failErr != nil {
		return apischema.DockerContainerUpdateResult{}, true, failErr
	}
	if task != nil {
		task.ReportData("ERROR: " + message + "\n")
	}
	executor.Collect(ctx, record.Executor.Handle)
	value, valueErr := dockerUpdateResultFromRecord(failed)
	return value, true, valueErr
}

func cancelDockerUpdate(store *durabletask.Store, executor dockerUpdateExecutor, id string, uid uint32) (apischema.DockerContainerUpdateResult, error) {
	stopCtx, cancel := durabletask.DetachedContext(dockerupdate.WorkerUnit.StopTimeout)
	defer cancel()
	record, err := store.Get(stopCtx, id, uid)
	if errors.Is(err, durabletask.ErrNotFound) {
		return apischema.DockerContainerUpdateResult{}, context.Canceled
	}
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	if record.Terminal() {
		return dockerUpdateResultFromRecord(record)
	}
	if record.State == durabletask.StateQueued {
		return markQueuedDockerUpdateCanceled(stopCtx, store, record)
	}
	if validationErr := dockerupdate.ValidatePersisted(record); validationErr != nil {
		return apischema.DockerContainerUpdateResult{}, validationErr
	}
	if validationErr := dockerupdate.ValidateExecutor(record); validationErr != nil {
		return apischema.DockerContainerUpdateResult{}, validationErr
	}
	if result, handled, reconcileErr := reconcileDockerExecutorResult(stopCtx, store, executor, record); handled {
		return result, reconcileErr
	}
	now := time.Now().UTC()
	updated, err := store.Update(stopCtx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.CancelRequestedAt = &now
		current.AppendProgress(now, "canceling", "Waiting for systemd to stop the Docker update worker")
		return nil
	})
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	if stopErr := executor.Stop(stopCtx, updated.Executor.Handle); stopErr != nil && !errors.Is(stopErr, errDockerUpdateUnitNotFound) {
		unknown, unknownErr := unknownDockerUpdateRecord(stopCtx, store, updated, fmt.Sprintf("failed to confirm Docker update cancellation: %v", stopErr))
		if unknownErr != nil {
			return apischema.DockerContainerUpdateResult{}, unknownErr
		}
		return dockerUpdateResultFromRecord(unknown)
	}
	return waitForDockerUpdateCancellation(stopCtx, store, executor, updated)
}

func markQueuedDockerUpdateCanceled(ctx context.Context, store *durabletask.Store, record durabletask.Record) (apischema.DockerContainerUpdateResult, error) {
	finished := time.Now().UTC()
	updated, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateCanceled
		current.CancelRequestedAt = &finished
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 499, Message: "operation canceled"}
		current.AppendProgress(finished, "canceled", "The update was canceled before worker launch")
		return nil
	})
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	return dockerUpdateResultFromRecord(updated)
}

func waitForDockerUpdateCancellation(ctx context.Context, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (apischema.DockerContainerUpdateResult, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return timeoutDockerUpdateCancellation(store, record)
		}
		result, done, err := reconcileDockerUpdateCancellation(ctx, store, executor, record)
		if done || err != nil {
			return result, err
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

func timeoutDockerUpdateCancellation(store *durabletask.Store, record durabletask.Record) (apischema.DockerContainerUpdateResult, error) {
	finalizeCtx, cancel := durabletask.DetachedContext(5 * time.Second)
	defer cancel()
	unknown, err := unknownDockerUpdateRecord(finalizeCtx, store, record, "timed out waiting for systemd to confirm cancellation")
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	return dockerUpdateResultFromRecord(unknown)
}

func reconcileDockerUpdateCancellation(ctx context.Context, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (apischema.DockerContainerUpdateResult, bool, error) {
	if result, handled, err := reconcileDockerExecutorResult(ctx, store, executor, record); handled {
		return result, true, err
	}
	state, inspectErr := executor.Inspect(ctx, record.Executor.Handle, dockerupdate.UnitDescription(record.ID, record.UID))
	if errors.Is(inspectErr, errDockerUpdateUnitNotFound) || (inspectErr == nil && !transientunit.IsActive(state)) {
		canceled, cancelErr := markDockerUpdateCanceled(ctx, store, executor, record)
		if cancelErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, cancelErr
		}
		result, resultErr := dockerUpdateResultFromRecord(canceled)
		return result, true, resultErr
	}
	if inspectErr != nil {
		unknown, unknownErr := unknownDockerUpdateRecord(ctx, store, record, inspectErr.Error())
		if unknownErr != nil {
			return apischema.DockerContainerUpdateResult{}, true, unknownErr
		}
		result, resultErr := dockerUpdateResultFromRecord(unknown)
		return result, true, resultErr
	}
	return apischema.DockerContainerUpdateResult{}, false, nil
}

func markDockerUpdateCanceled(ctx context.Context, store *durabletask.Store, executor dockerUpdateExecutor, record durabletask.Record) (durabletask.Record, error) {
	finished := time.Now().UTC()
	canceled, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateCanceled
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 499, Message: "operation canceled"}
		current.AppendProgress(finished, "canceled", "The Docker update worker stopped")
		return nil
	})
	if err != nil {
		return durabletask.Record{}, err
	}
	executor.Collect(ctx, canceled.Executor.Handle)
	return canceled, nil
}

func dockerUpdateResultFromRecord(record durabletask.Record) (apischema.DockerContainerUpdateResult, error) {
	var result apischema.DockerContainerUpdateResult
	if len(record.Result) > 0 {
		if err := json.Unmarshal(record.Result, &result); err != nil {
			return apischema.DockerContainerUpdateResult{}, fmt.Errorf("decode durable Docker update result: %w", err)
		}
	}
	switch record.State {
	case durabletask.StateCompleted:
		return result, nil
	case durabletask.StateCanceled:
		return result, context.Canceled
	case durabletask.StateFailed, durabletask.StateUnknown:
		if record.Error != nil {
			return result, bridgeipc.NewError(record.Error.Message, record.Error.Code)
		}
		return result, bridgeipc.NewError("durable Docker update did not complete", 500)
	default:
		return result, bridgeipc.NewError("durable Docker update is still active", 409)
	}
}

func applyDockerExecutorResult(
	ctx context.Context,
	store *durabletask.Store,
	record durabletask.Record,
	result durabletask.ExecutorResult,
) (durabletask.Record, error) {
	if err := validateDockerExecutorResult(result); err != nil {
		unknown, updateErr := unknownDockerUpdateRecord(ctx, store, record, err.Error())
		if updateErr != nil {
			return durabletask.Record{}, errors.Join(err, updateErr)
		}
		return unknown, nil
	}
	return store.ApplyExecutorResult(ctx, record.UID, result)
}

func validateDockerExecutorResult(result durabletask.ExecutorResult) error {
	if len(result.Result) == 0 {
		if result.State == durabletask.StateCompleted {
			return errors.New("completed Docker update result is missing its typed payload")
		}
		return nil
	}
	var payload apischema.DockerContainerUpdateResult
	if err := json.Unmarshal(result.Result, &payload); err != nil {
		return fmt.Errorf("decode Docker update executor result: %w", err)
	}
	if result.State == durabletask.StateCompleted && strings.TrimSpace(payload.ContainerID) == "" {
		return errors.New("completed Docker update result is missing its container identity")
	}
	return nil
}

func failDockerUpdateRecord(ctx context.Context, store *durabletask.Store, record durabletask.Record, code int, message string) (durabletask.Record, error) {
	finished := time.Now().UTC()
	message = dockerupdate.BoundedMessage(message)
	updated, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateFailed
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: code, Message: message}
		current.AppendProgress(finished, "failed", message)
		return nil
	})
	return updated, err
}

func unknownDockerUpdateRecord(ctx context.Context, store *durabletask.Store, record durabletask.Record, message string) (durabletask.Record, error) {
	finished := time.Now().UTC()
	message = dockerupdate.BoundedMessage(message)
	return store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateUnknown
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 500, Message: message}
		current.AppendProgress(finished, "unknown", message)
		return nil
	})
}

func recoverDockerUpdates(rt runtime.Runtime, router *bridgeipc.Router) {
	if rt.Session == nil || rt.Session.User.Username == "" {
		return
	}
	ctx, cancel := durabletask.DetachedContext(15 * time.Second)
	defer cancel()
	owner := bridgeipc.TaskOwner{SessionID: rt.Session.SessionID, Username: rt.Session.User.Username, UID: rt.Session.User.UID}
	records, err := durabletask.NewStore(dockerUpdateStoreRoot).ListActiveForUID(ctx, owner.UID)
	if err != nil {
		slog.Error("failed to list durable Docker updates", "component", "docker", "subsystem", "update", "uid", owner.UID, "error", err)
		return
	}
	for _, record := range records {
		if record.Route != dockerUpdateRoute {
			continue
		}
		request := apischema.DockerContainerUpdateRequest{RunID: record.ID, ContainerID: record.Target}
		identity := bridgeipc.TaskIdentity{ID: record.ID, Fingerprint: record.RequestFingerprint}
		if _, _, err := router.RecoverDurableTask(dockerUpdateRoute, request, owner, identity); err != nil {
			slog.Warn("failed to reattach durable Docker update", "component", "docker", "subsystem", "update", "operation_id", record.ID, "error", err)
		}
	}
}
