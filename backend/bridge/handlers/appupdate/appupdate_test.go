package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const testOperationID = "00000000-0000-4000-8000-000000000042"

type fakeUpdaterExecutor struct {
	mu           sync.Mutex
	inspectOnce  sync.Once
	inspected    chan struct{}
	launches     []updaterLaunch
	inspectState updaterUnitState
	inspectErr   error
	stopped      bool
	collected    int
	onStart      func(updaterLaunch) error
}

func (f *fakeUpdaterExecutor) Start(_ context.Context, launch updaterLaunch) error {
	f.mu.Lock()
	f.launches = append(f.launches, launch)
	onStart := f.onStart
	f.mu.Unlock()
	if onStart != nil {
		return onStart(launch)
	}
	return nil
}

func (f *fakeUpdaterExecutor) Inspect(context.Context, string, string) (updaterUnitState, error) {
	if f.inspected != nil {
		f.inspectOnce.Do(func() { close(f.inspected) })
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return updaterUnitState{ActiveState: "inactive", ServiceResult: "signal", ExitCode: 15}, nil
	}
	return f.inspectState, f.inspectErr
}

func (f *fakeUpdaterExecutor) Stop(context.Context, string) error {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	return nil
}

func (f *fakeUpdaterExecutor) Collect(context.Context, string) {
	f.mu.Lock()
	f.collected++
	f.mu.Unlock()
}

func TestAppUpdateTaskIdentityRequiresCanonicalUUIDAndBindsVersion(t *testing.T) {
	version := "v2.3.4"
	identity, err := appUpdateTaskIdentity(apischema.AppUpdateRequest{RunID: testOperationID, Version: &version})
	if err != nil {
		t.Fatalf("appUpdateTaskIdentity: %v", err)
	}
	if identity.ID != testOperationID || len(identity.Fingerprint) != 64 {
		t.Fatalf("identity = %+v", identity)
	}

	same, err := appUpdateTaskIdentity(apischema.AppUpdateRequest{RunID: testOperationID, Version: &version})
	if err != nil || same != identity {
		t.Fatalf("repeat identity = %+v, %v", same, err)
	}
	otherVersion := "v2.3.5"
	other, err := appUpdateTaskIdentity(apischema.AppUpdateRequest{RunID: testOperationID, Version: &otherVersion})
	if err != nil {
		t.Fatalf("other identity: %v", err)
	}
	if other.Fingerprint == identity.Fingerprint {
		t.Fatal("different versions produced the same request fingerprint")
	}

	invalidIDs := []string{"", "update-123", strings.ToUpper("abcdef00-0000-4000-8000-000000000042"), "00000000-0000-0000-0000-000000000000"}
	for _, id := range invalidIDs {
		if _, err := appUpdateTaskIdentity(apischema.AppUpdateRequest{RunID: id, Version: &version}); err == nil {
			t.Errorf("operation ID %q was accepted", id)
		}
	}
}

func TestAppUpdateProgressDetailBuildsCommonEnvelope(t *testing.T) {
	detail := AppUpdateProgressDetail{Phase: "installing", Message: "Installing update"}
	progress := detail.ProgressEnvelope()
	if progress.Phase != detail.Phase || progress.Message != detail.Message {
		t.Fatalf("progress summary = %#v, want phase and message from detail", progress)
	}
	if progress.Detail != detail {
		t.Fatalf("progress detail = %#v, want %#v", progress.Detail, detail)
	}
}

func TestUpdaterUnitPropertiesUseNativeDBusTypesAndExplicitSandbox(t *testing.T) {
	launch := updaterLaunch{
		OperationID:   testOperationID,
		UID:           1000,
		Unit:          appUpdateUnitName(testOperationID),
		Description:   appUpdateUnitDescription(testOperationID, 1000),
		ScriptPath:    "/var/lib/linuxIO/durable-operations/artifacts/" + testOperationID + "/install.sh",
		ResultPath:    "/var/lib/linuxIO/durable-operations/artifacts/" + testOperationID + "/executor-result.json",
		InstallerArgs: []string{"--defer-restart", "v2.3.4"},
		RestartAfter:  true,
	}
	properties := buildUpdaterUnitProperties(launch)
	values := make(map[string]any, len(properties))
	for _, property := range properties {
		values[property.Name] = property.Value.Value()
	}

	if values["Type"] != "exec" || values["ProtectSystem"] != "full" || values["User"] != "root" || values["Group"] != "root" {
		t.Fatalf("sandbox properties = %#v", values)
	}
	paths, ok := values["ReadWritePaths"].([]string)
	if !ok {
		t.Fatalf("ReadWritePaths type = %T, want []string", values["ReadWritePaths"])
	}
	for _, expected := range []string{version.BinDir, "/etc/linuxio", "/var/lib/linuxIO"} {
		if !slices.Contains(paths, expected) {
			t.Errorf("ReadWritePaths missing %q: %v", expected, paths)
		}
	}
	commands, ok := values["ExecStart"].([]transientExecCommand)
	if !ok || len(commands) != 1 {
		t.Fatalf("ExecStart = %#v", values["ExecStart"])
	}
	command := commands[0]
	if command.Path != "/bin/bash" || command.Arguments[len(command.Arguments)-1] != "v2.3.4" {
		t.Fatalf("ExecStart command = %+v", command)
	}
	if strings.Contains(updaterUnitRunner, "v2.3.4") || strings.Contains(updaterUnitRunner, launch.ScriptPath) {
		t.Fatal("request-derived values were interpolated into the fixed unit runner")
	}
	if got := appUpdateUnitName(testOperationID); got != "linuxio-app-update-00000000000040008000000000000042.service" {
		t.Fatalf("unit name = %q", got)
	}
}

func TestRunAppUpdateTaskPersistsAndReturnsTypedResult(t *testing.T) {
	executor := &fakeUpdaterExecutor{inspectState: updaterUnitState{ActiveState: "active", SubState: "running"}}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	executor.onStart = func(launch updaterLaunch) error {
		result := durabletask.ExecutorResult{
			ID:         launch.OperationID,
			State:      durabletask.StateCompleted,
			ExitCode:   0,
			FinishedAt: time.Now().UTC(),
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return utils.WriteFileAtomic(launch.ResultPath, data, 0o600)
	}

	version := "v2.3.4"
	task := newDurableUpdateTask(t, version)
	result, err := runAppUpdateTask(context.Background(), task, apischema.AppUpdateRequest{RunID: testOperationID, Version: &version})
	if err != nil {
		t.Fatalf("runAppUpdateTask: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}

	store := durabletask.NewStore(appUpdateStoreRoot)
	record, err := store.Get(context.Background(), testOperationID, 1000)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.State != durabletask.StateCompleted || record.Executor.Handle != appUpdateUnitName(testOperationID) || record.Executor.Identity != appUpdateExecutorIdentity {
		t.Fatalf("record = %+v", record)
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if len(executor.launches) != 1 || executor.collected != 1 {
		t.Fatalf("launches = %d, collected = %d", len(executor.launches), executor.collected)
	}
}

func TestRunAppUpdateTaskCancellationWaitsForExecutorStop(t *testing.T) {
	executor := &fakeUpdaterExecutor{inspectState: updaterUnitState{ActiveState: "active", SubState: "running"}}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	started := make(chan struct{})
	executor.onStart = func(updaterLaunch) error {
		close(started)
		return nil
	}

	version := "v2.3.4"
	task := newDurableUpdateTask(t, version)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runAppUpdateTask(ctx, task, apischema.AppUpdateRequest{RunID: testOperationID, Version: &version})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v, want context.Canceled", err)
	}

	record, err := durabletask.NewStore(appUpdateStoreRoot).Get(context.Background(), testOperationID, 1000)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.State != durabletask.StateCanceled || record.CancelRequestedAt == nil || record.FinishedAt == nil {
		t.Fatalf("canceled record = %+v", record)
	}
	executor.mu.Lock()
	stopped := executor.stopped
	executor.mu.Unlock()
	if !stopped {
		t.Fatal("Task returned canceled before the executor was stopped")
	}
}

func TestRunAppUpdateTaskCancellationDuringPreparationNeverLaunches(t *testing.T) {
	executor := &fakeUpdaterExecutor{}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	downloadStarted := make(chan struct{})
	downloadUpdaterScript = func(ctx context.Context, _ string) ([]byte, error) {
		close(downloadStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	version := "v2.3.4"
	task := newDurableUpdateTask(t, version)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runAppUpdateTask(ctx, task, apischema.AppUpdateRequest{RunID: testOperationID, Version: &version})
		done <- err
	}()
	<-downloadStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v, want context.Canceled", err)
	}
	record, err := durabletask.NewStore(appUpdateStoreRoot).Get(context.Background(), testOperationID, 1000)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.State != durabletask.StateCanceled || record.Executor.Handle != "" {
		t.Fatalf("pre-launch cancellation record = %+v", record)
	}
	executor.mu.Lock()
	launchCount := len(executor.launches)
	executor.mu.Unlock()
	if launchCount != 0 {
		t.Fatalf("pre-launch cancellation started %d executors", launchCount)
	}
}

func TestLaunchingRecoveryNeverRestartsMissingExecutor(t *testing.T) {
	executor := &fakeUpdaterExecutor{inspectErr: errUpdaterUnitNotFound}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	store := durabletask.NewStore(appUpdateStoreRoot)
	claim := durabletask.Claim{
		ID:                 testOperationID,
		Route:              routeAppUpdate,
		UID:                1000,
		RequestFingerprint: durabletask.Fingerprint(routeAppUpdate, "version=v2.3.4"),
		Target:             "v2.3.4",
	}
	record, _, err := store.Claim(context.Background(), claim)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	record, err = store.Update(context.Background(), record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateLaunching
		current.Executor = durabletask.Executor{Kind: appUpdateExecutorKind, Handle: appUpdateUnitName(record.ID), Identity: appUpdateExecutorIdentity}
		return nil
	})
	if err != nil {
		t.Fatalf("mark launching: %v", err)
	}

	_, done, reconcileErr := reconcileAppUpdate(context.Background(), nil, store, executor, record)
	if !done || reconcileErr == nil {
		t.Fatalf("reconcile = done %t, error %v", done, reconcileErr)
	}
	terminal, err := store.Get(context.Background(), record.ID, record.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if terminal.State != durabletask.StateFailed {
		t.Fatalf("recovered state = %q, want failed", terminal.State)
	}
	executor.mu.Lock()
	launchCount := len(executor.launches)
	executor.mu.Unlock()
	if launchCount != 0 {
		t.Fatalf("recovery launched %d replacement units", launchCount)
	}
}

func TestRunningRecoveryMarksMissingExecutorUnknownAfterHostRestart(t *testing.T) {
	executor := &fakeUpdaterExecutor{inspectErr: errUpdaterUnitNotFound}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	store := durabletask.NewStore(appUpdateStoreRoot)
	record, _, err := store.Claim(context.Background(), durabletask.Claim{
		ID:                 testOperationID,
		Route:              routeAppUpdate,
		UID:                1000,
		RequestFingerprint: durabletask.Fingerprint(routeAppUpdate, "version=v2.3.4"),
		Target:             "v2.3.4",
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	started := time.Now().UTC()
	record, err = store.Update(context.Background(), record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateRunning
		current.StartedAt = &started
		current.Executor = durabletask.Executor{Kind: appUpdateExecutorKind, Handle: appUpdateUnitName(record.ID), Identity: appUpdateExecutorIdentity}
		return nil
	})
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}

	_, done, reconcileErr := reconcileAppUpdate(context.Background(), nil, store, executor, record)
	if !done || reconcileErr == nil {
		t.Fatalf("reconcile = done %t, error %v", done, reconcileErr)
	}
	terminal, err := store.Get(context.Background(), record.ID, record.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if terminal.State != durabletask.StateUnknown || terminal.FinishedAt == nil {
		t.Fatalf("recovered state = %+v, want terminal unknown", terminal)
	}
	executor.mu.Lock()
	launchCount := len(executor.launches)
	executor.mu.Unlock()
	if launchCount != 0 {
		t.Fatalf("host-restart recovery launched %d replacement units", launchCount)
	}
}

func TestQueuedRecoveryResumesPreparationAndStartsExactlyOnce(t *testing.T) {
	executor := &fakeUpdaterExecutor{inspectState: updaterUnitState{ActiveState: "active", SubState: "running"}}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	store := durabletask.NewStore(appUpdateStoreRoot)
	record, _, err := store.Claim(context.Background(), durabletask.Claim{
		ID:                 testOperationID,
		Route:              routeAppUpdate,
		UID:                1000,
		RequestFingerprint: durabletask.Fingerprint(routeAppUpdate, "version=v2.3.4"),
		Target:             "v2.3.4",
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	executor.onStart = func(launch updaterLaunch) error {
		result := durabletask.ExecutorResult{
			ID:         launch.OperationID,
			State:      durabletask.StateCompleted,
			FinishedAt: time.Now().UTC(),
		}
		data, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return marshalErr
		}
		return utils.WriteFileAtomic(launch.ResultPath, data, 0o600)
	}

	version := "v2.3.4"
	request := apischema.AppUpdateRequest{RunID: record.ID, Version: &version}
	identity, err := appUpdateTaskIdentity(request)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	routeBindings().Register(router)
	task, created, err := router.RecoverDurableTask(routeAppUpdate, request, bridgeipc.TaskOwner{
		SessionID: "replacement-session",
		Username:  "alice",
		UID:       record.UID,
	}, identity)
	if err != nil || !created {
		t.Fatalf("RecoverDurableTask = %v, %t, %v", task, created, err)
	}
	select {
	case <-task.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovered Task")
	}
	terminal, err := store.Get(context.Background(), record.ID, record.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if terminal.State != durabletask.StateCompleted {
		t.Fatalf("recovered state = %q, want completed", terminal.State)
	}
	executor.mu.Lock()
	launchCount := len(executor.launches)
	executor.mu.Unlock()
	if launchCount != 1 {
		t.Fatalf("queued recovery launched %d units, want 1", launchCount)
	}
}

func TestRecoveredTaskCanBeCanceledFromReplacementSession(t *testing.T) {
	executor := &fakeUpdaterExecutor{
		inspected:    make(chan struct{}),
		inspectState: updaterUnitState{ActiveState: "active", SubState: "running"},
	}
	restore := configureAppUpdateTest(t, executor)
	defer restore()
	store := durabletask.NewStore(appUpdateStoreRoot)
	version := "v2.3.4"
	request := apischema.AppUpdateRequest{RunID: testOperationID, Version: &version}
	identity, err := appUpdateTaskIdentity(request)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	record, _, err := store.Claim(context.Background(), durabletask.Claim{
		ID:                 identity.ID,
		Route:              routeAppUpdate,
		UID:                1000,
		RequestFingerprint: identity.Fingerprint,
		Target:             version,
		ExclusiveRoute:     true,
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	started := time.Now().UTC()
	if _, updateErr := store.Update(context.Background(), record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateRunning
		current.StartedAt = &started
		current.Executor = durabletask.Executor{Kind: appUpdateExecutorKind, Handle: appUpdateUnitName(record.ID), Identity: appUpdateExecutorIdentity}
		return nil
	}); updateErr != nil {
		t.Fatalf("mark running: %v", updateErr)
	}

	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	routeBindings().Register(router)
	task, created, err := router.RecoverDurableTask(routeAppUpdate, request, bridgeipc.TaskOwner{
		SessionID: "replacement-session",
		Username:  "alice",
		UID:       record.UID,
	}, identity)
	if err != nil || !created {
		t.Fatalf("RecoverDurableTask = %v, %t, %v", task, created, err)
	}
	select {
	case <-executor.inspected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovered Task observation")
	}
	task.Cancel()
	select {
	case <-task.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovered Task cancellation")
	}
	terminal, err := store.Get(context.Background(), record.ID, record.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if terminal.State != durabletask.StateCanceled || terminal.CancelRequestedAt == nil {
		t.Fatalf("replacement-session cancellation = %+v", terminal)
	}
}

func configureAppUpdateTest(t *testing.T, executor updaterExecutor) func() {
	t.Helper()
	oldRoot := appUpdateStoreRoot
	oldPoll := appUpdatePollInterval
	oldExecutor := newUpdaterExecutor
	oldDownload := downloadUpdaterScript
	oldEnsure := ensureUpdaterPaths
	appUpdateStoreRoot = filepath.Join(t.TempDir(), "operations")
	appUpdatePollInterval = time.Millisecond
	newUpdaterExecutor = func() updaterExecutor { return executor }
	downloadUpdaterScript = func(context.Context, string) ([]byte, error) { return []byte("#!/bin/bash\n"), nil }
	ensureUpdaterPaths = func() error { return nil }
	return func() {
		appUpdateStoreRoot = oldRoot
		appUpdatePollInterval = oldPoll
		newUpdaterExecutor = oldExecutor
		downloadUpdaterScript = oldDownload
		ensureUpdaterPaths = oldEnsure
	}
}

func newDurableUpdateTask(t *testing.T, version string) *bridgeipc.Task {
	t.Helper()
	identity, err := appUpdateTaskIdentity(apischema.AppUpdateRequest{RunID: testOperationID, Version: &version})
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	task, created, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(
		routeAppUpdate,
		nil,
		bridgeipc.TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000},
		bridgeipc.TaskLifetimeDurable,
		identity,
	)
	if err != nil || !created {
		t.Fatalf("ClaimForOwnerWithIdentity = %v, %t, %v", task, created, err)
	}
	return task
}

func TestExecutorResultPathIsInsideOperationArtifactDirectory(t *testing.T) {
	store := durabletask.NewStore(t.TempDir())
	path, err := store.ExecutorResultPath(testOperationID)
	if err != nil {
		t.Fatalf("ExecutorResultPath: %v", err)
	}
	if filepath.Base(path) != "executor-result.json" || filepath.Base(filepath.Dir(path)) != testOperationID {
		t.Fatalf("result path = %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}
