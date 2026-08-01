package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestQueryExplicitAbortCancelsHandlerContext(t *testing.T) {
	router := NewRouter(NewRegistry())
	started := make(chan struct{})
	canceled := make(chan struct{})
	router.Query("test.query.abort", func(ctx context.Context, _ any, _ Events) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	})

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.Dispatch(context.Background(), server, Request{Route: "test.query.abort"})
	}()

	waitForSignal(t, started, "query handler did not start")
	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort}); err != nil {
		t.Fatalf("WriteRelayFrame(abort): %v", err)
	}
	waitForSignal(t, canceled, "explicit abort did not cancel the query context")

	_ = client.Close()
	if err := <-dispatchDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch error = %v, want context.Canceled", err)
	}
}

func TestQueryDisconnectDoesNotCancelHandlerContext(t *testing.T) {
	router := NewRouter(NewRegistry())
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	router.Query("test.query.disconnect", func(ctx context.Context, _ any, _ Events) error {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return ctx.Err()
		case <-release:
			return nil
		}
	})

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.Dispatch(context.Background(), server, Request{Route: "test.query.disconnect"})
	}()

	waitForSignal(t, started, "query handler did not start")
	_ = client.Close()
	assertNoSignal(t, canceled, "ordinary disconnect canceled the query context")
	close(release)
	if err := <-dispatchDone; err != nil {
		t.Fatalf("Dispatch error = %v, want nil", err)
	}
}

func TestJobQueryPrimitiveExplicitAbortCancelsHandlerContext(t *testing.T) {
	router := NewRouter(NewRegistry())
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
		dispatchDone <- router.dispatchJobQueryPrimitive(
			context.Background(),
			server,
			Request{Route: "jobs.get"},
			handler,
		)
	}()

	waitForSignal(t, started, "job query primitive handler did not start")
	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort}); err != nil {
		t.Fatalf("WriteRelayFrame(abort): %v", err)
	}
	waitForSignal(t, canceled, "explicit abort did not cancel the primitive context")
	_ = client.Close()
	if err := <-dispatchDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatchJobQueryPrimitive error = %v, want context.Canceled", err)
	}
}

func TestJobQueryPrimitivesHonorCanceledContext(t *testing.T) {
	for _, route := range []string{"jobs.get", "jobs.list", "jobs.cancel"} {
		t.Run(route, func(t *testing.T) {
			registry := NewRegistry()
			router := NewRouter(registry)
			owner := Owner{Username: "alice", UID: 1000}
			job, err := registry.CreateForOwner(
				"test.job.primitive.abort",
				nil,
				owner,
			)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			rawRequest := json.RawMessage(`{}`)
			if route != "jobs.list" {
				rawRequest = json.RawMessage(`{"jobId":"` + job.ID() + `"}`)
			}

			server, client := net.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := router.dispatchJobPrimitive(
				ctx,
				server,
				Request{Route: route, RawRequest: rawRequest, Owner: owner},
			); !errors.Is(err, context.Canceled) {
				_ = server.Close()
				_ = client.Close()
				t.Fatalf("dispatchJobPrimitive error = %v, want context.Canceled", err)
			}
			_ = server.Close()
			_ = client.Close()
			if route == "jobs.cancel" && job.Snapshot().State != StateQueued {
				t.Fatalf("aborted jobs.cancel changed state to %q", job.Snapshot().State)
			}
		})
	}
}

func TestJobStartDisconnectDoesNotCancelJobContext(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	router.JobRunner(
		"test.job.disconnect",
		func(ctx context.Context, _ *Job, _ any) (any, error) {
			close(started)
			select {
			case <-ctx.Done():
				close(canceled)
				return nil, ctx.Err()
			case <-release:
				return nil, nil
			}
		},
		ActionDefault,
	)

	server, client := net.Pipe()
	dispatchDone := make(chan error, 1)
	go func() {
		defer server.Close()
		dispatchDone <- router.Dispatch(context.Background(), server, Request{Route: "test.job.disconnect"})
	}()

	waitForSignal(t, started, "job runner did not start")
	_ = client.Close()
	assertNoSignal(t, canceled, "job-start disconnect canceled the detached job")
	close(release)

	select {
	case <-dispatchDone:
	case <-time.After(time.Second):
		t.Fatal("Dispatch did not return after the detached job completed")
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
