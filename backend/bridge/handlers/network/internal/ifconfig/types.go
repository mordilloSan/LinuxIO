package ifconfig

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/netdbus"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

type InterfaceConfig struct {
	Backend       string
	IPv4Method    string
	IPv4Addresses []string
	IPv6Method    string
	IPv6Addresses []string
	DNS           []string
	Gateway       string
	MTU           *uint32
	// Optional is nil when the backend cannot express "boot does not wait
	// for this link".
	Optional *bool
	// DNSOptions is nil when the backend cannot express search domains or
	// static DNS servers alongside DHCP.
	DNSOptions *DNSOptions
}

type DNSOptions struct {
	Search []string
	// IgnoreDHCP is true when static IPv4 DNS servers replace the ones DHCP
	// offers.
	IgnoreDHCP bool
}

type ConfigBackend interface {
	Name() string
	Read() (InterfaceConfig, error)
	SetIPv4DHCP(ctx context.Context) error
	SetIPv4Manual(ctx context.Context, addressCIDR, gateway string, dns []string) error
	SetIPv6DHCP(ctx context.Context) error
	SetIPv6Static(ctx context.Context, addressCIDR string) error
	SetMTU(ctx context.Context, mtu uint32) error
	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
}

// OptionalSetter is implemented by backends that can mark a link as not
// required for the host to be considered online at boot.
type OptionalSetter interface {
	SetOptional(ctx context.Context, optional bool) error
}

// DNSOptionsSetter is implemented by backends that report DNSOptions.
type DNSOptionsSetter interface {
	SetIPv4DHCPWithDNS(ctx context.Context, dns, search []string) error
	SetIPv4ManualWithSearch(ctx context.Context, addressCIDR, gateway string, dns, search []string) error
}

type CommandRunner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Environment struct {
	NetplanDir      string
	NMConnectionDir string
	NetworkdDir     string
	IfupdownMain    string
	IfupdownDir     string
	IfcfgDir        string
	Runner          CommandRunner
	NetplanCall     func(ctx context.Context, method string) error
	// NetworkdCall invokes an org.freedesktop.network1.Manager method; iface
	// is the link argument for ReconfigureLink and empty for Reload.
	NetworkdCall func(ctx context.Context, method, iface string) error
	RestartUnit  func(ctx context.Context, unit string) error
	WriteFile    func(path string, data []byte, mode fs.FileMode, ownership ...int) error
}

func DefaultEnvironment() Environment {
	return Environment{
		NetplanDir:      "/etc/netplan",
		NMConnectionDir: "/etc/NetworkManager/system-connections",
		NetworkdDir:     "/etc/systemd/network",
		IfupdownMain:    "/etc/network/interfaces",
		IfupdownDir:     "/etc/network/interfaces.d",
		IfcfgDir:        "/etc/sysconfig/network-scripts",
		Runner:          ExecRunner{},
		NetplanCall:     netdbus.CallNetplan,
		NetworkdCall:    netdbus.CallNetworkd,
		RestartUnit:     systemd.RestartUnit,
		WriteFile:       atomicfile.Write,
	}
}

// CallNetworkd reloads systemd-networkd or reconfigures one link through
// NetworkdCall.
func (e Environment) CallNetworkd(ctx context.Context, method, iface string) error {
	if e.NetworkdCall == nil {
		return fmt.Errorf("systemd-networkd D-Bus access is unavailable")
	}
	return e.NetworkdCall(ctx, method, iface)
}

func ExistingMode(path string, fallback fs.FileMode) fs.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return fallback
	}
	return info.Mode().Perm()
}

func globSorted(pattern string) ([]string, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(paths) <= 1 {
		return paths, nil
	}
	for i := 0; i < len(paths)-1; i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j] < paths[i] {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
	return paths, nil
}

func Unsupportedf(format string, args ...any) error {
	return fmt.Errorf("unsupported network backend configuration: "+format, args...)
}

func ambiguousf(iface, backend string, paths []string) error {
	return fmt.Errorf(
		"ambiguous %s configuration for interface %s: %s",
		backend, iface, strings.Join(paths, ", "),
	)
}
