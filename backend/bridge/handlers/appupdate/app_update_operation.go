package appupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/common/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/updatestatus"
)

const routeAppUpdate = "control.app_update"

var (
	validRunIDRE           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	validReleaseVersionRE  = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	runAppUpdateInstaller  = runInstallScript
	reloadSystemd          = systemdapi.DaemonReload
	restartSystemdUnit     = systemdapi.RestartUnit
	postUpdateRestartDelay = 500 * time.Millisecond
)

type AppUpdateResult struct {
	ExitCode int `json:"exit_code"`
}

type AppUpdateProgressDetail struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

func (p AppUpdateProgressDetail) ProgressEnvelope() bridgeipc.TaskProgress {
	return bridgeipc.TaskProgress{Phase: p.Phase, Message: p.Message, Detail: p}
}

type appUpdateRequest struct {
	runID   string
	version string
}

func runAppUpdateTask(ctx context.Context, rt runtime.Runtime, task *bridgeipc.Task, payload apischema.AppUpdateRequest) (AppUpdateResult, error) {
	req, err := parseAppUpdateRequest(payload)
	if err != nil {
		return AppUpdateResult{}, err
	}

	version, err := resolveAppUpdateVersion(ctx, req)
	if err != nil {
		return AppUpdateResult{}, err
	}

	return executeAppUpdate(ctx, rt, task, req.runID, version)
}

func parseAppUpdateRequest(payload apischema.AppUpdateRequest) (appUpdateRequest, error) {
	if !validRunIDRE.MatchString(payload.RunID) {
		return appUpdateRequest{}, fmt.Errorf("%w: run_id format", bridgeipc.ErrInvalidArgs)
	}

	req := appUpdateRequest{runID: payload.RunID}
	if payload.Version != nil {
		req.version = strings.TrimSpace(*payload.Version)
		if !validReleaseVersionRE.MatchString(req.version) {
			return appUpdateRequest{}, fmt.Errorf("%w: update version", bridgeipc.ErrInvalidArgs)
		}
	}
	return req, nil
}

func resolveAppUpdateVersion(ctx context.Context, req appUpdateRequest) (string, error) {
	if req.version != "" {
		return req.version, nil
	}

	latest, err := fetchLatestVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch latest version: %w", err)
	}
	if !validReleaseVersionRE.MatchString(latest) {
		return "", errors.New("latest release has an invalid version tag")
	}
	slog.Debug("resolved latest app version", "component", "control", "subsystem", "app_update", "run_id", req.runID, "version", latest)
	return latest, nil
}

func executeAppUpdate(ctx context.Context, rt runtime.Runtime, task *bridgeipc.Task, runID, version string) (AppUpdateResult, error) {
	slog.Debug("app update task starting", "component", "control", "subsystem", "app_update", "route", routeAppUpdate, "run_id", runID, "version", version, "user", rt.Username())

	ownerUID := task.Owner().UID
	startedAt := time.Now().Unix()
	relay := &taskOutputWriter{task: task}
	if err := writeStatusFile(runID, ownerUID, updatestatus.StatusRunning, nil, "", startedAt, 0); err != nil {
		return AppUpdateResult{}, fmt.Errorf("persist initial update status: %w", err)
	}

	reportAppUpdateProgress(task, relay, "preparing", fmt.Sprintf("Downloading and verifying install script for %s...", version))
	err := runAppUpdateInstaller(ctx, version, relay)
	finishedAt := time.Now().Unix()

	if isAppUpdateCanceled(ctx, err) {
		return AppUpdateResult{}, finishCanceledUpdate(runID, ownerUID, startedAt, finishedAt)
	}
	if err != nil {
		return AppUpdateResult{}, finishFailedUpdate(runID, ownerUID, version, relay, startedAt, finishedAt, err)
	}
	if reloadErr := reloadSystemd(ctx); reloadErr != nil {
		return AppUpdateResult{}, finishFailedUpdate(runID, ownerUID, version, relay, startedAt, finishedAt, fmt.Errorf("reload systemd after update: %w", reloadErr))
	}

	if err := finishSuccessfulUpdate(runID, ownerUID, task, relay, startedAt, finishedAt); err != nil {
		return AppUpdateResult{}, err
	}
	slog.Info("app updated", "component", "control", "subsystem", "app_update", "run_id", runID, "version", version)
	reloadAndRestartAfterUpdate(runID, task.Done())
	return AppUpdateResult{ExitCode: 0}, nil
}

func isAppUpdateCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
}

func finishCanceledUpdate(runID string, ownerUID uint32, startedAt, finishedAt int64) error {
	exitCode := 499
	errMsg := "operation aborted"
	writeUpdateStatusWithLog(runID, ownerUID, updatestatus.StatusError, &exitCode, errMsg, startedAt, finishedAt, "canceled")
	return context.Canceled
}

func finishFailedUpdate(runID string, ownerUID uint32, version string, relay *taskOutputWriter, startedAt, finishedAt int64, err error) error {
	exitCode := 1
	errMsg := err.Error()
	_, _ = fmt.Fprintf(relay, "ERROR: %s\n", errMsg)
	writeUpdateStatusWithLog(runID, ownerUID, updatestatus.StatusError, &exitCode, errMsg, startedAt, finishedAt, "final")
	return bridgeipc.NewError(fmt.Sprintf("update to %s failed (run %s): %s", version, runID, errMsg), exitCode)
}

func finishSuccessfulUpdate(runID string, ownerUID uint32, task *bridgeipc.Task, relay *taskOutputWriter, startedAt, finishedAt int64) error {
	exitCode := 0
	if err := writeStatusFile(runID, ownerUID, updatestatus.StatusOK, &exitCode, "", startedAt, finishedAt); err != nil {
		err = fmt.Errorf("persist completed update status: %w", err)
		_, _ = fmt.Fprintf(relay, "ERROR: %s\n", err)
		return err
	}
	reportAppUpdateProgress(task, relay, "installed", "Installation complete")
	return nil
}

func reloadAndRestartAfterUpdate(runID string, taskDone <-chan struct{}) {
	ctx, cancel := detachedPostUpdateContext()
	slog.Debug("scheduling service restart", "component", "control", "subsystem", "app_update", "run_id", runID)
	go func() {
		defer cancel()
		select {
		case <-taskDone:
		case <-ctx.Done():
			slog.Error("service restart context expired before Task completion", "component", "control", "subsystem", "app_update", "run_id", runID, "error", ctx.Err())
			return
		}
		timer := time.NewTimer(postUpdateRestartDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			slog.Error("service restart context expired before restart", "component", "control", "subsystem", "app_update", "run_id", runID, "error", ctx.Err())
			return
		}
		if restartErr := restartSystemdUnit(ctx, "linuxio.target"); restartErr != nil {
			slog.Error("failed to restart service after update", "component", "control", "subsystem", "app_update", "run_id", runID, "error", restartErr)
		}
	}()
}

func reportAppUpdateProgress(task *bridgeipc.Task, relay *taskOutputWriter, phase, message string) {
	task.ReportProgress(AppUpdateProgressDetail{Phase: phase, Message: message})
	_, _ = fmt.Fprintln(relay, message)
}

// detachedPostUpdateContext bounds the intentionally detached restart path:
// after a successful update the current Task can finish before the service restarts.
func detachedPostUpdateContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func writeUpdateStatusWithLog(runID string, ownerUID uint32, status string, exitCode *int, errMsg string, startedAt, finishedAt int64, phase string) {
	if err := writeStatusFile(runID, ownerUID, status, exitCode, errMsg, startedAt, finishedAt); err != nil {
		slog.Warn("failed to write update status file", "component", "control", "subsystem", "app_update", "run_id", runID, "path", updatestatus.Path, "phase", phase, "error", err)
	}
}

// taskOutputWriter writes process output as transient Task data events.
// Safe for concurrent use by multiple goroutines (stdout + stderr).
type taskOutputWriter struct {
	mu   sync.Mutex
	task *bridgeipc.Task
}

func (r *taskOutputWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.task.ReportData(string(p))
	return len(p), nil
}

func writeStatusFile(runID string, ownerUID uint32, status string, exitCode *int, errMsg string, startedAt, finishedAt int64) error {
	rec := updatestatus.Record{
		Version:   updatestatus.Version,
		ID:        runID,
		OwnerUID:  ownerUID,
		Status:    status,
		ExitCode:  exitCode,
		Error:     errMsg,
		StartedAt: &startedAt,
	}
	if finishedAt > 0 {
		rec.FinishedAt = &finishedAt
	}
	return updatestatus.Write(rec)
}
