package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestWireProgressRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	p := NewWireProgress(&buf)
	p.Step("Checking database integrity")
	p.ScanProgress(2, 10, 4096)
	p.Summary(IndexRunStats{Files: 10, Dirs: 2, TotalSize: 4096, DeletedEntries: 3, SkippedDirs: 1, Duration: 1500 * time.Millisecond})

	decoder := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	var step, scan, summary indexWireEvent
	if err := decoder.Decode(&step); err != nil || step.Type != "step" || step.Message != "Checking database integrity" {
		t.Errorf("step event = %+v err=%v", step, err)
	}
	if err := decoder.Decode(&scan); err != nil || scan.Type != "scan" || scan.Dirs != 2 || scan.Files != 10 || scan.Size != 4096 {
		t.Errorf("scan event = %+v err=%v", scan, err)
	}
	if err := decoder.Decode(&summary); err != nil || summary.Type != "summary" || summary.Files != 10 || summary.DeletedEntries != 3 ||
		summary.SkippedDirs != 1 || summary.DurationMs != 1500 {
		t.Errorf("summary event = %+v", summary)
	}
}

func TestRunIndexProcessForwardsEventsAndSummary(t *testing.T) {
	script := `echo '{"type":"step","message":"Scanning filesystem"}'
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

func TestRunIndexProcessReapsChildAfterDecodeFailure(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRunIndexProcessDecodeFailureHelper")
	cmd.Env = append(os.Environ(), "LINUXIO_INDEX_WIRE_DECODE_HELPER=1")
	cmd.WaitDelay = 50 * time.Millisecond

	started := time.Now()
	if _, err := runIndexProcess(cmd, nil); err == nil {
		t.Fatal("expected malformed child output error")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("decode failure took %v; child was not reaped promptly", elapsed)
	}
}

func TestRunIndexProcessDecodeFailureHelper(t *testing.T) {
	if os.Getenv("LINUXIO_INDEX_WIRE_DECODE_HELPER") != "1" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	_, _ = fmt.Fprintln(os.Stdout, "}")
	for {
		time.Sleep(time.Hour)
	}
}
