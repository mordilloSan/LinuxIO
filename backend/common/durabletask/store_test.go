package durabletask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testRoute = "control.app_update"

func TestStoreClaimIsIdempotentAndRejectsConflicts(t *testing.T) {
	store := newTestStore(t)
	claim := testClaim(1, 1000, "v1.2.3")

	created, wasCreated, err := store.Claim(context.Background(), claim)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !wasCreated || created.State != StateQueued || created.UID != 1000 {
		t.Fatalf("created record = %+v, created = %t", created, wasCreated)
	}

	existing, wasCreated, err := store.Claim(context.Background(), claim)
	if err != nil {
		t.Fatalf("repeat Claim: %v", err)
	}
	if wasCreated || existing.ID != created.ID || !existing.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("repeat claim = %+v, created = %t", existing, wasCreated)
	}

	conflicting := claim
	conflicting.RequestFingerprint = Fingerprint(testRoute, "v9.9.9")
	if _, _, err := store.Claim(context.Background(), conflicting); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting claim error = %v, want ErrConflict", err)
	}

	otherOwner := claim
	otherOwner.UID = 1001
	if _, _, err := store.Claim(context.Background(), otherOwner); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-owner claim error = %v, want ErrConflict", err)
	}
	if _, err := store.Get(context.Background(), claim.ID, 1001); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner read error = %v, want ErrNotFound", err)
	}
}

func TestStoreExclusiveRouteRejectsAnotherActiveOperation(t *testing.T) {
	store := newTestStore(t)
	first := testClaim(8, 1000, "v1.2.3")
	first.ExclusiveRoute = true
	if _, _, err := store.Claim(context.Background(), first); err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	second := testClaim(9, 1001, "v1.2.4")
	second.ExclusiveRoute = true
	if _, _, err := store.Claim(context.Background(), second); !errors.Is(err, ErrActive) {
		t.Fatalf("concurrent exclusive Claim error = %v, want ErrActive", err)
	}
	finished := time.Now().UTC()
	if _, err := store.Update(context.Background(), first.ID, first.UID, func(record *Record) error {
		record.State = StateCompleted
		record.FinishedAt = &finished
		return nil
	}); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	if _, created, err := store.Claim(context.Background(), second); err != nil || !created {
		t.Fatalf("Claim after completion = created %t, error %v", created, err)
	}
}

func TestStoreAppliesTypedExecutorResult(t *testing.T) {
	store := newTestStore(t)
	claim := testClaim(2, 1000, "v1.2.3")
	if _, _, err := store.Claim(context.Background(), claim); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	result := ExecutorResult{
		ID:         claim.ID,
		State:      StateFailed,
		ExitCode:   17,
		FinishedAt: time.Date(2026, 8, 10, 12, 1, 0, 0, time.UTC),
		Result:     json.RawMessage(`{"containerId":"new-id","updated":true}`),
		Error:      "installer failed",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, writeErr := store.WriteArtifact(claim.ID, "executor-result.json", data, 0o600); writeErr != nil {
		t.Fatalf("WriteArtifact: %v", writeErr)
	}

	read, err := store.ReadExecutorResult(claim.ID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	record, err := store.ApplyExecutorResult(context.Background(), claim.UID, read)
	if err != nil {
		t.Fatalf("ApplyExecutorResult: %v", err)
	}
	if record.State != StateFailed || record.Error == nil || record.Error.Code != 17 {
		t.Fatalf("terminal record = %+v", record)
	}
	var typedResult struct {
		ContainerID string `json:"containerId"`
		Updated     bool   `json:"updated"`
	}
	if decodeErr := json.Unmarshal(record.Result, &typedResult); decodeErr != nil {
		t.Fatalf("decode typed result: %v", decodeErr)
	}
	if typedResult.ContainerID != "new-id" || !typedResult.Updated {
		t.Fatalf("typed result = %+v", typedResult)
	}
	repeated := result
	repeated.ExitCode = 99
	repeated.Error = "late duplicate result"
	record, err = store.ApplyExecutorResult(context.Background(), claim.UID, repeated)
	if err != nil {
		t.Fatalf("repeat ApplyExecutorResult: %v", err)
	}
	if record.Error == nil || record.Error.Code != 17 {
		t.Fatalf("duplicate completion replaced terminal record: %+v", record)
	}
}

func TestStoreAppliesCanceledExecutorResultAndRejectsWrongExecutor(t *testing.T) {
	store := newTestStore(t)
	claim := testClaim(11, 1000, "container-id")
	if _, _, err := store.Claim(context.Background(), claim); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	executor := Executor{Kind: "systemd-transient-unit", Handle: "linuxio-docker-update.service", Identity: "root:linuxio-docker-update"}
	if _, err := store.Update(context.Background(), claim.ID, claim.UID, func(record *Record) error {
		record.State = StateRunning
		record.Executor = executor
		return nil
	}); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := store.GetForExecutor(context.Background(), claim.ID, testRoute, Executor{Kind: executor.Kind, Handle: executor.Handle, Identity: "root:other"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong executor error = %v, want ErrNotFound", err)
	}
	finished := time.Date(2026, 8, 10, 12, 2, 0, 0, time.UTC)
	result := ExecutorResult{ID: claim.ID, State: StateCanceled, ExitCode: 143, FinishedAt: finished, Error: "worker stopped"}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, writeErr := store.WriteArtifact(claim.ID, "executor-result.json", data, 0o600); writeErr != nil {
		t.Fatalf("WriteArtifact: %v", writeErr)
	}
	read, err := store.ReadExecutorResult(claim.ID)
	if err != nil {
		t.Fatalf("ReadExecutorResult: %v", err)
	}
	record, err := store.ApplyExecutorResult(context.Background(), claim.UID, read)
	if err != nil {
		t.Fatalf("ApplyExecutorResult: %v", err)
	}
	if record.State != StateCanceled || record.Error == nil || record.Error.Code != 499 {
		t.Fatalf("canceled record = %+v", record)
	}
}

func TestStoreBoundsProgressAndTerminalRetentionWithoutPruningActive(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	ctx := context.Background()

	active := testClaim(3, 1000, "active")
	if _, _, err := store.Claim(ctx, active); err != nil {
		t.Fatalf("claim active: %v", err)
	}
	if _, err := store.Update(ctx, active.ID, active.UID, func(record *Record) error {
		record.Executor = Executor{Kind: "systemd", Handle: "unit.service", Identity: "root:root"}
		record.State = StateRunning
		record.StartedAt = new(now)
		for index := range MaxProgressEntries + 8 {
			record.AppendProgress(now.Add(time.Duration(index)*time.Second), "running", fmt.Sprintf("step-%d", index))
		}
		return nil
	}); err != nil {
		t.Fatalf("update active: %v", err)
	}

	old := testClaim(4, 1000, "old")
	store.now = func() time.Time { return now.Add(-TerminalRetention - time.Hour) }
	if _, _, err := store.Claim(ctx, old); err != nil {
		t.Fatalf("claim old: %v", err)
	}
	if _, err := store.Update(ctx, old.ID, old.UID, func(record *Record) error {
		record.State = StateCompleted
		record.FinishedAt = new(store.now())
		return nil
	}); err != nil {
		t.Fatalf("finish old: %v", err)
	}

	store.now = func() time.Time { return now }
	trigger := testClaim(5, 1000, "trigger")
	if _, _, err := store.Claim(ctx, trigger); err != nil {
		t.Fatalf("trigger prune: %v", err)
	}
	if _, err := store.Get(ctx, old.ID, old.UID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old terminal record error = %v, want ErrNotFound", err)
	}
	record, err := store.Get(ctx, active.ID, active.UID)
	if err != nil {
		t.Fatalf("active record was pruned: %v", err)
	}
	if len(record.Progress) != MaxProgressEntries || record.Progress[0].Message != "step-8" {
		t.Fatalf("bounded progress = %+v", record.Progress)
	}
}

func TestStoreRetainsAtMostNewestTerminalRecordsPerUID(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	oldestID := ""

	for index := range MaxTerminalRecordsPerUID + 1 {
		claim := testClaim(1000+index, 1000, fmt.Sprintf("target-%d", index))
		if index == 0 {
			oldestID = claim.ID
		}
		stamp := base.Add(time.Duration(index) * time.Second)
		store.now = func() time.Time { return stamp }
		if _, _, err := store.Claim(ctx, claim); err != nil {
			t.Fatalf("claim %d: %v", index, err)
		}
		if _, err := store.Update(ctx, claim.ID, claim.UID, func(record *Record) error {
			record.State = StateCompleted
			record.FinishedAt = new(stamp)
			return nil
		}); err != nil {
			t.Fatalf("finish %d: %v", index, err)
		}
	}

	if _, err := store.Get(ctx, oldestID, 1000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest record error = %v, want ErrNotFound", err)
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if _, ok := recordIDFromName(entry.Name()); ok {
			count++
		}
	}
	if count != MaxTerminalRecordsPerUID {
		t.Fatalf("terminal record count = %d, want %d", count, MaxTerminalRecordsPerUID)
	}
}

func TestStoreRefusesSymlinkedRecordsAndUnsafeArtifactNames(t *testing.T) {
	store := newTestStore(t)
	claim := testClaim(6, 1000, "v1.2.3")
	if err := os.MkdirAll(store.root, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, store.recordPath(claim.ID)); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if _, err := store.Get(context.Background(), claim.ID, claim.UID); err == nil {
		t.Fatal("Get accepted a symlinked operation record")
	}
	if _, err := store.ArtifactPath(claim.ID, "../result.json"); err == nil {
		t.Fatal("ArtifactPath accepted traversal")
	}
}

func TestReadRegularFileRejectsPathsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, name := range []string{"../record.json", outside} {
		if _, err := readRegularFile(root, name, maxRecordBytes); err == nil {
			t.Errorf("readRegularFile(%q) accepted a path outside its root", name)
		}
	}
}

func TestStoreRejectsMalformedRecord(t *testing.T) {
	store := newTestStore(t)
	claim := testClaim(7, 1000, "v1.2.3")
	if err := os.MkdirAll(store.root, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(store.recordPath(claim.ID), []byte(`{"id":`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := store.Get(context.Background(), claim.ID, claim.UID); err == nil {
		t.Fatal("Get accepted a malformed operation record")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "operations"))
}

func testClaim(sequence int, uid uint32, target string) Claim {
	id := fmt.Sprintf("00000000-0000-4000-8000-%012x", sequence)
	return Claim{
		ID:                 id,
		Route:              testRoute,
		UID:                uid,
		RequestFingerprint: Fingerprint(testRoute, target),
		Target:             target,
	}
}
