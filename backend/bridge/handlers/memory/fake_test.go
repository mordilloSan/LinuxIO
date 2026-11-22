package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
)

// fakeSystem plays systemd and the kernel for the executor. Files and kernel
// parameters land in the fixture root; starting or stopping a unit adds or
// removes its device's line in the fixture's /proc/swaps, as swapon and
// swapoff do for a swap file. Swap files are made, resized and deleted in
// the fixture, with a swap signature for mkswap's header, and their fstab
// lines are edited there. Every side effect is recorded in order.
type fakeSystem struct {
	f     fixture
	tuned bool
	// provider is the installed zram tool; zram-tools unless a test says
	// otherwise.
	provider apischema.ZramProvider
	// calls lists the side effects in order: "start <unit>", "write <path>",
	// "set <param>=<value>", ...
	calls []string
	// failures makes the call with this name fail before it has an effect;
	// failAfter makes it fail after its effect landed, as a D-Bus call does
	// when ctx ends after systemd took the job, or atomicfile.Write when the
	// directory fsync fails after the rename.
	failures, failAfter map[string]error
	// stops scripts how systemd runs the stop of a unit; a unit not listed
	// stops at once.
	stops map[string]*fakeStop
	// activeStates are units' ActiveState outside a scripted stop; a unit
	// not listed is "active".
	activeStates map[string]string
	// devices is the /proc/swaps line each unit's device shows while it runs.
	devices map[string]string
	// stuck units are started or stopped without /proc/swaps ever changing.
	stuck map[string]bool
	// blocked devices never leave on a swapoff, which returns when ctx ends.
	blocked map[string]bool
	// fileStates are unit file states; a unit not listed is "disabled".
	fileStates map[string]string
	// noHeader makes a resize of the file at the path cut it to its new
	// length and fail with errNoSwapHeader, as a mkswap that failed twice.
	noHeader map[string]bool
	// ignoredSwapOns are files a swapon succeeds for without the file ever
	// showing in /proc/swaps.
	ignoredSwapOns map[string]bool
	// statusFails is how many status reads still fail with errStatusRace.
	statusFails int
	// onStatus runs once, after the next status read that succeeds, as the
	// host changing between that read and what comes after it.
	onStatus func()
	// busyResets is how many resets of a zram device the kernel still
	// refuses with EBUSY, as while udev holds it after a swapoff;
	// onBusyReset runs after each.
	busyResets  int
	onBusyReset func()
	// generatorResetFails makes the reset zram-generator's setup service
	// runs when its swap unit stops fail, leaving zram0 set up.
	generatorResetFails bool
	// after runs after a recorded call succeeds.
	after func(call string)
}

// errStatusRace is a status read that races the change.
var errStatusRace = errors.New("read swap devices: the table changed while it was read")

const (
	zramSwapsLine     = "/dev/zram0                              partition\t8154200\t\t0\t\t100\n"
	swapFileSwapsLine = "/swap.img                               file\t\t2097148\t\t524288\t\t-2\n"
)

func newFakeSystem(f fixture) *fakeSystem {
	return &fakeSystem{
		f:            f,
		provider:     apischema.ZramProviderTools,
		failures:     map[string]error{},
		failAfter:    map[string]error{},
		stops:        map[string]*fakeStop{},
		activeStates: map[string]string{},
		devices: map[string]string{
			zramService:           zramSwapsLine,
			zramGeneratorSwapUnit: zramSwapsLine,
		},
		stuck:          map[string]bool{},
		blocked:        map[string]bool{},
		fileStates:     map[string]string{},
		noHeader:       map[string]bool{},
		ignoredSwapOns: map[string]bool{},
	}
}

// executor returns an executor over the fake with short waits, recording
// the progress it reports.
func (s *fakeSystem) executor(progress *[]apischema.SwapProgress) *executor {
	return &executor{
		root: s.f.host(),
		sys:  s,
		report: func(p apischema.SwapProgress) {
			if progress != nil {
				*progress = append(*progress, p)
			}
		},
		waits: waits{swapOn: 200 * time.Millisecond, zramOff: 200 * time.Millisecond, poll: time.Millisecond, stopGrace: 100 * time.Millisecond, zramReset: 50 * time.Millisecond},
	}
}

// do records call, fails it when failures names it, and otherwise applies
// effect and then runs the after hook.
func (s *fakeSystem) do(call string, effect func() error) error {
	s.calls = append(s.calls, call)
	if err := s.failures[call]; err != nil {
		return err
	}
	if err := effect(); err != nil {
		return err
	}
	if s.after != nil {
		s.after(call)
	}
	return s.failAfter[call]
}

// unitDo is do for a D-Bus call, which fails once ctx is done.
func (s *fakeSystem) unitDo(ctx context.Context, call string, effect func() error) error {
	if err := ctx.Err(); err != nil {
		s.calls = append(s.calls, call)
		return err
	}
	return s.do(call, effect)
}

func (s *fakeSystem) status(context.Context) (apischema.SwapStatus, []fstabSwap, error) {
	if s.statusFails > 0 {
		s.statusFails--
		return apischema.SwapStatus{}, nil, errStatusRace
	}
	status, fstab, err := s.f.host().readSwapStatus(s.provider)
	if err != nil {
		return apischema.SwapStatus{}, nil, err
	}
	status.TunedActive = s.tuned
	if hook := s.onStatus; hook != nil {
		s.onStatus = nil
		hook()
	}
	return status, fstab, nil
}

// captureLogs sends the default logger's lines, Info and above, to the
// returned buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

// fakeStop scripts a stop the way systemd runs it: the unit stays active
// while the job waits, is deactivating while swapoff runs, then either the
// device leaves and the unit is inactive, or systemd gives up and the unit
// ends as ends says with the device still active. Each step lasts some
// ActiveState reads. leaves makes the device leave even so, as a stop
// command that removed zram and then exited non-zero.
type fakeStop struct {
	queued, polls int
	ends          string
	leaves        bool
	// pending is a stop running; restart is a start queued behind it.
	pending, restart bool
}

func (s *fakeSystem) startUnit(ctx context.Context, name string) error {
	return s.unitDo(ctx, "start "+name, func() error {
		// systemd runs a start queued during a stop once the stop ends.
		if stop := s.stops[name]; stop != nil && stop.pending {
			stop.restart = true
			return nil
		}
		// zramswap.service held by the install's runtime drop-in is skipped:
		// its condition fails, and the start succeeds without starting
		// anything.
		if name == zramService && s.zramToolsHeld() {
			return nil
		}
		s.unitOn(name)
		return nil
	})
}

func (s *fakeSystem) stopUnit(ctx context.Context, name string) error {
	return s.unitDo(ctx, "stop "+name, func() error {
		if stop := s.stops[name]; stop != nil {
			stop.pending = true
			return nil
		}
		s.unitOff(name)
		return nil
	})
}

// restartUnit stops and starts the unit at once, as zramswap.service does
// when nothing swaps on its device.
func (s *fakeSystem) restartUnit(ctx context.Context, name string) error {
	return s.unitDo(ctx, "restart "+name, func() error {
		s.unitOff(name)
		s.unitOn(name)
		return nil
	})
}

// swapOff takes the device at path out of /proc/swaps, as swapoff(2) does. A
// path in
// blocked never returns until ctx ends, as a swapoff that is still reading
// pages back, and leaves the device on.
func (s *fakeSystem) swapOff(ctx context.Context, path string) error {
	if s.blocked[path] {
		s.calls = append(s.calls, "swapoff "+path)
		<-ctx.Done()
		return ctx.Err()
	}
	return s.unitDo(ctx, "swapoff "+path, func() error {
		s.f.removeSwapsLine(path)
		return nil
	})
}

// zramToolsHeld reports whether a drop-in at the install hold's path keeps
// zramswap.service from starting: a file with a condition that never holds.
func (s *fakeSystem) zramToolsHeld() bool {
	data, err := os.ReadFile(filepath.Join(s.f.root, zramInstallHoldPath))
	return err == nil && strings.Contains(string(data), "ConditionPathExists=!/")
}

// resetZram frees the device, as the kernel does for one nothing holds.
func (s *fakeSystem) resetZram(ctx context.Context, name string) error {
	return s.unitDo(ctx, "reset "+name, func() error {
		if s.busyResets > 0 {
			s.busyResets--
			if s.onBusyReset != nil {
				s.onBusyReset()
			}
			return syscall.EBUSY
		}
		s.f.write(filepath.Join(sysBlockDir, name, "disksize"), "0\n")
		return nil
	})
}

// zramSetUp reports whether zram0 exists with a size. Without the zram
// module there is no /sys/block/zram0 at all.
func (s *fakeSystem) zramSetUp() bool {
	const disksize = "/sys/block/zram0/disksize"
	return s.f.exists(disksize) && s.f.param(disksize) != "0"
}

// zramUnit reports whether name is a unit that sets zram up.
func zramUnit(name string) bool {
	return name == zramService || name == zramGeneratorSwapUnit
}

// configuredZramSize is the disksize the provider's configuration asks for,
// or 0 when it names none.
func (s *fakeSystem) configuredZramSize() uint64 {
	path, key := zramswapConfigPath, "SIZE="
	if s.provider == apischema.ZramProviderGenerator {
		path, key = zramGeneratorConfigPath, "zram-size = "
	}
	if !s.f.exists(path) {
		return 0
	}
	for line := range strings.Lines(s.f.read(path)) {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), key); ok {
			n, _ := strconv.ParseUint(value, 10, 64)
			return n << 20
		}
	}
	return 0
}

func (s *fakeSystem) unitOn(name string) {
	// zram-generator's setup service cannot set up a device that is set up
	// already: it fails to set the compression algorithm, and the swap unit
	// fails on its dependency.
	if name == zramGeneratorSwapUnit && s.zramSetUp() && !strings.Contains(s.f.read(procSwaps), "/dev/zram0 ") {
		s.activeStates[name] = "inactive"
		s.activeStates[zramGeneratorSetupUnit] = "failed"
		return
	}
	if line, ok := s.devices[name]; ok && !s.stuck[name] {
		swaps := s.f.read(procSwaps)
		if !strings.Contains(swaps, line) {
			s.f.write(procSwaps, swaps+line)
			// The device gets the size its configuration gives only when it
			// is not set up yet: the kernel refuses a new size for one that
			// is, and the tool swaps on it as it stands.
			if size := s.configuredZramSize(); zramUnit(name) && size > 0 && !s.zramSetUp() {
				s.f.write("/sys/block/zram0/disksize", strconv.FormatUint(size, 10)+"\n")
			}
		}
		s.activeStates[name] = "active"
		s.bindSetupUnit(name, "active")
	}
}

// bindSetupUnit gives zram-generator's setup service the state of its swap
// unit, which it is bound to.
func (s *fakeSystem) bindSetupUnit(name, state string) {
	if name == zramGeneratorSwapUnit {
		s.activeStates[zramGeneratorSetupUnit] = state
	}
}

// unitOff takes the unit's device out of /proc/swaps. zram-generator's setup
// service resets its device when its swap unit stops; zram-tools' device
// stays set up, as it does when zramswap stop cannot unload the module.
func (s *fakeSystem) unitOff(name string) {
	if line, ok := s.devices[name]; ok && !s.stuck[name] {
		if name == zramGeneratorSwapUnit && !s.generatorResetFails {
			s.f.write("/sys/block/zram0/disksize", "0\n")
		}
		s.f.write(procSwaps, strings.ReplaceAll(s.f.read(procSwaps), line, ""))
		s.activeStates[name] = "inactive"
		s.bindSetupUnit(name, "inactive")
		if name == zramGeneratorSwapUnit && s.generatorResetFails {
			s.activeStates[zramGeneratorSetupUnit] = "failed"
		}
	}
}

// finishStop ends a scripted stop now, as a swapoff that outlived the route.
func (s *fakeSystem) finishStop(name string) {
	stop := s.stops[name]
	stop.pending = false
	if stop.ends != "" {
		if stop.leaves {
			s.unitOff(name)
		}
		s.activeStates[name] = stop.ends
		return
	}
	s.unitOff(name)
	if stop.restart {
		stop.restart = false
		s.unitOn(name)
	}
}

func (s *fakeSystem) activeState(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.failures["active "+name]; err != nil {
		return "", err
	}
	if stop := s.stops[name]; stop != nil && stop.pending {
		switch {
		case stop.queued > 0:
			stop.queued--
			return "active", nil
		case stop.polls > 0:
			stop.polls--
			return "deactivating", nil
		default:
			s.finishStop(name)
		}
	}
	if state, ok := s.activeStates[name]; ok {
		return state, nil
	}
	return "active", nil
}

// reload has systemd read its configuration again.
func (s *fakeSystem) reload(ctx context.Context) error {
	return s.unitDo(ctx, "reload", func() error { return nil })
}

func (s *fakeSystem) setFileState(ctx context.Context, call, name, state string) error {
	return s.unitDo(ctx, call, func() error {
		s.fileStates[name] = state
		return nil
	})
}

func (s *fakeSystem) enableUnit(ctx context.Context, name string) error {
	return s.setFileState(ctx, "enable "+name, name, "enabled")
}

func (s *fakeSystem) disableUnit(ctx context.Context, name string) error {
	return s.setFileState(ctx, "disable "+name, name, "disabled")
}

func (s *fakeSystem) unitFileState(_ context.Context, name string) (string, error) {
	if err := s.failures["state "+name]; err != nil {
		return "", err
	}
	if state, ok := s.fileStates[name]; ok {
		return state, nil
	}
	return "disabled", nil
}

func (s *fakeSystem) writeFile(name string, data []byte, mode fs.FileMode) error {
	return s.do("write "+name, func() error { return s.f.host().writeFile(name, data, mode) })
}

func (s *fakeSystem) removeFile(name string) error {
	return s.do("remove "+name, func() error { return s.f.host().removeFile(name) })
}

func (s *fakeSystem) writeParam(name, value string) error {
	return s.do("set "+name+"="+value, func() error { return liveSystem{root: s.f.host()}.writeParam(name, value) })
}

func (s *fakeSystem) readParam(name string) (string, error) {
	if err := s.failures["read "+name]; err != nil {
		return "", err
	}
	return liveSystem{root: s.f.host()}.readParam(name)
}

// zramswapPackageDefault is the /etc/default/zramswap zram-tools installs.
const zramswapPackageDefault = "# Compression algorithm selection\n#ALGO=lz4\n# PERCENT=50\n"

// diskHost is a desktop with an active 2 GiB /swap.img from fstab holding
// 512 MiB on a roomy ext4, zswap off with lzo, zram-tools installed and its
// zram not running.
func diskHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f := newFixture(t)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine)
	f.write(procMeminfo, meminfoText)
	f.write(etcFstab, "UUID=0a1b2c3d / ext4 defaults 0 1\n/swap.img none swap sw 0 0\n")
	f.writeSwapFile("/swap.img", 2*gib)
	f.writeVMSysctls("60", "3", "15000", "10")
	f.write("/sys/module/zswap/parameters/enabled", "N\n")
	f.write("/sys/module/zswap/parameters/compressor", "lzo\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")
	f.write("/sys/block/zram0/disksize", "0\n")
	f.write(zramswapConfigPath, zramswapPackageDefault)
	sys := newFakeSystem(f)
	sys.activeStates[zramService] = "inactive"
	return f, sys
}

// zramHost is diskHost after a switch to zram: zram0 swapping 256 MiB, the
// swap file removed with its fstab line, the zram presets live and in the
// sysctl drop-in.
func zramHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/dev/zram0                              partition\t8154200\t\t262144\t\t100\n")
	sys.devices[zramService] = "/dev/zram0                              partition\t8154200\t\t262144\t\t100\n"
	f.write(etcFstab, "UUID=0a1b2c3d / ext4 defaults 0 1\n")
	f.remove("/swap.img")
	f.write("/sys/block/zram0/disksize", "8349903872\n")
	f.write("/sys/block/zram0/mm_stat", "268435456 89478485 95000000 0 95000000 0 0 0 0\n")
	f.writeVMSysctls("180", "0", "0", "125")
	f.write(sysctlDropInPath, string(renderSysctlDropIn([]sysctlEntry{
		{key: "vm.swappiness", value: "180"},
		{key: "vm.page-cluster", value: "0"},
		{key: "vm.watermark_boost_factor", value: "0"},
		{key: "vm.watermark_scale_factor", value: "125"},
	})))
	f.write(zramswapConfigPath, string(renderZramswapConfig(7963*mib)))
	sys.fileStates[zramService] = "enabled"
	sys.activeStates[zramService] = "active"
	return f, sys
}

// generatorDiskHost is diskHost on a host where zram-generator, and not
// zram-tools, sets zram up; it has no configuration yet.
func generatorDiskHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	sys.provider = apischema.ZramProviderGenerator
	f.write(ZramGeneratorPath, "")
	return f, sys
}

// generatorZramHost is zramHost with zram0 made by zram-generator from
// LinuxIO's configuration.
func generatorZramHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := zramHost(t)
	sys.provider = apischema.ZramProviderGenerator
	f.write(ZramGeneratorPath, "")
	sys.devices[zramGeneratorSwapUnit] = sys.devices[zramService]
	f.write(zramGeneratorConfigPath, string(renderZramGeneratorConfig(7963*mib)))
	return f, sys
}

// setMemAvailable rewrites MemAvailable in the fixture's /proc/meminfo.
func (f fixture) setMemAvailable(bytes uint64) {
	f.t.Helper()
	var out []string
	for line := range strings.Lines(f.read(procMeminfo)) {
		if strings.HasPrefix(line, "MemAvailable:") {
			line = "MemAvailable:    " + strconv.FormatUint(bytes/kib, 10) + " kB\n"
		}
		out = append(out, line)
	}
	f.write(procMeminfo, strings.Join(out, ""))
}

// param reads a kernel parameter in the fixture.
func (f fixture) param(name string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// removeSwapsLine takes the line of the device at path out of the fixture's
// /proc/swaps.
func (f fixture) removeSwapsLine(path string) {
	f.t.Helper()
	var kept []string
	for i, line := range strings.SplitAfter(f.read(procSwaps), "\n") {
		if fields := strings.Fields(line); i > 0 && len(fields) > 0 && octalescape.Unescape(fields[0]) == path {
			continue
		}
		kept = append(kept, line)
	}
	f.write(procSwaps, strings.Join(kept, ""))
}

// remove removes a file of the fixture.
func (f fixture) remove(name string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.root, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		f.t.Fatal(err)
	}
}

// swapsLine is the /proc/swaps line of a swap file of length bytes.
func swapsLine(path string, length, used uint64) string {
	return fmt.Sprintf("%-40s file\t\t%d\t\t%d\t\t-2\n", path, length/kib-uint64(os.Getpagesize())/kib, used/kib)
}

// writeSwapFile makes a swap file of length bytes at name, sparse, with a
// swap signature, as mkswap leaves it.
func (f fixture) writeSwapFile(name string, length uint64) {
	f.t.Helper()
	f.writeSwapArea(name)
	if err := os.Truncate(filepath.Join(f.root, name), int64(length)); err != nil {
		f.t.Fatal(err)
	}
}

// statID is the identity of a fixture file.
func (s *fakeSystem) statID(path string) (fileID, error) {
	return s.f.host().fileIDOf(path)
}

// makeSwapFile makes the file in the fixture the way the real one does it on
// the host: a new name only, LinuxIO's folder first, then the length and the
// header. Its effect lands only when the call does not fail.
func (s *fakeSystem) makeSwapFile(ctx context.Context, path string, size uint64) (fileID, error) {
	var id fileID
	err := s.unitDo(ctx, "make "+path, func() error {
		if s.f.exists(path) {
			return fmt.Errorf("%w: %s is taken", bridgeipc.ErrConflict, path)
		}
		if path == ownSwapFilePath {
			s.f.mkdir(ownSwapDir)
		}
		s.f.writeSwapFile(path, size)
		var err error
		id, err = s.statID(path)
		return err
	})
	return id, err
}

// resizeSwapFile changes the length of an off swap file that is still the
// one id names and writes a new header, or none for a path in noHeader.
func (s *fakeSystem) resizeSwapFile(ctx context.Context, path string, id fileID, size uint64) error {
	return s.unitDo(ctx, "resize "+path, func() error {
		if strings.Contains(s.f.read(procSwaps), path+" ") {
			return fmt.Errorf("%s swaps", path)
		}
		if got, err := s.statID(path); err != nil || got != id {
			return fmt.Errorf("%w: %s is not the file that was checked", bridgeipc.ErrConflict, path)
		}
		if s.noHeader[path] {
			s.f.write(path, "")
			if err := os.Truncate(filepath.Join(s.f.root, path), int64(size)); err != nil {
				return err
			}
			return errNoSwapHeader
		}
		return os.Truncate(filepath.Join(s.f.root, path), int64(size))
	})
}

// deleteSwapFile deletes a swap file that does not swap and is still the
// one id names.
func (s *fakeSystem) deleteSwapFile(ctx context.Context, path string, id fileID) error {
	return s.unitDo(ctx, "delete "+path, func() error {
		swaps, err := s.f.host().readSwaps()
		if err != nil {
			return err
		}
		if slices.ContainsFunc(swaps, func(d apischema.SwapDevice) bool { return d.Path == path }) {
			return fmt.Errorf("%w: %s swaps", bridgeipc.ErrConflict, path)
		}
		got, err := s.statID(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || got != id {
			return fmt.Errorf("%w: %s is not the file that was checked", bridgeipc.ErrConflict, path)
		}
		s.f.remove(path)
		return nil
	})
}

// swapOn adds the file's line to /proc/swaps, as swapon does for a file
// with a swap header, and records the options it was given.
func (s *fakeSystem) swapOn(ctx context.Context, path, options string) error {
	return s.unitDo(ctx, "swapon "+path+" "+options, func() error {
		// The real swapOn refuses options swapon would not take.
		if _, err := swapOnFlags(path, options); err != nil {
			return err
		}
		swap, err := s.f.host().holdsSwap(path)
		if err != nil {
			return err
		}
		if !swap {
			return fmt.Errorf("swapon %s: read swap header failed", path)
		}
		if s.ignoredSwapOns[path] {
			return nil
		}
		info, err := os.Stat(filepath.Join(s.f.root, path))
		if err != nil {
			return err
		}
		s.f.write(procSwaps, s.f.read(procSwaps)+swapsLine(path, uint64(info.Size()), 0))
		return nil
	})
}

// addFstabSwap appends LinuxIO's line for path to the fixture's fstab.
func (s *fakeSystem) addFstabSwap(ctx context.Context, path string) error {
	return s.unitDo(ctx, "fstab add "+path, func() error {
		text := ""
		if s.f.exists(etcFstab) {
			text = s.f.read(etcFstab)
		}
		s.f.write(etcFstab, text+path+" none swap sw,nofail 0 0\n")
		return nil
	})
}

// removeFstabSwap drops every swap line of the fixture's fstab that names
// path.
func (s *fakeSystem) removeFstabSwap(ctx context.Context, path string) error {
	return s.unitDo(ctx, "fstab remove "+path, func() error {
		if !s.f.exists(etcFstab) {
			return nil
		}
		var kept []string
		for line := range strings.Lines(s.f.read(etcFstab)) {
			if fields := strings.Fields(line); len(fields) >= 3 && fields[2] == "swap" && octalescape.Unescape(fields[0]) == path {
				continue
			}
			kept = append(kept, line)
		}
		s.f.write(etcFstab, strings.Join(kept, ""))
		return nil
	})
}

// fixtureFolderFS is what readFolderFS reports in a test for every folder
// under the fixture root.
func (f fixture) useFolderFS(info folderFS) {
	f.t.Helper()
	previous := readFolderFS
	readFolderFS = func(dir string) (folderFS, error) {
		if dir != f.root && !strings.HasPrefix(dir, f.root+"/") {
			return folderFS{}, fmt.Errorf("%s is outside the fixture", dir)
		}
		return info, nil
	}
	f.t.Cleanup(func() { readFolderFS = previous })
}

// roomyExt4 is an ext4 filesystem of 100 GiB with 50 GiB free.
var roomyExt4 = folderFS{kind: "ext4", total: 100 * gib, free: 50 * gib}

// pageSize is the page /proc/swaps leaves out of a swap file's size.
func pageSize() uint64 { return uint64(os.Getpagesize()) }

// zramRequest is a switch to zram that confirms removing every swap file
// the host's status says it removes.
func zramRequest(t *testing.T, sys *fakeSystem) apischema.SwapModeRequest {
	t.Helper()
	status, _, err := sys.f.host().readSwapStatus(sys.provider)
	if err != nil {
		t.Fatal(err)
	}
	return apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmRemove: removableSwapFiles(status.SwapFiles)}
}

// zramReplacing is zramRequest that also confirms replacing the zram
// configuration file at path.
func zramReplacing(t *testing.T, sys *fakeSystem, path string) apischema.SwapModeRequest {
	t.Helper()
	req := zramRequest(t, sys)
	req.ConfirmReplace = path
	return req
}
