package packagekit

import (
	"context"
	"errors"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

func TestOperationGateSerializesOperations(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- withOperationGate(context.Background(), func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withOperationGate(context.Background(), func() error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("second operation entered before first released the gate")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first gate holder: %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second operation did not enter after first released the gate")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second gate holder: %v", err)
	}
}

func TestOperationGateHonorsContextCancellation(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- withOperationGate(context.Background(), func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered
	defer func() {
		close(releaseFirst)
		<-firstDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := withOperationGate(ctx, func() error {
		t.Fatal("canceled gate waiter unexpectedly entered")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("operation gate error = %v, want context deadline", err)
	}
}

func TestSignalsFlowWhilePackageKitGateHeld(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	conn := bus.OwnName(t, "org.example.Signals")
	sub, err := dbusclient.WatchObjectSignals(context.Background(), "/org/example/Object", 1, "org.example.Interface", "Changed")
	if err != nil {
		t.Fatalf("WatchObjectSignals: %v", err)
	}
	defer sub.Close(context.Background())

	gateEntered := make(chan struct{})
	releaseGate := make(chan struct{})
	gateDone := make(chan error, 1)
	go func() {
		gateDone <- withOperationGate(context.Background(), func() error {
			close(gateEntered)
			<-releaseGate
			return nil
		})
	}()
	<-gateEntered
	defer func() {
		close(releaseGate)
		if err := <-gateDone; err != nil {
			t.Errorf("gate holder: %v", err)
		}
	}()

	if err := conn.Emit(godbus.ObjectPath("/org/example/Object"), "org.example.Interface.Changed", "ok"); err != nil {
		t.Fatalf("emit signal: %v", err)
	}
	select {
	case sig := <-sub.Chan():
		if sig.Name != "org.example.Interface.Changed" {
			t.Fatalf("signal = %s", sig.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("signal was blocked while PackageKit gate was held")
	}
}
