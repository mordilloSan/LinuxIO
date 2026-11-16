package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// zswapCompressor is the compressor the zswap mode asks the kernel for.
const zswapCompressor = "zstd"

// SetSwapMode switches the host to mode, disk, zswap or zram, end to end,
// reporting each step as it starts. The result is the host's status after
// the change; it is mixed when a swap device stayed for lack of room.
func SetSwapMode(ctx context.Context, mode apischema.SwapMode, report func(apischema.SwapProgress)) (apischema.SwapStatus, error) {
	return newExecutor(report).setSwapMode(ctx, mode)
}

func (e *executor) setSwapMode(ctx context.Context, target apischema.SwapMode) (apischema.SwapStatus, error) {
	if _, ok := presetFor(target); !ok {
		return apischema.SwapStatus{}, fmt.Errorf("%w: unknown swap mode %q", bridgeipc.ErrInvalidArgs, target)
	}
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	steps, err := planSwapMode(view, target)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	run := &modeRun{executor: e, target: target, before: view.status}
	for i, step := range steps {
		e.report(apischema.SwapProgress{Step: step.code, Index: i + 1, Total: len(steps)})
		if err := run.do(ctx, step); err != nil {
			return apischema.SwapStatus{}, errors.Join(fmt.Errorf("step %s: %w", step.code, err), run.rollback(ctx))
		}
	}
	slog.Info("swap mode changed", "mode", target, "previous_mode", view.status.Mode,
		"swap_file_left_active", run.swapFileLeft, "zram_left_active", run.zramLeft)
	return e.sys.status(ctx)
}

// modeRun is one mode change in progress, with what it must undo if a step
// fails: the files it wrote, the kernel values it changed and whether it
// set zram to start at boot. Swap it added is never undone.
type modeRun struct {
	*executor
	target apischema.SwapMode
	// before is the status read when the request arrived.
	before           apischema.SwapStatus
	files            []savedFile
	params           []savedParam
	zramEnabledByRun bool
	// swapFileLeft and zramLeft record a device left active for lack of
	// room, which makes the result mixed.
	swapFileLeft, zramLeft bool
}

func (r *modeRun) do(ctx context.Context, step swapStep) error {
	switch step.code {
	case apischema.SwapStepWriteConfig:
		return r.writeConfig()
	case apischema.SwapStepStartZram:
		return r.startZram(ctx, step.run)
	case apischema.SwapStepApplySysctls:
		return r.applySysctls()
	case apischema.SwapStepDisableZswap:
		return r.disableZswap()
	case apischema.SwapStepEnableZswap:
		return r.enableZswap()
	case apischema.SwapStepStopSwapFile:
		return r.stopSwapFile(ctx, step)
	case apischema.SwapStepStartSwapFile:
		return r.startSwapFile(ctx, step)
	case apischema.SwapStepStopZram:
		return r.stopZram(ctx, step.run)
	default:
		return fmt.Errorf("unknown step %q", step.code)
	}
}

// generated reports whether zram-generator sets zram up on this host, and
// not zram-tools.
func (r *modeRun) generated() bool {
	return r.before.ZramProvider == apischema.ZramProviderGenerator
}

// writeConfig writes the zram provider's config for the planned size, then
// the sysctl drop-in.
func (r *modeRun) writeConfig() error {
	path, config := zramswapConfigPath, renderZramswapConfig(r.before.ZramPlannedSize)
	if r.generated() {
		path, config = zramGeneratorConfigPath, renderZramGeneratorConfig(r.before.ZramPlannedSize)
	}
	if err := r.writeFile(path, config); err != nil {
		return err
	}
	return r.writeDropIn()
}

// writeDropIn writes the target mode's sysctls into the drop-in, keeping
// any other key it holds.
func (r *modeRun) writeDropIn() error {
	entries, err := r.root.readSysctlDropIn()
	if err != nil {
		return err
	}
	set, remove := dropInChanges(r.target, r.before.TunedActive)
	return r.writeFile(sysctlDropInPath, renderSysctlDropIn(mergeSysctlDropIn(entries, set, remove)))
}

// startZram sets zram to start at boot and, when run, starts its unit and
// waits for a zram device in /proc/swaps. zram-tools has a service to
// enable. zram-generator has none: a systemd reload makes its swap unit
// from the configuration write_config wrote, wanted by swap.target.
func (r *modeRun) startZram(ctx context.Context, run bool) error {
	unit := zramService
	if r.generated() {
		unit = zramGeneratorSwapUnit
		if err := r.sys.reload(ctx); err != nil {
			return err
		}
		r.zramEnabledByRun = true
	} else {
		state, err := r.sys.unitFileState(ctx, zramService)
		if err != nil {
			return err
		}
		if err := r.sys.enableUnit(ctx, zramService); err != nil {
			return err
		}
		r.zramEnabledByRun = state != "enabled"
	}
	if !run {
		return nil
	}
	if err := r.sys.startUnit(ctx, unit); err != nil {
		return err
	}
	if err := waitUntil(ctx, r.waits.swapOn, r.waits.poll, r.zramSwapping); err != nil {
		return fmt.Errorf("wait for a zram device in %s: %w", procSwaps, err)
	}
	return nil
}

func (r *modeRun) zramSwapping() (bool, error) {
	devices, err := r.root.readSwaps()
	return hasDevice(devices, apischema.SwapDeviceZram), err
}

// applySysctls writes the target mode's vm sysctls live, after the drop-in
// unless the zram mode already wrote it with its config. A sysctl the kernel
// does not have is skipped.
func (r *modeRun) applySysctls() error {
	if r.target != apischema.SwapModeZram {
		if err := r.writeDropIn(); err != nil {
			return err
		}
	}
	for _, setting := range liveSysctls(r.target, r.before) {
		name := filepath.Join(vmSysctlDir, setting.name)
		// A kernel without the sysctl (watermark_boost_factor came in 5.0)
		// has nothing to write; the drop-in keeps the key.
		if !r.root.exists(name) {
			continue
		}
		if err := r.setParam(name, strconv.Itoa(setting.value)); err != nil {
			return err
		}
	}
	return nil
}

// disableZswap turns zswap off when the kernel has it on and writes the
// tmpfiles file that keeps it off at boot: a kernel built with zswap on, or
// booted with zswap.enabled=1, would otherwise return to it, in front of zram
// too. The file is written whatever zswap reads now, since off now says
// nothing about the next boot. zswap already off is not written live: where
// zswap failed to start, that write fails for nothing. A kernel without
// zswap has nothing to turn off, and a file left from another kernel goes.
func (r *modeRun) disableZswap() error {
	if !r.before.Zswap.Available {
		return r.removeFile(zswapTmpfilesPath)
	}
	enabled, err := r.sys.readParam(zswapEnabledParam)
	if err != nil {
		return fmt.Errorf("read %s: %w", zswapEnabledParam, err)
	}
	if enabled != zswapSwitch(false) {
		if err = r.setParam(zswapEnabledParam, zswapSwitch(false)); err != nil {
			return err
		}
	}
	off := renderZswapOffTmpfiles()
	current, ok, err := r.root.readOptional(zswapTmpfilesPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", zswapTmpfilesPath, err)
	}
	if ok && current == string(off) {
		return nil
	}
	return r.writeFile(zswapTmpfilesPath, off)
}

// enableZswap asks for zstd, keeping the compressor the kernel has when it
// refuses zstd, turns zswap on, and persists what the kernel reads back.
func (r *modeRun) enableZswap() error {
	if err := r.setParam(zswapCompressorParam, zswapCompressor); err != nil && !compressorRefused(err) {
		return err
	}
	if err := r.setParam(zswapEnabledParam, zswapSwitch(true)); err != nil {
		return err
	}
	compressor, err := r.sys.readParam(zswapCompressorParam)
	if err != nil {
		return fmt.Errorf("read %s: %w", zswapCompressorParam, err)
	}
	return r.writeFile(zswapTmpfilesPath, renderZswapTmpfiles(compressor))
}

// compressorRefused reports whether the kernel refused a zswap compressor:
// ENOENT when it has no such compressor, EINVAL when it cannot build a pool
// with it. Either way it keeps the compressor it has.
func compressorRefused(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOENT)
}

// stopSwapFile stops the swap file's unit and masks it, so it stays off at
// boot, when zram swaps, the file's data fits in memory on a fresh status
// and systemd reports the unit running; otherwise it leaves the file
// active. A file proven off is only masked: one active swap file that
// cannot be resolved might be it, and then the unit is left.
func (r *modeRun) stopSwapFile(ctx context.Context, step swapStep) error {
	status, err := r.sys.status(ctx)
	if err != nil {
		return err
	}
	device, on := r.findSwap(step.path, status.Devices)
	if !on {
		if !r.swapFilesResolve(status.Devices) {
			r.swapFileLeft = true
			return nil
		}
		return r.sys.maskUnit(ctx, step.unit)
	}
	// Without zram swapping now, as when it went away after the run found
	// it, stopping the file would leave no swap at all. A unit systemd does
	// not report running would not stop the file either.
	if !hasDevice(status.Devices, apischema.SwapDeviceZram) || !fitsInMemory(device.Used, status.MemAvailable) ||
		r.unitNotRunning(ctx, step.unit) {
		r.swapFileLeft = true
		return nil
	}
	if err := r.sys.stopUnit(ctx, step.unit); err != nil {
		return err
	}
	if err := r.waitStopped(ctx, step.unit, step.path, r.swapIs(step.path, false)); err != nil {
		return fmt.Errorf("wait for %s to leave %s: %w", step.path, procSwaps, err)
	}
	return r.sys.maskUnit(ctx, step.unit)
}

// swapFilesResolve reports whether every active swap file resolves under
// the root, so that none can be the swap file under another name.
func (r *modeRun) swapFilesResolve(devices []apischema.SwapDevice) bool {
	for _, device := range devices {
		if device.Kind != apischema.SwapDeviceFile {
			continue
		}
		if _, err := r.root.resolve(device.Path); err != nil {
			return false
		}
	}
	return true
}

// startSwapFile unmasks the swap file's unit, which brings back the unit
// fstab generates, starts it and waits for the file in /proc/swaps.
func (r *modeRun) startSwapFile(ctx context.Context, step swapStep) error {
	if err := r.sys.unmaskUnit(ctx, step.unit); err != nil {
		return err
	}
	if err := r.sys.startUnit(ctx, step.unit); err != nil {
		return err
	}
	if err := waitUntil(ctx, r.waits.swapOn, r.waits.poll, r.swapIs(step.path, true)); err != nil {
		return fmt.Errorf("wait for %s in %s: %w", step.path, procSwaps, err)
	}
	return nil
}

// stopZram stops zram's unit and waits for zram to leave /proc/swaps, then
// turns zram off at boot, when zram's data fits in memory and free disk swap
// on a fresh status and systemd reports the provider's unit running;
// otherwise it leaves zram running. With run false zram is not running and
// is only turned off at boot.
//
// zram-tools is stopped through zramswap.service. zram-generator's device is
// stopped through its swap unit, and only when the generator made it (see
// generatorMadeZram).
func (r *modeRun) stopZram(ctx context.Context, run bool) error {
	unit := zramService
	if r.generated() {
		unit = zramGeneratorSwapUnit
	}
	if run {
		status, err := r.sys.status(ctx)
		if err != nil {
			return err
		}
		if hasDevice(status.Devices, apischema.SwapDeviceZram) {
			// zram set up by another tool, or under a service that is not
			// running, would not stop with the service: it stays, and the
			// service is left as it is.
			var ours bool
			if r.generated() {
				ours = r.generatorMadeZram(ctx, status.Devices)
			} else {
				ours = !r.unitNotRunning(ctx, zramService)
			}
			if !zramFits(status) || !ours {
				r.zramLeft = true
				return nil
			}
			if err := r.sys.stopUnit(ctx, unit); err != nil {
				return err
			}
			if err := r.waitStopped(ctx, unit, "zram", func() (bool, error) {
				on, err := r.zramSwapping()
				return !on, err
			}); err != nil {
				return fmt.Errorf("wait for zram to leave %s: %w", procSwaps, err)
			}
		}
	}
	return r.zramOffAtBoot(ctx)
}

// generatorMadeZram reports whether the zram that swaps is the one device
// zram-generator makes from LinuxIO's configuration: zram0 alone, with the
// generator's setup service for it active. A state that cannot be read is
// not proof, unlike in unitNotRunning: any swap device has a swap unit
// systemd would stop, so the setup service is the only sign of who made it.
func (r *modeRun) generatorMadeZram(ctx context.Context, devices []apischema.SwapDevice) bool {
	if slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool {
		return d.Kind == apischema.SwapDeviceZram && d.Path != zramGeneratorDevice
	}) {
		return false
	}
	state, err := r.sys.activeState(ctx, zramGeneratorSetupUnit)
	if err != nil {
		slog.Debug("read unit state failed", "unit", zramGeneratorSetupUnit, "error", err)
		return false
	}
	return state == "active"
}

// zramOffAtBoot keeps zram from starting at the next boot. zram-tools'
// service is disabled. zram-generator gets a configuration with no device,
// followed by a reload that takes its units away; a file that already says
// so is left alone. A rollback puts the previous file back, and zram, which
// is stopped by then, stays stopped until the next boot.
func (r *modeRun) zramOffAtBoot(ctx context.Context) error {
	if !r.generated() {
		return r.sys.disableUnit(ctx, zramService)
	}
	off := renderZramGeneratorOff()
	if current, ok, err := r.root.readOptional(zramGeneratorConfigPath); err != nil {
		return fmt.Errorf("read %s: %w", zramGeneratorConfigPath, err)
	} else if ok && current == string(off) {
		return nil
	}
	if err := r.writeFile(zramGeneratorConfigPath, off); err != nil {
		return err
	}
	return r.sys.reload(ctx)
}

// findSwap returns the disk swap device path is, matched by name or
// through symlinks under the root.
func (r *modeRun) findSwap(path string, devices []apischema.SwapDevice) (apischema.SwapDevice, bool) {
	for _, device := range devices {
		if device.Kind != apischema.SwapDeviceZram && r.root.sameSwap(path, device.Path) {
			return device, true
		}
	}
	return apischema.SwapDevice{}, false
}

// swapIs returns a check that path is, or is not, active in /proc/swaps.
func (r *modeRun) swapIs(path string, active bool) func() (bool, error) {
	return func() (bool, error) {
		devices, err := r.root.readSwaps()
		if err != nil {
			return false, err
		}
		_, on := r.findSwap(path, devices)
		return on == active, nil
	}
}

// writeFile writes a managed file, saving what was there first. The saved
// content is kept before the write: a write can land and still fail, as
// when the directory fsync after the rename fails, and putting the old
// content back is harmless when it did not land.
func (r *modeRun) writeFile(name string, data []byte) error {
	saved, err := r.root.saveFile(name)
	if err != nil {
		return err
	}
	r.keepFile(saved)
	if err := r.sys.writeFile(name, data, managedFileMode); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// removeFile removes a managed file, saving it first.
func (r *modeRun) removeFile(name string) error {
	saved, err := r.root.saveFile(name)
	if err != nil {
		return err
	}
	if saved.existed {
		r.keepFile(saved)
	}
	if err := r.sys.removeFile(name); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// keepFile records a file's content from before the run, once.
func (r *modeRun) keepFile(saved savedFile) {
	for _, f := range r.files {
		if f.name == saved.name {
			return
		}
	}
	r.files = append(r.files, saved)
}

// setParam writes a kernel parameter and checks that it reads back, saving
// the value it had before the run first, since a write can land and still
// report an error from its close.
func (r *modeRun) setParam(name, value string) error {
	previous, err := r.sys.readParam(name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if !savedParamFor(r.params, name) {
		r.params = append(r.params, savedParam{name: name, value: previous})
	}
	if err = r.sys.writeParam(name, value); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	got, err := r.sys.readParam(name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if got != value {
		return fmt.Errorf("%s reads %s after writing %s", name, got, value)
	}
	return nil
}

// rollback undoes the run after a failed step: zram at boot if this run set
// it, then the kernel values and the files, the last change first, and a
// reload when zram-generator's configuration was put back, so its units
// match the file again. It runs past a cancelled request, within a bound,
// and joins every action that fails.
func (r *modeRun) rollback(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	var errs []error
	if r.zramEnabledByRun {
		errs = append(errs, r.undoZramEnable(ctx))
	}
	for _, p := range slices.Backward(r.params) {
		if err := r.sys.writeParam(p.name, p.value); err != nil {
			errs = append(errs, fmt.Errorf("rollback: restore %s to %s: %w", p.name, p.value, err))
		}
	}
	for _, f := range slices.Backward(r.files) {
		if err := f.restore(r.sys); err != nil {
			errs = append(errs, fmt.Errorf("rollback: %w", err))
		}
	}
	if slices.ContainsFunc(r.files, func(f savedFile) bool { return f.name == zramGeneratorConfigPath }) {
		if err := r.sys.reload(ctx); err != nil {
			errs = append(errs, fmt.Errorf("rollback: %w", err))
		}
	}
	return errors.Join(errs...)
}

// undoZramEnable keeps the next boot from starting zram with the
// configuration the rollback puts back: zram-tools' service is disabled
// again, and zram-generator's units follow the file the rollback restores.
// It also stops zram's unit, but only when no zram device swaps: zram that
// swaps is swap capacity and stays. The generator's swap unit is stopped
// only while systemd reports it doing something, since a configuration that
// made no unit leaves nothing to stop.
func (r *modeRun) undoZramEnable(ctx context.Context) error {
	on, err := r.zramSwapping()
	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	unit := zramService
	if r.generated() {
		unit = zramGeneratorSwapUnit
	}
	if !on && !(r.generated() && r.unitNotRunning(ctx, unit)) {
		if err := r.sys.stopUnit(ctx, unit); err != nil {
			return fmt.Errorf("rollback: %w", err)
		}
	}
	if r.generated() {
		return nil
	}
	if err := r.sys.disableUnit(ctx, zramService); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}
