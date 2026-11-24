// Package smart collects SMART health data through smartctl, plus eMMC and md
// RAID health read from sysfs.
package smart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

// Manager collects SMART data. Every refresh builds the list of targets
// (smartctl scan, SMART_DEVICES, eMMC and md arrays), queries each one and
// replaces the collected records, so a drive that is removed or stops
// answering leaves the list and no per-device state has to be kept in sync.
type Manager struct {
	sync.Mutex                                        // guards data and the refresh schedule below
	data             map[string]*smartmodel.SmartData // collected records keyed by serial (or sysfs key)
	refreshInterval  time.Duration                    // interval between automatic refreshes
	lastRefresh      time.Time                        // last refresh attempt
	lastRefreshError string                           // last refresh error, so callers log only changes

	refreshMutex    sync.Mutex             // serializes refreshes; guards the fields below
	last            map[string]smartResult // last success per target key
	woken           map[string]struct{}    // target keys already queried once without -n standby; never cleared
	failing         map[string]string      // target key → error text last logged for it
	scanned         []smartDevice          // cached smartctl --scan result
	scanErr         error                  // error of the cached scan
	lastScan        time.Time
	smartctlPath    string
	excludedDevices map[string]struct{}
	// run executes smartctl; nil means exec smartctlPath. Overridable for testing.
	run smartctlRunner
}

// smartctlRunner runs smartctl and returns its stdout and exit status. err is
// set only when smartctl could not be run, timed out or ctx ended.
type smartctlRunner func(ctx context.Context, timeout time.Duration, args ...string) (output []byte, exitCode int, err error)

// smartDevice is a device from the smartctl scan, SMART_DEVICES or sysfs.
type smartDevice struct {
	name  string
	dtype string
	// explicit marks a type set in SMART_DEVICES. It is a deliberate override,
	// so it is always passed to smartctl and never second-guessed.
	explicit bool
}

// smartAttempt is one way to query a device: a path and a -d type. An empty
// type lets smartctl detect it.
type smartAttempt struct {
	path  string
	dtype string
}

type smartTarget struct {
	key      string // name and type; stable across refreshes
	dtype    string // emmc and mdraid are read from sysfs instead of smartctl
	name     string
	attempts []smartAttempt
}

type smartResult struct {
	key     string // data map key
	data    *smartmodel.SmartData
	attempt int    // index of the attempt that succeeded, tried first next time
	ata     bool   // device speaks ATA, so -l devstat is requested
	path    string // the path that was read
	diskSeq uint64 // that path's diskseq when it was read; 0 when it has none
}

var (
	errNoValidSmartData = errors.New("no valid SMART data found")
	errStandby          = errors.New("device is in standby")
	errVirtualDevice    = errors.New("virtual device")
)

// diskSeq returns the kernel's sequence number of the block device at path
// (/sys/class/block/<name>/diskseq), which every newly attached disk gets
// anew, or 0 when the path is no block device or the kernel has none. Tests
// replace it.
var diskSeq = func(path string) uint64 {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	raw, err := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(path), "diskseq"))
	if err != nil {
		return 0
	}
	seq, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

const (
	smartScanInterval  = 30 * time.Minute
	smartScanTimeout   = 10 * time.Second
	smartDeviceTimeout = 15 * time.Second
)

// RefreshInterval returns the interval between automatic SMART refreshes.
func (sm *Manager) RefreshInterval() time.Duration {
	sm.Lock()
	defer sm.Unlock()
	return sm.refreshInterval
}

// SetRefreshInterval sets the interval between automatic SMART refreshes.
func (sm *Manager) SetRefreshInterval(interval time.Duration) {
	sm.Lock()
	defer sm.Unlock()
	sm.refreshInterval = interval
}

// RefreshDue reports whether no refresh has been attempted yet or the last one
// is at least one refresh interval old at now.
func (sm *Manager) RefreshDue(now time.Time) bool {
	sm.Lock()
	defer sm.Unlock()
	return sm.lastRefresh.IsZero() || now.Sub(sm.lastRefresh) >= sm.refreshInterval
}

// MarkRefreshed records a refresh attempt, successful or not, at now.
func (sm *Manager) MarkRefreshed(now time.Time) {
	sm.Lock()
	defer sm.Unlock()
	sm.lastRefresh = now
}

// SwapRefreshError records the latest refresh error text ("" for success) and
// returns the previous one, so callers can log only changes.
func (sm *Manager) SwapRefreshError(current string) (previous string) {
	sm.Lock()
	defer sm.Unlock()
	previous, sm.lastRefreshError = sm.lastRefreshError, current
	return previous
}

// driveFailure is a target whose read failed in one refresh.
type driveFailure struct {
	target smartTarget
	err    error
}

// Refresh reads every target and replaces the SMART records with what it read.
// A drive that fails or passes its time limit is left out and the refresh
// moves on to the next one; only the end of ctx stops it, and then the
// previous records stay. It returns an error only when nothing was collected.
func (sm *Manager) Refresh(ctx context.Context, forceScan bool) error {
	sm.refreshMutex.Lock()
	defer sm.refreshMutex.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	targets, scanErr := sm.buildTargets(ctx, forceScan)
	if err := ctx.Err(); err != nil {
		return err
	}

	data := make(map[string]*smartmodel.SmartData, len(targets))
	last := make(map[string]smartResult, len(targets))
	collected := make(map[string]struct{}, len(targets))
	var failed []driveFailure
	for _, target := range targets {
		result, err := sm.collectTarget(ctx, target)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			// Keep the last successful reading for later standby reuse. Only
			// targets still present are kept, so removed drives are forgotten.
			if prev, ok := sm.last[target.key]; ok {
				last[target.key] = prev
			}
			switch {
			case errors.Is(err, errVirtualDevice):
				slog.Debug("skipping virtual SMART device", "device", target.name, "error", err)
			case !errors.Is(err, errStandby):
				failed = append(failed, driveFailure{target: target, err: err})
			}
			continue
		}
		collected[target.key] = struct{}{}
		// A second path to one disk keeps its own reading too, so a later
		// standby on that path reuses it instead of waking the disk.
		last[target.key] = result
		if existing, ok := data[result.key]; ok {
			slog.Debug("skipping duplicate SMART device", "device", target.name, "serial", result.key, "first", existing.DiskName)
			continue
		}
		data[result.key] = result.data
	}
	sm.last = last

	sm.Lock()
	sm.data = data
	sm.Unlock()

	sm.logDriveFailures(failed, collected)
	if len(targets) == 0 && scanErr != nil {
		return scanErr
	}
	if scanErr != nil {
		slog.Debug("smartctl scan failed", "error", scanErr, "previous_devices", len(sm.scanned))
	}
	switch {
	case len(data) > 0:
		return nil
	case len(failed) > 0:
		// Each drive's own error is in its log line.
		return fmt.Errorf("%w: %d drives unreadable", errNoValidSmartData, len(failed))
	default:
		return errNoValidSmartData
	}
}

// logDriveFailures logs the drives that failed: a warning when a drive starts
// failing or fails differently, a debug line while it keeps failing the same
// way, and an info line when one answers again. Called with refreshMutex held.
func (sm *Manager) logDriveFailures(failed []driveFailure, collected map[string]struct{}) {
	failing := make(map[string]string, len(failed))
	for _, failure := range failed {
		text := failure.err.Error()
		failing[failure.target.key] = text
		if previous, ok := sm.failing[failure.target.key]; ok && previous == text {
			slog.Debug("SMART drive still unreadable", "device", failure.target.name, "error", failure.err)
			continue
		}
		slog.Warn("SMART drive unreadable, left out of the list", "device", failure.target.name, "error", failure.err)
	}
	for key := range sm.failing {
		if _, ok := collected[key]; ok {
			name, _, _ := strings.Cut(key, "|")
			slog.Info("SMART drive readable again", "device", name)
		}
	}
	sm.failing = failing
}

// GetCurrentData returns the current SMART records.
func (sm *Manager) GetCurrentData() map[string]smartmodel.SmartData {
	sm.Lock()
	defer sm.Unlock()
	result := make(map[string]smartmodel.SmartData, len(sm.data))
	for key, value := range sm.data {
		if value != nil {
			result[key] = *value
		}
	}
	return result
}

// buildTargets merges the smartctl scan, SMART_DEVICES and sysfs devices into
// the list of targets to query.
func (sm *Manager) buildTargets(ctx context.Context, forceScan bool) ([]smartTarget, error) {
	var configured []smartDevice
	if raw, ok := config.LookupEnv(config.EnvSmartDevices); ok {
		// An empty SMART_DEVICES turns SMART off.
		if strings.TrimSpace(raw) == "" {
			return nil, errNoValidSmartData
		}
		var err error
		if configured, err = parseConfiguredDevices(raw); err != nil {
			return nil, err
		}
	}

	scanned, scanErr := sm.scanSmartctl(ctx, forceScan)
	scanned = append(slices.Clone(scanned), scanSysfsDevices()...)

	devices := mergeDevices(scanned, configured)
	targets := make([]smartTarget, 0, len(devices))
	for _, device := range devices {
		if sm.isExcludedDevice(device.name) {
			continue
		}
		targets = append(targets, smartTarget{
			key:      device.name + "|" + device.dtype,
			dtype:    device.dtype,
			name:     device.name,
			attempts: sm.attemptsFor(device),
		})
	}
	return targets, scanErr
}

// scanSysfsDevices returns the eMMC and md devices read from sysfs instead of
// smartctl. It is a variable so tests can isolate themselves from the host.
var scanSysfsDevices = func() []smartDevice {
	var devices []smartDevice
	for _, path := range scanEmmcDevices() {
		devices = append(devices, smartDevice{name: path, dtype: "emmc"})
	}
	for _, path := range scanMdraidDevices() {
		devices = append(devices, smartDevice{name: path, dtype: "mdraid"})
	}
	return devices
}

// mergeDevices adds SMART_DEVICES entries to the scanned devices. A configured
// entry with a type replaces the scanned entry for the same path when that
// path was scanned exactly once; otherwise it is added (extra RAID members
// that share a controller path). Duplicates are dropped.
func mergeDevices(scanned, configured []smartDevice) []smartDevice {
	devices := slices.Clone(scanned)
	nameCounts := make(map[string]int, len(scanned))
	for _, device := range scanned {
		nameCounts[device.name]++
	}
	for _, device := range configured {
		if i := slices.IndexFunc(devices, func(d smartDevice) bool { return d.name == device.name && d.dtype == device.dtype }); i >= 0 {
			devices[i].explicit = devices[i].explicit || device.explicit
			continue
		}
		if nameCounts[device.name] == 1 {
			if device.dtype != "" {
				i := slices.IndexFunc(devices, func(d smartDevice) bool { return d.name == device.name })
				devices[i] = device
				nameCounts[device.name] = 0 // further types for this path are added
			}
			continue
		}
		devices = append(devices, device)
	}

	seen := make(map[smartDevice]struct{}, len(devices))
	return slices.DeleteFunc(devices, func(d smartDevice) bool {
		d.explicit = false
		if _, ok := seen[d]; ok || d.name == "" {
			return true
		}
		seen[d] = struct{}{}
		return false
	})
}

// attemptsFor returns the smartctl queries to try for a device, in order. All
// fallbacks live here:
//   - an explicit SMART_DEVICES type is used as it is;
//   - the scan types ata and scsi are often wrong (SATA disks behind a SAS HBA
//     or a USB bridge), so smartctl detects the type first; scsi then falls
//     back to sat for SATA disks the detection misses;
//   - an NVMe controller path falls back to its first namespace.
func (sm *Manager) attemptsFor(device smartDevice) []smartAttempt {
	dtype := strings.ToLower(device.dtype)
	var attempts []smartAttempt
	switch {
	case device.explicit:
		attempts = []smartAttempt{{device.name, device.dtype}}
	case dtype == "scsi":
		attempts = []smartAttempt{{device.name, ""}, {device.name, "sat"}}
	case dtype == "ata":
		attempts = []smartAttempt{{device.name, ""}}
	default:
		attempts = []smartAttempt{{device.name, device.dtype}}
	}
	if IsNvmeControllerPath(device.name) && (dtype == "" || dtype == "nvme") {
		if namespace := device.name + "n1"; !sm.isExcludedDevice(namespace) {
			attempts = append(attempts, smartAttempt{namespace, device.dtype})
		}
	}
	return attempts
}

// scanSmartctl returns the devices found by `smartctl --scan -j`. The result,
// a failure included, is cached for smartScanInterval, and a failed scan
// keeps the previous device list so drives do not disappear.
func (sm *Manager) scanSmartctl(ctx context.Context, force bool) ([]smartDevice, error) {
	if sm.smartctlPath == "" && sm.run == nil {
		return nil, nil
	}
	if !force && !sm.lastScan.IsZero() && time.Since(sm.lastScan) < smartScanInterval {
		return sm.scanned, sm.scanErr
	}

	output, exitCode, err := sm.runSmartctl(ctx, smartScanTimeout, "--scan", "-j")
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The caller ended, which says nothing about the scan.
		return sm.scanned, ctxErr
	}
	if err == nil && exitCode != 0 {
		err = fmt.Errorf("smartctl scan failed (exit status %d)%s", exitCode, smartctlMessages(output))
	}
	// A failure is cached only when there is a list to keep meanwhile; with
	// none, the next refresh scans again.
	if err == nil || len(sm.scanned) > 0 {
		sm.lastScan = time.Now()
	}
	if err == nil {
		var devices []smartDevice
		if devices, err = parseScan(output); err == nil {
			// An empty scan after devices were found is more likely a
			// passing failure than every disk being removed.
			if len(devices) == 0 && len(sm.scanned) > 0 {
				err = errors.New("smartctl scan found no devices")
			} else {
				sm.scanned = devices
			}
		}
	}
	sm.scanErr = err
	return sm.scanned, err
}

// parseScan parses the output of smartctl --scan -j.
func parseScan(output []byte) ([]smartDevice, error) {
	var scan struct {
		Devices []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Protocol string `json:"protocol"`
		} `json:"devices"`
	}
	if err := unmarshalSmartctl(output, &scan); err != nil {
		return nil, fmt.Errorf("invalid smartctl scan output: %w", err)
	}
	devices := make([]smartDevice, 0, len(scan.Devices))
	for _, device := range scan.Devices {
		devices = append(devices, smartDevice{name: device.Name, dtype: device.Type})
	}
	return devices, nil
}

// parseConfiguredDevices parses SMART_DEVICES entries of the form path[:type].
func parseConfiguredDevices(list string) ([]smartDevice, error) {
	splitChar, _ := config.LookupEnv(config.EnvSmartDevicesSeparator)
	if splitChar == "" {
		splitChar = ","
	}
	var devices []smartDevice
	for entry := range strings.SplitSeq(list, splitChar) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, dtype, _ := strings.Cut(entry, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("invalid SMART_DEVICES entry %q", entry)
		}
		dtype = strings.ToLower(strings.TrimSpace(dtype))
		devices = append(devices, smartDevice{name: name, dtype: dtype, explicit: dtype != ""})
	}
	if len(devices) == 0 {
		return nil, errNoValidSmartData
	}
	return devices, nil
}

// collectTarget reads one target. smartctl targets try each attempt in turn,
// starting with the one that worked last time. A drive that fails, or passes
// its time limit, returns an error and the caller moves on to the next one.
//
//nolint:gocognit // One loop holds the attempt order, the standby rules and the single wake.
func (sm *Manager) collectTarget(ctx context.Context, target smartTarget) (smartResult, error) {
	switch target.dtype {
	case "emmc", "mmc":
		if key, data, ok := readEmmcSmartData(target.name); ok {
			return smartResult{key: key, data: data}, nil
		}
		return smartResult{}, errNoValidSmartData
	case "mdraid":
		if key, data, ok := readMdraidSmartData(target.name); ok {
			return smartResult{key: key, data: data}, nil
		}
		return smartResult{}, errNoValidSmartData
	}
	if sm.smartctlPath == "" && sm.run == nil {
		return smartResult{}, errNoValidSmartData
	}

	prev, hasPrev := sm.last[target.key]
	order := make([]int, 0, len(target.attempts))
	if hasPrev && prev.attempt < len(target.attempts) {
		order = append(order, prev.attempt)
	}
	for i := range target.attempts {
		if !hasPrev || i != prev.attempt {
			order = append(order, i)
		}
	}

	// The last attempt's error is returned; one a later attempt replaces, or
	// that a standby answer ends, is logged here.
	var (
		err        error
		errAttempt smartAttempt
	)
	logPending := func() {
		if err != nil {
			slog.Debug("smartctl attempt failed", "device", errAttempt.path, "type", errAttempt.dtype, "error", err)
		}
	}
	fail := func(attempt smartAttempt, attemptErr error) {
		logPending()
		err, errAttempt = attemptErr, attempt
	}
	// After a wake the disk is spinning, or the wake did not reach it, so the
	// remaining attempts read it as it is rather than asking about standby.
	standby := true
	for _, i := range order {
		attempt := target.attempts[i]
		// A scanned ata type requests devstat until a reading confirms the protocol.
		ata := prev.ata || !hasPrev && strings.EqualFold(target.dtype, "ata")
		args := smartctlArgs(attempt, ata, standby)
		output, exitCode, runErr := sm.runSmartctl(ctx, smartDeviceTimeout, args...)
		if runErr != nil {
			if ctx.Err() != nil {
				return smartResult{}, runErr
			}
			fail(attempt, runErr)
			continue
		}
		data, protocol, parseErr := parseSmartctl(output, attempt.dtype)
		if parseErr == nil {
			return readResult(data, attempt, i, protocol), nil
		}
		if errors.Is(parseErr, errVirtualDevice) {
			logPending()
			return smartResult{}, parseErr
		}
		if !standby || !isStandby(output, exitCode) {
			fail(attempt, fmt.Errorf("%w (exit status %d)", parseErr, exitCode))
			continue
		}
		// Do not wake a sleeping disk that was read before: reuse its last
		// record, unless the kernel numbers the disk at that path anew, which
		// means another disk took its place.
		if hasPrev && (prev.diskSeq == 0 || diskSeq(prev.path) == prev.diskSeq) {
			logPending()
			slog.Debug("smartctl device in standby, kept its last record", "device", attempt.path)
			return prev, nil
		}
		// Wake a disk once per daemon run so it is read at least once. This is
		// kept apart from last, so a disk that fails after waking is not woken
		// again on every refresh.
		if _, ok := sm.woken[target.key]; !ok {
			if sm.woken == nil {
				sm.woken = make(map[string]struct{})
			}
			sm.woken[target.key] = struct{}{}
			slog.Debug("smartctl waking device in standby for its first reading", "device", attempt.path)
			result, wakeErr := sm.wakeTarget(ctx, attempt, i, ata)
			if wakeErr == nil {
				return result, nil
			}
			if ctx.Err() != nil {
				// The wake was cut short, not tried: keep it for a later refresh.
				delete(sm.woken, target.key)
				return smartResult{}, wakeErr
			}
			standby = false
			fail(attempt, wakeErr)
			continue
		}
		// Already woken once: skip it until it wakes. smartctl also reports
		// SLEEP when it lacks permission to check the power mode.
		logPending()
		slog.Debug("smartctl device in standby, skipped", "device", attempt.path, "sleep_or_no_permission", bytes.Contains(bytes.ToUpper(output), []byte("SLEEP MODE")))
		return smartResult{}, errStandby
	}
	return smartResult{}, err
}

// wakeTarget queries an attempt without -n standby, spinning up the disk.
// A failure is returned as a real error rather than errStandby, which also
// brings out the permission problem smartctl otherwise reports as SLEEP.
func (sm *Manager) wakeTarget(ctx context.Context, attempt smartAttempt, i int, ata bool) (smartResult, error) {
	args := smartctlArgs(attempt, ata, false)
	output, exitCode, err := sm.runSmartctl(ctx, smartDeviceTimeout, args...)
	if err != nil {
		return smartResult{}, err
	}
	data, protocol, err := parseSmartctl(output, attempt.dtype)
	if err != nil {
		return smartResult{}, fmt.Errorf("%w (exit status %d)", err, exitCode)
	}
	return readResult(data, attempt, i, protocol), nil
}

// readResult is a reading of one attempt, with the disk's diskseq so a later
// standby can tell it is still the same disk.
func readResult(data *smartmodel.SmartData, attempt smartAttempt, i int, protocol string) smartResult {
	return smartResult{
		key: data.SerialNumber, data: data, attempt: i, ata: protocol == "ata",
		path: attempt.path, diskSeq: diskSeq(attempt.path),
	}
}

// smartctlArgs returns the smartctl arguments for one attempt. standby adds
// -n standby so smartctl skips a sleeping disk instead of waking it.
func smartctlArgs(attempt smartAttempt, ata, standby bool) []string {
	args := make([]string, 0, 9)
	if attempt.dtype != "" {
		args = append(args, "-d", attempt.dtype)
	}
	args = append(args, "-a", "--json=c")
	if dtype := strings.ToLower(attempt.dtype); ata || dtype == "sat" || dtype == "ata" {
		args = append(args, "-l", "devstat")
	}
	if standby {
		args = append(args, "-n", "standby")
	}
	return append(args, attempt.path)
}

// isStandby reports whether smartctl skipped the device because -n standby
// found it in a low-power mode: exit bit 1 together with its "Device is in X
// mode" message. Exit bit 1 alone also means the device could not be opened.
func isStandby(output []byte, exitCode int) bool {
	if exitCode&2 == 0 {
		return false
	}
	upper := bytes.ToUpper(output)
	return bytes.Contains(upper, []byte("DEVICE IS IN ")) && bytes.Contains(upper, []byte(" MODE"))
}

// runSmartctl runs smartctl with a time limit under ctx. smartctl reports
// device problems through its exit status bitmask while still printing JSON,
// so a non-zero exit is returned as the status and not as an error. Passing
// the time limit is an error of its own that does not wrap ctx's, so the
// caller can tell it from the end of ctx.
func (sm *Manager) runSmartctl(ctx context.Context, timeout time.Duration, args ...string) ([]byte, int, error) {
	if sm.run != nil {
		return sm.run(ctx, timeout, args...)
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := exec.CommandContext(cmdCtx, sm.smartctlPath, args...).Output()
	if cmdCtx.Err() != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		return nil, 0, fmt.Errorf("smartctl timed out after %s", timeout)
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return output, exitErr.ExitCode(), nil
	}
	return output, 0, err
}

func (sm *Manager) refreshExcludedDevices() {
	rawValue, _ := config.LookupEnv(config.EnvExcludeSmart)
	sm.excludedDevices = make(map[string]struct{})

	for entry := range strings.SplitSeq(rawValue, ",") {
		device := strings.TrimSpace(entry)
		if device == "" {
			continue
		}
		sm.excludedDevices[device] = struct{}{}
	}
}

func (sm *Manager) isExcludedDevice(deviceName string) bool {
	_, exists := sm.excludedDevices[deviceName]
	return exists
}

// IsNvmeControllerPath checks if the path matches an NVMe controller pattern
// like /dev/nvme0, /dev/nvme1, etc. (without namespace suffix like n1)
func IsNvmeControllerPath(path string) bool {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "nvme") {
		return false
	}
	suffix := strings.TrimPrefix(base, "nvme")
	if suffix == "" {
		return false
	}
	// Controller paths are just "nvme" + digits (e.g., nvme0, nvme1)
	// Namespace paths have "n" after the controller number (e.g., nvme0n1)
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// NewManager creates and initializes a new Manager.
func NewManager() (*Manager, error) {
	sm := &Manager{
		data:            make(map[string]*smartmodel.SmartData),
		refreshInterval: config.DefaultSmartRefreshInterval,
	}
	sm.refreshExcludedDevices()
	path, err := exec.LookPath("smartctl")
	if err != nil {
		// Fail fast unless this host exposes eMMC or mdraid health via sysfs,
		// in which case smartctl is optional.
		if len(scanEmmcDevices()) > 0 || len(scanMdraidDevices()) > 0 {
			slog.Debug("capability check failed", "capability", "smartctl", "available", false, "error", err)
			return sm, nil
		}
		return nil, err
	}
	slog.Debug("capability check passed", "capability", "smartctl", "available", true, "path", path)
	sm.smartctlPath = path
	return sm, nil
}
