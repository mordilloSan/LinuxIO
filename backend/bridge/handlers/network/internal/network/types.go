package network

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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

type CommandRunner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Environment struct {
	NetplanDir          string
	NMConnectionDir     string
	NetworkdDir         string
	IfupdownMain        string
	IfupdownDir         string
	IfcfgDir            string
	Runner              CommandRunner
	WriteFile           func(path string, data []byte, mode fs.FileMode, ownership ...int) error
	RemoveFile          func(path string) error
	ReadFile            func(path string) ([]byte, error)
	InterfaceProbes     func(ctx context.Context) ([]InterfaceProbe, error)
	ManagerForInterface func(ctx context.Context, iface string) (string, error)
	VerifyBridge        func(ctx context.Context, bridge, member string) (bool, error)
	RemoveBridge        func(name string) error
	// VerifyBridgeHandoff is an optional runtime verification seam. The
	// default verifier checks the bridge/member links, MAC, and addresses.
	VerifyBridgeHandoff func(ctx context.Context, state *BridgeHandoffState) (bool, error)
}

// InterfaceProbe is the read-only host state needed before creating a bridge.
// It is deliberately small so bridge preflights cannot accidentally become a
// second network inventory implementation.
type InterfaceProbe struct {
	Name         string
	MAC          string
	Ethernet     bool
	Loopback     bool
	Wireless     bool
	Bridge       bool
	Master       string
	Addresses    []string
	DefaultRoute bool
}

// BridgeHandoffPlan is the explicit, risky operation that moves a host's
// management L3 configuration from Member to Name. ConsoleAcknowledged is
// deliberately part of the plan so callers cannot accidentally omit the
// out-of-band recovery acknowledgement.
type BridgeHandoffPlan struct {
	Name                string `json:"name"`
	Member              string `json:"member"`
	ConsoleAcknowledged bool   `json:"consoleAcknowledged"`
}

// BridgeHandoffState maps a user operation to the native rollback transaction
// owned by NetworkManager or Netplan.
type BridgeHandoffState struct {
	Plan                 BridgeHandoffPlan `json:"plan"`
	Backend              string            `json:"backend"`
	Handle               string            `json:"handle,omitempty"`
	MemberMAC            string            `json:"memberMac"`
	OriginalAddresses    []string          `json:"originalAddresses,omitempty"`
	OriginalDefaultRoute bool              `json:"originalDefaultRoute"`
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
		WriteFile:       utils.WriteFileAtomic,
		RemoveFile:      os.Remove,
	}
}

func existingMode(path string, fallback fs.FileMode) fs.FileMode {
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

func unsupportedf(format string, args ...any) error {
	return fmt.Errorf("unsupported network backend configuration: "+format, args...)
}

func ambiguousf(iface, backend string, paths []string) error {
	return fmt.Errorf(
		"ambiguous %s configuration for interface %s: %s",
		backend, iface, strings.Join(paths, ", "),
	)
}
