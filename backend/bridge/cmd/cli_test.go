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

func TestBridgeRefusesTheJobsVerb(t *testing.T) {
	if code := run([]string{"linuxio-bridge", "jobs", "submit", "--route", "x"}, nil); code != 2 {
		t.Fatalf("run(jobs submit) exit code = %d, want 2", code)
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
