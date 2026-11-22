package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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

func TestRunIndexProcessStartsTheScannerAtLowPriority(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRunIndexProcessPriorityHelper")
	cmd.Env = append(os.Environ(), "LINUXIO_INDEX_PRIORITY_HELPER=1")
	before, ok := threadNice("thread-self")
	if !ok {
		t.Fatal("cannot read the test's own nice value")
	}

	var report string
	if _, err := runIndexProcess(cmd, func(evt indexWireEvent) { report = evt.Message }); err != nil {
		t.Fatalf("runIndexProcess: %v", err)
	}
	if want := "nice=19 ioclass=3"; report != want {
		t.Fatalf("scanner threads report %q, want %q for every thread", report, want)
	}

	// The daemon answers searches from its own threads: starting a scan must
	// leave them as they were.
	if after, _ := threadNice("thread-self"); after != before {
		t.Fatalf("the test's own thread went from nice %d to %d by starting a scan", before, after)
	}
}

// TestRunIndexProcessPriorityHelper is the scanner child: it reports the nice
// value and the I/O class of each of its threads, as one value when they all
// agree and as every differing value otherwise. A thread that ends while it is
// being read is left out.
func TestRunIndexProcessPriorityHelper(t *testing.T) {
	if os.Getenv("LINUXIO_INDEX_PRIORITY_HELPER") != "1" {
		return
	}
	tasks, err := filepath.Glob("/proc/self/task/*")
	if err != nil || len(tasks) == 0 {
		t.Fatalf("list threads: %v", err)
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		tid, err := strconv.Atoi(filepath.Base(task))
		if err != nil {
			t.Fatalf("thread id %s: %v", task, err)
		}
		ioprio, _, errno := unix.Syscall(unix.SYS_IOPRIO_GET, ioprioWhoProcess, uintptr(tid), 0)
		nice, ok := threadNice("self/task/" + filepath.Base(task))
		if errno != 0 || !ok {
			continue
		}
		seen[fmt.Sprintf("nice=%d ioclass=%d", nice, ioprio>>ioprioClassShift)] = true
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	_, _ = fmt.Fprintf(os.Stdout, "{\"type\":\"step\",\"message\":%q}\n", strings.Join(values, " | "))
	os.Exit(0)
}

// threadNice reads the nice value of one thread from /proc/<path>/stat, and
// reports false when the thread is gone or the line cannot be read.
func threadNice(path string) (int, bool) {
	data, err := os.ReadFile("/proc/" + path + "/stat")
	if err != nil {
		return 0, false
	}
	// Fields after the command, which is in brackets and may hold spaces:
	// state is the first, nice the seventeenth.
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	if len(fields) < 17 {
		return 0, false
	}
	nice, err := strconv.Atoi(fields[16])
	return nice, err == nil
}
