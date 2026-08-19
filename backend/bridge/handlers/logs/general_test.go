package logs

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestParseGeneralLogsRequestDefaults(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{})
	if req.lines != "100" {
		t.Errorf("lines = %q, want default 100", req.lines)
	}
	if !req.follow {
		t.Error("follow = false, want default true")
	}
}

func TestParseGeneralLogsRequestFollowFalse(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Follow: new(false),
	})
	if req.follow {
		t.Error("follow = true, want false")
	}
}

func TestParseGeneralLogsRequestAfterCursor(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		AfterCursor: new("  s=abc;i=42  "),
	})
	if req.afterCursor != "s=abc;i=42" {
		t.Errorf("afterCursor = %q, want trimmed cursor", req.afterCursor)
	}
}

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

func TestParseGeneralLogsRequestRejectsInvalidFieldFilters(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		FieldFilters: []string{"PRIORITY=3", "-rf", "foo=bar", "_SYSTEMD_UNIT=ssh.service"},
	})
	want := []string{"PRIORITY=3", "_SYSTEMD_UNIT=ssh.service"}
	if !slices.Equal(req.fieldFilters, want) {
		t.Errorf("fieldFilters = %v, want %v", req.fieldFilters, want)
	}
}

func TestBacklogArgs(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Lines:      new("200"),
		TimePeriod: new("24h"),
		Priority:   new("4"),
		Identifier: new("sshd"),
	})
	args := backlogArgs(req)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--reverse", "-n 200", "--since 24h ago", "-p 4", "-t sshd", "-o json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("backlogArgs missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "--follow") || strings.Contains(joined, "-f ") {
		t.Errorf("backlog must not follow: %q", joined)
	}
}

func TestBacklogArgsAllLinesHasNoTailFlag(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{Lines: new("all")})
	joined := strings.Join(backlogArgs(req), " ")
	if strings.Contains(joined, "-n ") {
		t.Errorf("lines=all must not pass -n: %q", joined)
	}
}

func TestFollowArgsAnchorsOnCursor(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{TimePeriod: new("24h")})
	joined := strings.Join(followArgs(req, "s=abc;i=1"), " ")
	if !strings.Contains(joined, "--follow") {
		t.Errorf("follow args missing --follow: %q", joined)
	}
	if !strings.Contains(joined, "--after-cursor s=abc;i=1") {
		t.Errorf("follow args missing --after-cursor: %q", joined)
	}
	if strings.Contains(joined, "--since") {
		t.Errorf("cursor-anchored follow must not re-apply --since: %q", joined)
	}
}

func TestFollowArgsWithoutCursorTailsZero(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{})
	joined := strings.Join(followArgs(req, ""), " ")
	if !strings.Contains(joined, "-n 0") {
		t.Errorf("cursorless windowless follow must tail 0 lines: %q", joined)
	}
}

func TestFollowArgsWithoutCursorRecoversWindow(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{TimePeriod: new("24h")})
	joined := strings.Join(followArgs(req, ""), " ")
	if !strings.Contains(joined, "--since 24h ago") || !strings.Contains(joined, "--no-tail") {
		t.Errorf("empty-backlog follow must re-cover the window to close the gap: %q", joined)
	}
	if strings.Contains(joined, "-n 0") {
		t.Errorf("window fallback must not also tail 0: %q", joined)
	}
}

func TestGeneralPageArgsDoNotCombineCursorAndSince(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		TimePeriod: new("24h"),
		Priority:   new("4"),
		Identifier: new("sshd"),
	})
	joined := strings.Join(generalPageArgs(req, "s=abc;i=1"), " ")
	if !strings.Contains(joined, "--cursor s=abc;i=1") {
		t.Errorf("page args missing cursor: %q", joined)
	}
	if strings.Contains(joined, "--since") {
		t.Errorf("journalctl rejects --cursor with --since: %q", joined)
	}
	for _, want := range []string{"--reverse", "-p 4", "-t sshd", "-o json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("page args missing %q in %q", want, joined)
		}
	}
}

func TestJournalPeriodCutoff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		period string
		want   time.Duration
	}{
		{period: "1h", want: time.Hour},
		{period: "24h", want: 24 * time.Hour},
		{period: "7d", want: 7 * 24 * time.Hour},
		{period: "30d", want: 30 * 24 * time.Hour},
	} {
		cutoff, ok := journalPeriodCutoff(tc.period, now)
		if !ok {
			t.Fatalf("journalPeriodCutoff(%q) was not parsed", tc.period)
		}
		if want := now.Add(-tc.want).UnixMicro(); cutoff != want {
			t.Errorf("journalPeriodCutoff(%q) = %d, want %d", tc.period, cutoff, want)
		}
	}
}

func TestJournalLineOlderThan(t *testing.T) {
	const cutoff = int64(1_700_000_000_000_000)
	if !journalLineOlderThan(
		`{"__REALTIME_TIMESTAMP":"1699999999999999","MESSAGE":"old"}`,
		cutoff,
	) {
		t.Error("entry before cutoff was not recognized")
	}
	if journalLineOlderThan(
		`{"__REALTIME_TIMESTAMP":"1700000000000000","MESSAGE":"boundary"}`,
		cutoff,
	) {
		t.Error("entry exactly at cutoff should remain in the window")
	}
	if journalLineOlderThan(`{"MESSAGE":"missing timestamp"}`, cutoff) {
		t.Error("entry without timestamp should not terminate pagination")
	}
}

func TestTrimJournalLineKeepsAllowlistAndLinuxioFields(t *testing.T) {
	line := `{"__CURSOR":"s=abc;i=1","__REALTIME_TIMESTAMP":"1700000000000000",` +
		`"MESSAGE":"hello","PRIORITY":"6","SYSLOG_IDENTIFIER":"sshd",` +
		`"LINUXIO_SESSION":"x1","_CMDLINE":"/usr/sbin/sshd -D","_EXE":"/usr/sbin/sshd",` +
		`"_HOSTNAME":"box","CODE_FUNC":"doThing"}`
	trimmed, cursor := trimJournalLine(line)
	if cursor != "s=abc;i=1" {
		t.Errorf("cursor = %q, want s=abc;i=1", cursor)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		t.Fatalf("trimmed output is not valid JSON: %v", err)
	}
	for _, want := range []string{"__CURSOR", "__REALTIME_TIMESTAMP", "MESSAGE", "PRIORITY", "SYSLOG_IDENTIFIER", "LINUXIO_SESSION", "CODE_FUNC"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("trimmed output missing %q", want)
		}
	}
	for _, dropped := range []string{"_CMDLINE", "_EXE", "_HOSTNAME"} {
		if _, ok := fields[dropped]; ok {
			t.Errorf("trimmed output should have dropped %q", dropped)
		}
	}
}

func TestTrimJournalLinePassesThroughInvalidJSON(t *testing.T) {
	trimmed, cursor := trimJournalLine("not json at all")
	if trimmed != "not json at all" || cursor != "" {
		t.Errorf("invalid JSON should pass through, got %q / %q", trimmed, cursor)
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
		<-done
		t.Skipf("logger failed: %v", err)
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
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("follow phase did not exit after cancellation")
	}
}

// TestGetGeneralLogsPage pages backwards from the 1st-newest cursor and checks
// the boundary entry is excluded, journal order is preserved, and HasMore
// reflects the remaining history.
func TestGetGeneralLogsPage(t *testing.T) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		t.Skip("journalctl not available")
	}
	out, err := exec.Command("journalctl", "-q", "-n", "10", "-o", "json", "--no-pager", "--reverse").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Skip("journal not readable in this environment")
	}
	newest := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(newest) < 5 {
		t.Skip("journal too small for pagination test")
	}
	var boundary struct {
		Cursor string `json:"__CURSOR"`
	}
	if err = json.Unmarshal([]byte(newest[0]), &boundary); err != nil {
		t.Fatalf("parse boundary: %v", err)
	}
	expectedCursors := make([]string, 3)
	for i, entry := range newest[1:4] {
		var fields struct {
			Cursor string `json:"__CURSOR"`
		}
		if err = json.Unmarshal([]byte(entry), &fields); err != nil {
			t.Fatalf("parse expected page entry %d: %v", i, err)
		}
		expectedCursors[i] = fields.Cursor
	}

	if _, err = GetGeneralLogsPage(context.Background(), apischema.GeneralLogsPageRequest{
		Cursor:     boundary.Cursor,
		Lines:      new("1"),
		TimePeriod: new("24h"),
	}); err != nil {
		t.Fatalf("GetGeneralLogsPage with cursor and time window: %v", err)
	}

	resp, err := GetGeneralLogsPage(context.Background(), apischema.GeneralLogsPageRequest{
		Cursor: boundary.Cursor,
		Lines:  new("3"),
	})
	if err != nil {
		t.Fatalf("GetGeneralLogsPage: %v", err)
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(resp.Entries))
	}
	if !resp.HasMore {
		t.Error("HasMore = false, want true (journal has >4 entries)")
	}
	for i, entry := range resp.Entries {
		var fields struct {
			Cursor string `json:"__CURSOR"`
		}
		if err := json.Unmarshal([]byte(entry), &fields); err != nil {
			t.Fatalf("entry not valid JSON: %v", err)
		}
		if fields.Cursor == boundary.Cursor {
			t.Error("boundary entry leaked into the page")
		}
		if fields.Cursor != expectedCursors[i] {
			t.Errorf("entry %d cursor = %q, want %q", i, fields.Cursor, expectedCursors[i])
		}
	}
}

func TestGetGeneralLogsPageRejectsBadCursor(t *testing.T) {
	if _, err := GetGeneralLogsPage(context.Background(), apischema.GeneralLogsPageRequest{Cursor: "bad\ncursor"}); err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestIsValidJournalCursor(t *testing.T) {
	cases := []struct {
		cursor string
		want   bool
	}{
		{"s=81164172d30a41bf;i=1c937;b=c40aeda9", true},
		{"", false},
		{"has\nnewline", false},
		{"has\x00nul", false},
		{strings.Repeat("a", 1025), false},
		{strings.Repeat("a", 1024), true},
	}
	for _, tc := range cases {
		if got := isValidJournalCursor(tc.cursor); got != tc.want {
			t.Errorf("isValidJournalCursor(%.20q...) = %v, want %v", tc.cursor, got, tc.want)
		}
	}
}
