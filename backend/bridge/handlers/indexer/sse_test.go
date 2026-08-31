package indexer

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func collectEvents(t *testing.T, input string) ([]SSEEvent, error) {
	t.Helper()
	var result []SSEEvent
	decoder := NewSSEDecoder(context.Background(), strings.NewReader(input))
	for {
		evt, err := decoder.Next()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		result = append(result, evt)
	}
}

func TestSSEDecoder_StandardFlow(t *testing.T) {
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

func TestSSEDecoderReadsSynchronously(t *testing.T) {
	decoder := NewSSEDecoder(context.Background(), strings.NewReader("event:progress\ndata:{\"status\":\"running\"}\n\nevent:complete\ndata:{}\n\n"))
	first, err := decoder.Next()
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if first.Type != "progress" || first.Data != `{"status":"running"}` {
		t.Fatalf("first event = %#v", first)
	}
	second, err := decoder.Next()
	if err != nil {
		t.Fatalf("second event: %v", err)
	}
	if second.Type != "complete" || second.Data != "{}" {
		t.Fatalf("second event = %#v", second)
	}
}

func TestSSEDecoder_MultilineData(t *testing.T) {
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

func TestSSEDecoder_Comments(t *testing.T) {
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

func TestSSEDecoder_CRLFLineEndings(t *testing.T) {
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

func TestSSEDecoder_EOFFlushesPartial(t *testing.T) {
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

func TestSSEDecoder_EmptyDataField(t *testing.T) {
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

func TestSSEDecoder_ContextCancellation(t *testing.T) {
	// Use a pipe; close the reader side to simulate what happens when
	// an HTTP request context is cancelled (response body gets closed).
	pr, pw := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decoder := NewSSEDecoder(ctx, pr)

	// Close the reader to unblock the scanner after cancellation.
	pr.Close()
	pw.Close()

	if _, err := decoder.Next(); !errors.Is(err, context.Canceled) {
		t.Errorf("error after cancel = %v, want context canceled", err)
	}
}

func TestSSEDecoder_EOFIsCleanClose(t *testing.T) {
	// Empty input — should produce no events and no error
	decoder := NewSSEDecoder(context.Background(), strings.NewReader(""))
	if _, err := decoder.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("error at EOF = %v, want EOF", err)
	}
}

func TestSSEDecoder_EventTypeWithSpaces(t *testing.T) {
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

func TestSSEDecoder_MultipleEmptyLines(t *testing.T) {
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

func TestSSEDecoder_LargeDataLine(t *testing.T) {
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

func TestSSEDecoder_BoundsAccumulatedEventData(t *testing.T) {
	var b strings.Builder
	for range 6 {
		b.WriteString("event:progress\ndata:")
		b.WriteString(strings.Repeat("x", 200*1024))
		b.WriteString("\n")
	}

	_, err := collectEvents(t, b.String())
	if err == nil {
		t.Fatal("expected accumulated event data size error")
	}
	if !strings.Contains(err.Error(), "event data exceeds") {
		t.Fatalf("error = %v, want event data limit", err)
	}
}
