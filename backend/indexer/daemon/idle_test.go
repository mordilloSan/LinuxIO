package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestUnlockIndexStartsFreshIdleGrace(t *testing.T) {
	d := &daemon{}
	d.running.Store(true)
	d.activityMu.Lock()
	d.lastActivity = time.Now().Add(-idleGrace)
	d.activityMu.Unlock()

	d.activityMu.Lock()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		d.unlockIndex()
		close(done)
	}()
	<-started
	time.Sleep(10 * time.Millisecond)
	heldUntilActivityPublished := d.running.Load()
	d.activityMu.Unlock()
	<-done

	if !heldUntilActivityPublished {
		t.Fatal("operation pin released before activity was published")
	}
	if d.running.Load() {
		t.Fatal("operation pin remained active")
	}
	if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
		t.Fatalf("operation release did not start a fresh grace: idle for %v", idleFor)
	}
}

func TestRequestCompletionPublishesActivityBeforeRelease(t *testing.T) {
	d := &daemon{}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := d.activityMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	<-entered

	d.activityMu.Lock()
	close(release)
	time.Sleep(10 * time.Millisecond)
	heldUntilActivityPublished := d.activeRequests.Load()
	d.activityMu.Unlock()
	<-done

	if heldUntilActivityPublished != 1 {
		t.Fatalf("active requests while publishing completion = %d, want 1", heldUntilActivityPublished)
	}
	if d.activeRequests.Load() != 0 {
		t.Fatal("request pin remained active")
	}
	if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
		t.Fatalf("request completion did not start a fresh grace: idle for %v", idleFor)
	}
}
