package virt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestPreflightReadyForCreateRejectsMissingPrerequisites(t *testing.T) {
	ready := readyPreflight()
	tests := []struct {
		name string
		mut  func(*apischema.VMPreflight)
		want string
	}{
		{name: "libvirt", mut: func(p *apischema.VMPreflight) { p.LibvirtReachable = false }, want: "libvirt"},
		{name: "kvm", mut: func(p *apischema.VMPreflight) { p.KvmPresent = false }, want: "KVM"},
		{name: "qemu", mut: func(p *apischema.VMPreflight) { p.QemuPresent = false }, want: "qemu"},
		{name: "network", mut: func(p *apischema.VMPreflight) { p.DefaultNetworkExists = false }, want: "network"},
		{name: "iso", mut: func(p *apischema.VMPreflight) { p.ISOReadable = false }, want: "ISO"},
		{name: "firmware", mut: func(p *apischema.VMPreflight) { p.Firmware = apischema.VMPreflightFirmware{} }, want: "firmware"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ready
			tt.mut(&p)
			err := preflightReadyForNetwork(p, vmSourceTypeISO, defaultNetworkName)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("preflightReadyForCreate error = %v, want containing %q", err, tt.want)
			}
		})
	}
	if err := preflightReadyForNetwork(ready, vmSourceTypeISO, defaultNetworkName); err != nil {
		t.Fatalf("preflightReadyForCreate ready = %v", err)
	}
	ready.DefaultPoolExists = false
	if err := preflightReadyForNetwork(ready, vmSourceTypeISO, defaultNetworkName); err != nil {
		t.Fatalf("preflightReadyForCreate without default pool = %v", err)
	}
	ready.ISOReadable = false
	if err := preflightReadyForNetwork(ready, vmSourceTypeImagePreset, defaultNetworkName); err != nil {
		t.Fatalf("preflightReadyForCreate image preset without ISO = %v", err)
	}
}

func TestCheckDefaultStoragePoolWarnsWhenMissing(t *testing.T) {
	fake := newFakeConn()
	fake.poolLookupErr = libvirtErr(libvirt.ErrNoStoragePool, "pool missing")
	var preflight apischema.VMPreflight

	checkDefaultStoragePool(fake, &preflight)

	if len(preflight.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", preflight.Errors)
	}
	if len(preflight.Warnings) != 1 || !strings.Contains(preflight.Warnings[0], defaultPoolPath) {
		t.Fatalf("warnings = %#v", preflight.Warnings)
	}
}

func TestPreflightLookupErrorsAreReported(t *testing.T) {
	t.Run("storage pool", func(t *testing.T) {
		fake := newFakeConn()
		fake.poolLookupErr = libvirtErr(libvirt.ErrInternalError, "pool lookup failed")
		var preflight apischema.VMPreflight

		checkDefaultStoragePool(fake, &preflight)

		if len(preflight.Errors) != 1 || !strings.Contains(preflight.Errors[0], "lookup failed") {
			t.Fatalf("errors = %#v, want pool lookup failure", preflight.Errors)
		}
		if len(preflight.Warnings) != 0 {
			t.Fatalf("warnings = %#v, want none", preflight.Warnings)
		}
	})

	t.Run("network", func(t *testing.T) {
		fake := newFakeConn()
		fake.networkLookupErr = libvirtErr(libvirt.ErrInternalError, "network lookup failed")
		var preflight apischema.VMPreflight

		checkDefaultNetwork(fake, &preflight)

		if len(preflight.Errors) != 1 || !strings.Contains(preflight.Errors[0], "lookup failed") {
			t.Fatalf("errors = %#v, want network lookup failure", preflight.Errors)
		}
	})
}

func TestDetectOVMFCodePathUsesFirmwareDescriptors(t *testing.T) {
	dir := t.TempDir()
	plainFirmware := tempFile(t, "OVMF_CODE_4M.fd")
	secureFirmware := tempFile(t, "OVMF_CODE_4M.secboot.fd")
	writeFirmwareDescriptor(t, dir, "40-secure.json", secureFirmware, []string{"secure-boot", "requires-smm"})
	writeFirmwareDescriptor(t, dir, "60-plain.json", plainFirmware, []string{"acpi-s3"})
	oldDirs := firmwareDescriptorDirs
	firmwareDescriptorDirs = []string{dir}
	t.Cleanup(func() { firmwareDescriptorDirs = oldDirs })

	if got := detectOVMFCodePath(); got != plainFirmware {
		t.Fatalf("detectOVMFCodePath = %q, want %q", got, plainFirmware)
	}
}

func TestDetectOVMFCodePathSkipsStatelessDescriptors(t *testing.T) {
	dir := t.TempDir()
	plainFirmware := tempFile(t, "OVMF_CODE_4M.fd")
	sevFirmware := tempFile(t, "OVMF.amdsev.fd")
	// Sorts before the plain descriptor, as on Ubuntu.
	stateless := fmt.Sprintf(`{"interface-types":["uefi"],"mapping":{"mode":"stateless","executable":{"filename":%q}},"targets":[{"architecture":"x86_64"}],"features":["amd-sev"]}`, sevFirmware)
	if err := os.WriteFile(filepath.Join(dir, "60-edk2-x86_64-amdsev.json"), []byte(stateless), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFirmwareDescriptor(t, dir, "60-edk2-x86_64.json", plainFirmware, []string{"acpi-s3", "amd-sev"})
	oldDirs := firmwareDescriptorDirs
	firmwareDescriptorDirs = []string{dir}
	t.Cleanup(func() { firmwareDescriptorDirs = oldDirs })

	if got := detectOVMFCodePath(); got != plainFirmware {
		t.Fatalf("detectOVMFCodePath = %q, want %q", got, plainFirmware)
	}
}

func TestDetectOVMFCodePathRejectsSecureOnlyDescriptors(t *testing.T) {
	dir := t.TempDir()
	secureFirmware := tempFile(t, "OVMF_CODE_4M.secboot.fd")
	writeFirmwareDescriptor(t, dir, "50-secure.json", secureFirmware, []string{"secure-boot"})
	oldDirs := firmwareDescriptorDirs
	firmwareDescriptorDirs = []string{dir}
	t.Cleanup(func() { firmwareDescriptorDirs = oldDirs })

	if got := detectOVMFCodePath(); got != "" {
		t.Fatalf("detectOVMFCodePath = %q, want empty for secure-only descriptor", got)
	}
}
