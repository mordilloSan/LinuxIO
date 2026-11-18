package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

const zram1SwapsLine = "/dev/zram1                              partition\t1048572\t\t0\t\t100\n"

func TestRenderZramGeneratorConfig(t *testing.T) {
	want := managedHeader + "[zram0]\nzram-size = 4096\ncompression-algorithm = zstd\nswap-priority = 100\n"
	if got := string(renderZramGeneratorConfig(4*gib + 500*kib)); got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	off := string(renderZramGeneratorOff())
	if !strings.HasPrefix(off, managedHeader) {
		t.Errorf("off config = %q, want the managed header first", off)
	}
	for line := range strings.Lines(off) {
		if !strings.HasPrefix(line, "#") {
			t.Errorf("off config line %q is not a comment: it must define no device", line)
		}
	}
}

// The provider is the tool installed, the generator found by its file under
// the root. With both installed it is the one whose unit runs zram now, and
// zram-tools when neither does or systemd cannot be asked.
func TestZramProvider(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	sbinDir := t.TempDir()
	savedDirs := hostcmd.SearchDirs
	hostcmd.SearchDirs = []string{sbinDir}
	t.Cleanup(func() { hostcmd.SearchDirs = savedDirs })
	f := newFixture(t)
	var states map[string]string
	var stateErrs map[string]error
	activeState := func(_ context.Context, unit string) (string, error) {
		if err := stateErrs[unit]; err != nil {
			return "", err
		}
		if state, ok := states[unit]; ok {
			return state, nil
		}
		return "inactive", nil
	}
	provider := func() apischema.ZramProvider { return f.host().zramProvider(t.Context(), activeState) }

	if got := provider(); got != "" {
		t.Fatalf("provider = %q, want none", got)
	}
	f.write(ZramGeneratorPath, "")
	if got := provider(); got != apischema.ZramProviderGenerator {
		t.Fatalf("provider = %q, want zram-generator", got)
	}
	if err := os.WriteFile(filepath.Join(sbinDir, zramCommand), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	noBus := errors.New("no system bus")
	for _, test := range []struct {
		name      string
		states    map[string]string
		stateErrs map[string]error
		want      apischema.ZramProvider
	}{
		{name: "the generator runs zram0", states: map[string]string{zramGeneratorSetupUnit: "active"}, want: apischema.ZramProviderGenerator},
		{name: "the generator runs zram0 and zramswap.service failed",
			states: map[string]string{zramGeneratorSetupUnit: "active", zramService: "failed"}, want: apischema.ZramProviderGenerator},
		{name: "zramswap.service reads unknown",
			states: map[string]string{zramGeneratorSetupUnit: "active", zramService: "unknown"}, want: apischema.ZramProviderTools},
		{name: "zramswap.service is activating",
			states: map[string]string{zramGeneratorSetupUnit: "active", zramService: "activating"}, want: apischema.ZramProviderTools},
		{name: "zramswap.service is reloading",
			states: map[string]string{zramGeneratorSetupUnit: "active", zramService: "reloading"}, want: apischema.ZramProviderTools},
		{name: "zram-tools runs zram", states: map[string]string{zramService: "active"}, want: apischema.ZramProviderTools},
		{name: "both run", states: map[string]string{zramGeneratorSetupUnit: "active", zramService: "active"}, want: apischema.ZramProviderTools},
		{name: "neither runs", want: apischema.ZramProviderTools},
		{name: "the setup service cannot be read", stateErrs: map[string]error{zramGeneratorSetupUnit: noBus}, want: apischema.ZramProviderTools},
		{name: "zramswap.service cannot be read", states: map[string]string{zramGeneratorSetupUnit: "active"},
			stateErrs: map[string]error{zramService: noBus}, want: apischema.ZramProviderTools},
	} {
		t.Run("both installed, "+test.name, func(t *testing.T) {
			states, stateErrs = test.states, test.stateErrs
			if got := provider(); got != test.want {
				t.Fatalf("provider = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReadSwapStatusReportsTheZramProvider(t *testing.T) {
	f, _ := diskHost(t)
	for _, provider := range []apischema.ZramProvider{"", apischema.ZramProviderTools, apischema.ZramProviderGenerator} {
		got, err := f.host().readSwapStatus(provider)
		if err != nil {
			t.Fatal(err)
		}
		if got.ZramProvider != provider || got.ZramAvailable != (provider != "") {
			t.Errorf("provider %q: status provider = %q, available = %v", provider, got.ZramProvider, got.ZramAvailable)
		}
	}
}

// zram-generator has no service to enable: the configuration is written, a
// reload makes the swap unit, and the swap unit is started.
func TestSetSwapModeToZramWithTheGenerator(t *testing.T) {
	f, sys := generatorDiskHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"write " + zramGeneratorConfigPath,
		"write " + sysctlDropInPath,
		"reload",
		"start " + zramGeneratorSwapUnit,
		"set " + swappinessParam + "=180",
		"set " + pageClusterParam + "=0",
		"set " + boostParam + "=0",
		"set " + scaleParam + "=125",
		"write " + zswapTmpfilesPath,
		"swapoff /swap.img",
		holdOffCall("swap.img.swap"),
		"reload",
	})
	if got.Mode != apischema.SwapModeZram || got.ZramProvider != apischema.ZramProviderGenerator {
		t.Errorf("status mode = %q provider = %q, want zram from zram-generator", got.Mode, got.ZramProvider)
	}
	if got, want := f.read(zramGeneratorConfigPath), string(renderZramGeneratorConfig(16308408*kib/2)); got != want {
		t.Errorf("generator config = %q, want %q", got, want)
	}
	if got := f.read(zramswapConfigPath); got != zramswapPackageDefault {
		t.Errorf("zramswap config = %q, want it untouched", got)
	}
}

// zram that already swaps is not restarted, since that empties it: the new
// configuration counts from the next boot.
func TestSetSwapModeToZramWithTheGeneratorKeepsARunningDevice(t *testing.T) {
	_, sys := generatorZramHost(t)

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "reload") || slices.Contains(sys.calls, "start "+zramGeneratorSwapUnit) {
		t.Fatalf("calls = %v, want a reload and no start", sys.calls)
	}
}

// Leaving zram stops the generator's swap unit, then writes a configuration
// with no device and reloads, so the next boot makes no zram.
func TestSetSwapModeToDiskFromZramWithTheGenerator(t *testing.T) {
	f, sys := generatorZramHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"remove " + holdOffDropInPath("swap.img.swap"),
		"reload",
		"start swap.img.swap",
		"stop " + zramGeneratorSwapUnit,
		"write " + zramGeneratorConfigPath,
		"reload",
		"write " + sysctlDropInPath,
		"set " + swappinessParam + "=60",
		"set " + pageClusterParam + "=3",
		"set " + boostParam + "=15000",
		"set " + scaleParam + "=10",
		"write " + zswapTmpfilesPath,
	})
	if got.Mode != apischema.SwapModeDisk {
		t.Errorf("status mode = %q, want disk", got.Mode)
	}
	if got, want := f.read(zramGeneratorConfigPath), string(renderZramGeneratorOff()); got != want {
		t.Errorf("generator config = %q, want %q", got, want)
	}
}

// With zram not running, leaving it only turns it off at boot.
func TestSetSwapModeWithTheGeneratorTurnsOffAZramThatIsNotRunning(t *testing.T) {
	f, sys := generatorDiskHost(t)
	f.write(zramGeneratorConfigPath, string(renderZramGeneratorConfig(4*gib)))

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "stop "+zramGeneratorSwapUnit) {
		t.Fatalf("calls = %v, want no stop", sys.calls)
	}
	if got, want := f.read(zramGeneratorConfigPath), string(renderZramGeneratorOff()); got != want {
		t.Errorf("generator config = %q, want %q", got, want)
	}
}

// zram0 swapping without the generator's setup service running was made by
// something else: it stays, and so does the configuration.
func TestSetSwapModeLeavesZramTheGeneratorDidNotMake(t *testing.T) {
	for _, state := range []string{"inactive", "failed"} {
		t.Run(state, func(t *testing.T) {
			f, sys := generatorZramHost(t)
			sys.activeStates[zramGeneratorSetupUnit] = state
			config := f.read(zramGeneratorConfigPath)

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(sys.calls, "stop "+zramGeneratorSwapUnit) || slices.Contains(sys.calls, "write "+zramGeneratorConfigPath) {
				t.Fatalf("calls = %v, want zram neither stopped nor turned off", sys.calls)
			}
			if got.Mode != apischema.SwapModeMixed || f.read(zramGeneratorConfigPath) != config {
				t.Fatalf("mode = %q, want mixed with the configuration as it was", got.Mode)
			}
		})
	}
}

// The setup service is the only sign that the generator made zram0, so a
// state that cannot be read leaves zram on.
func TestSetSwapModeLeavesZramWhenTheSetupServiceCannotBeRead(t *testing.T) {
	_, sys := generatorZramHost(t)
	sys.failures["active "+zramGeneratorSetupUnit] = errors.New("bus gone")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "stop "+zramGeneratorSwapUnit) || got.Mode != apischema.SwapModeMixed {
		t.Fatalf("mode = %q, calls = %v; want mixed and no stop", got.Mode, sys.calls)
	}
}

// A host that is already off at boot is not written or reloaded again.
func TestSetSwapModeWithTheGeneratorLeavesAnOffFileAlone(t *testing.T) {
	f, sys := generatorDiskHost(t)
	f.write(zramGeneratorConfigPath, string(renderZramGeneratorOff()))

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "write "+zramGeneratorConfigPath) || slices.Contains(sys.calls, "reload") {
		t.Fatalf("calls = %v, want no write of the generator configuration and no reload", sys.calls)
	}
}

// A step that fails after zram stopped puts the configuration back and
// reloads; zram stays stopped, since a rollback never restarts swap.
func TestSetSwapModeWithTheGeneratorRestoresTheConfigurationAfterAStop(t *testing.T) {
	f, sys := generatorZramHost(t)
	config := f.read(zramGeneratorConfigPath)
	sys.failures["set "+swappinessParam+"=60"] = errors.New("read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err == nil {
		t.Fatal("want an error")
	}
	if got := f.read(zramGeneratorConfigPath); got != config {
		t.Errorf("generator config = %q, want %q back", got, config)
	}
	if strings.Contains(f.read(procSwaps), "/dev/zram0") || slices.Contains(sys.calls, "start "+zramGeneratorSwapUnit) {
		t.Errorf("calls = %v, want zram left stopped", sys.calls)
	}
	restored := slices.Index(sys.calls, "write "+zramGeneratorConfigPath)
	for i, call := range sys.calls {
		if call == "write "+zramGeneratorConfigPath {
			restored = i
		}
	}
	if restored < 0 || !slices.Contains(sys.calls[restored:], "reload") {
		t.Errorf("calls = %v, want a reload after the configuration came back", sys.calls)
	}
}

// LinuxIO's configuration holds zram0 alone, so a second zram device belongs
// to a drop-in or another tool: nothing is stopped.
func TestSetSwapModeLeavesZramWhenAnotherDeviceSwaps(t *testing.T) {
	f, sys := generatorZramHost(t)
	f.write(procSwaps, f.read(procSwaps)+zram1SwapsLine)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "stop "+zramGeneratorSwapUnit) || slices.Contains(sys.calls, "write "+zramGeneratorConfigPath) {
		t.Fatalf("calls = %v, want zram neither stopped nor turned off", sys.calls)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Fatalf("mode = %q, want mixed", got.Mode)
	}
}

// A zram device that never appears fails the step; the rollback stops the
// unit it started, removes the configuration it wrote and reloads, so no
// generated unit outlives its file.
func TestSetSwapModeWithTheGeneratorRollsBackWhenZramNeverAppears(t *testing.T) {
	f, sys := generatorDiskHost(t)
	sys.stuck[zramGeneratorSwapUnit] = true

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepStartZram)) || !errors.Is(err, errWaitTimedOut) {
		t.Fatalf("err = %v, want the start_zram step to time out", err)
	}
	assertCalls(t, sys.calls, []string{
		"write " + zramGeneratorConfigPath,
		"write " + sysctlDropInPath,
		"reload",
		"start " + zramGeneratorSwapUnit,
		// rollback
		"stop " + zramGeneratorSwapUnit,
		"remove " + sysctlDropInPath,
		"remove " + zramGeneratorConfigPath,
		"reload",
	})
	if f.exists(zramGeneratorConfigPath) {
		t.Error("generator configuration left behind")
	}
}

// A configuration that was there before the run comes back as it was.
func TestSetSwapModeWithTheGeneratorRestoresTheConfigurationItReplaced(t *testing.T) {
	f, sys := generatorDiskHost(t)
	const admins = "[zram0]\nzram-size = ram / 4\n"
	f.write(zramGeneratorConfigPath, admins)
	sys.failures["reload"] = errors.New("reload refused")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: zramGeneratorConfigPath}); err == nil {
		t.Fatal("want an error")
	}
	if got := f.read(zramGeneratorConfigPath); got != admins {
		t.Errorf("generator config = %q, want %q back", got, admins)
	}
	if slices.Contains(sys.calls, "stop "+zramGeneratorSwapUnit) {
		t.Fatalf("calls = %v, want no stop of a unit that was never made", sys.calls)
	}
}
