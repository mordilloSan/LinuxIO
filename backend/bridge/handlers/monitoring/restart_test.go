package monitoring

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTestControlSocket(t *testing.T, socketPath string) {
	t.Helper()
	orig := controlClient
	controlClient = unixClient(socketPath, 0)
	t.Cleanup(func() { controlClient = orig })
}

func withShortRestartReadyTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	orig := restartReadyTimeout
	restartReadyTimeout = timeout
	t.Cleanup(func() { restartReadyTimeout = orig })
}

// serveCommandSocket answers every request with a successful command envelope.
func serveCommandSocket(t *testing.T, socketPath string) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", socketPath, err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"command":"status.get"}`))
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}

func TestWaitAgentReadyOutlivesTheCommandRetryWindow(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "control.sock")
	withFastCommandRetry(t)
	withShortRestartReadyTimeout(t, 2*time.Second)
	withTestControlSocket(t, socketPath)

	ready := make(chan error, 1)
	go func() { ready <- WaitAgentReady(context.Background()) }()

	// The daemon's first collection outlives commandRetryTimeout, so only the
	// readiness loop can bridge the gap.
	time.Sleep(300 * time.Millisecond)
	serveCommandSocket(t, socketPath)

	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("WaitAgentReady: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitAgentReady did not return")
	}
}

func TestWaitAgentReadyFailsWhenSocketNeverAppears(t *testing.T) {
	withFastCommandRetry(t)
	withShortRestartReadyTimeout(t, 300*time.Millisecond)
	withTestControlSocket(t, filepath.Join(t.TempDir(), "missing.sock"))

	start := time.Now()
	err := WaitAgentReady(context.Background())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "wait for linuxio-monitoring readiness") {
		t.Fatalf("err = %v, want the readiness-timeout wrap", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("returned after %s, want at least the readiness timeout", elapsed)
	}
}
