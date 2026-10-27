package cmd

import (
	"bytes"
	"io"
	"os"
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
	if code := runJobsCommand([]string{"unknown"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("runJobsCommand exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), jobsUsage) {
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
	if code != 2 || !strings.Contains(string(output), jobsUsage) {
		t.Fatalf("run(jobs unknown) = %d with stderr %q; want 2 and the jobs usage", code, output)
	}
}

func TestJobsLaunchRequiresSocketStdin(t *testing.T) {
	var stderr bytes.Buffer
	if code := runJobsCommand([]string{"launch"}, strings.NewReader(""), io.Discard, &stderr); code != 2 {
		t.Fatalf("launch with a non-file stdin = %d, want 2", code)
	}
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	stderr.Reset()
	if code := runJobsCommand([]string{"launch"}, file, io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "not a socket") {
		t.Fatalf("launch with a regular file = %d, %q; want 2 and a not-a-socket message", code, stderr.String())
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
		if code := runJobsCommand(args, strings.NewReader(""), io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), jobsSubmitUsage) {
			t.Errorf("jobs %v = %d with stderr %q; want 2 and the submit usage", args, code, stderr.String())
		}
	}
}
