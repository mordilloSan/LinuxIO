package virt

import (
	"context"
	"slices"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestListAndGetVMs(t *testing.T) {
	fake := newFakeConn()
	fake.domains["alpha"] = testDomain("alpha")
	fake.domainXML["alpha"] = strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "alpha")
	withFakeLibvirt(t, fake)

	vms, err := ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	if len(vms) != 1 || vms[0].Name != "alpha" {
		t.Fatalf("ListVMs = %#v", vms)
	}

	vm, err := GetVM(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if vm.Name != "alpha" || vm.State != "shutoff" {
		t.Fatalf("GetVM = %#v", vm)
	}
}

func TestGetVMReturnsNotFoundError(t *testing.T) {
	withFakeLibvirt(t, newFakeConn())

	_, err := GetVM(context.Background(), "missing")

	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("GetVM error = %v, want not found", err)
	}
	if code := errorCode(err, 0); code != 404 {
		t.Fatalf("error code = %d, want 404", code)
	}
}

func TestDomainLookupErrorsAreNotCollapsedToNotFound(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "get",
			run: func(ctx context.Context) error {
				_, err := GetVM(ctx, "lookup-fail")
				return err
			},
		},
		{
			name: "lifecycle",
			run: func(ctx context.Context) error {
				return StartVM(ctx, "lookup-fail")
			},
		},
		{
			name: "delete",
			run: func(ctx context.Context) error {
				_, err := DeleteVM(ctx, apischema.VMDeleteRequest{Name: "lookup-fail"})
				return err
			},
		},
		{
			name: "console",
			run: func(ctx context.Context) error {
				return withLibvirtConn(ctx, func(conn libvirtConn) error {
					_, err := consoleEndpointFromDomain(conn, "lookup-fail")
					return err
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeConn()
			fake.domainLookupErr = libvirtErr(libvirt.ErrInternalError, "lookup failed")
			withFakeLibvirt(t, fake)

			err := tt.run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "look up VM") {
				t.Fatalf("%s error = %v, want wrapped lookup error", tt.name, err)
			}
			if code := errorCode(err, 0); code == 404 {
				t.Fatalf("%s error code = 404, want non-404 lookup failure", tt.name)
			}
		})
	}
}

func TestListVMsIncludesDHCPLeaseIPAddresses(t *testing.T) {
	fake := newFakeConn()
	fake.domains["lease-vm"] = testDomain("lease-vm")
	fake.domainXML["lease-vm"] = `<domain type="kvm">
  <name>lease-vm</name>
  <metadata><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="linuxio-lease-vm.qcow2" sizeGB="12"></disk></linuxio></metadata>
  <memory unit="MiB">1024</memory>
  <vcpu>1</vcpu>
  <devices>
    <disk type="file" device="disk"><source file="/var/lib/libvirt/images/linuxio-lease-vm.qcow2"></source><target dev="vda" bus="virtio"></target></disk>
    <interface type="network"><mac address="52:54:00:7d:a3:19"></mac><source network="default"></source><model type="virtio"></model></interface>
  </devices>
</domain>`
	fake.networkLeases["default"] = []libvirt.NetworkDhcpLease{
		{Mac: libvirt.OptString{"52:54:00:7d:a3:19"}, Ipaddr: "192.168.122.57"},
		{Mac: libvirt.OptString{"52:54:00:7d:a3:19"}, Ipaddr: "192.168.122.57"},
		{Mac: libvirt.OptString{"52:54:00:00:00:ff"}, Ipaddr: "192.168.122.99"},
	}
	withFakeLibvirt(t, fake)

	vms, err := ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	if len(vms) != 1 || len(vms[0].NICs) != 1 {
		t.Fatalf("ListVMs NICs = %#v", vms)
	}
	if got := vms[0].NICs[0].IPAddresses; !slices.Equal(got, []string{"192.168.122.57"}) {
		t.Fatalf("IPAddresses = %#v", got)
	}
}

func TestDomainStateName(t *testing.T) {
	tests := map[libvirt.DomainState]string{
		libvirt.DomainRunning:     "running",
		libvirt.DomainPaused:      "paused",
		libvirt.DomainShutoff:     "shutoff",
		libvirt.DomainCrashed:     "crashed",
		libvirt.DomainPmsuspended: "suspended",
	}
	for state, want := range tests {
		if got := domainStateName(int32(state)); got != want {
			t.Fatalf("domainStateName(%v) = %q, want %q", state, got, want)
		}
	}
	if got := domainStateName(999); got != "unknown" {
		t.Fatalf("domainStateName(999) = %q, want unknown", got)
	}
}
