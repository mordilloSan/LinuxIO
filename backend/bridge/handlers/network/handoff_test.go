package network

import (
	"context"
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
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
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
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
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
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	service := newHandoffTestService(t, &now)
	req := handoffRequest("00000000-0000-4000-8000-000000000095")
	if _, err := service.Start(context.Background(), 1000, req); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now = now.Add(networkbackend.BridgeHandoffConfirmationTimeout + time.Second)
	status, err := service.Status(context.Background(), 1000, req.OperationID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != apischema.NetworkBridgeHandoffReverted {
		t.Fatalf("status = %+v", status)
	}
}
