package daemon

import (
	"bytes"
	"os/exec"
	"testing"
	"time"
)

func TestWireProgressRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	p := NewWireProgress(&buf)
	p.Step("Checking database integrity")
	p.ScanProgress(2, 10, 4096)
	p.Summary(IndexRunStats{Files: 10, Dirs: 2, TotalSize: 4096, DeletedEntries: 3, SkippedDirs: 1, Duration: 1500 * time.Millisecond})

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 wire lines, got %d:\n%s", len(lines), buf.String())
	}

	step, ok := parseIndexWireLine(lines[0])
	if !ok || step.Type != "step" || step.Message != "Checking database integrity" {
		t.Errorf("step event = %+v ok=%v", step, ok)
	}
	scan, ok := parseIndexWireLine(lines[1])
	if !ok || scan.Type != "scan" || scan.Dirs != 2 || scan.Files != 10 || scan.Size != 4096 {
		t.Errorf("scan event = %+v ok=%v", scan, ok)
	}
	summary, ok := parseIndexWireLine(lines[2])
	if !ok || summary.Type != "summary" || summary.Files != 10 || summary.DeletedEntries != 3 ||
		summary.SkippedDirs != 1 || summary.DurationMs != 1500 {
		t.Errorf("summary event = %+v ok=%v", summary, ok)
	}
}

func TestParseIndexWireLineRejectsForeignOutput(t *testing.T) {
	for _, line := range []string{"", "plain text", "{not json", `{"no_type":true}`} {
		if evt, ok := parseIndexWireLine([]byte(line)); ok {
			t.Errorf("parseIndexWireLine(%q) accepted foreign line as %+v", line, evt)
		}
	}
}

func TestRunIndexProcessForwardsEventsAndSummary(t *testing.T) {
	script := `echo 'stray non-wire output'
echo '{"type":"step","message":"Scanning filesystem"}'
echo '{"type":"scan","dirs":2,"files":10,"size":4096}'
echo '{"type":"summary","dirs":2,"files":10,"size":4096,"deleted_entries":1,"duration_ms":1500}'`

	var events []indexWireEvent
	stats, err := runIndexProcess(exec.Command("sh", "-c", script), func(evt indexWireEvent) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("runIndexProcess: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 forwarded events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "step" || events[1].Type != "scan" || events[2].Type != "summary" {
		t.Errorf("unexpected event order: %+v", events)
	}
	if stats == nil {
		t.Fatal("expected summary stats, got nil")
	}
	if stats.Files != 10 || stats.Dirs != 2 || stats.TotalSize != 4096 ||
		stats.DeletedEntries != 1 || stats.Duration != 1500*time.Millisecond {
		t.Errorf("stats = %+v", *stats)
	}
}

func TestRunIndexProcessReportsFailure(t *testing.T) {
	stats, err := runIndexProcess(exec.Command("sh", "-c", "exit 3"), nil)
	if err == nil {
		t.Fatal("expected error from failing subprocess")
	}
	if stats != nil {
		t.Errorf("expected nil stats on failure, got %+v", *stats)
	}
}
