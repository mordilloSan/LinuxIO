package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// zswapCompressor is the compressor the zswap mode asks the kernel for.
const zswapCompressor = "zstd"

// SetSwapMode switches the host to the requested mode, disk, zswap or zram,
// end to end, reporting each step as it starts. The result is the host's
// status after the change and what the change left as it was, with the
// reason: a swap device that stayed, which makes the mode mixed, or zram at
// its old size.
func SetSwapMode(ctx context.Context, req apischema.SwapModeRequest, report func(apischema.SwapProgress)) (apischema.SwapModeResult, error) {
	return newExecutor(report).setSwapMode(ctx, req)
}

func (e *executor) setSwapMode(ctx context.Context, req apischema.SwapModeRequest) (apischema.SwapModeResult, error) {
	target := req.Mode
	if err := validateSwapModeRequest(req); err != nil {
		return apischema.SwapModeResult{}, err
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
	steps, err := planSwapMode(view, req)
	if err != nil {
		return apischema.SwapModeResult{}, err
	}
	// Which sysctls are LinuxIO's is read before a step rewrites the drop-in.
	dropIn, err := e.root.readSysctlDropIn()
	if err != nil {
		return apischema.SwapModeResult{}, err
	}
	held := heldSysctls(dropIn)
	// On a host that runs zram, zram's numbers are zram's, also where the
	// drop-in does not hold them, as after a settle wrote them live.
	if hasDevice(view.status.Devices, apischema.SwapDeviceZram) {
		zram, _ := presetFor(apischema.SwapModeZram)
		for _, setting := range zram.sysctls {
			held[setting.name] = true
		}
	}
	run := &modeRun{executor: e, target: target, before: view.status, dropInHeld: held}
	for i, step := range steps {
		e.report(apischema.SwapProgress{Step: step.code, Index: i + 1, Total: len(steps)})
		if stepErr := run.do(ctx, step); stepErr != nil {
			return apischema.SwapModeResult{}, errors.Join(fmt.Errorf("step %s: %w%s", step.code, stepErr, run.copyKept()), run.rollback(ctx), run.settleAfterRollback(ctx))
		}
	}
	status, err := e.statusAfterChange(ctx)
	if err != nil {
		// The host changed: what stayed cannot be told without a status.
		slog.Info("swap mode changed", append([]any{"mode", target, "previous_mode", view.status.Mode}, run.replacedConfigAttrs()...)...)
		return apischema.SwapModeResult{}, err
	}
	// The live sysctls are the target's. A host that ended in another mode,
	// as when zram went away and only the swap file swaps, gets that mode's
	// instead. Settling writes sysctls only, so what stayed is told from the
	// status read before it.
	settled, err := run.settleSysctls(status, target)
	if err != nil {
		slog.Warn("settle memory sysctls failed", "mode", status.Mode, "error", err)
	}
	run.explainMixed(status)
	// What stays active is read from the host, not from what the steps left:
	// a device no step reaches, such as a swap file LinuxIO does not manage,
	// stays too, also on a host that did not end mixed.
	slog.Info("swap mode changed", append([]any{"mode", target, "previous_mode", view.status.Mode,
		"swap_file_left_active", target == apischema.SwapModeZram && hasDevice(status.Devices, apischema.SwapDeviceFile),
		"zram_left_active", target != apischema.SwapModeZram && hasDevice(status.Devices, apischema.SwapDeviceZram),
		"zram_size_kept", run.leftHas(apischema.SwapLeftZramSize)}, run.replacedConfigAttrs()...)...)
	if settled {
		if status, err = e.statusAfterChange(ctx); err != nil {
			return apischema.SwapModeResult{}, err
		}
		// A device can appear in between; explainMixed adds a reason once
		// per kind of device.
		run.explainMixed(status)
	}
	// An empty list and not null on the wire.
	return apischema.SwapModeResult{SwapStatus: status, Left: append([]apischema.SwapLeft{}, run.left...)}, nil
}

// validateSwapModeRequest refuses a mode the dialog does not offer, and a
// confirmation that names anything but one of the two zram configuration
// files, before the change waits for the lock.
func validateSwapModeRequest(req apischema.SwapModeRequest) error {
	if _, ok := presetFor(req.Mode); !ok {
		return fmt.Errorf("%w: unknown swap mode %q", bridgeipc.ErrInvalidArgs, req.Mode)
	}
	if c := req.ConfirmReplace; c != "" && c != zramswapConfigPath && c != zramGeneratorConfigPath {
		return fmt.Errorf("%w: confirm_replace %q is not a zram configuration file", bridgeipc.ErrInvalidArgs, c)
	}
	return nil
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
	// dropInHeld is the vm sysctls, by name, whose values are LinuxIO's,
	// which leaving zram may set back to the kernel's defaults: those its
	// drop-in held before the run or that the run wrote into it, and every
	// one zram's preset sets when zram swapped as the run started.
	dropInHeld map[string]bool
	// left is what the run left as it was, with the reason: a device left
	// active, which makes the result mixed, or zram left at its size.
	left []apischema.SwapLeft
	// replacedConfig is the zram configuration file that was not LinuxIO's
	// the run replaced, and configCopy the copy it kept of it.
	replacedConfig, configCopy string
}

// copyKept says, for the error of a run that failed, where the copy of a
// zram configuration file it replaced stays: the rollback puts the file back
// and leaves the copy.
func (r *modeRun) copyKept() string {
	if r.configCopy == "" {
		return ""
	}
	return fmt.Sprintf(" (a copy of %s was kept as %s)", r.replacedConfig, r.configCopy)
}

// replacedConfigAttrs are the log attributes naming the configuration file
// the run replaced and the copy it kept, none when it replaced no such file.
func (r *modeRun) replacedConfigAttrs() []any {
	if r.replacedConfig == "" {
		return nil
	}
	return []any{"zram_config_replaced", r.replacedConfig, "zram_config_copy", r.configCopy}
}

// leave records something the run left as it was.
func (r *modeRun) leave(what apischema.SwapLeftWhat, reason apischema.SwapLeftReason) {
	r.left = append(r.left, apischema.SwapLeft{What: what, Reason: reason})
}

func (r *modeRun) leftHas(what apischema.SwapLeftWhat) bool {
	return slices.ContainsFunc(r.left, func(l apischema.SwapLeft) bool { return l.What == what })
}

// explainMixed gives a reason for a device that swaps on though no step
// left it: no step stops a swap file other than the one LinuxIO manages,
// and none stops zram without a zram provider. A mixed result always says
// why.
func (r *modeRun) explainMixed(status apischema.SwapStatus) {
	if status.Mode != apischema.SwapModeMixed {
		return
	}
	if r.target == apischema.SwapModeZram && !r.leftHas(apischema.SwapLeftSwapFile) {
		reason := apischema.SwapLeftNotManaged
		// The reported file is the one that stays only when it was swapping.
		if file := r.before.SwapFile; file != nil && file.Active && file.UnmanagedReason == apischema.SwapFileOthersCanWrite {
			reason = apischema.SwapLeftOthersCanWrite
		}
		r.leave(apischema.SwapLeftSwapFile, reason)
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
	for _, setting := range set {
		r.dropInHeld[setting.name] = true
	}
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
	status, _, err := r.sys.status(ctx)
	if err != nil {
		return err
	}
	start := r.sys.startUnit
	switch {
	case hasDevice(status.Devices, apischema.SwapDeviceZram):
		stopped, stopErr := r.stopZramToResize(ctx, unit, status)
		if stopErr != nil || !stopped {
			return stopErr
		}
		// zram is off: bringing it back is not cancelled with the request.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout+zramResetWait+r.waits.swapOn)
		defer cancel()
	case !r.generated() && !r.unitNotRunning(ctx, zramService):
		start = r.sys.restartUnit
	}
	if err = r.resetStaleZram(ctx); err != nil {
		return err
	}
	// A hold a killed zram-tools install left would skip the start.
	if !r.generated() {
		if err = r.clearLeftoverZramToolsHold(ctx); err != nil {
			return err
		}
	}
	if err = start(ctx, unit); err != nil {
		return err
	}
	if err = waitUntil(ctx, r.waits.swapOn, r.waits.poll, r.zramSwapping); err != nil {
		return fmt.Errorf("wait for a zram device in %s: %w", procSwaps, err)
	}
	// A provider that brings zram up at another size, as one a drop-in
	// overrides or a device that could not be reset, would be stopped again
	// at every later change: say so. zram swaps by now, so a status that
	// cannot be read does not fail the step.
	status, _, err = r.sys.status(ctx)
	if err != nil {
		slog.Debug("read the status after starting zram failed", "error", err)
		return nil
	}
	if !zramSized(status, r.before.ZramPlannedSize) {
		r.leave(apischema.SwapLeftZramSize, apischema.SwapLeftOtherSize)
	}
	return nil
}

// resetStaleZram frees the provider's zram0 when it is set up but does not
// swap, so that a start sets it up again at the planned size. Either tool
// can leave it that way: zramswap stop often cannot unload the zram module
// right after its own swapoff, and the reset zram-generator's setup service
// runs when its swap unit stops can meet the device still held, as udev
// holds it for a moment after a swapoff, and fail with EBUSY.
//
// zramswap start swaps on a device that is set up at the size it has, so
// for zram-tools a reset that fails is logged, and the size check after the
// start reports it. zram-generator's setup cannot set such a device up at
// all, as it fails to set the compression algorithm, so for it a reset that
// fails fails the step, naming the device.
//
// Only a device that is not in /proc/swaps and holds swap is reset, read
// again before each attempt; the kernel refuses to reset one that something
// holds open, and that is tried again for a while (resetZram). One that holds anything else
// was set up by someone for another use, and the step refuses instead.
func (r *modeRun) resetStaleZram(ctx context.Context) error {
	size, err := r.root.readOptionalUint(filepath.Join(sysBlockDir, zramDeviceName, "disksize"))
	if err != nil || size == 0 {
		return nil
	}
	switch err := r.resetZram(ctx); {
	case err == nil, errors.Is(err, errZramSwaps):
		return nil
	case errors.Is(err, bridgeipc.ErrConflict):
		return err
	case r.generated():
		return fmt.Errorf("/dev/%s is still set up, and zram-generator cannot set it up again until it is reset: %w", zramDeviceName, err)
	default:
		slog.Warn("reset zram device failed", "device", zramDeviceName, "error", err)
		return nil
	}
}

// errZramSwaps is zram0 swapping by the time it was to be reset: it is not
// stale, and nothing is reset.
var errZramSwaps = errors.New("zram0 swaps")

// resetZram resets zram0, trying again within the zramReset bound while the
// kernel refuses with EBUSY because something still holds the device. Before
// each attempt it reads again what the device holds, since another tool can
// start swapping on it or format it meanwhile: a device that swaps is left
// (errZramSwaps), and one that holds anything but swap is refused.
func (e *executor) resetZram(ctx context.Context) error {
	device := "/dev/" + zramDeviceName
	var busy error
	err := waitUntil(ctx, e.waits.zramReset, e.waits.poll, func() (bool, error) {
		if err := e.zramResettable(device); err != nil {
			return false, err
		}
		err := e.sys.resetZram(ctx, zramDeviceName)
		if errors.Is(err, syscall.EBUSY) {
			busy = err
			return false, nil
		}
		return err == nil, err
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errZramSwaps), errors.Is(err, bridgeipc.ErrConflict):
		return err
	case busy != nil:
		return fmt.Errorf("reset %s: %w: %w", zramDeviceName, err, busy)
	default:
		return fmt.Errorf("reset %s: %w", zramDeviceName, err)
	}
}

// zramResettable reports whether device may be reset now: it is not in
// /proc/swaps and holds a swap area, as mkswap writes it.
func (e *executor) zramResettable(device string) error {
	devices, err := e.root.readSwaps()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Path == device }) {
		return errZramSwaps
	}
	swap, err := e.root.holdsSwap(device)
	if err != nil {
		return fmt.Errorf("read %s: %w", device, err)
	}
	if !swap {
		return fmt.Errorf("%w: %s is set up and does not hold swap, and starting zram would format it", bridgeipc.ErrConflict, device)
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
// size already or cannot be stopped; the second case is recorded.
func (r *modeRun) stopZramToResize(ctx context.Context, unit string, status apischema.SwapStatus) (bool, error) {
	if zramSized(status, r.before.ZramPlannedSize) {
		return false, nil
	}
	if reason := r.restartZramRefusal(ctx, status); reason != "" {
		r.leave(apischema.SwapLeftZramSize, reason)
		return false, nil
	}
	if err := r.stopZramUnit(ctx, unit); err != nil {
		return false, err
	}
	return true, nil
}

// stopZramUnit stops the unit that runs zram and waits for zram to leave
// /proc/swaps. A stop that fails or is not waited out (an error from the
// stop, a cancel, the wait's bound, a stop systemd never carried out) is a
// systemd job that may still take zram away after the route gave up, with
// zram still set to start at boot, so a start of the unit is queued behind
// it, past a cancelled request, and the host keeps its zram. A stop systemd
// reports over with zram still swapping (errStopAbandoned, errStopEnded)
// queues nothing: zram stays.
func (r *modeRun) stopZramUnit(ctx context.Context, unit string) error {
	err := r.sys.stopUnit(ctx, unit)
	if err == nil {
		err = r.waitStopped(ctx, unit, "zram", func() (bool, error) {
			on, readErr := r.zramSwapping()
			return !on, readErr
		})
		if err == nil {
			return nil
		}
		err = fmt.Errorf("wait for zram to leave %s: %w", procSwaps, err)
		// systemd reports the stop over with zram still swapping: nothing can
		// take zram away later, and a start would run the provider's start
		// against a device that is set up and swapping.
		if errors.Is(err, errStopAbandoned) || errors.Is(err, errStopEnded) {
			return err
		}
	}
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	// zram-generator's own reset at the stop may have met the device still
	// held, and its setup would then fail the start: once zram no longer
	// swaps, the device is reset first. While zram still swaps the start is
	// queued behind the stop as it is.
	if r.generated() {
		if resetErr := r.resetStaleZram(startCtx); resetErr != nil {
			return errors.Join(err, fmt.Errorf("zram may be off: %w", resetErr))
		}
	}
	if startErr := r.sys.startUnit(startCtx, unit); startErr != nil {
		return errors.Join(err, fmt.Errorf("zram may be off: start %s: %w", unit, startErr))
	}
	return fmt.Errorf("%w; the stop may still take effect, so a start was queued", err)
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
	for _, setting := range liveSysctls(mode, status, r.dropInHeld) {
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

// stopSwapFile takes the swap file away with swapoff and holds its unit off,
// so it stays off at boot, when zram swaps and the file's data fits in
// memory on a fresh status; otherwise it leaves the file active. A file
// proven off is only held off: one active swap file that cannot be resolved
// might be it, and then the unit is left.
func (r *modeRun) stopSwapFile(ctx context.Context, step swapStep) error {
	status, _, err := r.sys.status(ctx)
	if err != nil {
		return err
	}
	device, on := r.findSwap(step.path, status.Devices)
	if !on {
		if !r.swapFilesResolve(status.Devices) {
			r.leave(apischema.SwapLeftSwapFile, apischema.SwapLeftNotIdentified)
			return nil
		}
		return r.holdOff(ctx, step.unit)
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
		// Refused before swapoff ran, so nothing changed.
		if errors.Is(err, errOthersCanRedirect) {
			r.leave(apischema.SwapLeftSwapFile, apischema.SwapLeftOthersCanWrite)
			return nil
		}
		return err
	}
	// The file is off: holding its unit off, which keeps it off at boot, is
	// not cancelled with the request.
	holdCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	return r.holdOff(holdCtx, step.unit)
}

// holdOff keeps unit, the swap file's, off at boot: LinuxIO's drop-in for it
// gives it a condition that never holds, and a reload makes systemd read it.
// The unit is not masked, since swap.target requires it and systemd starts
// no target that requires a masked unit; a skipped one does not hold the
// target back, so zram that swap.target wants still starts. A mask an older
// LinuxIO or an admin left is removed for the same reason: the drop-in holds
// the file off in its place.
func (r *modeRun) holdOff(ctx context.Context, unit string) error {
	if err := r.root.makeHoldOffDropInDir(unit); err != nil {
		return err
	}
	if err := r.writeFile(holdOffDropInPath(unit), renderHoldOffDropIn()); err != nil {
		return err
	}
	if err := r.sys.reload(ctx); err != nil {
		return err
	}
	return r.unmaskIfMasked(ctx, unit)
}

// letGo undoes what holds unit off at boot, so a start of it starts the
// file: LinuxIO's drop-in goes, systemd is reloaded in every case, since a
// unit fstab just gained has no unit until a reload and systemd keeps a
// condition whose file is gone until one, and a mask, as an admin or a
// LinuxIO that held swap files off with masks left it, is removed. The
// drop-in is not kept for a rollback: letting the file go is adding swap,
// which a rollback never undoes, and putting it back after zram was turned
// off at boot would leave the next boot with no swap.
func (r *modeRun) letGo(ctx context.Context, unit string) error {
	if dropIn := holdOffDropInPath(unit); r.root.exists(dropIn) {
		if err := r.sys.removeFile(dropIn); err != nil {
			return fmt.Errorf("remove %s: %w", dropIn, err)
		}
	}
	if err := r.sys.reload(ctx); err != nil {
		return err
	}
	return r.unmaskIfMasked(ctx, unit)
}

// unmaskIfMasked unmasks unit when systemd reads it masked; the unmask
// reloads systemd.
func (r *modeRun) unmaskIfMasked(ctx context.Context, unit string) error {
	state, err := r.sys.unitFileState(ctx, unit)
	if err != nil {
		return fmt.Errorf("read the file state of %s: %w", unit, err)
	}
	if strings.HasPrefix(state, "masked") {
		return r.sys.unmaskUnit(ctx, unit)
	}
	return nil
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

// startSwapFile lets the swap file's unit go (letGo), starts it and waits
// for the file in /proc/swaps. A file that already swaps is only let go.
func (r *modeRun) startSwapFile(ctx context.Context, step swapStep) error {
	if err := r.letGo(ctx, step.unit); err != nil {
		return err
	}
	if on, err := r.swapIs(step.path, true)(); err != nil || on {
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
		status, _, err := r.sys.status(ctx)
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
	return zramSetupActive(ctx, r.sys.activeState)
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
	if err := r.keepZramConfigCopy(saved); err != nil {
		return err
	}
	r.keepFile(saved)
	if err := r.sys.writeFile(name, data, managedFileMode); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// keepZramConfigCopy copies a zram configuration file that is not LinuxIO's
// beside it, with its mode, before the run replaces it; an older copy is
// replaced. The plan refused the run unless the request named the file. A
// rollback puts the file itself back and leaves the copy.
func (r *modeRun) keepZramConfigCopy(saved savedFile) error {
	if !saved.existed || (saved.name != zramswapConfigPath && saved.name != zramGeneratorConfigPath) || !r.root.asksToReplace(saved.name, saved.data) {
		return nil
	}
	copyPath := saved.name + zramConfigCopySuffix
	if err := r.sys.writeFile(copyPath, saved.data, saved.mode); err != nil {
		return fmt.Errorf("keep a copy of %s as %s: %w", saved.name, copyPath, err)
	}
	r.replacedConfig, r.configCopy = saved.name, copyPath
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
// reload when zram-generator's configuration or a hold-off drop-in was put
// back, so the units match the files again. It runs past a cancelled request, within a bound,
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
	if slices.ContainsFunc(r.files, func(f savedFile) bool { return f.name == zramGeneratorConfigPath || isHoldOffDropIn(f.name) }) {
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
	status, _, err := r.sys.status(ctx)
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
