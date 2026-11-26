package capabilities

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/memory"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/virt"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// CapabilitySpec describes a single capability: how to detect it, how to
// install it from the UI (if installable), and how to label it in logs.
type CapabilitySpec struct {
	Name    string // wire key of its status, e.g. "avahi"
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
	// PackageHookDebian runs around the package step when PackageDebian is
	// the package installed, for a package whose own scripts change the host
	// as it installs in a way the capability must not leave behind.
	PackageHookDebian PackageHook

	// OptionalComponent names a LinuxIO-managed install that is not provided by
	// the distro package manager ("docker" runs Docker's convenience script,
	// "node" installs nvm and Node.js LTS for the session user, "claude_code"
	// adds Anthropic's package repository, and "assistant_<agent>" installs
	// that agent's pinned package for the session user).
	OptionalComponent string
	RequiresDocker    bool
}

// AssistantAgentPrefix starts the capability name, and the OptionalComponent
// value, of each assistant agent: "assistant_" plus the acpagents id.
const AssistantAgentPrefix = "assistant_"

// agentHome resolves the home directory agents are installed under. It is a
// seam so tests can install a fake agent outside a real home.
var agentHome = userconfig.Homedir

// assistantAgentCapability reports whether the session user has the pinned
// version of one assistant agent installed. Agents are per user, like the
// Node.js that runs them, and the assistant never installs one on its own.
func assistantAgentCapability(id, logName string) CapabilitySpec {
	name := AssistantAgentPrefix + id
	return CapabilitySpec{
		Name:    name,
		LogName: logName,
		DetectAsUser: func(_ context.Context, u session.User) (bool, string) {
			home, err := agentHome(u.Username)
			if err != nil {
				return false, err.Error()
			}
			if err := acpagents.Installed(id, home); err != nil {
				return false, err.Error()
			}
			return true, ""
		},
		Install: &InstallSpec{OptionalComponent: name},
	}
}

// InstallCommand describes a capability-specific command that must run after
// its packages are installed and before any service actions. Keeping this
// metadata on the registry entry avoids making the capability installer
// depend on capability names for post-install behavior.
type InstallCommand struct {
	Name string
	Args []string
}

// PackageHook prepares the host right before a capability's packages are
// installed and returns the step to run after the package step, or nil when
// there is nothing to do. The installer runs the step exactly once whatever
// the package step's outcome, a failure or a cancel included, on a context
// the cancel does not reach, so whatever the hook holds is let go. The step
// returns a warning for the install's result when it left something as the
// package set it up. A hook that fails releases what it took itself. The
// feature package that owns what the package changes provides it, so the
// installer holds no feature logic.
type PackageHook func(ctx context.Context) (after func(ctx context.Context) (warning string, err error), err error)

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
		Name:    "claude_code",
		LogName: "Claude Code",
		// The Claude agent runs the claude on the session user's own PATH, so
		// detection asks that same login shell: a copy under the home
		// directory counts as much as the system package.
		DetectAsUser: func(ctx context.Context, u session.User) (bool, string) {
			if err := usercmd.ProbeLoginShell(ctx, u, "claude"); err != nil {
				return false, err.Error()
			}
			return true, ""
		},
		Install: &InstallSpec{OptionalComponent: "claude_code", PackageDebian: "claude-code", PackageRHEL: "claude-code"},
	},
	assistantAgentCapability("claude", "Claude assistant adapter"),
	assistantAgentCapability("gemini", "Gemini CLI for the assistant"),
	assistantAgentCapability("codex", "Codex assistant adapter"),
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
		// Either tool that sets zram up: zram-tools on Debian and Ubuntu,
		// zram-generator on Fedora and RHEL, which has no service to enable.
		// zram-tools' package enables and starts its service as it installs;
		// the hook keeps the start from happening and disables the service
		// again, so zram starts only when the admin chooses it in Memory
		// overflow. zram-generator's package starts nothing.
		Name:    "zram",
		LogName: "zram",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(memory.CheckZramAvailability())
		},
		Install: &InstallSpec{PackageDebian: "zram-tools", PackageRHEL: "zram-generator", PackageHookDebian: memory.ZramToolsInstallHook},
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
		// Shows the server in Windows' Network (docs/shared-folders.md,
		// "The SMB service"). Debian 13 and Ubuntu 25.04 on keep the
		// service in wsdd-server, the releases before them and Fedora in
		// wsdd; either is detected.
		Name:    "wsdd",
		LogName: "Windows discovery (WS-Discovery)",
		Detect: func(ctx context.Context) (bool, string) {
			return checkedCapability(nfsshares.CheckWindowsDiscoveryAvailability(ctx))
		},
		Install: &InstallSpec{
			PackageDebian: "wsdd-server",
			PackageRHEL:   "wsdd",
			ServiceDebian: "wsdd-server.service",
			ServiceRHEL:   "wsdd.service",
			EnableService: true,
		},
	},
	{
		Name:    "wireguard",
		LogName: "WireGuard tools",
		Detect: func(_ context.Context) (bool, string) {
			return checkedCapability(checkDependencyCommand("wg-quick", "wireguard-tools"))
		},
		// iptables too: the NAT rules of an interface LinuxIO creates are
		// iptables lines. It is not part of Detect, which would take the page
		// away from a host whose interfaces work without it; a create on a
		// host without it says so (wireguard.AddInterface).
		Install: &InstallSpec{PackageDebian: "wireguard-tools iptables", PackageRHEL: "wireguard-tools iptables-nft"},
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

// The registry's names are the apischema.CapabilityName union of the
// generated contract, so a capability is named once, in the registry. The
// generator loads this package through the handler registry.
func init() {
	apischema.StringEnums["CapabilityName"] = capabilityNames()
}

func capabilityNames() []string {
	names := make([]string, 0, len(capabilityRegistry))
	for _, spec := range capabilityRegistry {
		names = append(names, spec.Name)
	}
	return names
}

func CapabilitySpecByName(name string) (CapabilitySpec, bool) {
	for _, spec := range capabilityRegistry {
		if spec.Name == name {
			return spec, true
		}
	}
	return CapabilitySpec{}, false
}
