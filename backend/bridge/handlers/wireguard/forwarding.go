package wireguard

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
)

// A server passes its peers' traffic on only with IPv4 forwarding on. LinuxIO
// turns it on itself before it starts an interface whose hooks masquerade,
// and keeps it on at boot with a sysctl.d file. A `sysctl` line in PostUp,
// which LinuxIO wrote before, is refused by the AppArmor profile Ubuntu
// confines wg-quick with: sysctl says so and exits 0, the interface came up,
// and its peers reached nothing behind the server.
var (
	ipForwardPath     = "/proc/sys/net/ipv4/ip_forward"
	forwardSysctlPath = "/etc/sysctl.d/90-linuxio-wireguard.conf"
)

const forwardSysctlText = "# Written by LinuxIO: a WireGuard interface with NAT passes its peers' traffic on.\n" +
	"net.ipv4.ip_forward = 1\n"

// wantsForwarding reports whether cfg's up hooks make the host an IPv4
// router for its peers: a masquerade rule that is not ip6tables', or the
// sysctl line the configs LinuxIO wrote before carry.
func wantsForwarding(cfg WireGuardConfig) bool {
	for _, kind := range []apischema.WireGuardHookKind{apischema.WireGuardPreUp, apischema.WireGuardPostUp} {
		for _, line := range cfg.Hooks[kind] {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "net.ipv4.ip_forward=1") ||
				strings.Contains(lower, "masquerade") && !strings.Contains(lower, "ip6tables") {
				return true
			}
		}
	}
	return false
}

// needsForwarding reads the interface's config and reports whether it wants
// forwarding. A config that cannot be read is left to wg-quick, which says
// what is wrong with it when the unit starts.
func needsForwarding(name string) bool {
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		slog.Debug("skipped forwarding for an unreadable config", "interface", name, "error", err)
		return false
	}
	return wantsForwarding(cfg)
}

// ensureForwarding turns IPv4 forwarding on, now and for boot, when the
// interface's config needs it. It fails instead of starting an interface
// whose peers would reach nothing.
func ensureForwarding(name string) error {
	if !needsForwarding(name) {
		return nil
	}
	running, err := turnOnForwarding()
	if err != nil {
		return fmt.Errorf("turn on IPv4 forwarding for %s: %w", name, err)
	}
	boot, err := keepForwardingAtBoot()
	if err != nil {
		return fmt.Errorf("keep IPv4 forwarding on at boot for %s: %w", name, err)
	}
	if boot || running {
		slog.Info("turned on IPv4 forwarding", "interface", name, "running", running, "boot", boot)
	}
	return nil
}

// ensureForwardingAtBoot is the boot half alone, for an interface that is
// set to start at boot: one LinuxIO did not bring up itself (up since before
// this LinuxIO, or started with systemctl) has no sysctl.d file yet.
func ensureForwardingAtBoot(name string) error {
	if !needsForwarding(name) {
		return nil
	}
	boot, err := keepForwardingAtBoot()
	if err != nil {
		return fmt.Errorf("keep IPv4 forwarding on at boot for %s: %w", name, err)
	}
	if boot {
		slog.Info("kept IPv4 forwarding on at boot", "interface", name)
	}
	return nil
}

// keepForwardingAtBoot writes the sysctl.d file and reports whether it had to.
func keepForwardingAtBoot() (bool, error) {
	if data, err := os.ReadFile(forwardSysctlPath); err == nil && string(data) == forwardSysctlText {
		return false, nil
	}
	if err := atomicfile.Write(forwardSysctlPath, []byte(forwardSysctlText), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// turnOnForwarding sets the running value and reports whether it had to. A
// write of ip_forward resets the per-interface settings to their defaults,
// so a host that forwards already is left alone.
func turnOnForwarding() (bool, error) {
	if on, err := forwardingOn(); err != nil {
		return false, err
	} else if on {
		return false, nil
	}
	if err := os.WriteFile(ipForwardPath, []byte("1\n"), 0o644); err != nil {
		return false, err
	}
	// Read back, so that a write which changed nothing fails here and is
	// not found out by the peers.
	if on, err := forwardingOn(); err != nil {
		return false, err
	} else if !on {
		return false, fmt.Errorf("%s stayed off after the write", ipForwardPath)
	}
	return true, nil
}

func forwardingOn() (bool, error) {
	data, err := os.ReadFile(ipForwardPath)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == "1", nil
}

// dropForwardingAtBoot deletes the sysctl.d file once no interface config
// needs it. The running value stays: Docker, libvirt or the administrator
// may count on it, and LinuxIO cannot tell.
func dropForwardingAtBoot() {
	files, _ := filepath.Glob(filepath.Join(wgConfigDir, "*"+configExt))
	for _, file := range files {
		cfg, err := ParseWireGuardConfig(file)
		if err != nil || wantsForwarding(cfg) {
			// A config that cannot be read may need it.
			return
		}
	}
	if err := os.Remove(forwardSysctlPath); err == nil {
		slog.Info("removed the IPv4 forwarding boot setting", "path", forwardSysctlPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("failed to remove the IPv4 forwarding boot setting", "path", forwardSysctlPath, "error", err)
	}
}
