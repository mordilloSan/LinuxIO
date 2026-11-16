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
// the change and what the change left as it was, with the reason: a swap
// device that stayed, which makes the mode mixed, or zram at its old size.
func SetSwapMode(ctx context.Context, mode apischema.SwapMode, report func(apischema.SwapProgress)) (apischema.SwapModeResult, error) {
	return newExecutor(report).setSwapMode(ctx, mode)
}

func (e *executor) setSwapMode(ctx context.Context, target apischema.SwapMode) (apischema.SwapModeResult, error) {
	if _, ok := presetFor(target); !ok {
		return apischema.SwapModeResult{}, fmt.Errorf("%w: unknown swap mode %q", bridgeipc.ErrInvalidArgs, target)
	}
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapModeResult{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapModeResult{}, err
	}
	steps, err := planSwapMode(view, target)
	if err != nil {
		return apischema.SwapModeResult{}, err
	}
	run := &modeRun{executor: e, target: target, before: view.status}
	for i, step := range steps {
		e.report(apischema.SwapProgress{Step: step.code, Index: i + 1, Total: len(steps)})
		if stepErr := run.do(ctx, step); stepErr != nil {
			return apischema.SwapModeResult{}, errors.Join(fmt.Errorf("step %s: %w", step.code, stepErr), run.rollback(ctx), run.settleAfterRollback(ctx))
		}
	}
	status, err := e.sys.status(ctx)
	if err != nil {
		return apischema.SwapModeResult{}, err
	}
	// The live sysctls are the target's. A host that ended in another mode,
	// as when zram went away and only the swap file swaps, gets that mode's
	// instead.
	settled, err := run.settleSysctls(status, target)
	if err != nil {
		slog.Warn("settle memory sysctls failed", "mode", status.Mode, "error", err)
	}
	if settled {
		if status, err = e.sys.status(ctx); err != nil {
			return apischema.SwapModeResult{}, err
		}
	}
	run.explainMixed(status)
	slog.Info("swap mode changed", "mode", target, "previous_mode", view.status.Mode,
		"swap_file_left_active", run.leftHas(apischema.SwapLeftSwapFile), "zram_left_active", run.leftHas(apischema.SwapLeftZram),
		"zram_size_kept", run.leftHas(apischema.SwapLeftZramSize))
	// An empty list and not null on the wire.
	return apischema.SwapModeResult{SwapStatus: status, Left: append([]apischema.SwapLeft{}, run.left...)}, nil
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
	// left is what the run left as it was, with the reason: a device left
	// active, which makes the result mixed, or zram left at its size.
	left []apischema.SwapLeft
}

// leave records something the run left as it was.
func (r *modeRun) leave(what apischema.SwapLeftWhat, reason apischema.SwapLeftReason) {
	r.left = append(r.left, apischema.SwapLeft{What: what, Reason: reason})
}

func (r *modeRun) leftHas(what apischema.SwapLeftWhat) bool {
	return slices.ContainsFunc(r.left, func(l apischema.SwapLeft) bool { return l.What == what })
}

// explainMixed gives a reason for a device that swaps on though no step
// left it: no step stops a swap file other than the first one fstab names,
// and none stops zram without a zram provider. A mixed result always says
// why.
func (r *modeRun) explainMixed(status apischema.SwapStatus) {
	if status.Mode != apischema.SwapModeMixed {
		return
	}
	if r.target == apischema.SwapModeZram && !r.leftHas(apischema.SwapLeftSwapFile) {
		r.leave(apischema.SwapLeftSwapFile, apischema.SwapLeftNotManaged)
	}
	if r.target != apischema.SwapModeZram && !r.leftHas(apischema.SwapLeftZram) {
		r.leave(apischema.SwapLeftZram, apischema.SwapLeftNotManaged)
	}
}

func (r *modeRun) do(ctx context.Context, step swapStep) error {
	switch step.code {
	case apischema.SwapStepWriteConfig:
		return r.writeConfig()
	case apischema.SwapStepStartZram:
		return r.startZram(ctx)
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

// startZram sets zram to start at boot and makes it swap at the planned
// size. zram-tools has a service to enable. zram-generator has none: a
// systemd reload makes its swap unit from the configuration write_config
// wrote, wanted by swap.target.
//
// zram that does not swap is started. zram-tools' service can read active
// with nothing swapping, as after a swapoff by hand, and a start would do
// nothing, so that one is restarted. zram that swaps at the planned size is
// left alone. zram that swaps at another size, as the one zram-tools starts
// when it is installed, is stopped and started again, which empties it, and
// only when restartZramRefusal finds nothing against it; otherwise it keeps
// its size until its unit next restarts, and the result says so.
func (r *modeRun) startZram(ctx context.Context) error {
	unit, err := r.zramOnAtBoot(ctx)
	if err != nil {
		return err
	}
	status, err := r.sys.status(ctx)
	if err != nil {
		return err
	}
	start := r.sys.startUnit
	stopped := false
	switch {
	case hasDevice(status.Devices, apischema.SwapDeviceZram):
		if stopped, err = r.stopZramToResize(ctx, unit, status); err != nil || !stopped {
			return err
		}
		// zram is off: bringing it back is not cancelled with the request.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout+r.waits.swapOn)
		defer cancel()
	case !r.generated() && !r.unitNotRunning(ctx, zramService):
		start = r.sys.restartUnit
	}
	if err = start(ctx, unit); err != nil {
		return err
	}
	if err = waitUntil(ctx, r.waits.swapOn, r.waits.poll, r.zramSwapping); err != nil {
		return fmt.Errorf("wait for a zram device in %s: %w", procSwaps, err)
	}
	if !stopped {
		return nil
	}
	// A provider that brings zram back at another size, as one a drop-in
	// overrides, would be stopped again at every later change: say so.
	if status, err = r.sys.status(ctx); err != nil {
		return err
	}
	if !zramSized(status, r.before.ZramPlannedSize) {
		r.leave(apischema.SwapLeftZramSize, apischema.SwapLeftOtherSize)
	}
	return nil
}

// zramOnAtBoot sets zram to start at boot and returns the unit that starts
// and stops it now.
func (r *modeRun) zramOnAtBoot(ctx context.Context) (string, error) {
	if r.generated() {
		if err := r.sys.reload(ctx); err != nil {
			return "", err
		}
		r.zramEnabledByRun = true
		return zramGeneratorSwapUnit, nil
	}
	state, err := r.sys.unitFileState(ctx, zramService)
	if err != nil {
		return "", err
	}
	if err := r.sys.enableUnit(ctx, zramService); err != nil {
		return "", err
	}
	r.zramEnabledByRun = state != "enabled"
	return zramService, nil
}

// stopZramToResize stops zram that swaps at another size than planned and
// waits for it to leave, so that a start brings it back at the planned
// size. It reports false, with zram left running, when zram has the planned
// size already or cannot be stopped; the second case is recorded. A stop
// that fails or is not waited out may still take effect, so a start is
// queued behind it, past a cancelled request, as a drain does.
func (r *modeRun) stopZramToResize(ctx context.Context, unit string, status apischema.SwapStatus) (bool, error) {
	if zramSized(status, r.before.ZramPlannedSize) {
		return false, nil
	}
	if reason := r.restartZramRefusal(ctx, status); reason != "" {
		r.leave(apischema.SwapLeftZramSize, reason)
		return false, nil
	}
	if err := r.stopZramUnit(ctx, unit); err != nil {
		startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		if startErr := r.sys.startUnit(startCtx, unit); startErr != nil {
			return false, errors.Join(err, fmt.Errorf("zram may be off: start %s: %w", unit, startErr))
		}
		return false, fmt.Errorf("%w; the stop may still take effect, so a start was queued", err)
	}
	return true, nil
}

// stopZramUnit stops the unit that runs zram and waits for zram to leave
// /proc/swaps.
func (r *modeRun) stopZramUnit(ctx context.Context, unit string) error {
	if err := r.sys.stopUnit(ctx, unit); err != nil {
		return err
	}
	if err := r.waitStopped(ctx, unit, "zram", func() (bool, error) {
		on, err := r.zramSwapping()
		return !on, err
	}); err != nil {
		return fmt.Errorf("wait for zram to leave %s: %w", procSwaps, err)
	}
	return nil
}

// zramSized reports whether every zram device that swaps has the planned
// size, in whole MiB as the provider's configuration gives it. A device
// whose size cannot be read counts as sized: emptying zram needs a reason.
func zramSized(status apischema.SwapStatus, planned uint64) bool {
	for _, device := range status.Devices {
		if device.Kind != apischema.SwapDeviceZram {
			continue
		}
		i := slices.IndexFunc(status.ZramDevices, func(z apischema.ZramDevice) bool { return "/dev/"+z.Name == device.Path })
		if i >= 0 && status.ZramDevices[i].Disksize>>20 != planned>>20 {
			return false
		}
	}
	return true
}

// restartZramRefusal says why zram that swaps cannot be stopped and started
// again at the planned size, or is empty when it can: it must be the one
// device the provider's configuration makes, run by the provider, or
// stopping the provider's unit would not stop it; disk swap must be active,
// so the host has swap while zram is away and after a start that fails; and
// zram's data must fit in memory and free disk swap.
func (r *modeRun) restartZramRefusal(ctx context.Context, status apischema.SwapStatus) apischema.SwapLeftReason {
	zramDevices := 0
	for _, device := range status.Devices {
		if device.Kind == apischema.SwapDeviceZram {
			zramDevices++
		}
	}
	switch {
	case zramDevices != 1 || !r.providerRunsZram(ctx, status.Devices):
		return apischema.SwapLeftNotManaged
	case !hasDevice(status.Devices, apischema.SwapDeviceFile) && !hasDevice(status.Devices, apischema.SwapDevicePartition):
		return apischema.SwapLeftNoOtherSwap
	case !zramFits(status):
		return apischema.SwapLeftNotEnoughMemory
	default:
		return ""
	}
}

// providerRunsZram reports whether the zram that swaps would stop with the
// provider's unit. zram set up by another tool, or under a service that is
// not running, would not: zram-tools' service must be running, and
// zram-generator must have made the device (see generatorMadeZram).
func (r *modeRun) providerRunsZram(ctx context.Context, devices []apischema.SwapDevice) bool {
	if r.generated() {
		return r.generatorMadeZram(ctx, devices)
	}
	return !r.unitNotRunning(ctx, zramService)
}

func (r *modeRun) zramSwapping() (bool, error) {
	devices, err := r.root.readSwaps()
	return hasDevice(devices, apischema.SwapDeviceZram), err
}

// applySysctls writes the target mode's vm sysctls live, after the drop-in
// unless the zram mode already wrote it with its config.
func (r *modeRun) applySysctls() error {
	if r.target != apischema.SwapModeZram {
		if err := r.writeDropIn(); err != nil {
			return err
		}
	}
	return r.setLiveSysctls(r.target, r.before)
}

// setLiveSysctls writes what mode sets in /proc/sys/vm on a host whose
// values are status's. A sysctl the kernel does not have is skipped.
func (r *modeRun) setLiveSysctls(mode apischema.SwapMode, status apischema.SwapStatus) error {
	for _, setting := range liveSysctls(mode, status) {
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

// stopSwapFile takes the swap file away with swapoff and masks its unit, so
// it stays off at boot, when zram swaps and the file's data fits in memory
// on a fresh status; otherwise it leaves the file active. A file proven off
// is only masked: one active swap file that cannot be resolved might be it,
// and then the unit is left.
func (r *modeRun) stopSwapFile(ctx context.Context, step swapStep) error {
	status, err := r.sys.status(ctx)
	if err != nil {
		return err
	}
	device, on := r.findSwap(step.path, status.Devices)
	if !on {
		if !r.swapFilesResolve(status.Devices) {
			r.leave(apischema.SwapLeftSwapFile, apischema.SwapLeftNotIdentified)
			return nil
		}
		return r.sys.maskUnit(ctx, step.unit)
	}
	// Without zram swapping now, as when it went away after the run found
	// it, stopping the file would leave no swap at all.
	var reason apischema.SwapLeftReason
	switch {
	case !hasDevice(status.Devices, apischema.SwapDeviceZram):
		reason = apischema.SwapLeftZramNotSwapping
	case !fitsInMemory(device.Used, status.MemAvailable):
		reason = apischema.SwapLeftNotEnoughMemory
	}
	if reason != "" {
		r.leave(apischema.SwapLeftSwapFile, reason)
		return nil
	}
	if err := r.swapOffDevice(ctx, device.Path); err != nil {
		return err
	}
	// The file is off: masking its unit, which keeps it off at boot, is not
	// cancelled with the request.
	maskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	return r.sys.maskUnit(maskCtx, step.unit)
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
	// A file taken away with swapoff a moment ago may still read active to
	// systemd, which would not start it.
	r.waitUnitSeenOff(ctx, step.unit)
	if err := r.startUntilBack(ctx, step.unit, r.swapIs(step.path, true)); err != nil {
		return fmt.Errorf("%s: %w", step.path, err)
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
			// zram the provider does not run would not stop with its unit:
			// it stays, and the unit is left as it is.
			if !r.providerRunsZram(ctx, status.Devices) {
				r.leave(apischema.SwapLeftZram, apischema.SwapLeftNotManaged)
				return nil
			}
			if !zramFits(status) {
				r.leave(apischema.SwapLeftZram, apischema.SwapLeftNotEnoughMemory)
				return nil
			}
			if err := r.stopZramUnit(ctx, unit); err != nil {
				return err
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

// settleAfterRollback is settleSysctls for a run that failed: the rollback
// put back the live sysctls of the mode the host was in, and swap the run
// added or took away is not undone, so the host can be in another mode by
// now, as disk swap only after a switch away from zram failed past stopping
// zram. Like the rollback it runs past a cancelled request, within a bound.
func (r *modeRun) settleAfterRollback(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	status, err := r.sys.status(ctx)
	if err != nil {
		return fmt.Errorf("after rollback: read the status: %w", err)
	}
	if _, err := r.settleSysctls(status, r.before.Mode); err != nil {
		return fmt.Errorf("after rollback: %w", err)
	}
	return nil
}

// settleSysctls writes the live sysctls of the mode the host is in when
// that mode is disk, zswap or zram and the sysctls in force are those of
// another mode, inForce. zram's swappiness and readahead over disk-only
// swap, or disk's over zram, is what it prevents. The drop-in is left as it
// is: it goes with the zram and swap units the run set for the next boot,
// which the mode the host is in now says nothing about. It reports whether
// it wrote anything.
func (r *modeRun) settleSysctls(status apischema.SwapStatus, inForce apischema.SwapMode) (bool, error) {
	if _, ok := presetFor(status.Mode); !ok || status.Mode == inForce {
		return false, nil
	}
	return true, r.setLiveSysctls(status.Mode, status)
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
