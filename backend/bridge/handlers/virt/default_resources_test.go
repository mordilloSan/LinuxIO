package virt

import (
	"context"
	"errors"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestCreateVMReportsDefaultNetworkAddressConflict(t *testing.T) {
	fake := newFakeConn()
	fake.networkActive = 0
	fake.networkCreateErr = errors.New("internal error: Child process (VIR_BRIDGE_NAME=virbr0 /usr/sbin/dnsmasq --conf-file=/var/lib/libvirt/dnsmasq/default.conf --leasefile-ro --dhcp-script=/usr/lib/libvirt/libvirt_leaseshelper) unexpected exit status 2: dnsmasq: failed to create listening socket for 192.168.122.1: Address already in use")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)

	_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:     "network-conflict",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  "/isos/install.iso",
	})
	if err == nil {
		t.Fatal("CreateVM succeeded, want network conflict")
	}
	for _, want := range []string{
		"default NAT network cannot start",
		"192.168.122.1 is already in use",
		"reconfigure the libvirt default network address",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateVM error = %v, want containing %q", err, want)
		}
	}
	if len(fake.volumesByName) != 0 {
		t.Fatalf("volumesByName = %#v, want no volume created", fake.volumesByName)
	}
}

func TestDefaultNetworkStartErrorExplainsStaleBridge(t *testing.T) {
	cause := libvirtErr(libvirt.ErrInternalError, "internal error: Network is already in use by interface virbr0")
	err := defaultNetworkStartError(cause)
	if !errors.Is(err, cause) {
		t.Fatalf("defaultNetworkStartError lost the cause: %v", err)
	}
	if !strings.Contains(err.Error(), "already assigned to a host interface") {
		t.Fatalf("defaultNetworkStartError = %v, want stale bridge guidance", err)
	}
}

func TestEnsureDefaultNetworkActiveHandlesAutostartAndTOCTOU(t *testing.T) {
	t.Run("create failure tolerated when active after recheck", func(t *testing.T) {
		fake := newFakeConn()
		fake.networkActive = 0
		fake.networkCreateErr = libvirtErr(libvirt.ErrOperationInvalid, "network already active")
		fake.networkActivateOnCreateErr = true

		err := ensureDefaultNetworkActive(fake)
		if err != nil {
			t.Fatalf("ensureDefaultNetworkActive: %v", err)
		}
		if fake.networkAutostart != 1 {
			t.Fatalf("networkAutostart = %d, want 1", fake.networkAutostart)
		}
		if fake.networkCreateCount != 1 {
			t.Fatalf("networkCreateCount = %d, want 1", fake.networkCreateCount)
		}
	})

	t.Run("create failure surfaces when inactive after recheck", func(t *testing.T) {
		fake := newFakeConn()
		fake.networkActive = 0
		fake.networkCreateErr = libvirtErr(libvirt.ErrOperationFailed, "network start failed")

		err := ensureDefaultNetworkActive(fake)
		if err == nil || !strings.Contains(err.Error(), "start default NAT network") {
			t.Fatalf("ensureDefaultNetworkActive error = %v, want start failure", err)
		}
		if fake.networkAutostart != 1 {
			t.Fatalf("networkAutostart = %d, want 1", fake.networkAutostart)
		}
	})
}

func TestCreateVMDefinesMissingDefaultPool(t *testing.T) {
	fake := newFakeConn()
	fake.poolLookupErr = libvirtErr(libvirt.ErrNoStoragePool, "pool missing")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)
	withFakeMkdirAll(t)

	_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:     "new-pool",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  "/isos/install.iso",
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if len(fake.definedPoolXML) != 1 ||
		!strings.Contains(fake.definedPoolXML[0], "<name>default</name>") ||
		!strings.Contains(fake.definedPoolXML[0], defaultPoolPath) {
		t.Fatalf("definedPoolXML = %#v", fake.definedPoolXML)
	}
	if fake.poolAutostart != 1 {
		t.Fatalf("poolAutostart = %d, want 1", fake.poolAutostart)
	}
	if fake.poolCreateCount != 1 {
		t.Fatalf("poolCreateCount = %d, want 1", fake.poolCreateCount)
	}
}
