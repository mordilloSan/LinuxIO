package logs

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestGeneralLogsChannelRejectsInvalidAfterCursor(t *testing.T) {
	frames, done, closeClient := openGeneralLogsChannel(context.Background(), apischema.GeneralLogsFollowRequest{AfterCursor: new("bad\ncursor")})
	defer closeClient()
	capture := collectChannelFrames(t, frames)
	if capture.result == nil || capture.result.Status != "error" {
		t.Fatalf("result = %#v, want error", capture.result)
	}
	if !capture.closed {
		t.Fatal("channel did not send a close frame")
	}
	if err := <-done; err != nil {
		t.Logf("channel returned after error frame: %v", err)
	}
}

func requireExecutables(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not available", name)
		}
	}
}

func requireReadableJournal(t *testing.T) {
	t.Helper()
	requireExecutables(t, "journalctl")
	if err := exec.Command("journalctl", "-q", "-n", "1", "-o", "json", "--no-pager").Run(); err != nil {
		t.Skip("journal not readable in this environment")
	}
}

func openGeneralLogsChannel(ctx context.Context, req apischema.GeneralLogsFollowRequest) (<-chan *relay.StreamFrame, <-chan error, func()) {
	server, client := net.Pipe()
	frames := make(chan *relay.StreamFrame, 32)
	done := make(chan error, 1)
	go func() {
		err := streamGeneralLogsChannel(ctx, server, runtime.Runtime{}, req)
		_ = server.Close()
		done <- err
	}()
	go func() {
		defer close(frames)
		for {
			frame, err := relay.ReadRelayFrame(client)
			if err != nil {
				return
			}
			frames <- frame
			if frame.Opcode == relay.OpStreamClose {
				return
			}
		}
	}()
	return frames, done, func() { _ = client.Close() }
}

type channelCapture struct {
	lines    []string
	progress []map[string]any
	data     map[string]any
	result   *relay.ResultFrame
	closed   bool
}

func collectChannelFrames(t *testing.T, frames <-chan *relay.StreamFrame) channelCapture {
	t.Helper()
	var capture channelCapture
	for frame := range frames {
		if frame.StreamID != 0 {
			t.Errorf("frame stream ID = %d, want 0", frame.StreamID)
		}
		capture.add(t, frame)
	}
	return capture
}

func (capture *channelCapture) add(t *testing.T, frame *relay.StreamFrame) {
	t.Helper()
	switch frame.Opcode {
	case relay.OpStreamData:
		for line := range strings.SplitSeq(string(frame.Payload), "\n") {
			if line != "" {
				capture.lines = append(capture.lines, line)
			}
		}
	case relay.OpStreamProgress:
		capture.progress = append(capture.progress, decodeChannelJSON[map[string]any](t, frame.Payload))
	case relay.OpStreamResult:
		result := decodeChannelJSON[relay.ResultFrame](t, frame.Payload)
		capture.result = &result
		if len(result.Data) > 0 {
			capture.data = decodeChannelJSON[map[string]any](t, result.Data)
		}
	case relay.OpStreamClose:
		capture.closed = true
	}
}

func decodeChannelJSON[T any](t *testing.T, payload []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode Channel frame: %v", err)
	}
	return value
}

func assertBacklogLines(t *testing.T, lines []string, maxLines int) {
	t.Helper()
	if len(lines) == 0 || len(lines) > maxLines {
		t.Fatalf("got %d lines, want 1-%d", len(lines), maxLines)
	}

	for _, line := range lines {
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("line is not valid JSON: %v (%q)", err, line)
		}
		if _, ok := fields["__CURSOR"]; !ok {
			t.Error("line missing __CURSOR")
		}
		if _, ok := fields["_CMDLINE"]; ok {
			t.Error("line still carries _CMDLINE — trimming not applied")
		}
		if timestamp, ok := fields["__REALTIME_TIMESTAMP"].(string); !ok || timestamp == "" {
			t.Error("line missing __REALTIME_TIMESTAMP")
		}
	}
}

func TestEmitGeneralLogsBacklogReversesJournalOrder(t *testing.T) {
	server, client := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- emitGeneralLogsBacklog(server, []string{
			`{"__CURSOR":"newest"}`,
			`{"__CURSOR":"middle"}`,
			`{"__CURSOR":"oldest"}`,
		})
		_ = server.Close()
	}()
	defer func() { _ = client.Close() }()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("read backlog frame: %v", err)
	}
	if frame.Opcode != relay.OpStreamData {
		t.Fatalf("frame opcode = %d, want stream data", frame.Opcode)
	}
	want := "{\"__CURSOR\":\"oldest\"}\n{\"__CURSOR\":\"middle\"}\n{\"__CURSOR\":\"newest\"}\n"
	if got := string(frame.Payload); got != want {
		t.Errorf("backlog payload = %q, want %q", got, want)
	}
	if err := <-done; err != nil {
		t.Fatalf("emit backlog: %v", err)
	}
}

func assertBacklogComplete(t *testing.T, progress []map[string]any, wantTruncated bool) {
	t.Helper()
	for _, payload := range progress {
		if payload["type"] != "backlog_complete" {
			continue
		}
		if truncated, _ := payload["truncated"].(bool); truncated != wantTruncated {
			t.Errorf("backlog_complete truncated = %v, want %v", truncated, wantTruncated)
		}
		return
	}
	t.Error("backlog_complete progress marker not emitted")
}

// TestGeneralLogsChannelBacklogOnly runs the real journalctl backlog phase
// end-to-end: field trimming, cap respected, and the backlog_complete marker
// present. Reversal from journalctl's newest-first order is tested separately
// because realtime timestamps can jump when the host clock is corrected.
func TestGeneralLogsChannelBacklogOnly(t *testing.T) {
	requireReadableJournal(t)
	frames, done, closeClient := openGeneralLogsChannel(context.Background(), apischema.GeneralLogsFollowRequest{
		Lines:  new("5"),
		Follow: new(false),
	})
	defer closeClient()
	capture := collectChannelFrames(t, frames)
	err := <-done
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if capture.result == nil || capture.result.Status != "ok" || capture.data["status"] != "completed" {
		t.Fatalf("result = %#v data = %v, want completed success", capture.result, capture.data)
	}
	if !capture.closed {
		t.Fatal("channel did not send a close frame")
	}
	assertBacklogLines(t, capture.lines, 5)
	assertBacklogComplete(t, capture.progress, false)
}

// TestGeneralLogsChannelZeroMatches: an over-narrow filter must complete
// cleanly with zero entries and still emit backlog_complete — this is the
// backend half of the "no more infinite spinner" fix.
func TestGeneralLogsChannelZeroMatches(t *testing.T) {
	requireReadableJournal(t)
	frames, done, closeClient := openGeneralLogsChannel(context.Background(), apischema.GeneralLogsFollowRequest{
		Identifier: new("linuxio-test-nonexistent-identifier"),
		TimePeriod: new("1h"),
		Follow:     new(false),
	})
	defer closeClient()
	capture := collectChannelFrames(t, frames)
	err := <-done
	if err != nil {
		t.Fatalf("zero matches must not fail: %v", err)
	}
	if capture.result == nil || capture.result.Status != "ok" || capture.data["status"] != "completed" {
		t.Fatalf("result = %#v data = %v, want completed success", capture.result, capture.data)
	}
	if !capture.closed {
		t.Fatal("channel did not send a close frame")
	}
	if len(capture.lines) != 0 {
		t.Errorf("got %d lines, want 0", len(capture.lines))
	}
	assertBacklogComplete(t, capture.progress, false)
}

// TestGeneralLogsChannelFollowReceivesNewEntries exercises the live phase:
// entries logged after the channel starts must flow through --after-cursor and
// the frame batcher, and ctx cancellation must end the Channel cleanly.
func TestGeneralLogsChannelFollowReceivesNewEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	requireExecutables(t, "logger")
	requireReadableJournal(t)
	marker := "linuxio-follow-test-" + t.Name()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	frames, done, closeClient := openGeneralLogsChannel(ctx, apischema.GeneralLogsFollowRequest{Lines: new("5")})
	defer closeClient()

	// Give the follow process a moment to start, then emit a marker entry.
	time.Sleep(500 * time.Millisecond)
	if err := exec.Command("logger", "-t", "linuxio-test", marker).Run(); err != nil {
		cancel()
		t.Skipf("logger failed: %v (follow returned %v)", err, <-done)
	}

	found := false
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for !found {
		select {
		case frame := <-frames:
			if frame == nil {
				t.Fatal("channel closed before marker arrived")
			}
			if frame.Opcode == relay.OpStreamData && strings.Contains(string(frame.Payload), marker) {
				found = true
			}
		case <-deadline.C:
			t.Fatal("marker entry never arrived on the follow stream")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("follow phase returned %v after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow phase did not exit after cancellation")
	}
}
