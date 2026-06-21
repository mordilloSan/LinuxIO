package virt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var runPreflight = Preflight
var findOVMFCodePath = detectOVMFCodePath
var firmwareDescriptorDirs = []string{"/etc/qemu/firmware", "/usr/share/qemu/firmware"}

type qemuFirmwareDescriptor struct {
	InterfaceTypes []string `json:"interface-types"`
	Mapping        struct {
		Executable struct {
			Filename string `json:"filename"`
		} `json:"executable"`
	} `json:"mapping"`
	Targets []struct {
		Architecture string `json:"architecture"`
	} `json:"targets"`
	Features []string `json:"features"`
}

func Preflight(ctx context.Context, req apischema.VMPreflightRequest) (apischema.VMPreflight, error) {
	out := apischema.VMPreflight{
		ISOReadable: true,
		Firmware: apischema.VMPreflightFirmware{
			BIOSAvailable: true,
		},
		ManagedPaths: apischema.VMManagedPaths{
			Root:        managedRootPath,
			ISOs:        managedISOPath,
			CloudImages: managedCloudPath,
		},
	}

	collectHostPreflight(&out)
	out.Firmware.UEFIAvailable = ovmfAvailable()
	if !out.Firmware.UEFIAvailable {
		out.Warnings = append(out.Warnings, "OVMF firmware not found; VM creation will fall back to BIOS")
	}
	collectSourcePreflight(req, &out)

	connErr := withLibvirtConn(ctx, func(conn libvirtConn) error {
		out.LibvirtReachable = true
		checkDefaultStoragePool(conn, &out)
		checkDefaultNetwork(conn, &out)
		return nil
	})
	if connErr != nil {
		out.Errors = append(out.Errors, fmt.Sprintf("libvirt not reachable: %v", connErr))
		return out, nil
	}
	return out, nil
}

func collectHostPreflight(out *apischema.VMPreflight) {
	if _, statErr := os.Stat("/dev/kvm"); statErr == nil {
		out.KvmPresent = true
	} else {
		out.Errors = append(out.Errors, "KVM unavailable: /dev/kvm is missing")
	}

	if qemuSystemBinary() != "" {
		out.QemuPresent = true
	} else {
		out.Errors = append(out.Errors, "qemu-system-x86_64 not found")
	}
}

func collectSourcePreflight(req apischema.VMPreflightRequest, out *apischema.VMPreflight) {
	sourceType := normalizedVMSourceType(req.SourceType)
	if sourceErr := validateVMSourceType(req.SourceType); sourceErr != nil {
		out.Errors = append(out.Errors, sourceErr.Error())
		return
	}
	if sourceType == vmSourceTypeImagePreset {
		collectImagePresetPreflight(req, out)
		return
	}
	collectISOPreflight(req, out)
}

func collectImagePresetPreflight(req apischema.VMPreflightRequest, out *apischema.VMPreflight) {
	preset, presetErr := imagePreset(req.ImagePresetID)
	if presetErr != nil {
		out.Errors = append(out.Errors, presetErr.Error())
		return
	}
	if preset.RequiresUEFI && !out.Firmware.UEFIAvailable {
		out.Errors = append(out.Errors, preset.Label+" requires OVMF/UEFI firmware")
	}
	if preset.ImageCompression == "xz" {
		if _, err := execLookPath("xz"); err != nil {
			out.Errors = append(out.Errors, "xz is required to import compressed VM images")
		}
	}
	if _, err := execLookPath("qemu-img"); err != nil {
		out.Errors = append(out.Errors, "qemu-img is required to resize imported VM images")
	}
}

func collectISOPreflight(req apischema.VMPreflightRequest, out *apischema.VMPreflight) {
	if req.ISOPath == nil || strings.TrimSpace(*req.ISOPath) == "" {
		return
	}
	isoPath, mediaErr := validateInstallMedia(*req.ISOPath)
	if mediaErr != nil {
		out.ISOReadable = false
		out.Errors = append(out.Errors, mediaErr.Error())
		return
	}
	*req.ISOPath = isoPath
	out.Warnings = append(out.Warnings, "ISO path is readable, but AppArmor/SELinux or libvirt storage policy may still block arbitrary host paths")
}

func checkDefaultStoragePool(conn libvirtConn, out *apischema.VMPreflight) {
	pool, lookupErr := conn.StoragePoolLookupByName(defaultPoolName)
	if lookupErr != nil {
		if !isStoragePoolMissing(lookupErr) {
			out.Errors = append(out.Errors, fmt.Sprintf("default storage pool lookup failed: %v", lookupErr))
			return
		}
		out.Warnings = append(out.Warnings, "default storage pool is missing; create will define it at "+defaultPoolPath)
		return
	}
	out.DefaultPoolExists = true
	active, activeErr := conn.StoragePoolIsActive(pool)
	if activeErr == nil && active != 0 {
		out.DefaultPoolActive = true
		return
	}
	out.Warnings = append(out.Warnings, "default storage pool exists but is inactive; create will try to start it")
}

func checkDefaultNetwork(conn libvirtConn, out *apischema.VMPreflight) {
	network, lookupErr := conn.NetworkLookupByName(defaultNetworkName)
	if lookupErr != nil {
		if !isNetworkMissing(lookupErr) {
			out.Errors = append(out.Errors, fmt.Sprintf("default NAT network lookup failed: %v", lookupErr))
			return
		}
		out.Errors = append(out.Errors, "default NAT network is missing")
		return
	}
	out.DefaultNetworkExists = true
	active, activeErr := conn.NetworkIsActive(network)
	if activeErr == nil && active != 0 {
		out.DefaultNetworkActive = true
		return
	}
	out.Warnings = append(out.Warnings, "default NAT network exists but is inactive; create will try to start it")
}

func preflightReadyForCreate(p apischema.VMPreflight, sourceType apischema.VMSourceType) error {
	if !p.LibvirtReachable {
		return conflictf("libvirt is not reachable")
	}
	if !p.KvmPresent {
		return conflictf("KVM is unavailable")
	}
	if !p.QemuPresent {
		return conflictf("qemu-system-x86_64 is unavailable")
	}
	if !p.DefaultNetworkExists {
		return conflictf("default NAT network is missing")
	}
	if normalizedVMSourceType(sourceType) == vmSourceTypeISO && !p.ISOReadable {
		return conflictf("ISO is not readable")
	}
	if len(p.Errors) > 0 {
		return conflictf("%s", p.Errors[0])
	}
	if !p.Firmware.BIOSAvailable && !p.Firmware.UEFIAvailable {
		return conflictf("no supported firmware is available")
	}
	return nil
}

func qemuSystemBinary() string {
	for _, name := range []string{"qemu-system-x86_64", "qemu-kvm"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func ovmfAvailable() bool {
	return ovmfCodePath() != ""
}

func ovmfCodePath() string {
	return findOVMFCodePath()
}

func detectOVMFCodePath() string {
	descriptors := firmwareDescriptorPaths()
	for _, descriptorPath := range descriptors {
		data, err := os.ReadFile(descriptorPath)
		if err != nil {
			continue
		}
		var descriptor qemuFirmwareDescriptor
		if err := json.Unmarshal(data, &descriptor); err != nil {
			continue
		}
		if plainUEFIFirmwareExecutable(descriptor) {
			return descriptor.Mapping.Executable.Filename
		}
	}
	return ""
}

func firmwareDescriptorPaths() []string {
	var out []string
	for _, dir := range firmwareDescriptorDirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			continue
		}
		out = append(out, matches...)
	}
	sort.Strings(out)
	return out
}

func plainUEFIFirmwareExecutable(descriptor qemuFirmwareDescriptor) bool {
	executable := descriptor.Mapping.Executable.Filename
	return containsValue(descriptor.InterfaceTypes, "uefi") &&
		firmwareTargetsArch(descriptor, "x86_64") &&
		!containsValue(descriptor.Features, "secure-boot") &&
		!containsValue(descriptor.Features, "requires-smm") &&
		executable != "" &&
		qemuReadable(executable)
}

func firmwareTargetsArch(descriptor qemuFirmwareDescriptor, arch string) bool {
	for _, target := range descriptor.Targets {
		if target.Architecture == arch {
			return true
		}
	}
	return false
}

func containsValue(values []string, want string) bool {
	return slices.Contains(values, want)
}
