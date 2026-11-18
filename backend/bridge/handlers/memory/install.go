package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// zramInstallHoldPath is the runtime drop-in that keeps zramswap.service from
// starting while zram-tools installs. It is under /run, so a boot clears it.
//
// It is a condition drop-in, the way a swap file is held off at boot
// (holdOffDropInPath), and not a mask: under a mask the package's postinst
// does not enable the unit either, deb-systemd-helper then records no state
// for it, and the next upgrade of the package enables and starts it (seen in
// the Ubuntu 26.04 test VM, init-system-helpers 1.69). zramswap.service is
// wanted by multi-user.target only, no swap.target, so holding it holds
// nothing else back.
const zramInstallHoldPath = "/run/systemd/system/" + zramService + ".d/" + holdOffDropInName

// renderZramInstallHold is the hold: "/" always exists, so the negated
// condition never holds and systemd skips a start of the unit. Its content
// is how a later install tells a hold a killed job host left from a file
// someone else put there.
func renderZramInstallHold() []byte {
	return []byte("# Managed by LinuxIO (Settings, Capabilities); removed when the zram-tools install ends.\n" +
		"# Keeps zram-tools' package from starting zram as it installs.\n" +
		"[Unit]\nConditionPathExists=!/\n")
}

// ZramToolsInstallHook is the zram capability's package hook where the
// package installed is zram-tools (capabilities.InstallSpec.PackageHookDebian).
// zram-tools' postinst enables zramswap.service and starts it, so zram would
// swap at zram-tools' defaults before the admin chose a mode. The hook keeps
// the start from happening (holdZramToolsOff), so nothing about the host's
// swap changes until a mode is chosen in Memory overflow. With zram-tools
// installed already the package step installs nothing, or upgrades the
// package, whose scripts keep the service's start at boot as it was, and
// there is no hook.
func ZramToolsInstallHook(ctx context.Context) (func(context.Context) (string, error), error) {
	if tools, _ := host.zramToolsInstalled(); tools {
		return nil, nil
	}
	return newExecutor(func(apischema.SwapProgress) {}).holdZramToolsOff(ctx)
}

// holdZramToolsOff takes the memory mutation lock, so no memory change runs
// while the package installs, reads whether zramswap.service starts at boot,
// and holds the unit with zramInstallHoldPath. The step it returns, which the
// installer runs once the package step ended whatever its outcome, lets the
// unit go (releaseZramToolsHold) and releases the lock.
//
// While another memory change runs, or a file LinuxIO did not write is at
// the hold's path, nothing is changed: the install goes on without the hook,
// and the step only returns why as a warning.
func (e *executor) holdZramToolsOff(ctx context.Context) (func(context.Context) (string, error), error) {
	unlock, err := lockMutation()
	if err != nil {
		slog.Warn("zram-tools install not held off", "error", err)
		return leftToThePackage("Another memory change was running", "turns"), nil
	}
	held := false
	defer func() {
		if !held {
			unlock()
		}
	}()
	enabledBefore, err := e.zramToolsEnabled(ctx)
	if err != nil {
		return nil, err
	}
	current, ok, err := e.root.readOptional(zramInstallHoldPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", zramInstallHoldPath, err)
	}
	if ok && current != string(renderZramInstallHold()) {
		// That file may hold the unit itself, so the package may start nothing.
		slog.Warn("zram-tools install not held off", "path", zramInstallHoldPath)
		return leftToThePackage("A file LinuxIO did not write is at "+zramInstallHoldPath, "may turn"), nil
	}
	if err := e.holdZramTools(ctx); err != nil {
		// A write can land and still fail, and a reload may not have run.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		return nil, joinOnOneLine(err, e.releaseZramToolsHold(releaseCtx))
	}
	held = true
	return func(ctx context.Context) (string, error) {
		defer unlock()
		return "", e.letZramToolsGo(ctx, enabledBefore)
	}, nil
}

// leftToThePackage is the step of a hook that changed nothing: it says why,
// and what the package does instead, which turns, or may turn, zram on.
func leftToThePackage(why, turns string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return why + ", so zramswap.service was left as zram-tools' package sets it up, which " + turns +
			" zram on now and at every boot. Choose a mode in Memory overflow to change it.", nil
	}
}

// joinOnOneLine joins two errors, either of which may be nil, on one line:
// the text is shown in the Capabilities row and a toast, where the newline
// errors.Join puts between them would not read. Both stay reachable by
// errors.Is and errors.As.
func joinOnOneLine(first, second error) error {
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	}
	return fmt.Errorf("%w; %w", first, second)
}

// zramToolsEnabled reports whether zramswap.service starts at boot. A unit
// no unit file defines, as before zram-tools is installed, does not.
func (e *executor) zramToolsEnabled(ctx context.Context) (bool, error) {
	state, err := e.sys.unitFileState(ctx, zramService)
	if errors.Is(err, systemd.ErrNoUnitFile) {
		return false, nil
	}
	return state == "enabled", err
}

// holdZramTools writes the hold and has systemd read it.
func (e *executor) holdZramTools(ctx context.Context) error {
	dir := filepath.Dir(zramInstallHoldPath)
	if err := os.MkdirAll(e.root.path(dir), 0o755); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	if err := e.sys.writeFile(zramInstallHoldPath, renderZramInstallHold(), managedFileMode); err != nil {
		return fmt.Errorf("write %s: %w", zramInstallHoldPath, err)
	}
	return e.sys.reload(ctx)
}

// letZramToolsGo is the step after the package: zramswap.service, which the
// package enabled, is disabled unless it started at boot before the install,
// and the hold goes, so the unit can be started again. The release has a
// time budget of its own, so a disable that stalls does not keep the hold
// in place. Each error says what the host is left with.
func (e *executor) letZramToolsGo(ctx context.Context, enabledBefore bool) error {
	disabled, bootErr := e.zramToolsOffAtBoot(ctx, enabledBefore)
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	if err := joinOnOneLine(bootErr, e.releaseZramToolsHold(releaseCtx)); err != nil {
		return err
	}
	slog.Info("zram service kept off during install", "unit", zramService, "disabled", disabled)
	return nil
}

// zramToolsOffAtBoot disables zramswap.service when it is enabled now and
// was not before the install, and reports whether it did. With no unit file,
// as after a package step that failed early, there is nothing to disable.
func (e *executor) zramToolsOffAtBoot(ctx context.Context, enabledBefore bool) (bool, error) {
	state, err := e.sys.unitFileState(ctx, zramService)
	switch {
	case errors.Is(err, systemd.ErrNoUnitFile):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%w; %s may start zram at the next boot", err, zramService)
	case enabledBefore || state != "enabled":
		return false, nil
	}
	if err := e.sys.disableUnit(ctx, zramService); err != nil {
		return false, fmt.Errorf("%w; %s starts zram at the next boot", err, zramService)
	}
	return true, nil
}

// releaseZramToolsHold removes the hold and its folder, and has systemd read
// the unit without it.
func (e *executor) releaseZramToolsHold(ctx context.Context) error {
	if err := e.sys.removeFile(zramInstallHoldPath); err != nil {
		return fmt.Errorf("remove %s: %w; %s cannot start until it is removed and systemd reloads, or the next boot", zramInstallHoldPath, err, zramService)
	}
	dir := filepath.Dir(zramInstallHoldPath)
	if err := os.Remove(e.root.path(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("remove the zram-tools hold folder failed", "path", dir, "error", err)
	}
	if err := e.sys.reload(ctx); err != nil {
		return fmt.Errorf("%w; %s cannot start until systemd reloads or the next boot", err, zramService)
	}
	return nil
}

// clearLeftoverZramToolsHold removes a hold that a job host killed during a
// zram-tools install left in /run, so a start of zramswap.service is not
// skipped until the next boot. Only LinuxIO's hold, by its content, is
// removed; any other file there is left alone, and so is one that cannot be
// read, and the start goes on as it would. The caller holds the mutation
// lock, so no install holds the unit meanwhile.
func (e *executor) clearLeftoverZramToolsHold(ctx context.Context) error {
	current, ok, err := e.root.readOptional(zramInstallHoldPath)
	if err != nil {
		slog.Warn("read zram-tools install hold failed", "path", zramInstallHoldPath, "error", err)
		return nil
	}
	if !ok || current != string(renderZramInstallHold()) {
		return nil
	}
	if err := e.releaseZramToolsHold(ctx); err != nil {
		return err
	}
	slog.Info("leftover zram-tools install hold removed", "path", zramInstallHoldPath)
	return nil
}
