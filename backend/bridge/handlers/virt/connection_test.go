package virt

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/libvirttest"
	"github.com/digitalocean/go-libvirt/socket/dialers"
)

func TestCloseOnContextDoneClosesConnectionOnCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	stopCancellation := closeOnContextDone(ctx, client)

	readDone := make(chan error, 1)
	go func() {
		_, err := server.Read(make([]byte, 1))
		readDone <- err
	}()
	cancel()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("connection remained blocked after context cancellation")
	}
	stopCancellation()
}

func TestLibvirtRPCUnblocksWhenTransportCloses(t *testing.T) {
	mock := libvirttest.New()
	raw, err := mock.Dial()
	if err != nil {
		t.Fatalf("mock Dial: %v", err)
	}
	l := libvirt.NewWithDialer(dialers.NewAlreadyConnected(raw))
	if err := l.Connect(); err != nil {
		_ = raw.Close()
		t.Fatalf("libvirt Connect: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stopCancellation := closeOnContextDone(ctx, raw)
	defer func() {
		_ = raw.Close()
		<-l.Disconnected()
		stopCancellation()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := l.DomainInterfaceAddresses(libvirt.Domain{Name: "blocked"}, uint32(libvirt.DomainInterfaceAddressesSrcArp), 0)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, libvirt.ErrInterrupted) {
			t.Fatalf("DomainInterfaceAddresses error = %v, want interrupted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked libvirt RPC did not return after transport close")
	}
}
