package system

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

type capabilitiesResponse struct {
	DockerAvailable        bool   `json:"docker_available"`
	IndexerAvailable       bool   `json:"indexer_available"`
	LMSensorsAvailable     bool   `json:"lm_sensors_available"`
	SmartmontoolsAvailable bool   `json:"smartmontools_available"`
	PackageKitAvailable    bool   `json:"packagekit_available"`
	NFSClientAvailable     bool   `json:"nfs_client_available"`
	NFSServerAvailable     bool   `json:"nfs_server_available"`
	TunedAvailable         bool   `json:"tuned_available"`
	AvahiAvailable         bool   `json:"avahi_available"`
	DockerError            string `json:"docker_error,omitempty"`
	IndexerError           string `json:"indexer_error,omitempty"`
	LMSensorsError         string `json:"lm_sensors_error,omitempty"`
	SmartmontoolsError     string `json:"smartmontools_error,omitempty"`
	PackageKitError        string `json:"packagekit_error,omitempty"`
	NFSClientError         string `json:"nfs_client_error,omitempty"`
	NFSServerError         string `json:"nfs_server_error,omitempty"`
	TunedError             string `json:"tuned_error,omitempty"`
	AvahiError             string `json:"avahi_error,omitempty"`
}

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
	// PackageDebian / PackageRHEL: name of the package to install on each
	// distro family (looked up via PackageKit Resolve). Empty = no package
	// step.
	PackageDebian string
	PackageRHEL   string
	// ServiceDebian / ServiceRHEL: systemd unit to enable+start after install.
	// Empty = no service step.
	ServiceDebian string
	ServiceRHEL   string
	// EnableService: when true, also `systemctl enable` the unit, not just
	// start it.
	EnableService bool
}

var capabilityRegistry = []CapabilitySpec{
	{
		Name:    "docker",
		LogName: "Docker service",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(docker.CheckDockerAvailability(ctx))
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
// the given wire name. A bare switch keeps the wire struct strongly typed; the
// anti-drift test guarantees every wire field has a registry entry, so no
// silent misses are possible.
func setCapabilityField(out *capabilitiesResponse, name string, ok bool, errMsg string) {
	switch name {
	case "docker":
		out.DockerAvailable, out.DockerError = ok, errMsg
	case "indexer":
		out.IndexerAvailable, out.IndexerError = ok, errMsg
	case "lm_sensors":
		out.LMSensorsAvailable, out.LMSensorsError = ok, errMsg
	case "smartmontools":
		out.SmartmontoolsAvailable, out.SmartmontoolsError = ok, errMsg
	case "packagekit":
		out.PackageKitAvailable, out.PackageKitError = ok, errMsg
	case "nfs_client":
		out.NFSClientAvailable, out.NFSClientError = ok, errMsg
	case "nfs_server":
		out.NFSServerAvailable, out.NFSServerError = ok, errMsg
	case "tuned":
		out.TunedAvailable, out.TunedError = ok, errMsg
	case "avahi":
		out.AvahiAvailable, out.AvahiError = ok, errMsg
	default:
		panic("system: unknown capability wire name " + name)
	}
}

func buildCapabilitiesResponse(ctx context.Context) capabilitiesResponse {
	slog.Info("Checking system capabilities.")

	var out capabilitiesResponse
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
