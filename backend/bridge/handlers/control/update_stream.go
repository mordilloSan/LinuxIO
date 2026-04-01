package control

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/mordilloSan/go-logger/logger"

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

	logger.Infof("[app-update] starting: run_id=%s version=%q user=%s", runID, version, sess.User.Username)

	// Resolve version if not specified
	if version == "" {
		latest, err := fetchLatestVersion()
		if err != nil {
			return writeUpdateError(stream, fmt.Sprintf("failed to fetch latest version: %v", err), 500)
		}
		version = latest
		logger.Infof("[app-update] resolved latest version: %s", version)
	}

	startedAt := time.Now().Unix()

	// Write initial status file
	if err := writeStatusFile(runID, "running", nil, startedAt, 0); err != nil {
		logger.Warnf("[app-update] failed to write initial status file: %v", err)
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
		logger.Errorf("[app-update] install script failed: %v", err)
	}

	// Write final status file
	status := "ok"
	if exitCode != 0 {
		status = "error"
	}
	if writeErr := writeStatusFile(runID, status, &exitCode, startedAt, finishedAt); writeErr != nil {
		logger.Warnf("[app-update] failed to write final status file: %v", writeErr)
	}

	// Send result frame
	if exitCode == 0 {
		_, _ = fmt.Fprintf(relay, "Installation complete\n")
		if writeErr := ipc.WriteResultOKAndClose(stream, 0, map[string]any{"exit_code": 0}); writeErr != nil {
			logger.Debugf("[app-update] failed to write ok+close frame: %v", writeErr)
		}
	} else {
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("update failed: %v", err), exitCode); writeErr != nil {
			logger.Debugf("[app-update] failed to write error+close frame: %v", writeErr)
		}
		return nil
	}

	// Success path: daemon-reload and schedule service restart
	logger.Debugf("[app-update] reloading systemd daemon")
	if reloadErr := systemdapi.DaemonReload(); reloadErr != nil {
		logger.Warnf("[app-update] daemon-reload failed: %v (continuing anyway)", reloadErr)
	}

	logger.Infof("[app-update] scheduling service restart")
	go func() {
		time.Sleep(500 * time.Millisecond)
		if restartErr := restartService(); restartErr != nil {
			logger.Errorf("[app-update] failed to restart service: %v", restartErr)
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
		logger.Debugf("[app-update] failed to write error+close frame: %v", err)
	}
	return fmt.Errorf("%s", message)
}
