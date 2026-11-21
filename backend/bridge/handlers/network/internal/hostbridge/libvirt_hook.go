package hostbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const (
	// Installed from packaging/libvirt/hooks/qemu.d by the LinuxIO package.
	libvirtBridgeNetfilterHook = "/etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter"
	// The package's untouched copy of the hook, which RestoreHook copies to
	// libvirtBridgeNetfilterHook. The hook is a configuration file, and a
	// Debian-family reinstall does not bring one back once it is deleted.
	libvirtBridgeNetfilterHookSource = "/usr/share/linuxio/libvirt/linuxio-bridge-netfilter"
	// The hook is a short shell script; a larger source is not the package's.
	maxLibvirtHookBytes = 1 << 20
)

// libvirtDaemons run the QEMU hooks: the monolithic libvirtd or the modular
// virtqemud, whichever the host runs. Both are named, as the package's
// post-install step does.
var libvirtDaemons = []string{"libvirtd.service", "virtqemud.service"}

// HookState is what libvirt makes of the LinuxIO libvirt hook's file.
type HookState string

const (
	HookInstalled     HookState = "installed"
	HookMissing       HookState = "missing"
	HookNotExecutable HookState = "not_executable"
)

// HookStatus is the LinuxIO libvirt hook while the host firewall drops
// bridged traffic. Restorable says the package's copy RestoreHook takes the
// hook from is there.
type HookStatus struct {
	State      HookState
	Restorable bool
}

func (env Environment) libvirtHookPath() string {
	if env.LibvirtHookPath != "" {
		return env.LibvirtHookPath
	}
	return libvirtBridgeNetfilterHook
}

func (env Environment) libvirtHookSource() string {
	if env.LibvirtHookSource != "" {
		return env.LibvirtHookSource
	}
	return libvirtBridgeNetfilterHookSource
}

// firewallChecks is what the bridge dialogs show before a bridge is made:
// the hook and its warning, or that the FORWARD policy could not be read.
func firewallChecks(ctx context.Context, env Environment) (*HookStatus, []string) {
	drops, err := forwardDropsBridgedTraffic(ctx, env)
	if err != nil {
		return nil, []string{"bridge netfilter is enabled, but the iptables FORWARD policy could not be inspected"}
	}
	if !drops {
		return nil, nil
	}
	hook, warnings := hookStatus(env)
	return &hook, warnings
}

// CheckHook reports the LinuxIO libvirt hook, with a warning when libvirt
// will not run it, while the host firewall drops bridged VM traffic, and nil
// otherwise. It is for a page that asks again and again, so a FORWARD policy
// that cannot be read gives nothing here.
func CheckHook(ctx context.Context, env Environment) (*HookStatus, []string) {
	drops, err := forwardDropsBridgedTraffic(ctx, env)
	if err != nil {
		slog.Debug("iptables forward policy unavailable for the libvirt hook check", "error", err)
		return nil, nil
	}
	if !drops {
		return nil, nil
	}
	hook, warnings := hookStatus(env)
	return &hook, warnings
}

// hookStatus assumes the FORWARD policy drops bridged traffic. The packaged
// libvirt hook accepts intra-bridge forwarding for every bridge a VM attaches
// to, so that policy (Docker's default) is only a problem when libvirt does
// not run the hook: it runs an executable regular file, and finds its hooks
// only when its daemon starts.
func hookStatus(env Environment) (HookStatus, []string) {
	path, source := env.libvirtHookPath(), env.libvirtHookSource()
	status := HookStatus{State: HookInstalled}
	if info, err := statEnvironmentFile(env, source); err == nil && info.Mode().IsRegular() {
		status.Restorable = true
	}
	const dropping = "bridge netfilter is enabled while the iptables FORWARD policy is DROP (Docker does this) and "
	var warning string
	info, err := statEnvironmentFile(env, path)
	switch {
	case err != nil:
		status.State = HookMissing
		warning = dropping + "the LinuxIO libvirt hook " + path + " that allows bridged VM traffic is missing, so VM traffic on the bridge will be dropped"
	case !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0:
		status.State = HookNotExecutable
		warning = dropping + "the LinuxIO libvirt hook " + path + " is not an executable regular file, so libvirt will not run it and VM traffic on the bridge will be dropped"
	default:
		return status, nil
	}
	if !status.Restorable {
		warning += "; LinuxIO's own copy of the hook, " + source + ", is missing as well, so it cannot be restored"
	}
	return status, []string{warning}
}

// RestoreHook copies the package's copy of the libvirt hook over the
// installed hook, mode 0755, and restarts the libvirt daemons, which find
// their hooks only when they start. It takes nothing from the request. The
// copy goes through a new temporary file in the hook's folder and a rename,
// so libvirt never runs a half-written hook; a symlink or a folder at the
// hook's path is refused, never followed or removed. The privileged bridge
// runs as uid and gid 0, so the file is owned by root.
func RestoreHook(ctx context.Context, env Environment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, source := env.libvirtHookPath(), env.libvirtHookSource()
	data, err := readHookSource(source)
	if err != nil {
		return err
	}
	if err := makeHookDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := checkHookDestination(path); err != nil {
		return err
	}
	if err := atomicfile.Write(path, data, 0o755); err != nil {
		return fmt.Errorf("write libvirt hook: %w", err)
	}
	slog.Info("restored libvirt bridge hook", "path", path, "source", source)
	return restartLibvirtDaemons(ctx, env)
}

// readHookSource reads the package's copy of the hook. Its absence is the
// host's state, not anything the request named, so it is a conflict.
func readHookSource(source string) ([]byte, error) {
	f, err := os.Open(source)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: LinuxIO's copy of the libvirt hook, %s, is missing, so the hook cannot be restored", bridgeipc.ErrConflict, source)
	}
	if err != nil {
		return nil, fmt.Errorf("open libvirt hook copy: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect libvirt hook copy: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: LinuxIO's copy of the libvirt hook, %s, is not a regular file", bridgeipc.ErrConflict, source)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxLibvirtHookBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read libvirt hook copy: %w", err)
	}
	if len(data) > maxLibvirtHookBytes {
		return nil, fmt.Errorf("%w: LinuxIO's copy of the libvirt hook, %s, is larger than %d bytes", bridgeipc.ErrConflict, source, maxLibvirtHookBytes)
	}
	return data, nil
}

// makeHookDir makes the hook's folder, and any missing folder above it, mode
// 0755 whatever the bridge's umask (077), as the package installs it. A
// folder that is there keeps its mode.
func makeHookDir(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%w: %s is not a folder", bridgeipc.ErrConflict, dir)
		}
		return nil
	}
	parent := filepath.Dir(dir)
	if !errors.Is(err, fs.ErrNotExist) || parent == dir {
		return fmt.Errorf("inspect libvirt hook folder: %w", err)
	}
	if err := makeHookDir(parent); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("make libvirt hook folder: %w", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return fmt.Errorf("set libvirt hook folder mode: %w", err)
	}
	return nil
}

// checkHookDestination lets the restore replace a regular file, whatever its
// mode, or fill a missing name. Anything else at the hook's path was put there
// by root on purpose, so it is left for an administrator to remove.
func checkHookDestination(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect libvirt hook: %w", err)
	}
	mode := info.Mode()
	switch {
	case mode.IsRegular():
		return nil
	case mode&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: the libvirt hook %s is a symbolic link; remove it, then restore the hook", bridgeipc.ErrConflict, path)
	case mode.IsDir():
		return fmt.Errorf("%w: the libvirt hook %s is a folder; remove it, then restore the hook", bridgeipc.ErrConflict, path)
	default:
		return fmt.Errorf("%w: the libvirt hook %s is not a regular file; remove it, then restore the hook", bridgeipc.ErrConflict, path)
	}
}

// restartLibvirtDaemons restarts whichever libvirt daemon runs, as the
// package's post-install try-restart does; one the host does not have or does
// not run is left alone. A failure leaves the restored hook in place, and
// libvirt runs it once its daemon next starts.
func restartLibvirtDaemons(ctx context.Context, env Environment) error {
	tryRestart := env.TryRestartUnit
	if tryRestart == nil {
		tryRestart = systemd.TryRestartUnit
	}
	var errs []error
	for _, unit := range libvirtDaemons {
		if err := tryRestart(ctx, unit); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("libvirt hook restored, but restarting libvirt failed, so libvirt runs the hook only after its daemon next starts: %w", err)
	}
	return nil
}
