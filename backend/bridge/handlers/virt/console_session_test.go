package virt

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestConsoleSocketHelpers(t *testing.T) {
	if !isSafeVNCSocket("/var/lib/libvirt/qemu/linuxio-test.vnc") {
		t.Fatal("expected LinuxIO VNC socket path to be safe")
	}
	for _, unsafePath := range []string{
		"/tmp/linuxio-test.vnc",
		"/var/lib/libvirt/qemu/../secret",
		"/var/lib/libvirt/qemu",
	} {
		if isSafeVNCSocket(unsafePath) {
			t.Fatalf("isSafeVNCSocket(%q) = true, want false", unsafePath)
		}
	}

	socketEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" socket="/var/lib/libvirt/qemu/linuxio-test.vnc" autoport="no"></graphics></devices></domain>`)
	if socketEndpoint.Network != "unix" || socketEndpoint.Address != "/var/lib/libvirt/qemu/linuxio-test.vnc" {
		t.Fatalf("vncEndpointFromDomainXML (socket attr) = %#v", socketEndpoint)
	}

	// libvirt rewrites the socket attribute into a <listen type="socket">
	// child element when the domain is defined and started, so the running
	// XML carries the path on the listener rather than the attribute.
	listenerEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc"><listen type="socket" socket="/var/lib/libvirt/qemu/linuxio-test.vnc"></listen></graphics></devices></domain>`)
	if listenerEndpoint.Network != "unix" || listenerEndpoint.Address != "/var/lib/libvirt/qemu/linuxio-test.vnc" {
		t.Fatalf("vncEndpointFromDomainXML (listener form) = %#v", listenerEndpoint)
	}

	if empty := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" autoport="yes"></graphics></devices></domain>`); empty.Network != "" {
		t.Fatalf("vncEndpointFromDomainXML (no endpoint) = %#v, want empty", empty)
	}

	tcpEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" port="5900" autoport="yes" listen="127.0.0.1"><listen type="address" address="127.0.0.1"></listen></graphics></devices></domain>`)
	if tcpEndpoint.Network != "tcp" || tcpEndpoint.Address != "127.0.0.1:5900" {
		t.Fatalf("vncEndpointFromDomainXML (tcp) = %#v", tcpEndpoint)
	}
	validatedEndpoint, err := validateConsoleEndpoint("test", tcpEndpoint)
	if err != nil {
		t.Fatalf("validateConsoleEndpoint(tcp): %v", err)
	}
	if validatedEndpoint.Address != "127.0.0.1:5900" {
		t.Fatalf("validated tcp address = %q", validatedEndpoint.Address)
	}

	wildcardEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" port="5901" listen="0.0.0.0"></graphics></devices></domain>`)
	validatedEndpoint, err = validateConsoleEndpoint("test", wildcardEndpoint)
	if err != nil {
		t.Fatalf("validateConsoleEndpoint(wildcard): %v", err)
	}
	if validatedEndpoint.Address != "127.0.0.1:5901" {
		t.Fatalf("validated wildcard tcp address = %q", validatedEndpoint.Address)
	}

	unsafeEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" port="5902" listen="192.168.1.20"></graphics></devices></domain>`)
	if _, err := validateConsoleEndpoint("test", unsafeEndpoint); err == nil {
		t.Fatal("validateConsoleEndpoint(unsafe tcp) error = nil, want error")
	}
}

func TestHandleConsoleSessionWritesErrorResult(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- HandleConsoleSession(context.Background(), server, apischema.NameRequest{Name: "../bad"})
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult || frame.StreamID != 1 {
		t.Fatalf("result frame = opcode 0x%02x stream %d", frame.Opcode, frame.StreamID)
	}
	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	if result.Status != "error" || result.Error == "" {
		t.Fatalf("result = %#v, want error result", result)
	}
	if result.Code != 400 {
		t.Fatalf("result code = %d, want 400", result.Code)
	}

	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose || closeFrame.StreamID != 1 {
		t.Fatalf("close frame = opcode 0x%02x stream %d", closeFrame.Opcode, closeFrame.StreamID)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("HandleConsoleSession error = nil, want validation error")
		}
	case <-time.After(time.Second):
		t.Fatal("HandleConsoleSession did not return")
	}
}

func TestHandleConsoleSessionReturnsOnClientAbort(t *testing.T) {
	fake := newFakeConn()
	fake.domainState = int32(libvirt.DomainRunning)
	fake.domains["console"] = testDomain("console")
	listener, port := listenLoopbackVNCPort(t)
	fake.domainXML["console"] = fmt.Sprintf(`<domain type="kvm">
  <name>console</name>
  <devices><graphics type="vnc" port="%d" listen="127.0.0.1"></graphics></devices>
</domain>`, port)
	withFakeLibvirt(t, fake)

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- HandleConsoleSession(ctx, server, apischema.NameRequest{Name: "console"})
	}()

	select {
	case conn := <-accepted:
		defer conn.Close()
	case err := <-acceptErr:
		t.Fatalf("accept VNC connection: %v", err)
	case <-time.After(time.Second):
		t.Fatal("VNC connection was not accepted")
	}

	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort, StreamID: 1}); err != nil {
		t.Fatalf("write abort frame: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleConsoleSession returned error after abort: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleConsoleSession did not return after client abort")
	}
}

func listenLoopbackVNCPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	for port := 5900; port <= 5999; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		t.Cleanup(func() {
			_ = listener.Close()
		})
		return listener, port
	}
	t.Fatal("could not allocate loopback VNC test port")
	return nil, 0
}
