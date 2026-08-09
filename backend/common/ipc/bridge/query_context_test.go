package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestReceiveOnlyChannelContextClientEndCancels(t *testing.T) {
	tests := []struct {
		name   string
		opcode byte
	}{
		{name: "abort", opcode: relay.OpStreamAbort},
		{name: "close", opcode: relay.OpStreamClose},
		{name: "disconnect"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, client := net.Pipe()
			ctx, cleanup := ReceiveOnlyChannelContext(context.Background(), server)
			defer cleanup()
			defer client.Close()
			if tc.opcode == 0 {
				_ = client.Close()
			} else if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: tc.opcode}); err != nil {
				t.Fatalf("write client frame: %v", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("receive-only Channel context did not cancel")
			}
		})
	}
}

func TestCallExplicitAbortCancelsHandlerContext(t *testing.T) {
	router := NewRouter(NewTaskService())
	started := make(chan struct{})
	canceled := make(chan struct{})
	router.Call("test.call.abort", func(ctx context.Context, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.Dispatch(context.Background(), server, Request{Route: "test.call.abort"})
	}()

	waitForSignal(t, started, "call handler did not start")
	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort}); err != nil {
		t.Fatalf("WriteRelayFrame(abort): %v", err)
	}
	waitForSignal(t, canceled, "explicit abort did not cancel the call context")
	_ = client.Close()
	if err := <-dispatchDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch error = %v, want context.Canceled", err)
	}
}

func TestCallDisconnectDoesNotCancelHandlerContext(t *testing.T) {
	router := NewRouter(NewTaskService())
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	router.Call("test.call.disconnect", func(ctx context.Context, _ any) (any, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return nil, ctx.Err()
		case <-release:
			return nil, nil
		}
	})

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.Dispatch(context.Background(), server, Request{Route: "test.call.disconnect"})
	}()

	waitForSignal(t, started, "call handler did not start")
	_ = client.Close()
	assertNoSignal(t, canceled, "ordinary disconnect canceled the call context")
	close(release)
	if err := <-dispatchDone; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Dispatch error = %v, want io.ErrClosedPipe", err)
	}
}

func TestTaskCallPrimitiveExplicitAbortCancelsHandlerContext(t *testing.T) {
	router := NewRouter(NewTaskService())
	started := make(chan struct{})
	canceled := make(chan struct{})
	handler := func(ctx context.Context, _ net.Conn, _ Request) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.dispatchTaskCallPrimitive(
			context.Background(),
			server,
			Request{Route: "tasks.get"},
			handler,
		)
	}()

	waitForSignal(t, started, "task call primitive handler did not start")
	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort}); err != nil {
		t.Fatalf("WriteRelayFrame(abort): %v", err)
	}
	waitForSignal(t, canceled, "explicit abort did not cancel the primitive context")
	_ = client.Close()
	if err := <-dispatchDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatchTaskCallPrimitive error = %v, want context.Canceled", err)
	}
}

func TestTaskCallPrimitivesHonorCanceledContext(t *testing.T) {
	for _, route := range []string{"tasks.get", "tasks.list", "tasks.cancel"} {
		t.Run(route, func(t *testing.T) {
			registry := NewTaskService()
			router := NewRouter(registry)
			owner := TaskOwner{Username: "alice", UID: 1000}
			task, err := registry.CreateForOwner(
				"test.task.primitive.abort",
				nil,
				owner,
			)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			rawRequest := json.RawMessage(`{}`)
			if route != "tasks.list" {
				rawRequest = json.RawMessage(`{"taskId":"` + task.ID() + `"}`)
			}

			server, client := net.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := router.dispatchTaskPrimitive(
				ctx,
				server,
				Request{Route: route, RawRequest: rawRequest, Owner: owner},
			); !errors.Is(err, context.Canceled) {
				_ = server.Close()
				_ = client.Close()
				t.Fatalf("dispatchTaskPrimitive error = %v, want context.Canceled", err)
			}
			_ = server.Close()
			_ = client.Close()
			if route == "tasks.cancel" && task.Snapshot().State != TaskStateQueued {
				t.Fatalf("aborted tasks.cancel changed state to %q", task.Snapshot().State)
			}
		})
	}
}

func TestTaskStartDisconnectDoesNotCancelTaskContext(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	router.TaskRunner(
		"test.task.disconnect",
		func(ctx context.Context, _ *Task, _ any) (any, error) {
			close(started)
			select {
			case <-ctx.Done():
				close(canceled)
				return nil, ctx.Err()
			case <-release:
				return nil, nil
			}
		},
		TaskDefault,
	)

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.Dispatch(context.Background(), server, Request{Route: "test.task.disconnect"})
	}()

	waitForSignal(t, started, "task runner did not start")
	_ = client.Close()
	assertNoSignal(t, canceled, "task-start disconnect canceled the detached task")
	close(release)

	select {
	case <-dispatchDone:
	case <-time.After(time.Second):
		t.Fatal("Dispatch did not return after the detached task completed")
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func assertNoSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(message)
	case <-time.After(50 * time.Millisecond):
	}
}
