package control

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	streamTypeAppUpdate = "app-update"
	updateStatusPath    = "/run/linuxio/update-status.json"
)

var validRunIDRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type updateStatus struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

// RegisterStreamHandlers registers the app-update stream handler.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[streamTypeAppUpdate] = HandleAppUpdateStream
}

// HandleAppUpdateStream handles streaming app update with verified install script.
// args: [runId] or [runId, version]
func HandleAppUpdateStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) == 0 {
		return writeUpdateError(stream, "missing run_id argument", 400)
	}

	runID := args[0]
	if !validRunIDRE.MatchString(runID) {
		return writeUpdateError(stream, "invalid run_id format", 400)
	}

	var version string
	if len(args) > 1 {
		version = args[1]
	}
	slog.Info("app update stream starting", "component", "control", "subsystem", "app_update", "stream_id", runID, "version", version, "user", sess.User.Username)

	// Resolve version if not specified
	if version == "" {
		latest, err := fetchLatestVersion()
		if err != nil {
			return writeUpdateError(stream, fmt.Sprintf("failed to fetch latest version: %v", err), 500)
		}
		version = latest
		slog.Info("resolved latest app version", "component", "control", "subsystem", "app_update", "stream_id", runID, "version", version)
	}

	startedAt := time.Now().Unix()

	// Write initial status file
	if err := writeStatusFile(runID, "running", nil, startedAt, 0); err != nil {
		slog.Warn("failed to write initial update status file", "component", "control", "subsystem", "app_update", "stream_id", runID, "path", updateStatusPath, "error", err)
	}

	// Create a relay writer that sends OpStreamData frames
	relay := &streamRelay{stream: stream}

	// Send initial progress
	_, _ = fmt.Fprintf(relay, "Downloading and verifying install script for %s...\n", version)

	// Run the verified install script with output relayed to the frontend
	err := runInstallScript(version, relay)

	finishedAt := time.Now().Unix()
	exitCode := 0
	if err != nil {
		exitCode = 1
		slog.Error("app update install script failed", "component", "control", "subsystem", "app_update", "stream_id", runID, "version", version, "error", err)
	}

	// Write final status file
	status := "ok"
	if exitCode != 0 {
		status = "error"
	}
	if writeErr := writeStatusFile(runID, status, &exitCode, startedAt, finishedAt); writeErr != nil {
		slog.Warn("failed to write final update status file", "component", "control", "subsystem", "app_update", "stream_id", runID, "path", updateStatusPath, "error", writeErr)
	}

	// Send result frame
	if exitCode == 0 {
		_, _ = fmt.Fprintf(relay, "Installation complete\n")
		if writeErr := ipc.WriteResultOKAndClose(stream, 0, map[string]any{"exit_code": 0}); writeErr != nil {
			slog.Debug("failed to write ok+close frame", "component", "control", "subsystem", "app_update", "stream_id", runID, "error", writeErr)
		}
	} else {
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("update failed: %v", err), exitCode); writeErr != nil {
			slog.Debug("failed to write error+close frame", "component", "control", "subsystem", "app_update", "stream_id", runID, "error", writeErr)
		}
		return nil
	}
	// Success path: daemon-reload and schedule service restart
	slog.Debug("reloading systemd daemon", "component", "control", "subsystem", "app_update", "stream_id", runID)
	if reloadErr := systemdapi.DaemonReload(); reloadErr != nil {
		slog.Warn("systemd daemon-reload failed", "component", "control", "subsystem", "app_update", "stream_id", runID, "error", reloadErr)
	}
	slog.Info("scheduling service restart", "component", "control", "subsystem", "app_update", "stream_id", runID)
	go func() {
		time.Sleep(500 * time.Millisecond)
		if restartErr := restartService(); restartErr != nil {
			slog.Error("failed to restart service after update", "component", "control", "subsystem", "app_update", "stream_id", runID, "error", restartErr)
		}
	}()

	return nil
}

// streamRelay writes data as OpStreamData frames.
// Safe for concurrent use by multiple goroutines (stdout + stderr).
type streamRelay struct {
	mu     sync.Mutex
	stream net.Conn
}

func (r *streamRelay) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ipc.WriteRelayFrame(r.stream, &ipc.StreamFrame{
		Opcode:  ipc.OpStreamData,
		Payload: p,
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func writeStatusFile(runID, status string, exitCode *int, startedAt, finishedAt int64) error {
	s := updateStatus{
		ID:        runID,
		Status:    status,
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

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(updateStatusPath), 0o755); err != nil {
		return err
	}

	return writeFileAtomic(updateStatusPath, append(data, '\n'), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeUpdateError(stream net.Conn, message string, code int) error {
	if err := ipc.WriteResultErrorAndClose(stream, 0, message, code); err != nil {
		slog.Debug("failed to write error+close frame", "component", "control", "subsystem", "app_update", "error", err)
	}
	return fmt.Errorf("%s", message)
}
