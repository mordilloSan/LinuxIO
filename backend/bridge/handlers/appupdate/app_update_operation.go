package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	routeAppUpdate   = "control.app_update"
	updateStatusPath = "/run/linuxio/update-status.json"
)

var validRunIDRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type updateStatus struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

type appUpdateRequest struct {
	runID   string
	version string
}

func runAppUpdateJob(ctx context.Context, rt runtime.Runtime, job *bridgeipc.Job, payload apischema.AppUpdateRequest) (any, error) {
	req, err := parseAppUpdateRequest(payload)
	if err != nil {
		return nil, err
	}

	version, err := resolveAppUpdateVersion(ctx, req)
	if err != nil {
		return nil, err
	}

	return executeAppUpdate(ctx, rt, job, req.runID, version)
}

func parseAppUpdateRequest(payload apischema.AppUpdateRequest) (appUpdateRequest, error) {
	if payload.RunID == "" {
		return appUpdateRequest{}, bridgeipc.NewError("missing run_id argument", 400)
	}

	runID := payload.RunID
	if !validRunIDRE.MatchString(runID) {
		return appUpdateRequest{}, bridgeipc.NewError("invalid run_id format", 400)
	}

	req := appUpdateRequest{runID: runID}
	if payload.Version != nil {
		req.version = *payload.Version
	}
	return req, nil
}

func resolveAppUpdateVersion(ctx context.Context, req appUpdateRequest) (string, error) {
	if req.version != "" {
		return req.version, nil
	}

	latest, err := fetchLatestVersion(ctx)
	if err != nil {
		return "", bridgeipc.NewError(fmt.Sprintf("failed to fetch latest version: %v", err), 500)
	}
	slog.Info("resolved latest app version", "component", "control", "subsystem", "app_update", "run_id", req.runID, "version", latest)
	return latest, nil
}

func executeAppUpdate(ctx context.Context, rt runtime.Runtime, job *bridgeipc.Job, runID, version string) (any, error) {
	slog.Info("app update job starting", "component", "control", "subsystem", "app_update", "route", routeAppUpdate, "run_id", runID, "version", version, "user", rt.Username())

	startedAt := time.Now().Unix()
	writeUpdateStatusWithLog(runID, "running", nil, "", startedAt, 0, "initial")
	relay := &jobOutputWriter{job: job}

	_, _ = fmt.Fprintf(relay, "Downloading and verifying install script for %s...\n", version)
	err := runInstallScript(ctx, version, relay)
	finishedAt := time.Now().Unix()

	if isAppUpdateCanceled(ctx, err) {
		return nil, finishCanceledUpdate(runID, startedAt, finishedAt)
	}
	if err != nil {
		return nil, finishFailedUpdate(runID, version, relay, startedAt, finishedAt, err)
	}

	finishSuccessfulUpdate(runID, relay, startedAt, finishedAt)
	reloadAndRestartAfterUpdate(runID)
	return map[string]any{"exit_code": 0}, nil
}

func isAppUpdateCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
}

func finishCanceledUpdate(runID string, startedAt, finishedAt int64) error {
	exitCode := 499
	errMsg := "operation aborted"
	writeUpdateStatusWithLog(runID, "canceled", &exitCode, errMsg, startedAt, finishedAt, "canceled")
	return context.Canceled
}

func finishFailedUpdate(runID, version string, relay *jobOutputWriter, startedAt, finishedAt int64, err error) error {
	exitCode := 1
	errMsg := err.Error()
	// Embed the error in the MESSAGE field so it shows up under `journalctl -o cat`,
	// and keep the structured `error=` field for verbose/JSON consumers.
	slog.Error(fmt.Sprintf("app update install script failed: %v", err), "component", "control", "subsystem", "app_update", "run_id", runID, "version", version, "error", err)
	_, _ = fmt.Fprintf(relay, "ERROR: %s\n", errMsg)
	writeUpdateStatusWithLog(runID, "error", &exitCode, errMsg, startedAt, finishedAt, "final")
	return bridgeipc.NewError(fmt.Sprintf("update failed: %s", errMsg), exitCode)
}

func finishSuccessfulUpdate(runID string, relay *jobOutputWriter, startedAt, finishedAt int64) {
	exitCode := 0
	writeUpdateStatusWithLog(runID, "ok", &exitCode, "", startedAt, finishedAt, "final")
	_, _ = fmt.Fprintf(relay, "Installation complete\n")
}

func reloadAndRestartAfterUpdate(runID string) {
	ctx, cancel := detachedPostUpdateContext()
	slog.Debug("reloading systemd daemon", "component", "control", "subsystem", "app_update", "run_id", runID)
	if reloadErr := systemdapi.DaemonReload(ctx); reloadErr != nil {
		slog.Warn("systemd daemon-reload failed", "component", "control", "subsystem", "app_update", "run_id", runID, "error", reloadErr)
	}
	slog.Info("scheduling service restart", "component", "control", "subsystem", "app_update", "run_id", runID)
	go func() {
		defer cancel()
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			slog.Error("service restart context expired before restart", "component", "control", "subsystem", "app_update", "run_id", runID, "error", ctx.Err())
			return
		}
		if restartErr := restartService(ctx); restartErr != nil {
			slog.Error("failed to restart service after update", "component", "control", "subsystem", "app_update", "run_id", runID, "error", restartErr)
		}
	}()
}

// detachedPostUpdateContext bounds the intentionally detached restart path:
// after a successful update the current job can finish before the service restarts.
func detachedPostUpdateContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func writeUpdateStatusWithLog(runID, status string, exitCode *int, errMsg string, startedAt, finishedAt int64, phase string) {
	if err := writeStatusFile(runID, status, exitCode, errMsg, startedAt, finishedAt); err != nil {
		slog.Warn("failed to write update status file", "component", "control", "subsystem", "app_update", "run_id", runID, "path", updateStatusPath, "phase", phase, "error", err)
	}
}

// jobOutputWriter writes process output as transient job data events.
// Safe for concurrent use by multiple goroutines (stdout + stderr).
type jobOutputWriter struct {
	mu  sync.Mutex
	job *bridgeipc.Job
}

func (r *jobOutputWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.job.ReportData(string(p))
	return len(p), nil
}

func writeStatusFile(runID, status string, exitCode *int, errMsg string, startedAt, finishedAt int64) error {
	s := updateStatus{
		ID:        runID,
		Status:    status,
		Error:     errMsg,
		StartedAt: startedAt,
	}
	if exitCode != nil {
		s.ExitCode = exitCode
	}
	if finishedAt > 0 {
		s.FinishedAt = finishedAt
	}

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	return utils.WriteFileAtomic(updateStatusPath, append(data, '\n'), 0o644)
}
