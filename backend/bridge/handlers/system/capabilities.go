package system

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/virt"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/watchtower"
)

// CapabilitySpec describes a single capability: how to detect it, how to
// install it from the UI (if installable), and how to label it in logs.
type CapabilitySpec struct {
	Name    string // wire prefix, e.g. "avahi"
	LogName string // human-friendly name for logs, e.g. "Avahi mDNS"
	Detect  func(ctx context.Context) (bool, string)
	Install *InstallSpec // nil = "not installable from the UI"
}

// InstallSpec describes what `system.install_capability` should do for one
// capability. Either or both of the package/service halves may be set.
type InstallSpec struct {
	PackageDebian string
	PackageRHEL   string
	ServiceDebian string
	ServiceRHEL   string
	EnableService bool

	// OptionalComponent names a LinuxIO-managed install that is not provided by
	// the distro package manager.
	OptionalComponent string
	RequiresDocker    bool
}

const OptionalComponentWatchtower = "watchtower"

var capabilityRegistry = []CapabilitySpec{
	{
		Name:    "docker",
		LogName: "Docker service",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(docker.CheckDockerAvailability(ctx))
		},
	},
	{
		Name:    "watchtower",
		LogName: "Watchtower",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(watchtower.CheckInstalled())
		},
		Install: &InstallSpec{
			OptionalComponent: OptionalComponentWatchtower,
			RequiresDocker:    true,
		},
	},
	{
		Name:    "indexer",
		LogName: "Indexer API",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(filebrowser.CheckIndexerAvailability(ctx))
		},
	},
	{
		Name:    "lm_sensors",
		LogName: "lm-sensors",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("sensors", "lm-sensors"))
		},
		Install: &InstallSpec{PackageDebian: "lm-sensors", PackageRHEL: "lm_sensors"},
	},
	{
		Name:    "memory_inventory",
		LogName: "Memory module inventory",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(CheckMemoryModuleInventoryAvailability(ctx))
		},
		Install: &InstallSpec{PackageDebian: "dmidecode", PackageRHEL: "dmidecode"},
	},
	{
		Name:    "smartmontools",
		LogName: "smartmontools",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("smartctl", "smartmontools"))
		},
		Install: &InstallSpec{PackageDebian: "smartmontools", PackageRHEL: "smartmontools"},
	},
	{
		Name:    "packagekit",
		LogName: "PackageKit",
		Detect: func(ctx context.Context) (bool, string) {
			ok, err := dbusclient.PackageKit.Available(ctx)
			return checkedCapabilityErr(ok, err, dbusclient.ErrPackageKitUnavailable)
		},
	},
	{
		Name:    "nfs_client",
		LogName: "NFS client",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(storage.CheckNFSClientAvailability())
		},
		Install: &InstallSpec{PackageDebian: "nfs-common", PackageRHEL: "nfs-utils"},
	},
	{
		Name:    "nfs_server",
		LogName: "NFS server",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(nfsshares.CheckNFSServerAvailability())
		},
		Install: &InstallSpec{
			PackageDebian: "nfs-kernel-server",
			PackageRHEL:   "nfs-utils",
			ServiceDebian: "nfs-kernel-server.service",
			ServiceRHEL:   "nfs-server.service",
			EnableService: true,
		},
	},
	{
		Name:    "samba_server",
		LogName: "Samba server",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(nfsshares.CheckSambaServerAvailability())
		},
		Install: &InstallSpec{
			PackageDebian: "samba",
			PackageRHEL:   "samba",
			ServiceDebian: "smbd.service",
			ServiceRHEL:   "smb.service",
			EnableService: true,
		},
	},
	{
		Name:    "tuned",
		LogName: "TuneD",
		Detect: func(ctx context.Context) (bool, string) {
			ok, err := power.Available(ctx)
			return checkedCapabilityErr(ok, err, power.ErrUnavailable)
		},
		Install: &InstallSpec{
			PackageDebian: "tuned",
			PackageRHEL:   "tuned",
			ServiceDebian: "tuned.service",
			ServiceRHEL:   "tuned.service",
			EnableService: true,
		},
	},
	{
		Name:    "avahi",
		LogName: "Avahi mDNS",
		Detect: func(ctx context.Context) (bool, string) {
			ok, err := checkAvahiAvailability(ctx)
			return checkedCapabilityErr(ok, err, errAvahiUnavailable)
		},
		Install: &InstallSpec{
			PackageDebian: "avahi-daemon",
			PackageRHEL:   "avahi",
			ServiceDebian: "avahi-daemon.service",
			ServiceRHEL:   "avahi-daemon.service",
			EnableService: true,
		},
	},
	{
		Name:    "wireguard",
		LogName: "WireGuard tools",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("wg-quick", "wireguard-tools"))
		},
		Install: &InstallSpec{PackageDebian: "wireguard-tools", PackageRHEL: "wireguard-tools"},
	},
	{
		Name:    "libvirt",
		LogName: "libvirt",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(virt.CheckLibvirtAvailability(ctx))
		},
		Install: &InstallSpec{
			PackageDebian: "libvirt-daemon-system qemu-system-x86 qemu-utils ovmf xz-utils",
			PackageRHEL:   "libvirt qemu-kvm qemu-img edk2-ovmf xz",
		},
	},
}

func CapabilitySpecByName(name string) (CapabilitySpec, bool) {
	for _, spec := range capabilityRegistry {
		if spec.Name == name {
			return spec, true
		}
	}
	return CapabilitySpec{}, false
}

func checkDependencyCommand(command, dependencyName string) (bool, error) {
	if _, err := exec.LookPath(command); err != nil {
		return false, fmt.Errorf("%s not found (missing %s dependency)", command, dependencyName)
	}
	return true, nil
}

func checkedCapability(ok bool, err error) (bool, string) {
	return checkedCapabilityErr(ok, err, nil)
}

func checkedCapabilityErr(ok bool, err error, unavailable error) (bool, string) {
	if err != nil {
		return false, err.Error()
	}
	if !ok && unavailable != nil {
		return false, unavailable.Error()
	}
	return ok, ""
}

func capabilityStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}

func logUnavailableCapability(name, message string) {
	if message == "" {
		return
	}
	slog.Info(name+" unavailable.", "error", message)
}

// setCapabilityField writes (ok, errMsg) into the matching fields of out for
// the given wire name. The available/error fields are promoted from the
// embedded session.Capabilities* structs (the single source of truth); the
// bare switch keeps them strongly typed, and the anti-drift test guarantees
// every wire name has a matching field, so no silent misses are possible.
func setCapabilityField(out *apischema.CapabilitiesResponse, name string, ok bool, errMsg string) {
	var errPtr *string
	if errMsg != "" {
		msg := errMsg
		errPtr = &msg
	}
	switch name {
	case "docker":
		out.DockerAvailable, out.DockerError = ok, errPtr
	case "watchtower":
		out.WatchtowerAvailable, out.WatchtowerError = ok, errPtr
	case "indexer":
		out.IndexerAvailable, out.IndexerError = ok, errPtr
	case "lm_sensors":
		out.LMSensorsAvailable, out.LMSensorsError = ok, errPtr
	case "memory_inventory":
		out.MemoryInventoryAvailable, out.MemoryInventoryError = ok, errPtr
	case "smartmontools":
		out.SmartmontoolsAvailable, out.SmartmontoolsError = ok, errPtr
	case "packagekit":
		out.PackageKitAvailable, out.PackageKitError = ok, errPtr
	case "nfs_client":
		out.NFSClientAvailable, out.NFSClientError = ok, errPtr
	case "nfs_server":
		out.NFSServerAvailable, out.NFSServerError = ok, errPtr
	case "samba_server":
		out.SambaServerAvailable, out.SambaServerError = ok, errPtr
	case "tuned":
		out.TunedAvailable, out.TunedError = ok, errPtr
	case "avahi":
		out.AvahiAvailable, out.AvahiError = ok, errPtr
	case "wireguard":
		out.WireGuardAvailable, out.WireGuardError = ok, errPtr
	case "libvirt":
		out.LibvirtAvailable, out.LibvirtError = ok, errPtr
	default:
		panic("system: unknown capability wire name " + name)
	}
}

func buildCapabilitiesResponse(ctx context.Context) apischema.CapabilitiesResponse {
	slog.Info("Checking system capabilities.")

	var out apischema.CapabilitiesResponse
	summary := make([]string, 0, len(capabilityRegistry))

	for _, spec := range capabilityRegistry {
		ok, errMsg := spec.Detect(ctx)
		setCapabilityField(&out, spec.Name, ok, errMsg)
		summary = append(summary, fmt.Sprintf("%s=%s", strings.ReplaceAll(spec.Name, "_", "-"), capabilityStatus(ok)))
		logUnavailableCapability(spec.LogName, errMsg)
	}

	slog.Info("Capabilities: " + strings.Join(summary, " ") + ".")

	return out
}

var errAvahiUnavailable = fmt.Errorf("avahi-daemon is not running")

// checkAvahiAvailability uses BusNameActive (not Available) because Avahi only
// publishes mDNS records while the daemon is actually running. An activatable-
// but-stopped daemon would satisfy the looser check yet leave <hostname>.local
// unreachable from the LAN.
func checkAvahiAvailability(ctx context.Context) (bool, error) {
	return dbusclient.BusNameActive(ctx, "org.freedesktop.Avahi")
}
