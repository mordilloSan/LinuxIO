package capabilities

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/virt"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// CapabilitySpec describes a single capability: how to detect it, how to
// install it from the UI (if installable), and how to label it in logs.
type CapabilitySpec struct {
	Name    string // wire prefix, e.g. "avahi"
	LogName string // human-friendly name for logs, e.g. "Avahi mDNS"
	Detect  func(ctx context.Context) (bool, string)
	// DetectAsUser replaces Detect for capabilities that depend on the session
	// user's own environment (a per-user Node install, for example). Exactly
	// one of Detect and DetectAsUser is set.
	DetectAsUser func(ctx context.Context, user session.User) (bool, string)
	Install      *InstallSpec // nil = "not installable from the UI"
}

// DetectFor runs whichever of DetectAsUser and Detect the spec sets.
func (s CapabilitySpec) DetectFor(ctx context.Context, user session.User) (bool, string) {
	if s.DetectAsUser != nil {
		return s.DetectAsUser(ctx, user)
	}
	return s.Detect(ctx)
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
	// the distro package manager ("docker" runs Docker's convenience script,
	// "node" installs nvm and Node.js LTS for the session user).
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

var capabilityRegistry = []CapabilitySpec{
	{
		Name:    "docker",
		LogName: "Docker service",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(docker.CheckDockerAvailability(ctx))
		},
		Install: &InstallSpec{
			OptionalComponent: "docker",
			ServiceDebian:     "docker.service",
			ServiceRHEL:       "docker.service",
			EnableService:     true,
		},
	},
	{
		Name:    "docker_updates",
		LogName: "Docker updates",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(dockerupdate.CheckRunnerInstalled())
		},
	},
	{
		Name:    "monitoring",
		LogName: "linuxio-monitoring",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(checkMonitoringAvailability(ctx))
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
			return checkedCapability(checkMemoryModuleInventoryAvailability(ctx))
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
		Name:    "rsync",
		LogName: "rsync",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("rsync", "rsync"))
		},
		Install: &InstallSpec{PackageDebian: "rsync", PackageRHEL: "rsync"},
	},
	{
		Name:    "node",
		LogName: "Node.js (npx)",
		// The agent is spawned through the user's login shell, so detection
		// asks that same shell: nvm-style installs live under the home
		// directory and never appear on the bridge's PATH.
		DetectAsUser: func(ctx context.Context, u session.User) (bool, string) {
			if err := usercmd.ProbeLoginShell(ctx, u, "npx"); err != nil {
				return false, err.Error()
			}
			return true, ""
		},
		Install: &InstallSpec{OptionalComponent: "node"},
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
			// BusNameActive (not Available) is intentional because Avahi only
			// publishes mDNS records while the daemon is actually running.
			ok, err := dbusclient.BusNameActive(ctx, "org.freedesktop.Avahi")
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
	{
		Name:    "btrfs_maintenance",
		LogName: "btrfsmaintenance",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(storage.CheckBtrfsMaintenanceAvailability())
		},
		Install: &InstallSpec{PackageDebian: "btrfsmaintenance", PackageRHEL: "btrfsmaintenance"},
	},
	{
		Name:    "btrfs",
		LogName: "btrfs-progs",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("btrfs", "btrfs-progs"))
		},
		Install: &InstallSpec{PackageDebian: "btrfs-progs", PackageRHEL: "btrfs-progs"},
	},
	{
		Name:    "e2fsprogs",
		LogName: "e2fsprogs",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("mkfs.ext4", "e2fsprogs"))
		},
		Install: &InstallSpec{PackageDebian: "e2fsprogs", PackageRHEL: "e2fsprogs"},
	},
	{
		Name:    "xfsprogs",
		LogName: "xfsprogs",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("mkfs.xfs", "xfsprogs"))
		},
		Install: &InstallSpec{PackageDebian: "xfsprogs", PackageRHEL: "xfsprogs"},
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
