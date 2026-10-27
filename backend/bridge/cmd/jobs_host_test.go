package cmd

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestDrainQueuedClosesTheWaitingConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var clients []net.Conn
	for range 2 {
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		clients = append(clients, conn)
	}

	drainQueued(ln)
	for _, conn := range clients {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("queued connection read = %v, want EOF", err)
		}
	}
}
