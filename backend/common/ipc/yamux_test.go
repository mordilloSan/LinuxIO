package ipc

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestYamuxSessionOnCloseRunsWhenPeerCloses(t *testing.T) {
	left, right := net.Pipe()
	client, err := NewYamuxClient(left)
	if err != nil {
		t.Fatalf("NewYamuxClient error: %v", err)
	}
	server, err := NewYamuxServer(right)
	if err != nil {
		t.Fatalf("NewYamuxServer error: %v", err)
	}
	defer client.Close()

	done := make(chan struct{})
	client.SetOnClose(func() { close(done) })

	if err := server.Close(); err != nil {
		t.Fatalf("server.Close error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client close callback")
	}
}

func TestYamuxSessionOnCloseRunsOnce(t *testing.T) {
	left, right := net.Pipe()
	client, err := NewYamuxClient(left)
	if err != nil {
		t.Fatalf("NewYamuxClient error: %v", err)
	}
	server, err := NewYamuxServer(right)
	if err != nil {
		t.Fatalf("NewYamuxServer error: %v", err)
	}
	defer server.Close()

	var calls atomic.Int32
	client.SetOnClose(func() { calls.Add(1) })

	if err := client.Close(); err != nil {
		t.Fatalf("client.Close error: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second client.Close error: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestYamuxSessionSetOnCloseAfterClose(t *testing.T) {
	left, right := net.Pipe()
	client, err := NewYamuxClient(left)
	if err != nil {
		t.Fatalf("NewYamuxClient error: %v", err)
	}
	server, err := NewYamuxServer(right)
	if err != nil {
		t.Fatalf("NewYamuxServer error: %v", err)
	}
	defer server.Close()

	if err := client.Close(); err != nil {
		t.Fatalf("client.Close error: %v", err)
	}

	done := make(chan struct{})
	client.SetOnClose(func() { close(done) })

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for late close callback")
	}
}
