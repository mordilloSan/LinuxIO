package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

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
