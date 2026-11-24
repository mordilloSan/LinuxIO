package wireguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
	ipv4ConfDir       = "/proc/sys/net/ipv4/conf"
	forwardSysctlPath = "/etc/sysctl.d/90-linuxio-wireguard.conf"
)

const forwardSysctlText = "# Written by LinuxIO: a WireGuard interface with NAT passes its peers' traffic on.\n" +
	"net.ipv4.ip_forward = 1\n"

// hookCommands splits a hook line into its commands, as the shell would.
var hookCommands = regexp.MustCompile(`;|&&|\|\|`)

// wantsForwarding reports whether cfg's up hooks make the host an IPv4
// router for its peers: a command with a masquerade rule that is not
// ip6tables', or the sysctl line the configs LinuxIO wrote before carry.
func wantsForwarding(cfg WireGuardConfig) bool {
	for _, kind := range []apischema.WireGuardHookKind{apischema.WireGuardPreUp, apischema.WireGuardPostUp} {
		for _, line := range cfg.Hooks[kind] {
			for _, command := range hookCommands.Split(strings.ToLower(line), -1) {
				if strings.Contains(command, "net.ipv4.ip_forward=1") ||
					strings.Contains(command, "masquerade") && !strings.Contains(command, "ip6tables") {
					return true
				}
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
	return enableForwarding(name)
}

// enableForwarding turns the host's forwarding on and keeps it on at boot.
func enableForwarding(name string) error {
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

// turnOnForwarding sets the host's running value and reports whether it had
// to. A write of ip_forward resets the per-interface settings to their
// defaults, so a host that forwards already is left alone.
func turnOnForwarding() (bool, error) {
	return turnOn(ipForwardPath)
}

// turnOn writes 1 to a forwarding switch under /proc/sys that is not on, and
// reports whether it had to. It reads the value back, so that a write which
// changed nothing fails here and is not found out by the peers.
func turnOn(path string) (bool, error) {
	if on, err := switchOn(path); err != nil {
		return false, err
	} else if on {
		return false, nil
	}
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		return false, err
	}
	if on, err := switchOn(path); err != nil {
		return false, err
	} else if !on {
		return false, fmt.Errorf("%s stayed off after the write", path)
	}
	return true, nil
}

func switchOn(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == "1", nil
}

// interfaceForwardPath is the interface's own forwarding switch, which is
// what the kernel reads for a packet that arrives on it. A new interface
// takes it from conf/default, and a write of ip_forward sets them all.
func interfaceForwardPath(name string) string {
	return filepath.Join(ipv4ConfDir, name, "forwarding")
}

// ensureInterfaceForwards turns the interface's own switch on, once the
// interface is up, when its config needs forwarding: the host may forward
// while conf/default is off, and the interface then came up with it off.
func ensureInterfaceForwards(name string) error {
	if !needsForwarding(name) {
		return nil
	}
	return forwardOnInterface(name)
}

func forwardOnInterface(name string) error {
	changed, err := turnOn(interfaceForwardPath(name))
	if err != nil {
		return fmt.Errorf("turn on IPv4 forwarding on %s: %w", name, err)
	}
	if changed {
		slog.Info("turned on IPv4 forwarding on the interface", "interface", name)
	}
	return nil
}

// notForwarding reports whether an interface that is up, with a config that
// needs forwarding, has it off: on the host or on the interface. It is what
// the page warns about, since the interface looks up and its peers connect.
func notForwarding(name string, cfg WireGuardConfig) bool {
	if !wantsForwarding(cfg) {
		return false
	}
	for _, path := range []string{ipForwardPath, interfaceForwardPath(name)} {
		on, err := switchOn(path)
		if err != nil {
			// Gone between the two reads of a list, or a host that hides it.
			slog.Debug("failed to read a forwarding switch", "interface", name, "path", path, "error", err)
			return false
		}
		if !on {
			return true
		}
	}
	return false
}

// RestoreForwarding turns forwarding on again, now and for boot, for an
// interface the page shows as not forwarding. It does not start or restart
// the interface, so its peers stay connected. An interface whose config
// needs no forwarding is refused, so the answer is never a success that
// changed nothing.
func RestoreForwarding(_ context.Context, req apischema.NameRequest) error {
	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return fmt.Errorf("%w: invalid interface name: %v", bridgeipc.ErrInvalidArgs, err)
	}
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: interface %s not found", bridgeipc.ErrNotFound, name)
		}
		return fmt.Errorf("read config: %w", err)
	}
	if !wantsForwarding(cfg) {
		return fmt.Errorf("%w: interface %s has no NAT rule that needs forwarding", bridgeipc.ErrConflict, name)
	}
	if err := enableForwarding(name); err != nil {
		return err
	}
	if !isInterfaceUpFunc(name) {
		return nil
	}
	return forwardOnInterface(name)
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
