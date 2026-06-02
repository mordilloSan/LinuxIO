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
	NetplanDir      string
	NMConnectionDir string
	NetworkdDir     string
	IfupdownMain    string
	IfupdownDir     string
	IfcfgDir        string
	Runner          CommandRunner
	WriteFile       func(path string, data []byte, mode fs.FileMode) error
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
