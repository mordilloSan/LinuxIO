package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// fakeSystem plays systemd and the kernel for the executor. Files and kernel
// parameters land in the fixture root; starting or stopping a unit adds or
// removes its device's line in the fixture's /proc/swaps, as swapon and
// swapoff would. Every side effect is recorded in order.
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
	// fileStates are unit file states; a unit not listed is "disabled".
	fileStates map[string]string
	// after runs after a recorded call succeeds.
	after func(call string)
}

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
			"swap.img.swap":       swapFileSwapsLine,
		},
		stuck:      map[string]bool{},
		fileStates: map[string]string{},
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
		waits: waits{swapOn: 200 * time.Millisecond, swapOff: 200 * time.Millisecond, poll: time.Millisecond, stopGrace: 100 * time.Millisecond},
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

func (s *fakeSystem) status(context.Context) (apischema.SwapStatus, error) {
	status, err := s.f.host().readSwapStatus(s.provider)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	status.TunedActive = s.tuned
	return status, nil
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
		s.swapOn(name)
		return nil
	})
}

func (s *fakeSystem) stopUnit(ctx context.Context, name string) error {
	return s.unitDo(ctx, "stop "+name, func() error {
		if stop := s.stops[name]; stop != nil {
			stop.pending = true
			return nil
		}
		s.swapOff(name)
		return nil
	})
}

func (s *fakeSystem) swapOn(name string) {
	if line, ok := s.devices[name]; ok && !s.stuck[name] {
		swaps := s.f.read(procSwaps)
		if !strings.Contains(swaps, line) {
			s.f.write(procSwaps, swaps+line)
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

func (s *fakeSystem) swapOff(name string) {
	if line, ok := s.devices[name]; ok && !s.stuck[name] {
		s.f.write(procSwaps, strings.ReplaceAll(s.f.read(procSwaps), line, ""))
		s.activeStates[name] = "inactive"
		s.bindSetupUnit(name, "inactive")
	}
}

// finishStop ends a scripted stop now, as a swapoff that outlived the route.
func (s *fakeSystem) finishStop(name string) {
	stop := s.stops[name]
	stop.pending = false
	if stop.ends != "" {
		if stop.leaves {
			s.swapOff(name)
		}
		s.activeStates[name] = stop.ends
		return
	}
	s.swapOff(name)
	if stop.restart {
		stop.restart = false
		s.swapOn(name)
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

// maskUnit leaves what systemd leaves: a link to /dev/null in
// /etc/systemd/system.
func (s *fakeSystem) maskUnit(ctx context.Context, name string) error {
	return s.unitDo(ctx, "mask "+name, func() error {
		s.fileStates[name] = "masked"
		s.f.maskInFixture(name)
		return nil
	})
}

func (s *fakeSystem) unmaskUnit(ctx context.Context, name string) error {
	return s.unitDo(ctx, "unmask "+name, func() error {
		s.fileStates[name] = "generated"
		if err := os.Remove(filepath.Join(s.f.root, "/etc/systemd/system", name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

// maskInFixture masks a unit in the fixture the way systemctl mask does.
func (f fixture) maskInFixture(unit string) {
	f.t.Helper()
	_ = os.Remove(filepath.Join(f.root, "/etc/systemd/system", unit))
	f.symlink("/dev/null", "/etc/systemd/system/"+unit)
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

// diskHost is a desktop with an active /swap.img from fstab holding 512 MiB,
// zswap off with lzo, zram-tools installed and its zram not running.
func diskHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f := newFixture(t)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine)
	f.write(procMeminfo, meminfoText)
	f.write(etcFstab, "UUID=0a1b2c3d / ext4 defaults 0 1\n/swap.img none swap sw 0 0\n")
	f.write("/swap.img", "")
	f.writeVMSysctls("60", "3", "15000", "10")
	f.write("/sys/module/zswap/parameters/enabled", "N\n")
	f.write("/sys/module/zswap/parameters/compressor", "lzo\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")
	f.write("/sys/block/zram0/disksize", "0\n")
	f.write(zramswapConfigPath, zramswapPackageDefault)
	return f, newFakeSystem(f)
}

// zramHost is diskHost after a switch to zram: zram0 swapping 256 MiB, the
// swap file stopped and masked, the zram presets live and in the drop-in.
func zramHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/dev/zram0                              partition\t8154200\t\t262144\t\t100\n")
	sys.devices[zramService] = "/dev/zram0                              partition\t8154200\t\t262144\t\t100\n"
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
	sys.fileStates["swap.img.swap"] = "masked"
	f.maskInFixture("swap.img.swap")
	return f, sys
}

// generatorDiskHost is diskHost on a host where zram-generator, and not
// zram-tools, sets zram up; it has no configuration yet.
func generatorDiskHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	sys.provider = apischema.ZramProviderGenerator
	return f, sys
}

// generatorZramHost is zramHost with zram0 made by zram-generator from
// LinuxIO's configuration.
func generatorZramHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := zramHost(t)
	sys.provider = apischema.ZramProviderGenerator
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
