package system

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/virt"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
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
	// OptionalPackageRHEL is installed on RHEL-family systems when available,
	// but a package-manager failure must not prevent the required capability
	// service from being enabled and verified.
	OptionalPackageRHEL string
	// OptionalPackageRHELFailureWarning explains the capability-specific
	// consequence when OptionalPackageRHEL cannot be installed. Package-manager
	// details remain in task output rather than this user-facing summary.
	OptionalPackageRHELFailureWarning string
	ServiceDebian                     string
	ServiceRHEL                       string
	EnableService                     bool
	PostInstall                       *InstallCommand

	// OptionalComponent names a LinuxIO-managed install that is not provided by
	// the distro package manager.
	OptionalComponent string
	RequiresDocker    bool
}

// InstallCommand describes a capability-specific command that must run after
// its packages are installed and before any service actions. Keeping this
// metadata on the registry entry avoids making the capability installer
// depend on capability names for post-install behavior.
type InstallCommand struct {
	Name string
	Args []string
}

const (
	OptionalComponentMonitoring = "monitoring"
)

const monitoringHealthTimeout = 5 * time.Second

var (
	monitoringCLILookPath = exec.LookPath
	monitoringCLIOutput   = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
)

var capabilityRegistry = []CapabilitySpec{
	{
		Name:    "docker",
		LogName: "Docker service",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(docker.CheckDockerAvailability(ctx))
		},
	},
	{
		Name:    "docker_updates",
		LogName: "Docker updates",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(docker.CheckDockerUpdateRunnerInstalled())
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
		Name:    "monitoring",
		LogName: "go-monitoring agent",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(checkMonitoringAvailability(ctx))
		},
		Install: &InstallSpec{
			OptionalComponent: OptionalComponentMonitoring,
			ServiceDebian:     "go-monitoring.service",
			ServiceRHEL:       "go-monitoring.service",
			EnableService:     true,
		},
	},
	{
		Name:    "lm_sensors",
		LogName: "lm-sensors",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("sensors", "lm-sensors"))
		},
		Install: &InstallSpec{
			PackageDebian: "lm-sensors",
			PackageRHEL:   "lm_sensors",
			PostInstall:   &InstallCommand{Name: "sensors-detect", Args: []string{"--auto"}},
		},
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
		Name:    "samba_client",
		LogName: "Samba client",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(storage.CheckCIFSClientAvailability())
		},
		Install: &InstallSpec{
			PackageDebian: "cifs-utils smbclient",
			PackageRHEL:   "cifs-utils samba-client",
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
			PackageDebian:                     "avahi-daemon libnss-mdns",
			PackageRHEL:                       "avahi",
			OptionalPackageRHEL:               "nss-mdns",
			OptionalPackageRHELFailureWarning: "nss-mdns was not installed. Avahi is running, but this host may need EPEL for .local name resolution.",
			ServiceDebian:                     "avahi-daemon.service",
			ServiceRHEL:                       "avahi-daemon.service",
			EnableService:                     true,
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

func checkMonitoringAvailability(ctx context.Context) (bool, error) {
	if _, err := monitoringCLILookPath("go-monitoring"); err != nil {
		return false, fmt.Errorf("go-monitoring not found (missing go-monitoring dependency)")
	}

	checkCtx, cancel := context.WithTimeout(ctx, monitoringHealthTimeout)
	defer cancel()

	output, err := monitoringCLIOutput(checkCtx, "go-monitoring", "health")
	if err == nil {
		return true, nil
	}

	message := strings.TrimSpace(string(output))
	if message != "" {
		return false, fmt.Errorf("go-monitoring health failed: %s", message)
	}
	return false, fmt.Errorf("go-monitoring health failed: %w", err)
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
// the given wire name. The available/error fields are promoted from the shared
// session.Capabilities* structs embedded in the wire response; the bare switch
// keeps them strongly typed, and the anti-drift test guarantees every wire name
// has a matching field, so no silent misses are possible.
func setCapabilityField(out *apischema.CapabilitiesResponse, name string, ok bool, errMsg string) {
	var errPtr *string
	if errMsg != "" {
		msg := errMsg
		errPtr = &msg
	}
	switch name {
	case "docker":
		out.DockerAvailable, out.DockerError = ok, errPtr
	case "docker_updates":
		out.DockerUpdatesAvailable, out.DockerUpdatesError = ok, errPtr
	case "indexer":
		out.IndexerAvailable, out.IndexerError = ok, errPtr
	case "monitoring":
		out.MonitoringAvailable, out.MonitoringError = ok, errPtr
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
	case "samba_client":
		out.SambaClientAvailable, out.SambaClientError = ok, errPtr
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

func buildCapabilitiesResponse(ctx context.Context) (apischema.CapabilitiesResponse, error) {
	slog.Info("Checking system capabilities.")

	if err := ctx.Err(); err != nil {
		return apischema.CapabilitiesResponse{}, err
	}

	type detectionResult struct {
		ok       bool
		errMsg   string
		duration time.Duration
	}
	results := make([]detectionResult, len(capabilityRegistry))
	detectionStarted := time.Now()
	var wg sync.WaitGroup
	for index, spec := range capabilityRegistry {
		wg.Go(func() {
			started := time.Now()
			results[index].ok, results[index].errMsg = spec.Detect(ctx)
			results[index].duration = time.Since(started)
		})
	}
	wg.Wait()
	detectionDuration := time.Since(detectionStarted)

	if err := ctx.Err(); err != nil {
		return apischema.CapabilitiesResponse{}, err
	}

	var out apischema.CapabilitiesResponse
	summary := make([]string, 0, len(capabilityRegistry))

	for index, spec := range capabilityRegistry {
		result := results[index]
		setCapabilityField(&out, spec.Name, result.ok, result.errMsg)
		summary = append(summary, fmt.Sprintf("%s=%s", strings.ReplaceAll(spec.Name, "_", "-"), capabilityStatus(result.ok)))
		logUnavailableCapability(spec.LogName, result.errMsg)
	}

	slog.Info("Capabilities: " + strings.Join(summary, " ") + ".")
	timingAttrs := make([]slog.Attr, 0, len(capabilityRegistry)+1)
	timingAttrs = append(timingAttrs, slog.Int64("capabilities_us", detectionDuration.Microseconds()))
	for index, spec := range capabilityRegistry {
		timingAttrs = append(timingAttrs,
			slog.Int64("capabilities_"+spec.Name+"_us", results[index].duration.Microseconds()))
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "capabilities timing", timingAttrs...)

	return out, nil
}

var errAvahiUnavailable = fmt.Errorf("avahi-daemon is not running")

// checkAvahiAvailability uses BusNameActive (not Available) because Avahi only
// publishes mDNS records while the daemon is actually running. An activatable-
// but-stopped daemon would satisfy the looser check yet leave <hostname>.local
// unreachable from the LAN.
func checkAvahiAvailability(ctx context.Context) (bool, error) {
	return dbusclient.BusNameActive(ctx, "org.freedesktop.Avahi")
}
