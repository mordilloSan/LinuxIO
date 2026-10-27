package cmd

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestRunUnknownArgumentReturnsUsageError(t *testing.T) {
	if code := run([]string{"linuxio-bridge", "unknown"}, nil); code != 2 {
		t.Fatalf("run exit code = %d, want 2", code)
	}
}

func TestRunDirectInvocationReturnsUsageError(t *testing.T) {
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = stdin.Close() })

	if code := run([]string{"linuxio-bridge"}, stdin); code != 2 {
		t.Fatalf("run exit code = %d, want 2", code)
	}
}

func TestRunStdinStatFailureReturnsRuntimeError(t *testing.T) {
	stdin, peer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close stdin pipe: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	if code := run([]string{"linuxio-bridge"}, stdin); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
}

func TestIsDirectBridgeInvocationRecognizesBootstrapPipe(t *testing.T) {
	stdin, peer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = peer.Close()
	})

	direct, err := isDirectBridgeInvocation(stdin)
	if err != nil {
		t.Fatalf("isDirectBridgeInvocation: %v", err)
	}
	if direct {
		t.Fatal("bootstrap pipe classified as direct invocation")
	}
}

func TestJobsSubcommandsAreDispatched(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runJobsCommand([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("runJobsCommand exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), jobsSubmitUsage) {
		t.Fatalf("stderr = %q, want the jobs usage", stderr.String())
	}

	// Through run: "jobs" reaches runJobsCommand, not the unknown-argument
	// path, and skips the direct-invocation check (stdin is nil here).
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = write
	code := run([]string{"linuxio-bridge", "jobs", "unknown"}, nil)
	os.Stderr = saved
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if code != 2 || !strings.Contains(string(output), jobsSubmitUsage) {
		t.Fatalf("run(jobs unknown) = %d with stderr %q; want 2 and the jobs usage", code, output)
	}
}

func TestJobHostIsChosenBySystemdNotByArguments(t *testing.T) {
	if jobsHostActivated() {
		t.Fatal("job host mode without systemd's socket")
	}
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDNAMES", jobsSocketName)
	if !jobsHostActivated() {
		t.Fatal("job host mode not chosen for systemd's job host socket")
	}
	// Without the socket itself and the unit's directories the host refuses.
	var stderr bytes.Buffer
	if code := runJobsHost(&stderr); code != 2 {
		t.Fatalf("runJobsHost without its socket = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "job host needs its socket") {
		t.Fatalf("stderr = %q, want the job host message", stderr.String())
	}
	for _, verb := range []string{"host", "reconcile", "launch"} {
		stderr.Reset()
		if code := runJobsCommand([]string{verb}, io.Discard, &stderr); code != 2 {
			t.Fatalf("jobs %s = %d, want 2: only submit is a verb", verb, code)
		}
	}
}

func TestJobHostUnitWithoutItsSocketSaysSo(t *testing.T) {
	// The host unit sets STATE_DIRECTORY; a masked or failed socket leaves
	// the host without LISTEN_FDS.
	t.Setenv("STATE_DIRECTORY", t.TempDir())
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = write
	code := run([]string{"linuxio-bridge"}, nil)
	os.Stderr = saved
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if code != 2 || !strings.Contains(string(output), "job host needs its socket") {
		t.Fatalf("run = %d with stderr %q; want 2 and the job host message", code, output)
	}
}

func TestJobsSubmitRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"submit"},
		{"submit", "--route", "storage.scrub", "--resume", "00000000-0000-4000-8000-00000000000a"},
		{"submit", "--route", "storage.scrub", "--request", "{"},
		{"submit", "--route", "storage.scrub", "extra"},
		{"submit", "--unknown"},
	} {
		var stderr bytes.Buffer
		if code := runJobsCommand(args, io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), jobsSubmitUsage) {
			t.Errorf("jobs %v = %d with stderr %q; want 2 and the submit usage", args, code, stderr.String())
		}
	}
}
