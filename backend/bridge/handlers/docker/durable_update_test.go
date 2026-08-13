package docker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/transientunit"
	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const testDockerOperationID = "00000000-0000-4000-8000-000000000011"

type fakeDockerUpdateExecutor struct {
	startCalls int
	stopCalls  int
	collects   int
	inspect    transientunit.State
	inspectErr error
	stopErr    error
}

func (f *fakeDockerUpdateExecutor) Start(context.Context, dockerUpdateLaunch) error {
	f.startCalls++
	return nil
}

func (f *fakeDockerUpdateExecutor) Inspect(context.Context, string, string) (transientunit.State, error) {
	return f.inspect, f.inspectErr
}

func (f *fakeDockerUpdateExecutor) Stop(context.Context, string) error {
	f.stopCalls++
	return f.stopErr
}

func (f *fakeDockerUpdateExecutor) Collect(context.Context, string) { f.collects++ }

func TestBuildDockerUpdateUnitPropertiesRunsWorkerWithArgvZero(t *testing.T) {
	launch := dockerUpdateLaunch{
		OperationID: testDockerOperationID,
		Description: "test Docker update",
	}
	properties := buildDockerUpdateUnitProperties(launch)
	var commands []transientunit.ExecCommand
	for _, property := range properties {
		if property.Name == "ExecStart" {
			commands, _ = property.Value.Value().([]transientunit.ExecCommand)
			break
		}
	}
	if len(commands) != 1 {
		t.Fatalf("ExecStart commands = %#v, want one command", commands)
	}
	want := []string{dockerUpdateRunnerPath, "run-operation", "--id", testDockerOperationID}
	if commands[0].Path != dockerUpdateRunnerPath || !slices.Equal(commands[0].Arguments, want) {
		t.Fatalf("ExecStart command = %+v, want path %q args %q", commands[0], dockerUpdateRunnerPath, want)
	}
}

func TestRunDurableDockerUpdateWritesTypedResult(t *testing.T) {
	oldRoot, oldMutation := dockerUpdateStoreRoot, runDockerContainerMutation
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		runDockerContainerMutation = oldMutation
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	runDockerContainerMutation = func(context.Context, string) (apischema.DockerContainerUpdateResult, error) {
		return apischema.DockerContainerUpdateResult{ContainerID: "web", Updated: true}, nil
	}

	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: dockerUpdateExecutorKind, Handle: dockerDurableUpdateUnitName(testDockerOperationID), Identity: dockerUpdateExecutorIdentity}
	if _, _, err := store.Claim(context.Background(), durabletask.Claim{
		ID: testDockerOperationID, Route: dockerUpdateRoute, UID: 0,
		RequestFingerprint: durabletask.Fingerprint(dockerUpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true,
	}); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := store.Update(context.Background(), testDockerOperationID, 0, func(record *durabletask.Record) error {
		record.State = durabletask.StateRunning
		record.Executor = executor
		return nil
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	if err := RunDurableDockerUpdate(context.Background(), testDockerOperationID); err != nil {
		t.Fatalf("RunDurableDockerUpdate: %v", err)
	}
	artifact, err := store.ReadExecutorResult(testDockerOperationID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	if artifact.State != durabletask.StateCompleted || artifact.ExitCode != 0 {
		t.Fatalf("executor artifact = %+v", artifact)
	}
	var result apischema.DockerContainerUpdateResult
	if err := json.Unmarshal(artifact.Result, &result); err != nil {
		t.Fatalf("decode typed result: %v", err)
	}
	if result.ContainerID != "web" || !result.Updated {
		t.Fatalf("typed result = %+v", result)
	}
}

func TestDurableDockerRecoveryReusesPersistedResultWithoutRelaunch(t *testing.T) {
	oldRoot, oldExecutor := dockerUpdateStoreRoot, newDockerUpdateExecutor
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		newDockerUpdateExecutor = oldExecutor
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	fake := &fakeDockerUpdateExecutor{inspectErr: transientunit.ErrNotFound}
	newDockerUpdateExecutor = func() dockerUpdateExecutor { return fake }

	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: dockerUpdateExecutorKind, Handle: dockerDurableUpdateUnitName(testDockerOperationID), Identity: dockerUpdateExecutorIdentity}
	claim := durabletask.Claim{
		ID: testDockerOperationID, Route: dockerUpdateRoute, UID: 1000,
		RequestFingerprint: durabletask.Fingerprint(dockerUpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true,
	}
	if _, _, err := store.Claim(context.Background(), claim); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := store.Update(context.Background(), claim.ID, claim.UID, func(record *durabletask.Record) error {
		record.State = durabletask.StateRunning
		record.Executor = executor
		return nil
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	result := apischema.DockerContainerUpdateResult{ContainerID: "web", Updated: true}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	artifact, err := json.Marshal(durabletask.ExecutorResult{ID: claim.ID, State: durabletask.StateCompleted, FinishedAt: time.Now().UTC(), Result: payload})
	if err != nil {
		t.Fatalf("Marshal artifact: %v", err)
	}
	if _, writeErr := store.WriteArtifact(claim.ID, "executor-result.json", artifact, 0o600); writeErr != nil {
		t.Fatalf("WriteArtifact: %v", writeErr)
	}

	service := bridgeipc.NewTaskService()
	task, err := service.CreateForOwnerWithLifetime(dockerUpdateRoute, apischema.DockerContainerUpdateRequest{RunID: claim.ID, ContainerID: claim.Target}, bridgeipc.TaskOwner{SessionID: "session", Username: "alice", UID: claim.UID}, bridgeipc.TaskLifetimeDurable)
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	got, err := runDockerUpdateTask(context.Background(), task, apischema.DockerContainerUpdateRequest{RunID: claim.ID, ContainerID: claim.Target})
	if err != nil {
		t.Fatalf("runDockerUpdateTask: %v", err)
	}
	if got.ContainerID != "web" || !got.Updated || fake.startCalls != 0 {
		t.Fatalf("recovery result=%+v start calls=%d", got, fake.startCalls)
	}
}

func TestRunDurableDockerUpdatePersistsCanceledResult(t *testing.T) {
	oldRoot, oldMutation := dockerUpdateStoreRoot, runDockerContainerMutation
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		runDockerContainerMutation = oldMutation
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	ctx, cancel := context.WithCancel(context.Background())
	runDockerContainerMutation = func(context.Context, string) (apischema.DockerContainerUpdateResult, error) {
		cancel()
		return apischema.DockerContainerUpdateResult{}, context.Canceled
	}
	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: dockerUpdateExecutorKind, Handle: dockerDurableUpdateUnitName(testDockerOperationID), Identity: dockerUpdateExecutorIdentity}
	claim := durabletask.Claim{ID: testDockerOperationID, Route: dockerUpdateRoute, UID: 0, RequestFingerprint: durabletask.Fingerprint(dockerUpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true}
	if _, _, err := store.Claim(context.Background(), claim); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := store.Update(context.Background(), claim.ID, claim.UID, func(record *durabletask.Record) error {
		record.State = durabletask.StateRunning
		record.Executor = executor
		return nil
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := RunDurableDockerUpdate(ctx, claim.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDurableDockerUpdate error = %v, want context.Canceled", err)
	}
	artifact, err := store.ReadExecutorResult(claim.ID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	if artifact.State != durabletask.StateCanceled || artifact.ExitCode != 143 {
		t.Fatalf("canceled artifact = %+v", artifact)
	}
}

func TestRunDurableDockerUpdateHonorsPersistedCancellationBeforeMutation(t *testing.T) {
	oldRoot, oldMutation := dockerUpdateStoreRoot, runDockerContainerMutation
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		runDockerContainerMutation = oldMutation
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	mutationCalls := 0
	runDockerContainerMutation = func(context.Context, string) (apischema.DockerContainerUpdateResult, error) {
		mutationCalls++
		return apischema.DockerContainerUpdateResult{ContainerID: "web"}, nil
	}

	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: dockerUpdateExecutorKind, Handle: dockerDurableUpdateUnitName(testDockerOperationID), Identity: dockerUpdateExecutorIdentity}
	claim := durabletask.Claim{ID: testDockerOperationID, Route: dockerUpdateRoute, UID: 0, RequestFingerprint: durabletask.Fingerprint(dockerUpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true}
	if _, _, err := store.Claim(context.Background(), claim); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	requested := time.Now().UTC()
	if _, err := store.Update(context.Background(), claim.ID, claim.UID, func(record *durabletask.Record) error {
		record.State = durabletask.StateLaunching
		record.Executor = executor
		record.CancelRequestedAt = &requested
		return nil
	}); err != nil {
		t.Fatalf("mark cancel requested: %v", err)
	}

	if err := RunDurableDockerUpdate(context.Background(), claim.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDurableDockerUpdate error = %v, want context.Canceled", err)
	}
	if mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", mutationCalls)
	}
	artifact, err := store.ReadExecutorResult(claim.ID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	if artifact.State != durabletask.StateCanceled {
		t.Fatalf("executor artifact = %+v, want canceled", artifact)
	}
}

func TestReconcileDockerUpdateRejectsMalformedCompletedPayload(t *testing.T) {
	store := durabletask.NewStore(filepath.Join(t.TempDir(), "tasks"))
	claim := durabletask.Claim{ID: testDockerOperationID, Route: dockerUpdateRoute, UID: 1000, RequestFingerprint: durabletask.Fingerprint(dockerUpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true}
	_, _, err := store.Claim(context.Background(), claim)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	record, err := store.Update(context.Background(), claim.ID, claim.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateRunning
		current.Executor = durabletask.Executor{Kind: dockerUpdateExecutorKind, Handle: dockerDurableUpdateUnitName(claim.ID), Identity: dockerUpdateExecutorIdentity}
		return nil
	})
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	artifact, err := json.Marshal(durabletask.ExecutorResult{
		ID: claim.ID, State: durabletask.StateCompleted, FinishedAt: time.Now().UTC(), Result: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Marshal artifact: %v", err)
	}
	if _, writeErr := store.WriteArtifact(claim.ID, "executor-result.json", artifact, 0o600); writeErr != nil {
		t.Fatalf("WriteArtifact: %v", writeErr)
	}

	fake := &fakeDockerUpdateExecutor{}
	_, handled, reconcileErr := reconcileDockerExecutorResult(context.Background(), store, fake, record)
	if !handled || reconcileErr == nil {
		t.Fatalf("reconcile result = handled %v, error %v; want terminal error", handled, reconcileErr)
	}
	terminal, err := store.Get(context.Background(), claim.ID, claim.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if terminal.State != durabletask.StateUnknown || terminal.Error == nil || !strings.Contains(terminal.Error.Message, "container identity") {
		t.Fatalf("terminal record = %+v, want unknown malformed result", terminal)
	}
}

func TestCancelDockerUpdateReconcilesCompletedArtifactBeforeStop(t *testing.T) {
	store := durabletask.NewStore(filepath.Join(t.TempDir(), "tasks"))
	claim := durabletask.Claim{ID: testDockerOperationID, Route: dockerUpdateRoute, UID: 1000, RequestFingerprint: durabletask.Fingerprint(dockerUpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true}
	_, _, err := store.Claim(context.Background(), claim)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	record, err := store.Update(context.Background(), claim.ID, claim.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateRunning
		current.Executor = durabletask.Executor{Kind: dockerUpdateExecutorKind, Handle: dockerDurableUpdateUnitName(claim.ID), Identity: dockerUpdateExecutorIdentity}
		return nil
	})
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	payload, err := json.Marshal(apischema.DockerContainerUpdateResult{ContainerID: "web", Updated: true})
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	artifact, err := json.Marshal(durabletask.ExecutorResult{ID: claim.ID, State: durabletask.StateCompleted, FinishedAt: time.Now().UTC(), Result: payload})
	if err != nil {
		t.Fatalf("Marshal artifact: %v", err)
	}
	if _, writeErr := store.WriteArtifact(claim.ID, "executor-result.json", artifact, 0o600); writeErr != nil {
		t.Fatalf("WriteArtifact: %v", writeErr)
	}

	fake := &fakeDockerUpdateExecutor{stopErr: errors.New("stop must not be called")}
	result, err := cancelDockerUpdate(store, fake, record.ID, record.UID)
	if err != nil {
		t.Fatalf("cancelDockerUpdate: %v", err)
	}
	if !result.Updated || result.ContainerID != "web" || fake.stopCalls != 0 {
		t.Fatalf("cancel result = %+v, stop calls = %d", result, fake.stopCalls)
	}
}
