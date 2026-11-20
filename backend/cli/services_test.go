package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

var (
	socketPending   = systemd.SocketState{JobPending: true}
	socketListening = systemd.SocketState{ActiveState: "active", Result: "success"}
	// How systemd leaves a socket whose bind found its address in use.
	socketCouldNotListen = systemd.SocketState{ActiveState: "failed", Result: "resources"}
)

// fakeManager answers the manager calls of a restart. Each read of the web
// socket takes the next of states; events records every call in order.
type fakeManager struct {
	states   []systemd.SocketState
	startErr error
	events   []string
}

func useFakeManager(t *testing.T, states ...systemd.SocketState) *fakeManager {
	t.Helper()
	m := &fakeManager{states: states}
	prevRestart, prevStart, prevRead, prevPause := restartUnit, startUnit, readSocketState, pause
	t.Cleanup(func() {
		restartUnit, startUnit, readSocketState, pause = prevRestart, prevStart, prevRead, prevPause
	})
	restartUnit = func(_ context.Context, name string) error {
		m.events = append(m.events, "restart "+name)
		return nil
	}
	startUnit = func(_ context.Context, name string) error {
		m.events = append(m.events, "start "+name)
		return m.startErr
	}
	readSocketState = func(_ context.Context, name string) (systemd.SocketState, error) {
		m.events = append(m.events, "read "+name)
		if len(m.states) == 0 {
			t.Errorf("%s read more often than planned", name)
			return systemd.SocketState{}, errors.New("no state planned")
		}
		state := m.states[0]
		m.states = m.states[1:]
		return state, nil
	}
	pause = func(ctx context.Context, d time.Duration) error {
		m.events = append(m.events, "wait "+d.String())
		return ctx.Err()
	}
	return m
}

const webSocket = linuxioWebserverSocketName

func TestFullRestartStartsTheWebSocketAgainWhenItCouldNotListen(t *testing.T) {
	m := useFakeManager(t, socketPending, socketCouldNotListen, socketPending, socketListening)

	if code := runRestart(t.Context(), []string{"--full"}); code != 0 {
		t.Fatalf("restart --full exited %d, want 0", code)
	}
	poll, retry := "wait "+unitJobPollInterval.String(), "wait "+webSocketRetryDelay.String()
	want := []string{
		"restart " + linuxioTargetName,
		"read " + webSocket, poll, "read " + webSocket,
		retry, "start " + webSocket,
		"read " + webSocket, poll, "read " + webSocket,
	}
	if !slices.Equal(m.events, want) {
		t.Fatalf("calls = %q, want %q", m.events, want)
	}
}

func TestControlPlaneRestartLeavesTheWebSocketAlone(t *testing.T) {
	m := useFakeManager(t)

	if code := runRestart(t.Context(), nil); code != 0 {
		t.Fatalf("restart exited %d, want 0", code)
	}
	want := []string{
		"restart " + linuxioBridgeSocketUserService,
		"restart " + linuxioAuthSocketName,
		"restart " + linuxioWebserverServiceName,
	}
	if !slices.Equal(m.events, want) {
		t.Fatalf("calls = %q, want %q", m.events, want)
	}
}

func TestFullRestartDoesNotRetryAnotherFailure(t *testing.T) {
	for _, state := range []systemd.SocketState{
		{ActiveState: "failed", Result: "start-limit-hit"},
		{ActiveState: "failed", Result: "trigger-limit-hit"},
		{ActiveState: "inactive", Result: "success"},
	} {
		t.Run(state.ActiveState+" "+state.Result, func(t *testing.T) {
			m := useFakeManager(t, state)

			err := awaitWebSocket(t.Context())
			if err == nil || !strings.Contains(err.Error(), state.ActiveState) || !strings.Contains(err.Error(), state.Result) {
				t.Fatalf("awaitWebSocket = %v, want an error naming %s and %s", err, state.ActiveState, state.Result)
			}
			if want := []string{"read " + webSocket}; !slices.Equal(m.events, want) {
				t.Fatalf("calls = %q, want %q", m.events, want)
			}
		})
	}

	t.Run("start refused", func(t *testing.T) {
		m := useFakeManager(t, socketCouldNotListen)
		m.startErr = errors.New("unit masked")

		if err := awaitWebSocket(t.Context()); !errors.Is(err, m.startErr) {
			t.Fatalf("awaitWebSocket = %v, want %v", err, m.startErr)
		}
		if got := countEvents(m.events, "start "+webSocket); got != 1 {
			t.Fatalf("started %s %d times, want once: %q", webSocket, got, m.events)
		}
	})
}

func TestFullRestartGivesUpAfterItsRetries(t *testing.T) {
	m := useFakeManager(t, slices.Repeat([]systemd.SocketState{socketCouldNotListen}, webSocketRetries+1)...)

	err := awaitWebSocket(t.Context())
	if err == nil || !strings.Contains(err.Error(), `"resources"`) {
		t.Fatalf("awaitWebSocket = %v, want the last failure with its result", err)
	}
	if got := countEvents(m.events, "start "+webSocket); got != webSocketRetries {
		t.Fatalf("started %s %d times, want %d: %q", webSocket, got, webSocketRetries, m.events)
	}
	if len(m.states) != 0 {
		t.Fatalf("%d planned states left unread", len(m.states))
	}
}

func TestFullRestartStopsWaitingWhenCancelled(t *testing.T) {
	m := useFakeManager(t, socketCouldNotListen)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiting := make(chan struct{})
	pause = func(ctx context.Context, _ time.Duration) error {
		close(waiting)
		return sleepContext(ctx, time.Hour)
	}

	done := make(chan error, 1)
	go func() { done <- awaitWebSocket(ctx) }()
	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("awaitWebSocket = %v before it waited to start the socket again", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("awaitWebSocket = %v, want %v", err, context.Canceled)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("awaitWebSocket kept waiting after its context was cancelled")
	}
	if got := countEvents(m.events, "start "+webSocket); got != 0 {
		t.Fatalf("started %s after the cancel: %q", webSocket, m.events)
	}
}

func countEvents(events []string, event string) int {
	n := 0
	for _, e := range events {
		if e == event {
			n++
		}
	}
	return n
}
