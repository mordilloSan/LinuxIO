package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestSetSwappinessWritesLiveThenTheDropIn(t *testing.T) {
	f, sys := diskHost(t)
	f.write(sysctlDropInPath, "vm.page-cluster = 0\nvm.swappiness = 180\nvm.vfs_cache_pressure = 50\n")

	got, err := sys.executor(nil).setSwappiness(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{"set " + swappinessParam + "=100", "write " + sysctlDropInPath})
	if got.Swappiness != 100 {
		t.Errorf("status swappiness = %d, want 100", got.Swappiness)
	}
	want := "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
		"vm.page-cluster = 0\nvm.swappiness = 100\nvm.vfs_cache_pressure = 50\n"
	if got := f.read(sysctlDropInPath); got != want {
		t.Errorf("drop-in = %q, want %q", got, want)
	}
}

func TestSetSwappinessRange(t *testing.T) {
	for _, value := range []int{0, 200} {
		_, sys := diskHost(t)
		if _, err := sys.executor(nil).setSwappiness(t.Context(), value); err != nil {
			t.Errorf("swappiness %d: %v", value, err)
		}
	}
	for _, value := range []int{-1, 201} {
		_, sys := diskHost(t)
		if _, err := sys.executor(nil).setSwappiness(t.Context(), value); !errors.Is(err, bridgeipc.ErrInvalidArgs) || len(sys.calls) != 0 {
			t.Errorf("swappiness %d: err = %v, calls = %v; want invalid arguments and no change", value, err, sys.calls)
		}
	}
}

// TuneD sets swappiness itself on every profile switch, so LinuxIO does not.
func TestSetSwappinessRefusesUnderTuned(t *testing.T) {
	_, sys := diskHost(t)
	sys.tuned = true

	if _, err := sys.executor(nil).setSwappiness(t.Context(), 100); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// A kernel that reads back another value (one capped at 100 refuses or
// clamps 180) fails the change and gets its previous value back.
func TestSetSwappinessChecksTheReadBack(t *testing.T) {
	f, sys := diskHost(t)
	sys.after = func(call string) {
		if call == "set "+swappinessParam+"=180" {
			f.write(swappinessParam, "100\n")
		}
	}

	if _, err := sys.executor(nil).setSwappiness(t.Context(), 180); err == nil || !strings.Contains(err.Error(), "100") {
		t.Fatalf("err = %v, want the read-back mismatch", err)
	}
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %q, want 60 back", got)
	}
	if f.exists(sysctlDropInPath) {
		t.Error("drop-in written after a failed live change")
	}
}

func TestSetSwappinessRestoresTheLiveValueWhenTheDropInFails(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["write "+sysctlDropInPath] = errors.New("read-only file system")

	_, err := sys.executor(nil).setSwappiness(t.Context(), 100)
	if err == nil || !strings.Contains(err.Error(), "read-only file system") {
		t.Fatalf("err = %v, want the drop-in error", err)
	}
	assertCalls(t, sys.calls, []string{
		"set " + swappinessParam + "=100",
		"write " + sysctlDropInPath,
		"set " + swappinessParam + "=60",
		"remove " + sysctlDropInPath,
	})
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %q, want 60 back", got)
	}
}

// A live write that lands and still reports an error is put back.
func TestSetSwappinessUndoesALiveWriteThatLanded(t *testing.T) {
	f, sys := diskHost(t)
	sys.failAfter["set "+swappinessParam+"=100"] = errors.New("input/output error")

	if _, err := sys.executor(nil).setSwappiness(t.Context(), 100); err == nil {
		t.Fatal("want an error")
	}
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %q, want 60 back", got)
	}
	if f.exists(sysctlDropInPath) {
		t.Error("drop-in written after a failed live change")
	}
}

// A drop-in write that lands and still reports an error is undone with the
// live value, so the next boot does not apply what the change gave up.
func TestSetSwappinessUndoesADropInWriteThatLanded(t *testing.T) {
	f, sys := diskHost(t)
	f.write(sysctlDropInPath, "vm.swappiness = 30\n")
	sys.failAfter["write "+sysctlDropInPath] = errors.New("input/output error")

	if _, err := sys.executor(nil).setSwappiness(t.Context(), 100); err == nil {
		t.Fatal("want an error")
	}
	if got := f.read(sysctlDropInPath); got != "vm.swappiness = 30\n" {
		t.Errorf("drop-in = %q, want the previous content back", got)
	}
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %q, want 60 back", got)
	}
}

func TestSetSwappinessJoinsAFailedRestore(t *testing.T) {
	_, sys := diskHost(t)
	sys.failures["write "+sysctlDropInPath] = errors.New("read-only file system")
	sys.failures["set "+swappinessParam+"=60"] = errors.New("swappiness refused")

	_, err := sys.executor(nil).setSwappiness(t.Context(), 100)
	if err == nil || !strings.Contains(err.Error(), "read-only file system") || !strings.Contains(err.Error(), "swappiness refused") {
		t.Fatalf("err = %v, want both errors", err)
	}
}

// The progress detail fills the common envelope: the step as its phase and
// the share of steps done.
func TestSwapProgressEnvelope(t *testing.T) {
	got := apischema.SwapProgress{Step: apischema.SwapStepStopSwapFile, Index: 5, Total: 5}.ProgressEnvelope()
	if got.Phase != "stop_swap_file" || got.Percentage == nil || *got.Percentage != 80 || got.Message == "" {
		t.Fatalf("envelope = %+v, want phase stop_swap_file at 80%% with a message", got)
	}
}
