package dockerupdate

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
)

const testDockerOperationID = "00000000-0000-4000-8000-000000000011"

func TestRunDurableWritesTypedResult(t *testing.T) {
	oldRoot, oldMutation := dockerUpdateStoreRoot, runDockerContainerMutation
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		runDockerContainerMutation = oldMutation
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	runDockerContainerMutation = func(_ context.Context, _ string, report progressReporter) (DockerContainerUpdateResult, error) {
		report("pulling", "Pulling the updated image")
		return DockerContainerUpdateResult{ContainerID: "web", Updated: true}, nil
	}

	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: ExecutorKind, Handle: UnitName(testDockerOperationID), Identity: ExecutorIdentity}
	if _, _, err := store.Claim(context.Background(), durabletask.Claim{
		ID: testDockerOperationID, Route: UpdateRoute, UID: 0,
		RequestFingerprint: durabletask.Fingerprint(UpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true,
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

	if err := RunDurable(context.Background(), testDockerOperationID); err != nil {
		t.Fatalf("RunDurable: %v", err)
	}
	artifact, err := store.ReadExecutorResult(testDockerOperationID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	if artifact.State != durabletask.StateCompleted || artifact.ExitCode != 0 {
		t.Fatalf("executor artifact = %+v", artifact)
	}
	var result DockerContainerUpdateResult
	if unmarshalErr := json.Unmarshal(artifact.Result, &result); unmarshalErr != nil {
		t.Fatalf("decode typed result: %v", unmarshalErr)
	}
	if result.ContainerID != "web" || !result.Updated {
		t.Fatalf("typed result = %+v", result)
	}
	record, err := store.Get(context.Background(), testDockerOperationID, 0)
	if err != nil {
		t.Fatalf("Get durable record: %v", err)
	}
	if got := record.Progress[len(record.Progress)-1]; got.Phase != "pulling" || got.Message != "Pulling the updated image" {
		t.Fatalf("persisted progress = %+v", got)
	}
}

func TestRunDurablePersistsCanceledResult(t *testing.T) {
	oldRoot, oldMutation := dockerUpdateStoreRoot, runDockerContainerMutation
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		runDockerContainerMutation = oldMutation
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	ctx, cancel := context.WithCancel(context.Background())
	runDockerContainerMutation = func(context.Context, string, progressReporter) (DockerContainerUpdateResult, error) {
		cancel()
		return DockerContainerUpdateResult{}, context.Canceled
	}
	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: ExecutorKind, Handle: UnitName(testDockerOperationID), Identity: ExecutorIdentity}
	claim := durabletask.Claim{ID: testDockerOperationID, Route: UpdateRoute, UID: 0, RequestFingerprint: durabletask.Fingerprint(UpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true}
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
	if err := RunDurable(ctx, claim.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDurable error = %v, want context.Canceled", err)
	}
	artifact, err := store.ReadExecutorResult(claim.ID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	if artifact.State != durabletask.StateCanceled || artifact.ExitCode != 143 {
		t.Fatalf("canceled artifact = %+v", artifact)
	}
}

func TestRunDurableHonorsPersistedCancellationBeforeMutation(t *testing.T) {
	oldRoot, oldMutation := dockerUpdateStoreRoot, runDockerContainerMutation
	t.Cleanup(func() {
		dockerUpdateStoreRoot = oldRoot
		runDockerContainerMutation = oldMutation
	})
	dockerUpdateStoreRoot = filepath.Join(t.TempDir(), "tasks")
	mutationCalls := 0
	runDockerContainerMutation = func(context.Context, string, progressReporter) (DockerContainerUpdateResult, error) {
		mutationCalls++
		return DockerContainerUpdateResult{ContainerID: "web"}, nil
	}

	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: ExecutorKind, Handle: UnitName(testDockerOperationID), Identity: ExecutorIdentity}
	claim := durabletask.Claim{ID: testDockerOperationID, Route: UpdateRoute, UID: 0, RequestFingerprint: durabletask.Fingerprint(UpdateRoute, "container=web"), Target: "web", ExclusiveRoute: true}
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

	if err := RunDurable(context.Background(), claim.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDurable error = %v, want context.Canceled", err)
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
