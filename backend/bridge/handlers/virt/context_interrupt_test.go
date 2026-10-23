package virt

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestInterruptOnContextDoneClosesConnectionOnCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	stopCancellation := interruptOnContextDone(ctx, func() { _ = client.Close() })

	readDone := make(chan error, 1)
	go func() {
		_, err := server.Read(make([]byte, 1))
		readDone <- err
	}()
	cancel()
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("Read error = nil, want closed connection")
		}
	case <-time.After(time.Second):
		t.Fatal("connection remained blocked after context cancellation")
	}
	stopCancellation()
}
