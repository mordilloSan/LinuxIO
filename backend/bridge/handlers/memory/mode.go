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
// enabled zramswap.service. Swap it added is never undone.
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

// writeConfig writes zram-tools' config for the planned size, then the
// sysctl drop-in.
func (r *modeRun) writeConfig() error {
	if err := r.writeFile(zramswapConfigPath, renderZramswapConfig(r.before.ZramPlannedSize)); err != nil {
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

// startZram enables zramswap.service and, when run, starts it and waits for
// a zram device in /proc/swaps.
func (r *modeRun) startZram(ctx context.Context, run bool) error {
	state, err := r.sys.unitFileState(ctx, zramService)
	if err != nil {
		return err
	}
	if err := r.sys.enableUnit(ctx, zramService); err != nil {
		return err
	}
	r.zramEnabledByRun = state != "enabled"
	if !run {
		return nil
	}
	if err := r.sys.startUnit(ctx, zramService); err != nil {
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

// disableZswap turns zswap off when the kernel has it on and removes the
// tmpfiles file that turns it on at boot. zswap already off is not written:
// where zswap failed to start, that write fails for nothing.
func (r *modeRun) disableZswap() error {
	if r.before.Zswap.Available {
		enabled, err := r.sys.readParam(zswapEnabledParam)
		if err != nil {
			return fmt.Errorf("read %s: %w", zswapEnabledParam, err)
		}
		if enabled != zswapSwitch(false) {
			if err := r.setParam(zswapEnabledParam, zswapSwitch(false)); err != nil {
				return err
			}
		}
	}
	return r.removeFile(zswapTmpfilesPath)
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

// stopZram stops zramswap.service and waits for zram to leave /proc/swaps,
// then disables it, when zram's data fits in memory and free disk swap on a
// fresh status and systemd reports the service running; otherwise it leaves
// zram running. With run false zram is not running and the service is only
// disabled.
func (r *modeRun) stopZram(ctx context.Context, run bool) error {
	if run {
		status, err := r.sys.status(ctx)
		if err != nil {
			return err
		}
		if hasDevice(status.Devices, apischema.SwapDeviceZram) {
			// zram set up by another tool, or under a service that is not
			// running, would not stop with the service: it stays, and the
			// service is left as it is.
			if !zramFits(status) || r.unitNotRunning(ctx, zramService) {
				r.zramLeft = true
				return nil
			}
			if err := r.sys.stopUnit(ctx, zramService); err != nil {
				return err
			}
			if err := r.waitStopped(ctx, zramService, "zram", func() (bool, error) {
				on, err := r.zramSwapping()
				return !on, err
			}); err != nil {
				return fmt.Errorf("wait for zram to leave %s: %w", procSwaps, err)
			}
		}
	}
	return r.sys.disableUnit(ctx, zramService)
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

// rollback undoes the run after a failed step: zramswap.service if this run
// enabled it, then the kernel values and the files, the
// last change first. It runs past a cancelled request, within a bound, and
// joins every action that fails.
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
	return errors.Join(errs...)
}

// undoZramEnable disables zramswap.service again, so the next boot does not
// start zram with the configuration the rollback put back. It also stops the
// service, but only when no zram device swaps: zram that swaps is swap
// capacity and stays.
func (r *modeRun) undoZramEnable(ctx context.Context) error {
	on, err := r.zramSwapping()
	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	if !on {
		if err := r.sys.stopUnit(ctx, zramService); err != nil {
			return fmt.Errorf("rollback: %w", err)
		}
	}
	if err := r.sys.disableUnit(ctx, zramService); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}
