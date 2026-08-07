package cmd

import (
	"os"
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
