package network

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	networkbackend "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/network"
	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
)

func stubBridgeHandoffBackend(t *testing.T) *int {
	t.Helper()
	oldPrepare := prepareBridgeHandoff
	oldApply := applyBridgeHandoff
	oldConfirm := confirmBridgeHandoff
	oldRevert := revertBridgeHandoff
	t.Cleanup(func() {
		prepareBridgeHandoff = oldPrepare
		applyBridgeHandoff = oldApply
		confirmBridgeHandoff = oldConfirm
		revertBridgeHandoff = oldRevert
	})
	applyCalls := 0
	prepareBridgeHandoff = func(_ context.Context, _ networkbackend.Environment, plan networkbackend.BridgeHandoffPlan) (networkbackend.BridgeHandoffState, error) {
		return networkbackend.BridgeHandoffState{Plan: plan, Backend: "netplan", MemberMAC: "00:11:22:33:44:55"}, nil
	}
	applyBridgeHandoff = func(_ context.Context, _ networkbackend.Environment, state *networkbackend.BridgeHandoffState) error {
		applyCalls++
		state.Handle = "/io/netplan/Netplan/config/1"
		return nil
	}
	confirmBridgeHandoff = func(context.Context, networkbackend.Environment, *networkbackend.BridgeHandoffState) error {
		return nil
	}
	revertBridgeHandoff = func(context.Context, *networkbackend.BridgeHandoffState) error { return nil }
	return &applyCalls
}

func newHandoffTestService(t *testing.T, now *time.Time) *durableBridgeHandoffService {
	t.Helper()
	return &durableBridgeHandoffService{
		store: durabletask.NewStore(filepath.Join(t.TempDir(), "durable-operations")),
		now:   func() time.Time { return *now },
	}
}

func handoffRequest(id string) apischema.NetworkBridgeHandoffRequest {
	return apischema.NetworkBridgeHandoffRequest{
		OperationID: id, Name: "br0", Member: "eth0", ConsoleAcknowledged: true,
	}
}

func TestDurableBridgeHandoffStoresOnlyNativeHandleAndResumes(t *testing.T) {
	applyCalls := stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	req := handoffRequest("00000000-0000-4000-8000-000000000092")

	status, err := service.Start(context.Background(), 1000, req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if status.State != apischema.NetworkBridgeHandoffAwaitingConfirmation || status.Deadline == "" {
		t.Fatalf("status = %+v", status)
	}
	status, err = service.Start(context.Background(), 1000, req)
	if err != nil {
		t.Fatalf("resume Start: %v", err)
	}
	if status.State != apischema.NetworkBridgeHandoffAwaitingConfirmation || *applyCalls != 1 {
		t.Fatalf("resumed status = %+v, apply calls = %d", status, *applyCalls)
	}
	record, err := service.store.Get(context.Background(), req.OperationID, 1000)
	if err != nil {
		t.Fatalf("get durable record: %v", err)
	}
	if record.State != durabletask.StateRunning || record.Executor.Handle != "/io/netplan/Netplan/config/1" || record.Executor.Identity != "system-bus" {
		t.Fatalf("record = %+v", record)
	}
}

func TestBridgeHandoffConfirmAndRevertUseStoredHandle(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)

	confirmed := ""
	confirmBridgeHandoff = func(_ context.Context, _ networkbackend.Environment, state *networkbackend.BridgeHandoffState) error {
		confirmed = state.Handle
		return nil
	}
	first := handoffRequest("00000000-0000-4000-8000-000000000093")
	if _, err := service.Start(context.Background(), 1000, first); err != nil {
		t.Fatalf("Start confirm operation: %v", err)
	}
	status, err := service.Confirm(context.Background(), 1000, first.OperationID)
	if err != nil || status.State != apischema.NetworkBridgeHandoffConfirmed || confirmed != "/io/netplan/Netplan/config/1" {
		t.Fatalf("Confirm status = %+v, handle = %q, error = %v", status, confirmed, err)
	}

	reverted := ""
	revertBridgeHandoff = func(_ context.Context, state *networkbackend.BridgeHandoffState) error {
		reverted = state.Handle
		return nil
	}
	second := handoffRequest("00000000-0000-4000-8000-000000000094")
	if _, startErr := service.Start(context.Background(), 1000, second); startErr != nil {
		t.Fatalf("Start revert operation: %v", startErr)
	}
	status, err = service.Revert(context.Background(), 1000, second.OperationID)
	if err != nil || status.State != apischema.NetworkBridgeHandoffReverted || reverted != "/io/netplan/Netplan/config/1" {
		t.Fatalf("Revert status = %+v, handle = %q, error = %v", status, reverted, err)
	}
}

func TestBridgeHandoffTimeoutTrustsNativeRollback(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	req := handoffRequest("00000000-0000-4000-8000-000000000095")
	if _, err := service.Start(context.Background(), 1000, req); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now = now.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout + time.Second)
	status, err := service.Status(context.Background(), 1000, req.OperationID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != apischema.NetworkBridgeHandoffReverted {
		t.Fatalf("status = %+v", status)
	}
}

func TestBridgeHandoffReconcilesExpiredOperationAcrossUIDs(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	first := handoffRequest("00000000-0000-4000-8000-000000000096")
	if _, err := service.Start(context.Background(), 1000, first); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	now = now.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout + time.Second)
	second := handoffRequest("00000000-0000-4000-8000-000000000097")
	if _, err := service.Start(context.Background(), 1001, second); err != nil {
		t.Fatalf("expired operation still blocks a different UID: %v", err)
	}
	record, err := service.store.Get(context.Background(), first.OperationID, 1000)
	if err != nil {
		t.Fatalf("get expired operation: %v", err)
	}
	if record.State != durabletask.StateCanceled {
		t.Fatalf("expired operation state = %q, want canceled", record.State)
	}
}

func TestBridgeHandoffKeepsLiveOperationExclusiveAcrossUIDs(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	first := handoffRequest("00000000-0000-4000-8000-000000000098")
	if _, err := service.Start(context.Background(), 1000, first); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	now = now.Add(networkbackend.BridgeHandoffConfirmationTimeout - time.Second)
	second := handoffRequest("00000000-0000-4000-8000-000000000099")
	if _, err := service.Start(context.Background(), 1001, second); err == nil {
		t.Fatal("live operation did not keep the route exclusive")
	}
}

func TestBridgeHandoffRejectsDecisionAfterDeadlineBeforeSafeRelease(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	request := handoffRequest("00000000-0000-4000-8000-00000000009d")
	if _, err := service.Start(context.Background(), 1000, request); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now = now.Add(networkbackend.BridgeHandoffConfirmationTimeout + time.Second)
	if _, err := service.Confirm(context.Background(), 1000, request.OperationID); err == nil {
		t.Fatal("Confirm succeeded after the confirmation deadline")
	}
	record, err := service.store.Get(context.Background(), request.OperationID, 1000)
	if err != nil {
		t.Fatalf("get handoff: %v", err)
	}
	if record.State != durabletask.StateRunning {
		t.Fatalf("expired handoff state = %q, want running until safe release", record.State)
	}
}

func TestBridgeHandoffReconcilesStaleQueuedOperationWithoutResult(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	first := handoffRequest("00000000-0000-4000-8000-00000000009a")
	if _, _, err := service.store.Claim(context.Background(), durabletask.Claim{
		ID:                 first.OperationID,
		Route:              bridgeHandoffRoute,
		UID:                1000,
		RequestFingerprint: durabletask.Fingerprint(bridgeHandoffRoute, first.Name+"\x00"+first.Member),
		Target:             first.Member,
		ExclusiveRoute:     true,
	}); err != nil {
		t.Fatalf("queue operation: %v", err)
	}

	now = now.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout - time.Second)
	second := handoffRequest("00000000-0000-4000-8000-00000000009b")
	if _, err := service.Start(context.Background(), 1001, second); err == nil {
		t.Fatal("live queued operation did not keep the route exclusive")
	}

	now = now.Add(2 * time.Second)
	if _, err := service.Start(context.Background(), 1001, second); err != nil {
		t.Fatalf("stale queued operation still blocks a fresh start: %v", err)
	}
}

func TestBridgeHandoffApplyFailureRetainsExclusivityUntilSafeRelease(t *testing.T) {
	stubBridgeHandoffBackend(t)
	successfulApply := applyBridgeHandoff
	applyBridgeHandoff = func(_ context.Context, _ networkbackend.Environment, state *networkbackend.BridgeHandoffState) error {
		state.Handle = "/io/netplan/Netplan/config/1"
		return errors.New("native rollback failed")
	}
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	first := handoffRequest("00000000-0000-4000-8000-00000000009e")
	if _, err := service.Start(context.Background(), 1000, first); err == nil {
		t.Fatal("Start unexpectedly succeeded after apply failure")
	}
	record, err := service.store.Get(context.Background(), first.OperationID, 1000)
	if err != nil {
		t.Fatalf("get failed handoff: %v", err)
	}
	if record.State != durabletask.StateQueued || record.Error == nil {
		t.Fatalf("failed handoff record = %+v, want nonterminal error", record)
	}

	now = now.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout - time.Second)
	second := handoffRequest("00000000-0000-4000-8000-00000000009f")
	if _, err := service.Start(context.Background(), 1001, second); err == nil {
		t.Fatal("active native rollback window did not keep the route exclusive")
	}

	now = now.Add(2 * time.Second)
	applyBridgeHandoff = successfulApply
	if _, err := service.Start(context.Background(), 1001, second); err != nil {
		t.Fatalf("safe release did not permit a fresh start: %v", err)
	}
}

func TestBridgeHandoffRollbackFailureDoesNotBecomeSuccessfulRevert(t *testing.T) {
	stubBridgeHandoffBackend(t)
	revertBridgeHandoff = func(context.Context, *networkbackend.BridgeHandoffState) error {
		return errors.New("rollback failed for device eth0")
	}
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	request := handoffRequest("00000000-0000-4000-8000-0000000000a3")
	if _, err := service.Start(context.Background(), 1000, request); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := service.Revert(context.Background(), 1000, request.OperationID); err == nil {
		t.Fatal("Revert unexpectedly succeeded")
	}
	now = now.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout + time.Second)
	status, err := service.Status(context.Background(), 1000, request.OperationID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != apischema.NetworkBridgeHandoffUnknown || status.Error != "rollback failed for device eth0" {
		t.Fatalf("expired failed rollback = %+v, want unknown with native error", status)
	}
}

func TestBridgeHandoffRejectsWrongUIDForStatusAndDecision(t *testing.T) {
	stubBridgeHandoffBackend(t)
	now := time.Now().UTC()
	service := newHandoffTestService(t, &now)
	request := handoffRequest("00000000-0000-4000-8000-00000000009c")
	if _, err := service.Start(context.Background(), 1000, request); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := service.Status(context.Background(), 1001, request.OperationID); err == nil {
		t.Fatal("wrong UID could read handoff status")
	}
	if _, err := service.Confirm(context.Background(), 1001, request.OperationID); err == nil {
		t.Fatal("wrong UID could confirm handoff")
	}
	if _, err := service.Revert(context.Background(), 1001, request.OperationID); err == nil {
		t.Fatal("wrong UID could revert handoff")
	}
}

func TestInterruptedHandoffDecisionKeepsLockUntilDecisionTimeout(t *testing.T) {
	now := time.Now().UTC()
	record := durabletask.Record{State: durabletask.StateLaunching, UpdatedAt: now}
	data := bridgeHandoffRecord{SafeReleaseDeadline: now.Add(5 * time.Second)}
	if handoffExpired(record, data, now.Add(6*time.Second)) {
		t.Fatal("native deadline released an active decision")
	}
	finished := now.Add(bridgeHandoffDecisionTimeout + time.Second)
	if !handoffExpired(record, data, finished) {
		t.Fatal("interrupted decision did not expire")
	}
	expireHandoffRecord(&record, finished)
	if record.State != durabletask.StateUnknown || record.Error == nil {
		t.Fatalf("interrupted decision = %+v, want unknown outcome", record)
	}
}
