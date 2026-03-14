package ipc

import (
	"net"
	"testing"
	"time"
)

func TestFixedBudgetMemoryManagerEnforcesLimit(t *testing.T) {
	mm := NewFixedBudgetMemoryManager(64)

	if err := mm.ReserveMemory(48, 0); err != nil {
		t.Fatalf("ReserveMemory(48): %v", err)
	}
	if err := mm.ReserveMemory(17, 0); err == nil {
		t.Fatal("ReserveMemory over limit succeeded, want error")
	}

	mm.ReleaseMemory(16)
	if err := mm.ReserveMemory(16, 0); err != nil {
		t.Fatalf("ReserveMemory after release: %v", err)
	}
}

func TestFixedBudgetMemoryManagerDoneClosesManager(t *testing.T) {
	mm := NewFixedBudgetMemoryManager(64)

	if err := mm.ReserveMemory(32, 0); err != nil {
		t.Fatalf("ReserveMemory(32): %v", err)
	}

	mm.Done()

	if err := mm.ReserveMemory(1, 0); err == nil {
		t.Fatal("ReserveMemory after Done succeeded, want error")
	}
}

func TestYamuxSessionOnCloseFiresOnRemoteClose(t *testing.T) {
	server, client := newTestYamuxPair(t)
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	done := make(chan struct{})
	server.SetOnClose(func() { close(done) })

	if err := client.Close(); err != nil {
		t.Fatalf("client.Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote close callback")
	}
}

func TestYamuxSessionSetOnCloseAfterCloseStillRuns(t *testing.T) {
	server, client := newTestYamuxPair(t)
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	if err := client.Close(); err != nil {
		t.Fatalf("client.Close: %v", err)
	}

	done := make(chan struct{})
	server.SetOnClose(func() { close(done) })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for deferred close callback")
	}
}

func newTestYamuxPair(t *testing.T) (*YamuxSession, *YamuxSession) {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	serverCh := make(chan *YamuxSession, 1)
	errCh := make(chan error, 1)

	go func() {
		server, err := NewYamuxServer(serverConn)
		if err != nil {
			errCh <- err
			return
		}
		serverCh <- server
	}()

	client, err := NewYamuxClient(clientConn)
	if err != nil {
		t.Fatalf("NewYamuxClient: %v", err)
		return nil, nil
	}

	select {
	case err := <-errCh:
		t.Fatalf("NewYamuxServer: %v", err)
		return nil, nil
	case server := <-serverCh:
		return server, client
	case <-time.After(2 * time.Second):
		t.Fatal("timed out creating yamux pair")
		return nil, nil
	}
}
