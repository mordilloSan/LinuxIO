package virt

import (
	"context"
	"net"
	"testing"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type blockedAddressConn struct {
	*fakeConn
	transport net.Conn
}

func (c *blockedAddressConn) DomainInterfaceAddresses(libvirt.Domain, uint32, uint32) ([]libvirt.DomainInterface, error) {
	_, err := c.transport.Read(make([]byte, 1))
	return nil, err
}

func (c *blockedAddressConn) closeTransport() {
	_ = c.transport.Close()
}

func TestEnrichBridgeAddressesInterruptsBlockedRPC(t *testing.T) {
	tests := []struct {
		name   string
		cancel func(context.CancelFunc, chan<- struct{})
	}{
		{
			name: "deadline",
			cancel: func(_ context.CancelFunc, _ chan<- struct{}) {
			},
		},
		{
			name: "caller cancellation",
			cancel: func(cancel context.CancelFunc, done chan<- struct{}) {
				go func() {
					time.Sleep(20 * time.Millisecond)
					cancel()
					close(done)
				}()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			conn := &blockedAddressConn{fakeConn: newFakeConn(), transport: client}
			vm := apischema.VirtualMachine{
				Name:  "blocked",
				State: "running",
				NICs:  []apischema.VMNIC{{AttachmentType: "bridge", MAC: "52:54:00:00:00:01"}},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelDone := make(chan struct{})
			if test.name == "deadline" {
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			test.cancel(cancel, cancelDone)

			done := make(chan struct{})
			go func() {
				enrichBridgeAddresses(
					ctx,
					conn,
					[]libvirt.Domain{testDomain("blocked")},
					[]apischema.VirtualMachine{vm},
				)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				conn.closeTransport()
				<-done
				t.Fatal("blocked address RPC did not return")
			}
			if test.name == "caller cancellation" {
				select {
				case <-cancelDone:
				case <-time.After(time.Second):
					t.Fatal("caller cancellation did not run")
				}
			}
		})
	}
}

func TestListAndGetVMsReturnBaseDataWhenAddressDiscoveryBlocks(t *testing.T) {
	for _, test := range []struct {
		name string
		get  bool
	}{
		{name: "list"},
		{name: "get", get: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			fake := newFakeConn()
			fake.domainState = int32(libvirt.DomainRunning)
			fake.domains["blocked"] = testDomain("blocked")
			fake.domainXML["blocked"] = `<domain type="kvm"><name>blocked</name><devices><interface type="bridge"><mac address="52:54:00:00:00:01"></mac><source bridge="br0"></source></interface></devices></domain>`
			conn := &blockedAddressConn{fakeConn: fake, transport: client}
			withFakeLibvirt(t, conn)
			watchdog := time.AfterFunc(5*time.Second, conn.closeTransport)
			defer watchdog.Stop()

			started := time.Now()
			var (
				vms []apischema.VirtualMachine
				vm  apischema.VirtualMachine
				err error
			)
			if test.get {
				vm, err = GetVM(context.Background(), "blocked")
				vms = []apischema.VirtualMachine{vm}
			} else {
				vms, err = ListVMs(context.Background())
			}
			if err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
			if elapsed := time.Since(started); elapsed < domainAddressDiscoveryTimeout || elapsed >= 5*time.Second {
				t.Fatalf("%s took %s, want bounded discovery", test.name, elapsed)
			}
			if len(vms) != 1 || vms[0].Name != "blocked" || vms[0].State != "running" {
				t.Fatalf("%s result = %#v, want base VM data", test.name, vms)
			}
		})
	}
}
