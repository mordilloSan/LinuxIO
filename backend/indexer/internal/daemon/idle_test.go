package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleTimingMatchesWebserver(t *testing.T) {
	if idleGrace != 90*time.Second {
		t.Errorf("idleGrace = %v, want 90s", idleGrace)
	}
	if idleCheckInterval != 15*time.Second {
		t.Errorf("idleCheckInterval = %v, want 15s", idleCheckInterval)
	}
}

func TestStopWhenIdleCancelsDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &daemon{}
	d.activityMu.Lock()
	d.lastActivity = time.Now().Add(-idleGrace - time.Second)
	d.activityMu.Unlock()
	go d.stopWhenIdle(ctx, cancel, func() bool { return false }, time.Millisecond)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("idle daemon did not stop")
	}
}

func TestRuntimeDirectoryPinsIdleWatcher(t *testing.T) {
	var markerPresent atomic.Bool
	markerPresent.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	d := &daemon{}
	d.activityMu.Lock()
	d.lastActivity = time.Now().Add(-idleGrace - time.Second)
	d.activityMu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.stopWhenIdle(ctx, cancel, markerPresent.Load, time.Millisecond)
	}()
	defer func() {
		cancel()
		<-done
	}()

	previous := d.activityTime()
	waitForCondition(t, time.Second, func() bool {
		return d.activityTime().After(previous)
	})
	markerPresent.Store(false)
	if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
		t.Fatalf("removing runtime marker did not leave a fresh grace: idle for %v", idleFor)
	}

	select {
	case <-time.After(25 * time.Millisecond):
		if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
			t.Fatalf("runtime marker did not refresh activity clock: idle for %v", idleFor)
		}
	case <-ctx.Done():
		t.Fatal("runtime marker should pin daemon")
	}
}
