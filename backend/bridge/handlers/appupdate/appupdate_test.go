package appupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const testRunID = "00000000-0000-4000-8000-000000000042"

func TestBuildInstallCommandArgsUsesExplicitWritablePaths(t *testing.T) {
	args := buildInstallCommandArgs("linuxio-updater-test")

	var protectSystem string
	var readWritePaths string
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-p" {
			continue
		}
		switch {
		case strings.HasPrefix(args[i+1], "ProtectSystem="):
			protectSystem = args[i+1]
		case strings.HasPrefix(args[i+1], "ReadWritePaths="):
			readWritePaths = strings.TrimPrefix(args[i+1], "ReadWritePaths=")
		}
	}

	if protectSystem != "ProtectSystem=full" {
		t.Fatalf("ProtectSystem property = %q, want %q", protectSystem, "ProtectSystem=full")
	}
	if readWritePaths == "" {
		t.Fatal("missing ReadWritePaths property")
	}

	expectedPaths := []string{
		version.BinDir,
		"/etc/linuxio",
		"/etc/pam.d",
		"/etc/systemd/system",
		"-/etc/motd.d",
		"/usr/lib/tmpfiles.d",
		"/usr/share/linuxio",
		version.DataDir,
	}
	for _, path := range expectedPaths {
		if !strings.Contains(" "+readWritePaths+" ", " "+path+" ") {
			t.Fatalf("ReadWritePaths missing %q: %q", path, readWritePaths)
		}
	}
	if strings.Contains(" "+readWritePaths+" ", " /etc ") {
		t.Fatalf("ReadWritePaths should use explicit subpaths, got %q", readWritePaths)
	}
	if strings.Contains(" "+readWritePaths+" ", " /etc/motd.d ") {
		t.Fatalf("ReadWritePaths should mark /etc/motd.d optional, got %q", readWritePaths)
	}
	if strings.Contains(" "+readWritePaths+" ", " /etc/pam.d/linuxio ") {
		t.Fatalf("ReadWritePaths should not require the PAM file to already exist, got %q", readWritePaths)
	}
}

func TestBuildInstallCommandArgsAppendsVersion(t *testing.T) {
	version := "v0.9.3"
	args := buildInstallCommandArgs("linuxio-updater-test", scriptArgs(version)...)

	if got := args[len(args)-1]; got != version {
		t.Fatalf("last arg = %q, want %q", got, version)
	}
	for _, required := range []string{"--wait", "--pipe", "--defer-restart"} {
		if !slices.Contains(args, required) {
			t.Fatalf("systemd-run arguments missing %q: %v", required, args)
		}
	}
	for _, property := range []string{"RuntimeMaxSec=10min", "TimeoutStopSec=30s"} {
		if !slices.Contains(args, property) {
			t.Fatalf("systemd-run arguments missing %q: %v", property, args)
		}
	}
}

func TestStopCanceledUpdaterUsesExactServiceUnit(t *testing.T) {
	oldStop := stopUpdaterUnit
	defer func() { stopUpdaterUnit = oldStop }()
	called := ""
	stopUpdaterUnit = func(_ context.Context, unit string) error {
		called = unit
		return nil
	}

	stopCanceledUpdater(context.Background(), "linuxio-updater-42.service")
	if called != "linuxio-updater-42.service" {
		t.Fatalf("stopped unit = %q", called)
	}
}

func TestLogStreamRelaysSanitizedInstallerOutput(t *testing.T) {
	var relay bytes.Buffer
	tail := newOutputTail(2)
	logStream(strings.NewReader("\x1b[32mStep 1/5: Downloading\x1b[0m\nInstalling files\nDone\n"), "stdout", true, &relay, tail)

	if got, want := relay.String(), "Step 1/5: Downloading\nInstalling files\nDone\n"; got != want {
		t.Fatalf("relayed output = %q, want %q", got, want)
	}
	if got, want := tail.String(), "Installing files\nDone"; got != want {
		t.Fatalf("output tail = %q, want %q", got, want)
	}
}

func TestParseAppUpdateRequestRequiresCanonicalIDAndReleaseVersion(t *testing.T) {
	version := " v2.3.4 "
	req, err := parseAppUpdateRequest(apischema.AppUpdateRequest{RunID: testRunID, Version: &version})
	if err != nil {
		t.Fatalf("parseAppUpdateRequest: %v", err)
	}
	if req.runID != testRunID || req.version != "v2.3.4" {
		t.Fatalf("request = %+v", req)
	}

	for _, invalid := range []apischema.AppUpdateRequest{
		{RunID: "update-42", Version: &version},
		{RunID: testRunID, Version: new("latest")},
	} {
		if _, parseErr := parseAppUpdateRequest(invalid); parseErr == nil {
			t.Fatalf("parseAppUpdateRequest(%+v) succeeded", invalid)
		}
	}
}

func TestExecuteAppUpdateRelaysOutputBeforeRestart(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "update-status.json")
	restore := configurePipedUpdateTest(t, statusPath)
	defer restore()

	runAppUpdateInstaller = func(_ context.Context, version string, relay io.Writer) error {
		if version != "v2.3.4" {
			t.Fatalf("installer version = %q", version)
		}
		_, err := io.WriteString(relay, "Step 3/5: Installing binaries\n")
		return err
	}

	restarted := make(chan string, 1)
	restartSystemdUnit = recordSuccessfulRestart(t, statusPath, restarted)

	task := newSessionUpdateTask(t)
	events, unsubscribe := task.Subscribe(8)
	defer unsubscribe()
	if !task.Start(func(ctx context.Context, runningTask *bridge.Task, _ any) (any, error) {
		return executeAppUpdate(ctx, testRuntime(), runningTask, testRunID, "v2.3.4")
	}) {
		t.Fatal("failed to start update Task")
	}
	waitForCompletedUpdateTask(t, task)
	if unit := waitForRestart(t, restarted); unit != "linuxio.target" {
		t.Fatalf("restarted unit = %q", unit)
	}
	info, err := os.Stat(statusPath)
	if err != nil {
		t.Fatalf("stat status: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("status mode = %o, want 644", got)
	}

	output := taskOutput(events)
	for _, expected := range []string{
		"Downloading and verifying install script for v2.3.4...",
		"Step 3/5: Installing binaries",
		"Installation complete",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task output missing %q: %q", expected, output)
		}
	}
}

func recordSuccessfulRestart(t *testing.T, statusPath string, restarted chan<- string) func(context.Context, string) error {
	t.Helper()
	return func(_ context.Context, unit string) error {
		data, err := os.ReadFile(statusPath)
		if err != nil {
			t.Errorf("read status before restart: %v", err)
		} else {
			var status updateStatus
			if unmarshalErr := json.Unmarshal(data, &status); unmarshalErr != nil {
				t.Errorf("decode status before restart: %v", unmarshalErr)
			} else if status.Status != "ok" || status.OwnerUID != 1000 {
				t.Errorf("status before restart = %+v", status)
			}
		}
		restarted <- unit
		return nil
	}
}

func waitForCompletedUpdateTask(t *testing.T, task *bridge.Task) {
	t.Helper()
	select {
	case <-task.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update Task")
	}
	if snapshot := task.Snapshot(); snapshot.State != bridge.TaskStateCompleted {
		t.Fatalf("Task snapshot = %+v", snapshot)
	}
}

func waitForRestart(t *testing.T, restarted <-chan string) string {
	t.Helper()
	select {
	case unit := <-restarted:
		return unit
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for target restart")
		return ""
	}
}

func taskOutput(events <-chan bridge.TaskEvent) string {
	var output strings.Builder
	for len(events) > 0 {
		event := <-events
		progress, ok := event.Progress.(map[string]any)
		if ok && progress["type"] == "data" {
			data, isString := progress["data"].(string)
			if isString {
				_, _ = output.WriteString(data)
			}
		}
	}
	return output.String()
}

func TestExecuteAppUpdateReloadFailureDoesNotRestart(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "update-status.json")
	restore := configurePipedUpdateTest(t, statusPath)
	defer restore()

	runAppUpdateInstaller = func(context.Context, string, io.Writer) error { return nil }
	reloadSystemd = func(context.Context) error { return errors.New("reload failed") }
	restartSystemdUnit = func(context.Context, string) error {
		t.Fatal("update restarted LinuxIO after daemon-reload failed")
		return nil
	}

	_, err := executeAppUpdate(context.Background(), testRuntime(), newSessionUpdateTask(t), testRunID, "v2.3.4")
	if err == nil || !strings.Contains(err.Error(), "reload failed") {
		t.Fatalf("executeAppUpdate error = %v", err)
	}
	data, readErr := os.ReadFile(statusPath)
	if readErr != nil {
		t.Fatalf("read status: %v", readErr)
	}
	var status updateStatus
	if unmarshalErr := json.Unmarshal(data, &status); unmarshalErr != nil || status.Status != "error" {
		t.Fatalf("reload failure status = %+v, %v", status, unmarshalErr)
	}
}

func TestExecuteAppUpdateRequiresInitialStatusProjection(t *testing.T) {
	restore := configurePipedUpdateTest(t, t.TempDir())
	defer restore()

	installerCalled := false
	runAppUpdateInstaller = func(context.Context, string, io.Writer) error {
		installerCalled = true
		return nil
	}

	_, err := executeAppUpdate(context.Background(), testRuntime(), newSessionUpdateTask(t), testRunID, "v2.3.4")
	if err == nil || !strings.Contains(err.Error(), "persist initial update status") {
		t.Fatalf("executeAppUpdate error = %v", err)
	}
	if installerCalled {
		t.Fatal("installer ran without a writable status projection")
	}
}

func TestExecuteAppUpdateFailureDoesNotRestart(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "update-status.json")
	restore := configurePipedUpdateTest(t, statusPath)
	defer restore()

	runAppUpdateInstaller = func(context.Context, string, io.Writer) error {
		return errors.New("checksum mismatch")
	}
	restartSystemdUnit = func(context.Context, string) error {
		t.Fatal("failed update restarted LinuxIO")
		return nil
	}

	_, err := executeAppUpdate(context.Background(), testRuntime(), newSessionUpdateTask(t), testRunID, "v2.3.4")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("executeAppUpdate error = %v", err)
	}
	data, readErr := os.ReadFile(statusPath)
	if readErr != nil {
		t.Fatalf("read status: %v", readErr)
	}
	var status updateStatus
	if unmarshalErr := json.Unmarshal(data, &status); unmarshalErr != nil {
		t.Fatalf("decode status: %v", unmarshalErr)
	}
	if status.Status != "error" || status.ExitCode == nil || *status.ExitCode != 1 {
		t.Fatalf("failure status = %+v", status)
	}
}

func configurePipedUpdateTest(t *testing.T, statusPath string) func() {
	t.Helper()
	oldStatusPath := updateStatusPath
	oldInstaller := runAppUpdateInstaller
	oldReload := reloadSystemd
	oldRestart := restartSystemdUnit
	oldDelay := postUpdateRestartDelay
	updateStatusPath = statusPath
	reloadSystemd = func(context.Context) error { return nil }
	postUpdateRestartDelay = 0
	return func() {
		updateStatusPath = oldStatusPath
		runAppUpdateInstaller = oldInstaller
		reloadSystemd = oldReload
		restartSystemdUnit = oldRestart
		postUpdateRestartDelay = oldDelay
	}
}

func newSessionUpdateTask(t *testing.T) *bridge.Task {
	t.Helper()
	task, err := bridge.NewTaskService().CreateForOwner(routeAppUpdate, nil, bridge.TaskOwner{
		SessionID: "session-a",
		Username:  "alice",
		UID:       1000,
	})
	if err != nil {
		t.Fatalf("create Task: %v", err)
	}
	return task
}

func testRuntime() runtime.Runtime {
	return runtime.Runtime{Session: &session.Session{User: session.User{Username: "alice", UID: 1000}}}
}
