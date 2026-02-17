package indexer

import (
	"context"
	"io"
	"strings"
	"testing"
)

func collectEvents(t *testing.T, input string) ([]SSEEvent, error) {
	t.Helper()
	ctx := context.Background()
	events, errCh := ReadSSE(ctx, strings.NewReader(input))

	var result []SSEEvent
	for evt := range events {
		result = append(result, evt)
	}
	if err, ok := <-errCh; ok && err != nil {
		return result, err
	}
	return result, nil
}

func TestReadSSE_StandardFlow(t *testing.T) {
	input := "event:started\ndata:{\"status\":\"ok\"}\n\nevent:complete\ndata:{\"done\":true}\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "started" || events[0].Data != `{"status":"ok"}` {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "complete" || events[1].Data != `{"done":true}` {
		t.Errorf("event[1] = %+v", events[1])
	}
}

func TestReadSSE_MultilineData(t *testing.T) {
	input := "event:message\ndata:line1\ndata:line2\ndata:line3\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "line1\nline2\nline3" {
		t.Errorf("expected multiline data, got %q", events[0].Data)
	}
}

func TestReadSSE_Comments(t *testing.T) {
	input := ": this is a comment\nevent:ping\ndata:hello\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "ping" {
		t.Errorf("expected type 'ping', got %q", events[0].Type)
	}
}

func TestReadSSE_CRLFLineEndings(t *testing.T) {
	input := "event:test\r\ndata:value\r\n\r\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "test" || events[0].Data != "value" {
		t.Errorf("event = %+v", events[0])
	}
}

func TestReadSSE_EOFFlushesPartial(t *testing.T) {
	// No trailing empty line — event should still be flushed on EOF
	input := "event:partial\ndata:some data"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event from EOF flush, got %d", len(events))
	}
	if events[0].Type != "partial" || events[0].Data != "some data" {
		t.Errorf("event = %+v", events[0])
	}
}

func TestReadSSE_EmptyDataField(t *testing.T) {
	input := "event:empty\ndata:\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "" {
		t.Errorf("expected empty data, got %q", events[0].Data)
	}
}

func TestReadSSE_ContextCancellation(t *testing.T) {
	// Use a pipe; close the reader side to simulate what happens when
	// an HTTP request context is cancelled (response body gets closed).
	pr, pw := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	events, errCh := ReadSSE(ctx, pr)

	// Cancel context and close the reader to unblock the scanner.
	cancel()
	pr.Close()
	pw.Close()

	// Drain — should complete quickly
	var count int
	for range events {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 events after cancel, got %d", count)
	}

	// errCh should be closed with no error (context cancellation is not reported)
	if err, ok := <-errCh; ok && err != nil {
		t.Errorf("unexpected error after cancel: %v", err)
	}
}

func TestReadSSE_EOFIsCleanClose(t *testing.T) {
	// Empty input — should produce no events and no error
	events, errCh := ReadSSE(context.Background(), strings.NewReader(""))

	var count int
	for range events {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 events, got %d", count)
	}

	if err, ok := <-errCh; ok && err != nil {
		t.Errorf("EOF should not produce error, got: %v", err)
	}
}

func TestReadSSE_EventTypeWithSpaces(t *testing.T) {
	input := "event:  progress  \ndata:42\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "progress" {
		t.Errorf("expected trimmed type 'progress', got %q", events[0].Type)
	}
}

func TestReadSSE_MultipleEmptyLines(t *testing.T) {
	// Multiple consecutive empty lines should not produce empty events
	input := "event:one\ndata:1\n\n\n\nevent:two\ndata:2\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
}

func TestReadSSE_LargeDataLine(t *testing.T) {
	large := strings.Repeat("x", 70*1024)
	input := "event:progress\ndata:" + large + "\n\n"

	events, err := collectEvents(t, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != large {
		t.Fatalf("unexpected data size: got=%d want=%d", len(events[0].Data), len(large))
	}
}
