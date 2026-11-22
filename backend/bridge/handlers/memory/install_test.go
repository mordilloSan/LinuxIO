package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// noZramToolsHost is diskHost with no swap active and zram-tools not
// installed, as the Ubuntu test VM is: systemd knows no zramswap.service.
func noZramToolsHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader)
	sys.failures["state "+zramService] = fmt.Errorf("get unit file state %s: %w", zramService, systemd.ErrNoUnitFile)
	return f, sys
}

// installZramTools does to the fake what zram-tools' postinst does: the unit
// file appears, deb-systemd-helper enables it, and deb-systemd-invoke starts
// it, which systemd skips while the hold's condition does not hold.
func (s *fakeSystem) installZramTools(t *testing.T) {
	t.Helper()
	delete(s.failures, "state "+zramService)
	s.fileStates[zramService] = "enabled"
	if err := s.startUnit(context.Background(), zramService); err != nil {
		t.Fatal(err)
	}
}

// hold takes the hold as the installer does before the package step. The
// step it returns is run at the end of the test if the test did not run it,
// so a test that fails cannot leave the package-global lock taken.
func hold(t *testing.T, e *executor) func(context.Context) (string, error) {
	t.Helper()
	after, err := e.holdZramToolsOff(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	t.Cleanup(func() {
		if !ran {
			_, _ = after(context.Background())
		}
	})
	return func(ctx context.Context) (string, error) {
		ran = true
		return after(ctx)
	}
}

// assertLockFree fails when the memory mutation lock is still held.
func assertLockFree(t *testing.T) {
	t.Helper()
	unlock, err := lockMutation()
	if err != nil {
		t.Fatalf("the memory mutation lock is still held: %v", err)
	}
	unlock()
}

// The package's start is skipped while the hook holds the unit, so zram
// never swaps; after the package, the hold goes and the unit the package
// enabled is disabled: the host is as it was before the install.
func TestZramToolsInstallHookKeepsZramOff(t *testing.T) {
	f, sys := noZramToolsHost(t)
	after := hold(t, sys.executor(nil))

	assertCalls(t, sys.calls, []string{"write " + zramInstallHoldPath, "reload"})
	if got := f.read(zramInstallHoldPath); got != string(renderZramInstallHold()) {
		t.Errorf("hold = %q", got)
	}
	if _, lockErr := lockMutation(); lockErr == nil {
		t.Fatal("the lock is free during the package step, want it held")
	}

	sys.installZramTools(t)
	if swaps := f.read(procSwaps); strings.Contains(swaps, "/dev/zram") {
		t.Fatalf("/proc/swaps = %q during the install, want no zram", swaps)
	}
	sys.calls = nil
	warning, err := after(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{"disable " + zramService, "remove " + zramInstallHoldPath, "reload"})
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
	if state := sys.fileStates[zramService]; state != "disabled" {
		t.Errorf("%s is %s, want disabled", zramService, state)
	}
	if f.exists(zramInstallHoldPath) || f.exists(filepath.Dir(zramInstallHoldPath)) {
		t.Error("the hold or its folder is still there")
	}
	assertLockFree(t)
}

// An admin's own zramswap.service that is enabled stays enabled: its start
// at boot was theirs before the install.
func TestZramToolsInstallHookKeepsAnEnabledUnitEnabled(t *testing.T) {
	_, sys := noZramToolsHost(t)
	delete(sys.failures, "state "+zramService)
	sys.fileStates[zramService] = "enabled"
	after := hold(t, sys.executor(nil))
	sys.installZramTools(t)
	sys.calls = nil

	if _, err := after(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{"remove " + zramInstallHoldPath, "reload"})
	if state := sys.fileStates[zramService]; state != "enabled" {
		t.Errorf("%s is %s, want enabled", zramService, state)
	}
}

// After a package step that failed, as one cancelled before dpkg ran, the
// hold still goes and the lock is released; with no unit there is nothing
// to disable.
func TestZramToolsInstallHookReleasesAfterAFailedPackageStep(t *testing.T) {
	f, sys := noZramToolsHost(t)
	after := hold(t, sys.executor(nil))
	sys.calls = nil

	if _, err := after(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{"remove " + zramInstallHoldPath, "reload"})
	if f.exists(zramInstallHoldPath) {
		t.Error("the hold is still there")
	}
	assertLockFree(t)
}

// While another memory change runs the hook changes nothing, and says what
// the package does instead.
func TestZramToolsInstallHookChangesNothingWhileAnotherChangeRuns(t *testing.T) {
	f, sys := noZramToolsHost(t)
	unlock, err := lockMutation()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	after, err := sys.executor(nil).holdZramToolsOff(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	warning, err := after(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, nil)
	if f.exists(zramInstallHoldPath) {
		t.Error("the hold was written")
	}
	if !strings.HasPrefix(warning, "Another memory change was running") || !strings.Contains(warning, "which turns zram on") || strings.Contains(warning, "left on") {
		t.Errorf("warning = %q, want why and what the package does, and no claim about what swaps", warning)
	}
}

// A hold a killed job host left in /run is LinuxIO's by its content: it is
// written again and removed at the end.
func TestZramToolsInstallHookTakesOverItsOwnLeftoverHold(t *testing.T) {
	f, sys := noZramToolsHost(t)
	f.write(zramInstallHoldPath, string(renderZramInstallHold()))
	after := hold(t, sys.executor(nil))
	sys.installZramTools(t)

	if _, err := after(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.exists(zramInstallHoldPath) {
		t.Error("the leftover hold is still there")
	}
}

// A file at the hold's path that LinuxIO did not write is left as it is,
// nothing else is changed, and the result says so. That file may hold the
// unit itself, so the warning does not say the package starts zram.
func TestZramToolsInstallHookLeavesAFileItDidNotWrite(t *testing.T) {
	f, sys := noZramToolsHost(t)
	const admins = "[Service]\nNice=5\n"
	f.write(zramInstallHoldPath, admins)

	after, err := sys.executor(nil).holdZramToolsOff(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	warning, err := after(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, nil)
	if got := f.read(zramInstallHoldPath); got != admins {
		t.Errorf("the file = %q, want it left as it was", got)
	}
	if !strings.Contains(warning, zramInstallHoldPath) || !strings.Contains(warning, "which may turn zram on") {
		t.Errorf("warning = %q, want it to name the file and say the package may start zram", warning)
	}
	assertLockFree(t)
}

// What the hook cannot read or write before the package step fails it with
// nothing left behind: no hold, and the lock released.
func TestZramToolsInstallHookFailsBeforeThePackageWithNothingLeft(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(f fixture, sys *fakeSystem)
		wantErr   string
		wantCalls []string
	}{
		{
			name: "the unit's state cannot be read",
			prepare: func(_ fixture, sys *fakeSystem) {
				sys.failures["state "+zramService] = errors.New("bus closed")
			},
			wantErr: "bus closed",
		},
		{
			name: "what is at the hold's path cannot be read",
			prepare: func(f fixture, _ *fakeSystem) {
				if err := os.MkdirAll(filepath.Join(f.root, zramInstallHoldPath), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "read " + zramInstallHoldPath,
		},
		{
			name: "the hold cannot be written",
			prepare: func(_ fixture, sys *fakeSystem) {
				sys.failures["write "+zramInstallHoldPath] = syscall.EROFS
			},
			wantErr:   "write " + zramInstallHoldPath,
			wantCalls: []string{"write " + zramInstallHoldPath, "remove " + zramInstallHoldPath, "reload"},
		},
		{
			name: "systemd cannot reload",
			prepare: func(_ fixture, sys *fakeSystem) {
				sys.failAfter["reload"] = errors.New("reload timed out")
			},
			wantErr:   "reload timed out",
			wantCalls: []string{"write " + zramInstallHoldPath, "reload", "remove " + zramInstallHoldPath, "reload"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := noZramToolsHost(t)
			test.prepare(f, sys)

			after, err := sys.executor(nil).holdZramToolsOff(t.Context())
			if err == nil || after != nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, after = %v; want %q and no step", err, after != nil, test.wantErr)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("err = %q, want one line", err)
			}
			assertCalls(t, sys.calls, test.wantCalls)
			if data, readErr := os.ReadFile(filepath.Join(f.root, zramInstallHoldPath)); readErr == nil && string(data) == string(renderZramInstallHold()) {
				t.Error("the hold was left behind")
			}
			assertLockFree(t)
		})
	}
}

// A step after the package that fails says what state the host is left in,
// on one line, and still releases the hold and the lock when it can.
func TestZramToolsInstallHookSaysWhatStaysWhenAStepFails(t *testing.T) {
	disableErr := errors.New("disable unit files zramswap.service: access denied")
	removeErr := syscall.EROFS
	tests := []struct {
		name      string
		failures  map[string]error
		wantCalls []string
		wantText  []string
	}{
		{
			name:      "the disable",
			failures:  map[string]error{"disable " + zramService: disableErr},
			wantCalls: []string{"disable " + zramService, "remove " + zramInstallHoldPath, "reload"},
			wantText:  []string{"zramswap.service starts zram at the next boot"},
		},
		{
			name:      "the state read",
			failures:  map[string]error{"state " + zramService: errors.New("get unit file state zramswap.service: bus closed")},
			wantCalls: []string{"remove " + zramInstallHoldPath, "reload"},
			wantText:  []string{"zramswap.service may start zram at the next boot"},
		},
		{
			name:      "the removal",
			failures:  map[string]error{"remove " + zramInstallHoldPath: removeErr},
			wantCalls: []string{"disable " + zramService, "remove " + zramInstallHoldPath},
			wantText:  []string{"zramswap.service cannot start until"},
		},
		{
			name:      "the disable and the removal",
			failures:  map[string]error{"disable " + zramService: disableErr, "remove " + zramInstallHoldPath: removeErr},
			wantCalls: []string{"disable " + zramService, "remove " + zramInstallHoldPath},
			wantText:  []string{"starts zram at the next boot", "cannot start until"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, sys := noZramToolsHost(t)
			after := hold(t, sys.executor(nil))
			sys.installZramTools(t)
			maps.Copy(sys.failures, test.failures)
			sys.calls = nil

			_, err := after(t.Context())
			assertSaysWhatStays(t, err, test.wantText, test.failures)
			assertCalls(t, sys.calls, test.wantCalls)
			assertLockFree(t)
		})
	}
}

// assertSaysWhatStays checks an error of the step after the package: it
// holds each of texts, wraps each cause, is one line and names the unit file
// state read once.
func assertSaysWhatStays(t *testing.T, err error, texts []string, causes map[string]error) {
	t.Helper()
	if err == nil {
		t.Fatal("no error, want what stays")
	}
	for _, text := range texts {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("err = %v, want %q", err, text)
		}
	}
	for _, cause := range causes {
		if !errors.Is(err, cause) {
			t.Errorf("err = %v, want it to wrap %v", err, cause)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("err = %q, want one line", err)
	}
	if strings.Count(err.Error(), "get unit file state") > 1 {
		t.Errorf("err = %v, want the operation named once", err)
	}
}

// The release has a time budget of its own: a disable that uses up the
// context it was given still leaves the reload room, and so does a context
// that is done before the step starts.
func TestZramToolsInstallHookReleasesOnItsOwnBudget(t *testing.T) {
	t.Run("a disable that uses up the context", func(t *testing.T) {
		f, sys := noZramToolsHost(t)
		after := hold(t, sys.executor(nil))
		sys.installZramTools(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sys.after = func(call string) {
			if call == "disable "+zramService {
				cancel()
			}
		}
		sys.calls = nil

		if _, err := after(ctx); err != nil {
			t.Fatal(err)
		}
		assertCalls(t, sys.calls, []string{"disable " + zramService, "remove " + zramInstallHoldPath, "reload"})
		if f.exists(zramInstallHoldPath) {
			t.Error("the hold is still there")
		}
	})
	t.Run("a done context", func(t *testing.T) {
		f, sys := noZramToolsHost(t)
		after := hold(t, sys.executor(nil))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if _, err := after(ctx); err != nil {
			t.Fatal(err)
		}
		if f.exists(zramInstallHoldPath) {
			t.Error("the hold is still there")
		}
		assertLockFree(t)
	})
}

// A hold a killed job host left behind is LinuxIO's by its content, and the
// zram-tools start step of a switch to zram removes it first, so zram
// starts; it logs that it did.
func TestSetSwapModeToZramClearsALeftoverInstallHold(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zramInstallHoldPath, string(renderZramInstallHold()))
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"write " + zramswapConfigPath,
		"write " + sysctlDropInPath,
		"enable " + zramService,
		"remove " + zramInstallHoldPath,
		"reload",
		"start " + zramService,
		"set " + swappinessParam + "=180",
		"set " + pageClusterParam + "=0",
		"set " + boostParam + "=0",
		"set " + scaleParam + "=125",
		"write " + zswapTmpfilesPath,
		"swapoff /swap.img",
		"fstab remove /swap.img",
		"reload",
		"delete /swap.img",
	})
	if got.Mode != apischema.SwapModeZram {
		t.Errorf("mode = %q, want zram", got.Mode)
	}
	if f.exists(zramInstallHoldPath) || f.exists(filepath.Dir(zramInstallHoldPath)) {
		t.Error("the leftover hold or its folder is still there")
	}
	if !strings.Contains(logs.String(), "leftover zram-tools install hold removed") {
		t.Errorf("logs = %q, want the removal logged", logs.String())
	}
}

// Any other file at the hold's path is left alone, and the start goes on as
// it would without it.
func TestSetSwapModeToZramLeavesAFileAtTheHoldsPathItDidNotWrite(t *testing.T) {
	f, sys := diskHost(t)
	const admins = "[Service]\nNice=5\n"
	f.write(zramInstallHoldPath, admins)

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.calls {
		if call == "remove "+zramInstallHoldPath {
			t.Fatalf("calls = %v, want the file left alone", sys.calls)
		}
	}
	if got.Mode != apischema.SwapModeZram {
		t.Errorf("mode = %q, want zram", got.Mode)
	}
	if got := f.read(zramInstallHoldPath); got != admins {
		t.Errorf("the file = %q, want it left as it was", got)
	}
}
