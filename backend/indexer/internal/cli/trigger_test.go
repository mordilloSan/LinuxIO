package cli

import (
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func TestTriggerIndexPostsOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "indexer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	request := make(chan *http.Request, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request <- r
		w.WriteHeader(http.StatusAccepted)
	})}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close server: %v", err)
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
	})

	if err := triggerIndex(socketPath); err != nil {
		t.Fatalf("triggerIndex: %v", err)
	}
	got := <-request
	if got.Method != http.MethodPost || got.URL.Path != api.RouteIndex {
		t.Fatalf("request = %s %s, want POST %s", got.Method, got.URL.Path, api.RouteIndex)
	}
}
