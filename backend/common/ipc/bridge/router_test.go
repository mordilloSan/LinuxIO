package bridge

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestRouterJobFastCompleteReturnsTerminalSnapshot(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	router.JobRunner("test.fast", func(ctx context.Context, job *Job, args []string) (any, error) {
		return map[string]any{"ok": true}, nil
	}, ActionDefault)

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{Route: "test.fast"})
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}

	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	var snapshot Snapshot
	if unmarshalErr := json.Unmarshal(result.Data, &snapshot); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", unmarshalErr)
	}
	if snapshot.State != StateCompleted {
		t.Fatalf("state = %q, want completed", snapshot.State)
	}
	if snapshot.Result == nil {
		t.Fatal("fast-complete snapshot missing result")
	}
	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose {
		t.Fatalf("close opcode = 0x%02x, want OpStreamClose", closeFrame.Opcode)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestRouterJobTimeoutReturnsFailedSnapshot(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "timeout_test"
	policy.Timeout = 10 * time.Millisecond
	router.JobRunner("test.timeout", func(ctx context.Context, job *Job, args []string) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, policy)

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{Route: "test.timeout"})
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}

	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	var snapshot Snapshot
	if unmarshalErr := json.Unmarshal(result.Data, &snapshot); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", unmarshalErr)
	}
	if snapshot.State != StateFailed {
		t.Fatalf("state = %q, want failed", snapshot.State)
	}
	if snapshot.Error == nil {
		t.Fatal("timeout snapshot missing error")
	}
	if snapshot.Error.Code != 504 {
		t.Fatalf("error code = %d, want 504", snapshot.Error.Code)
	}
	if !strings.Contains(snapshot.Error.Message, "timed out") {
		t.Fatalf("error message = %q, want timeout message", snapshot.Error.Message)
	}

	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose {
		t.Fatalf("close opcode = 0x%02x, want OpStreamClose", closeFrame.Opcode)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestRouterRejectsRegisteredJobsNamespace(t *testing.T) {
	router := NewRouter(NewRegistry())
	defer func() {
		if recover() == nil {
			t.Fatal("expected reserved jobs.* registration to panic")
		}
	}()
	router.Query("jobs.get", func(ctx context.Context, args []string, emit Events) error {
		return emit.Result(nil)
	})
}
