package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const (
	routeAppUpdate            = "control.app_update"
	appUpdateInstallerName    = "install.sh"
	appUpdateExecutorKind     = "systemd-transient-unit"
	appUpdateExecutorIdentity = "root:root"
)

var (
	validReleaseVersionRE = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	appUpdateStoreRoot    = durabletask.DefaultRoot
	appUpdatePollInterval = 500 * time.Millisecond
	newUpdaterExecutor    = func() updaterExecutor { return systemdUpdaterExecutor{} }
	downloadUpdaterScript = downloadVerifiedInstallScript
	ensureUpdaterPaths    = ensureUpdaterWritablePathDirs
)

type AppUpdateResult struct {
	ExitCode int `json:"exit_code"`
}

type appUpdateRequest struct {
	operationID string
	version     string
	fingerprint string
}

func appUpdateTaskIdentity(payload apischema.AppUpdateRequest) (bridgeipc.TaskIdentity, error) {
	req, err := parseAppUpdateRequest(payload)
	if err != nil {
		return bridgeipc.TaskIdentity{}, err
	}
	return bridgeipc.TaskIdentity{ID: req.operationID, Fingerprint: req.fingerprint}, nil
}

func runAppUpdateTask(ctx context.Context, task *bridgeipc.Task, payload apischema.AppUpdateRequest) (AppUpdateResult, error) {
	req, err := parseAppUpdateRequest(payload)
	if err != nil {
		return AppUpdateResult{}, err
	}
	owner := task.Owner()
	if owner.Username == "" || owner.SessionID == "" {
		return AppUpdateResult{}, bridgeipc.NewError("durable update requires an authenticated owner", 403)
	}

	store := durabletask.NewStore(appUpdateStoreRoot)
	if ctx.Err() != nil {
		return cancelClaimedAppUpdate(store, newUpdaterExecutor(), req.operationID, owner.UID)
	}
	record, created, err := store.Claim(ctx, durabletask.Claim{
		ID:                 req.operationID,
		Route:              routeAppUpdate,
		UID:                owner.UID,
		RequestFingerprint: req.fingerprint,
		Target:             req.version,
		ExclusiveRoute:     true,
	})
	if errors.Is(err, durabletask.ErrConflict) {
		return AppUpdateResult{}, bridgeipc.NewError("operation ID was already used for another update request", 409)
	}
	if errors.Is(err, durabletask.ErrActive) {
		return AppUpdateResult{}, bridgeipc.NewError("another app update is already active", 409)
	}
	if err != nil {
		return AppUpdateResult{}, fmt.Errorf("claim durable app update: %w", err)
	}

	slog.Info("app update task claimed",
		"component", "control",
		"subsystem", "app_update",
		"operation_id", req.operationID,
		"uid", owner.UID,
		"state", record.State,
		"created", created)

	if record.Terminal() {
		return appUpdateResultFromRecord(record)
	}
	if record.State != durabletask.StateQueued {
		if executorErr := validateAppUpdateExecutor(record); executorErr != nil {
			unknown, updateErr := unknownAppUpdateRecord(ctx, store, record, executorErr.Error())
			if updateErr != nil {
				return AppUpdateResult{}, updateErr
			}
			return appUpdateResultFromRecord(unknown)
		}
	}

	executor := newUpdaterExecutor()
	if record.State == durabletask.StateQueued {
		record, err = prepareAndLaunchAppUpdate(ctx, task, store, executor, record, req)
		if err != nil {
			if ctx.Err() != nil {
				return cancelClaimedAppUpdate(store, executor, req.operationID, owner.UID)
			}
			return AppUpdateResult{}, err
		}
	}
	return observeAppUpdate(ctx, task, store, executor, record)
}

func cancelClaimedAppUpdate(store *durabletask.Store, executor updaterExecutor, id string, uid uint32) (AppUpdateResult, error) {
	ctx, cancel := durabletask.DetachedContext(updaterStopTimeout)
	defer cancel()
	record, err := store.Get(ctx, id, uid)
	if errors.Is(err, durabletask.ErrNotFound) {
		return AppUpdateResult{}, context.Canceled
	}
	if err != nil {
		return AppUpdateResult{}, err
	}
	if record.Terminal() {
		return appUpdateResultFromRecord(record)
	}
	if record.State == durabletask.StateQueued {
		return markQueuedAppUpdateCanceled(ctx, store, record)
	}
	if err := validateAppUpdateExecutor(record); err != nil {
		unknown, updateErr := unknownAppUpdateRecord(ctx, store, record, err.Error())
		if updateErr != nil {
			return AppUpdateResult{}, updateErr
		}
		return appUpdateResultFromRecord(unknown)
	}
	return cancelAppUpdate(store, executor, record)
}

func markQueuedAppUpdateCanceled(ctx context.Context, store *durabletask.Store, record durabletask.Record) (AppUpdateResult, error) {
	finished := time.Now().UTC()
	_, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateCanceled
		current.CancelRequestedAt = &finished
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 499, Message: "operation canceled"}
		current.AppendProgress(finished, "canceled", "The update was canceled before executor launch")
		return nil
	})
	if err != nil {
		return AppUpdateResult{}, err
	}
	return AppUpdateResult{}, context.Canceled
}

func validateAppUpdateExecutor(record durabletask.Record) error {
	if record.Executor.Kind != appUpdateExecutorKind ||
		record.Executor.Identity != appUpdateExecutorIdentity ||
		record.Executor.Handle != appUpdateUnitName(record.ID) {
		return errors.New("durable app update has an unexpected executor identity")
	}
	return nil
}

func parseAppUpdateRequest(payload apischema.AppUpdateRequest) (appUpdateRequest, error) {
	if err := durabletask.ValidateID(payload.RunID); err != nil {
		return appUpdateRequest{}, bridgeipc.NewError(err.Error(), 400)
	}

	version := ""
	if payload.Version != nil {
		version = strings.TrimSpace(*payload.Version)
		if !validReleaseVersionRE.MatchString(version) {
			return appUpdateRequest{}, bridgeipc.NewError("invalid update version", 400)
		}
	}
	safeRequest := "version=latest"
	if version != "" {
		safeRequest = "version=" + version
	}
	return appUpdateRequest{
		operationID: payload.RunID,
		version:     version,
		fingerprint: durabletask.Fingerprint(routeAppUpdate, safeRequest),
	}, nil
}

func resolveAppUpdateVersion(ctx context.Context, req appUpdateRequest) (string, error) {
	if req.version != "" {
		return req.version, nil
	}

	latest, err := fetchLatestVersion(ctx)
	if err != nil {
		return "", bridgeipc.NewError(fmt.Sprintf("failed to fetch latest version: %v", err), 500)
	}
	if !validReleaseVersionRE.MatchString(latest) {
		return "", bridgeipc.NewError("latest release has an invalid version tag", 500)
	}
	slog.Info("resolved latest app version", "component", "control", "subsystem", "app_update", "operation_id", req.operationID, "version", latest)
	return latest, nil
}

func prepareAndLaunchAppUpdate(
	ctx context.Context,
	task *bridgeipc.Task,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
	req appUpdateRequest,
) (durabletask.Record, error) {
	version, err := appUpdateTarget(ctx, record, req)
	if err != nil {
		return failAppUpdateRecord(ctx, store, record, 500, err.Error())
	}
	record, err = updateAppUpdateProgress(ctx, task, store, record, "preparing", "Downloading and verifying the install script", func(current *durabletask.Record) {
		current.Target = version
	})
	if err != nil {
		return durabletask.Record{}, err
	}
	scriptPath, resultPath, err := prepareAppUpdateArtifacts(ctx, store, record, version)
	if err != nil {
		return failAppUpdateRecord(ctx, store, record, 500, err.Error())
	}
	unitName := appUpdateUnitName(record.ID)
	description := appUpdateUnitDescription(record.ID, record.UID)
	record, err = updateAppUpdateProgress(ctx, task, store, record, "launching", "Starting the systemd update executor", func(current *durabletask.Record) {
		current.State = durabletask.StateLaunching
		current.Executor = durabletask.Executor{
			Kind:     appUpdateExecutorKind,
			Handle:   unitName,
			Identity: appUpdateExecutorIdentity,
		}
	})
	if err != nil {
		return durabletask.Record{}, err
	}

	launch := updaterLaunch{
		OperationID:   record.ID,
		UID:           record.UID,
		Unit:          unitName,
		Description:   description,
		ScriptPath:    scriptPath,
		ResultPath:    resultPath,
		InstallerArgs: scriptArgs(version),
		RestartAfter:  true,
	}
	startErr := executor.Start(ctx, launch)
	if ctx.Err() != nil {
		_, cancelErr := cancelAppUpdate(store, executor, record)
		return record, cancelErr
	}
	if startErr != nil {
		return reconcileAppUpdateStart(ctx, task, store, executor, record, launch, startErr)
	}
	running, err := markAppUpdateRunning(ctx, task, store, record, "Update executor accepted by systemd")
	if err != nil && ctx.Err() != nil {
		_, cancelErr := cancelAppUpdate(store, executor, record)
		return record, errors.Join(err, cancelErr)
	}
	return running, err
}

func appUpdateTarget(ctx context.Context, record durabletask.Record, req appUpdateRequest) (string, error) {
	if record.Target != "" {
		return record.Target, nil
	}
	return resolveAppUpdateVersion(ctx, req)
}

func prepareAppUpdateArtifacts(ctx context.Context, store *durabletask.Store, record durabletask.Record, version string) (string, string, error) {
	script, err := downloadUpdaterScript(ctx, version)
	if err != nil {
		return "", "", err
	}
	if ensureErr := ensureUpdaterPaths(); ensureErr != nil {
		return "", "", ensureErr
	}
	scriptPath, err := store.WriteArtifact(record.ID, appUpdateInstallerName, script, 0o700)
	if err != nil {
		return "", "", err
	}
	resultPath, err := store.ExecutorResultPath(record.ID)
	return scriptPath, resultPath, err
}

func reconcileAppUpdateStart(
	ctx context.Context,
	task *bridgeipc.Task,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
	launch updaterLaunch,
	startErr error,
) (durabletask.Record, error) {
	state, inspectErr := executor.Inspect(ctx, launch.Unit, launch.Description)
	if inspectErr != nil {
		if errors.Is(inspectErr, errUpdaterUnitNotFound) {
			return failAppUpdateRecord(ctx, store, record, 500, startErr.Error())
		}
		unknown, unknownErr := unknownAppUpdateRecord(ctx, store, record, fmt.Sprintf("transient-unit start outcome is unknown: %v", startErr))
		if unknownErr != nil {
			return durabletask.Record{}, unknownErr
		}
		_, terminalErr := appUpdateResultFromRecord(unknown)
		return unknown, terminalErr
	}
	if updaterUnitActive(state) {
		return markAppUpdateRunning(ctx, task, store, record, "Adopted the systemd update executor after an uncertain start")
	}
	_, stopErr := reconcileStoppedUpdater(ctx, task, store, executor, record, state)
	terminal, getErr := store.Get(ctx, record.ID, record.UID)
	if getErr != nil {
		return durabletask.Record{}, errors.Join(stopErr, getErr)
	}
	return terminal, stopErr
}

func markAppUpdateRunning(ctx context.Context, task *bridgeipc.Task, store *durabletask.Store, record durabletask.Record, message string) (durabletask.Record, error) {
	now := time.Now().UTC()
	return updateAppUpdateProgress(ctx, task, store, record, "running", message, func(current *durabletask.Record) {
		current.State = durabletask.StateRunning
		if current.StartedAt == nil {
			current.StartedAt = &now
		}
	})
}

func observeAppUpdate(
	ctx context.Context,
	task *bridgeipc.Task,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
) (AppUpdateResult, error) {
	ticker := time.NewTicker(appUpdatePollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return cancelAppUpdate(store, executor, record)
		}
		result, done, err := reconcileAppUpdate(ctx, task, store, executor, record)
		if done || err != nil {
			return result, err
		}
		select {
		case <-ctx.Done():
			return cancelAppUpdate(store, executor, record)
		case <-ticker.C:
			current, getErr := store.Get(ctx, record.ID, record.UID)
			if getErr != nil {
				return AppUpdateResult{}, fmt.Errorf("reload durable app update: %w", getErr)
			}
			record = current
		}
	}
}

func reconcileAppUpdate(
	ctx context.Context,
	task *bridgeipc.Task,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
) (AppUpdateResult, bool, error) {
	if result, handled, err := reconcileExecutorResult(ctx, store, executor, record); handled {
		return result, true, err
	}

	state, err := executor.Inspect(ctx, record.Executor.Handle, appUpdateUnitDescription(record.ID, record.UID))
	if err != nil {
		return reconcileAppUpdateInspectionError(ctx, store, record, err)
	}
	if updaterUnitActive(state) {
		if record.State == durabletask.StateLaunching {
			_, updateErr := markAppUpdateRunning(ctx, task, store, record, "Recovered the active systemd update executor")
			if updateErr != nil {
				return AppUpdateResult{}, false, updateErr
			}
		}
		return AppUpdateResult{}, false, nil
	}
	returnResult, returnErr := reconcileStoppedUpdater(ctx, task, store, executor, record, state)
	return returnResult, true, returnErr
}

func reconcileExecutorResult(
	ctx context.Context,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
) (AppUpdateResult, bool, error) {
	result, err := store.ReadExecutorResult(record.ID)
	if errors.Is(err, durabletask.ErrNotFound) {
		return AppUpdateResult{}, false, nil
	}
	if err != nil {
		unknown, unknownErr := unknownAppUpdateRecord(ctx, store, record, err.Error())
		if unknownErr != nil {
			return AppUpdateResult{}, true, unknownErr
		}
		terminalResult, terminalErr := appUpdateResultFromRecord(unknown)
		return terminalResult, true, terminalErr
	}
	terminal, err := store.ApplyExecutorResult(ctx, record.UID, result)
	if err != nil {
		return AppUpdateResult{}, true, err
	}
	executor.Collect(ctx, record.Executor.Handle)
	terminalResult, terminalErr := appUpdateResultFromRecord(terminal)
	return terminalResult, true, terminalErr
}

func reconcileAppUpdateInspectionError(
	ctx context.Context,
	store *durabletask.Store,
	record durabletask.Record,
	inspectErr error,
) (AppUpdateResult, bool, error) {
	if !errors.Is(inspectErr, errUpdaterUnitNotFound) {
		return AppUpdateResult{}, false, inspectErr
	}
	if record.State == durabletask.StateLaunching {
		failed, failErr := failAppUpdateRecord(ctx, store, record, 500, "systemd did not retain the interrupted updater launch")
		if failErr != nil {
			return AppUpdateResult{}, true, failErr
		}
		terminalResult, terminalErr := appUpdateResultFromRecord(failed)
		return terminalResult, true, terminalErr
	}
	unknown, unknownErr := unknownAppUpdateRecord(ctx, store, record, "updater unit disappeared before a typed result was recorded")
	if unknownErr != nil {
		return AppUpdateResult{}, true, unknownErr
	}
	terminalResult, terminalErr := appUpdateResultFromRecord(unknown)
	return terminalResult, true, terminalErr
}

func reconcileStoppedUpdater(
	ctx context.Context,
	task *bridgeipc.Task,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
	state updaterUnitState,
) (AppUpdateResult, error) {
	result, err := store.ReadExecutorResult(record.ID)
	if err == nil {
		terminal, applyErr := store.ApplyExecutorResult(ctx, record.UID, result)
		if applyErr != nil {
			return AppUpdateResult{}, applyErr
		}
		executor.Collect(ctx, record.Executor.Handle)
		return appUpdateResultFromRecord(terminal)
	}
	if !errors.Is(err, durabletask.ErrNotFound) {
		unknown, unknownErr := unknownAppUpdateRecord(ctx, store, record, err.Error())
		if unknownErr != nil {
			return AppUpdateResult{}, unknownErr
		}
		return appUpdateResultFromRecord(unknown)
	}

	message := fmt.Sprintf("updater stopped without a typed result (systemd result=%s, exit=%d)", state.ServiceResult, state.ExitCode)
	if journal := systemdUnitJournalTail(ctx, record.Executor.Handle); journal != "" {
		message += ": " + journal
	}
	if state.ServiceResult == "success" && state.ExitCode == 0 {
		unknown, unknownErr := unknownAppUpdateRecord(ctx, store, record, message)
		if unknownErr != nil {
			return AppUpdateResult{}, unknownErr
		}
		return appUpdateResultFromRecord(unknown)
	}
	failed, failErr := failAppUpdateRecord(ctx, store, record, state.ExitCode, message)
	if failErr != nil {
		return AppUpdateResult{}, failErr
	}
	if task != nil {
		task.ReportData("ERROR: " + message + "\n")
	}
	executor.Collect(ctx, record.Executor.Handle)
	return appUpdateResultFromRecord(failed)
}

func cancelAppUpdate(store *durabletask.Store, executor updaterExecutor, record durabletask.Record) (AppUpdateResult, error) {
	stopCtx, cancel := durabletask.DetachedContext(updaterStopTimeout)
	defer cancel()
	now := time.Now().UTC()
	updated, err := store.Update(stopCtx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.CancelRequestedAt = &now
		current.AppendProgress(now, "canceling", "Waiting for systemd to stop the update executor")
		return nil
	})
	if err != nil {
		return AppUpdateResult{}, err
	}
	if stopErr := executor.Stop(stopCtx, updated.Executor.Handle); stopErr != nil && !errors.Is(stopErr, errUpdaterUnitNotFound) {
		unknown, unknownErr := unknownAppUpdateRecord(stopCtx, store, updated, fmt.Sprintf("failed to confirm updater cancellation: %v", stopErr))
		if unknownErr != nil {
			return AppUpdateResult{}, unknownErr
		}
		return appUpdateResultFromRecord(unknown)
	}
	return waitForAppUpdateCancellation(stopCtx, store, executor, updated)
}

func waitForAppUpdateCancellation(ctx context.Context, store *durabletask.Store, executor updaterExecutor, record durabletask.Record) (AppUpdateResult, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return persistCancellationTimeout(store, record)
		}
		result, done, err := reconcileAppUpdateCancellation(ctx, store, executor, record)
		if done || err != nil {
			return result, err
		}
		select {
		case <-ctx.Done():
			return persistCancellationTimeout(store, record)
		case <-ticker.C:
		}
	}
}

func reconcileAppUpdateCancellation(
	ctx context.Context,
	store *durabletask.Store,
	executor updaterExecutor,
	record durabletask.Record,
) (AppUpdateResult, bool, error) {
	if result, handled, err := reconcileExecutorResult(ctx, store, executor, record); handled {
		return result, true, err
	}
	state, inspectErr := executor.Inspect(ctx, record.Executor.Handle, appUpdateUnitDescription(record.ID, record.UID))
	if errors.Is(inspectErr, errUpdaterUnitNotFound) || (inspectErr == nil && !updaterUnitActive(state)) {
		return markAppUpdateCanceled(ctx, store, executor, record)
	}
	if inspectErr == nil {
		return AppUpdateResult{}, false, nil
	}
	unknown, unknownErr := unknownAppUpdateRecord(ctx, store, record, inspectErr.Error())
	if unknownErr != nil {
		return AppUpdateResult{}, true, unknownErr
	}
	result, resultErr := appUpdateResultFromRecord(unknown)
	return result, true, resultErr
}

func markAppUpdateCanceled(ctx context.Context, store *durabletask.Store, executor updaterExecutor, record durabletask.Record) (AppUpdateResult, bool, error) {
	finished := time.Now().UTC()
	canceled, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateCanceled
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 499, Message: "operation canceled"}
		current.AppendProgress(finished, "canceled", "The systemd update executor stopped")
		return nil
	})
	if err != nil {
		return AppUpdateResult{}, true, err
	}
	executor.Collect(ctx, canceled.Executor.Handle)
	return AppUpdateResult{}, true, context.Canceled
}

func persistCancellationTimeout(store *durabletask.Store, record durabletask.Record) (AppUpdateResult, error) {
	persistCtx, cancel := durabletask.DetachedContext(10 * time.Second)
	defer cancel()
	unknown, err := unknownAppUpdateRecord(persistCtx, store, record, "timed out waiting for systemd to confirm cancellation")
	if err != nil {
		return AppUpdateResult{}, err
	}
	return appUpdateResultFromRecord(unknown)
}

func updateAppUpdateProgress(
	ctx context.Context,
	task *bridgeipc.Task,
	store *durabletask.Store,
	record durabletask.Record,
	phase string,
	message string,
	mutate func(*durabletask.Record),
) (durabletask.Record, error) {
	now := time.Now().UTC()
	updated, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		if mutate != nil {
			mutate(current)
		}
		current.AppendProgress(now, phase, message)
		return nil
	})
	if err == nil && task != nil {
		task.ReportProgress(map[string]any{"phase": phase, "message": message})
		task.ReportData(message + "\n")
	}
	return updated, err
}

func failAppUpdateRecord(ctx context.Context, store *durabletask.Store, record durabletask.Record, code int, message string) (durabletask.Record, error) {
	finished := time.Now().UTC()
	message = boundedOperationMessage(message)
	updated, err := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateFailed
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: code, Message: message}
		current.AppendProgress(finished, "failed", message)
		return nil
	})
	if err != nil {
		return durabletask.Record{}, err
	}
	return updated, bridgeipc.NewError(message, code)
}

func unknownAppUpdateRecord(ctx context.Context, store *durabletask.Store, record durabletask.Record, message string) (durabletask.Record, error) {
	finished := time.Now().UTC()
	message = boundedOperationMessage(message)
	return store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateUnknown
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 500, Message: message}
		current.AppendProgress(finished, "unknown", message)
		return nil
	})
}

func appUpdateResultFromRecord(record durabletask.Record) (AppUpdateResult, error) {
	var result AppUpdateResult
	if len(record.Result) > 0 {
		if err := json.Unmarshal(record.Result, &result); err != nil {
			return AppUpdateResult{}, fmt.Errorf("decode durable app update result: %w", err)
		}
	}
	switch record.State {
	case durabletask.StateCompleted:
		return result, nil
	case durabletask.StateCanceled:
		return AppUpdateResult{}, context.Canceled
	case durabletask.StateFailed, durabletask.StateUnknown:
		if record.Error != nil {
			return AppUpdateResult{}, bridgeipc.NewError(record.Error.Message, record.Error.Code)
		}
		return AppUpdateResult{}, bridgeipc.NewError("durable app update did not complete", 500)
	default:
		return AppUpdateResult{}, bridgeipc.NewError("durable app update is still active", 409)
	}
}

func updaterUnitActive(state updaterUnitState) bool {
	return state.ActiveState == "active" || state.ActiveState == "activating" || state.ActiveState == "reloading" || state.ActiveState == "deactivating"
}

func boundedOperationMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		return message[:1024]
	}
	if message == "" {
		return "durable app update failed"
	}
	return message
}

func recoverAppUpdates(rt runtime.Runtime, router *bridgeipc.Router) {
	if rt.Session == nil || rt.Session.User.Username == "" {
		return
	}
	ctx, cancel := durabletask.DetachedContext(15 * time.Second)
	defer cancel()
	owner := bridgeipc.TaskOwner{
		SessionID: rt.Session.SessionID,
		Username:  rt.Session.User.Username,
		UID:       rt.Session.User.UID,
	}
	store := durabletask.NewStore(appUpdateStoreRoot)
	records, err := store.ListActiveForUID(ctx, owner.UID)
	if err != nil {
		slog.Error("failed to list durable app updates", "component", "control", "subsystem", "app_update", "uid", owner.UID, "error", err)
		return
	}
	for _, record := range records {
		if record.Route == routeAppUpdate {
			recoverAppUpdateTask(router, owner, record)
		}
	}
}

func recoverAppUpdateTask(router *bridgeipc.Router, owner bridgeipc.TaskOwner, record durabletask.Record) {
	request := appUpdateRequestFromRecord(record)
	_, _, err := router.RecoverDurableTask(routeAppUpdate, request, owner, bridgeipc.TaskIdentity{
		ID:          record.ID,
		Fingerprint: record.RequestFingerprint,
	})
	if err != nil {
		slog.Warn("failed to reattach durable app update", "component", "control", "subsystem", "app_update", "operation_id", record.ID, "error", err)
	}
}

func appUpdateRequestFromRecord(record durabletask.Record) apischema.AppUpdateRequest {
	request := apischema.AppUpdateRequest{RunID: record.ID}
	if record.RequestFingerprint != durabletask.Fingerprint(routeAppUpdate, "version=latest") && record.Target != "" {
		request.Version = new(record.Target)
	}
	return request
}
