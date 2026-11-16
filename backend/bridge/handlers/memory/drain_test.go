package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

const (
	partitionUnit      = `dev-disk-by\x2duuid-7e8f.swap`
	partitionSwapsLine = "/dev/sda3                               partition\t8388604\t\t262144\t\t-3\n"
	unmanagedSwapsLine = "/home/extra                             file\t\t1048572\t\t131072\t\t-4\n"
)

// drainHost has data parked on /swap.img and on a partition fstab names by
// UUID; the partition's unit is named after its /dev/disk/by-uuid link.
func drainHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(etcFstab, "/swap.img none swap sw 0 0\nUUID=7e8f none swap sw 0 0\n")
	f.write("/dev/sda3", "")
	f.symlink("../../sda3", "/dev/disk/by-uuid/7e8f")
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+partitionSwapsLine)
	sys.devices[partitionUnit] = partitionSwapsLine
	return f, sys
}

func TestDrainSwapStopsAndStartsEachDeviceInTurn(t *testing.T) {
	f, sys := drainHost(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).drainSwap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"stop swap.img.swap",
		"start swap.img.swap",
		"stop " + partitionUnit,
		"start " + partitionUnit,
	})
	want := []apischema.SwapProgress{
		{Step: apischema.SwapStepDrainDevice, Index: 1, Total: 2, Device: "/swap.img"},
		{Step: apischema.SwapStepDrainDevice, Index: 2, Total: 2, Device: "/dev/sda3"},
	}
	if !slices.Equal(progress, want) {
		t.Errorf("progress = %+v, want %+v", progress, want)
	}
	if len(got.Devices) != 2 || !strings.Contains(f.read(procSwaps), "/dev/sda3") {
		t.Errorf("devices = %+v, want both back", got.Devices)
	}
}

// A device whose unit is masked could not be started again, so the drain
// refuses before stopping anything.
func TestDrainSwapRefusesAMaskedUnit(t *testing.T) {
	_, sys := drainHost(t)
	sys.fileStates[partitionUnit] = "masked"

	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// Room is checked per device on fresh status; a device already drained
// stays drained when the next one no longer fits.
func TestDrainSwapRefusesADeviceWithoutRoom(t *testing.T) {
	f, sys := drainHost(t)
	sys.after = func(call string) {
		if call == "start swap.img.swap" {
			f.setMemAvailable(600 * mib)
		}
	}

	_, err := sys.executor(nil).drainSwap(t.Context())
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	assertCalls(t, sys.calls, []string{"stop swap.img.swap", "start swap.img.swap"})
}

func TestDrainSwapRefusesWithoutRoomForTheParkedData(t *testing.T) {
	f, sys := drainHost(t)
	f.setMemAvailable(1 * gib)

	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// Data parked only on a device fstab does not name cannot be drained: its
// unit could not be started again by name.
func TestDrainSwapRefusesAnUnmanagedDevice(t *testing.T) {
	f, sys := drainHost(t)
	f.write(procSwaps, swapsHeader+unmanagedSwapsLine)
	f.write("/home/extra", "")

	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// Beside a managed device, an unmanaged one is left untouched.
func TestDrainSwapLeavesAnUnmanagedDeviceAlone(t *testing.T) {
	f, sys := drainHost(t)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+unmanagedSwapsLine)
	f.write("/home/extra", "")

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{"stop swap.img.swap", "start swap.img.swap"})
}

func TestDrainSwapRefusesWithNothingParked(t *testing.T) {
	f, sys := drainHost(t)
	f.write(procSwaps, swapsHeader+"/swap.img                               file\t\t2097148\t\t0\t\t-2\n")

	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// A request cancelled once a device has left /proc/swaps still brings the
// device back: the start runs on a context the cancel does not reach.
func TestDrainSwapStartsAStoppedDeviceAfterACancel(t *testing.T) {
	f, sys := drainHost(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "stop swap.img.swap" {
			cancel()
		}
	}

	if _, err := sys.executor(nil).drainSwap(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancel", err)
	}
	if !slices.Contains(sys.calls, "start swap.img.swap") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want swap.img.swap started again", sys.calls)
	}
}

// A stop call that returns an error may still have reached systemd, as when
// ctx ends during the D-Bus round trip: a start is queued behind it so the
// device comes back, and the error names the unit.
func TestDrainSwapQueuesAStartWhenTheStopCallFails(t *testing.T) {
	f, sys := drainHost(t)
	sys.failAfter["stop swap.img.swap"] = context.Canceled

	_, err := sys.executor(nil).drainSwap(t.Context())
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "swap.img.swap") || !strings.Contains(err.Error(), "may still") {
		t.Fatalf("err = %v, want the cancel, the unit and that the stop may still take effect", err)
	}
	if !slices.Contains(sys.calls, "start swap.img.swap") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want swap.img.swap started again", sys.calls)
	}
}

// A request cancelled while swapoff runs queues a start behind the stop, so
// the device is back once the swapoff finishes after the route returned.
func TestDrainSwapQueuesAStartWhenCancelledDuringSwapoff(t *testing.T) {
	f, sys := drainHost(t)
	sys.stops["swap.img.swap"] = &fakeStop{polls: 1 << 30}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "stop swap.img.swap" {
			cancel()
		}
	}

	if _, err := sys.executor(nil).drainSwap(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancel", err)
	}
	if !slices.Contains(sys.calls, "start swap.img.swap") {
		t.Fatalf("calls = %v, want a start queued", sys.calls)
	}
	sys.finishStop("swap.img.swap")
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatal("the device stayed off after its swapoff finished")
	}
}

// systemd giving up on a swapoff fails the drain at once with the device
// still active, and the next device is not touched.
func TestDrainSwapFailsFastWhenSystemdGivesUp(t *testing.T) {
	f, sys := drainHost(t)
	sys.stops["swap.img.swap"] = &fakeStop{polls: 3, ends: "active"}
	e := sys.executor(nil)
	e.waits.swapOff = time.Minute

	start := time.Now()
	_, err := e.drainSwap(t.Context())
	if !errors.Is(err, errStopAbandoned) || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("err = %v, want systemd giving up reported", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want as soon as systemd gave up", elapsed)
	}
	if slices.Contains(sys.calls, "stop "+partitionUnit) || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want the drain stopped with /swap.img active", sys.calls)
	}
}

func TestDrainSwapWaitsForASlowSwapoff(t *testing.T) {
	f, sys := drainHost(t)
	sys.stops["swap.img.swap"] = &fakeStop{queued: 2, polls: 4}

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "stop "+partitionUnit) || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want both devices drained and back", sys.calls)
	}
}

// A unit masked on disk shows in the status as unit_masked, and the drain
// refuses it the same way, before stopping anything.
func TestDrainSwapAndStatusAgreeOnAMaskedUnit(t *testing.T) {
	f, sys := drainHost(t)
	f.maskInFixture(partitionUnit)

	status, err := sys.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.CanDrain || status.DrainBlockedReason != apischema.SwapDrainUnitMasked {
		t.Fatalf("can drain = %v, reason = %q; want false, unit_masked", status.CanDrain, status.DrainBlockedReason)
	}
	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// A device that does not come back is reported off by its unit, and the
// drain stops there.
func TestDrainSwapReportsADeviceItCouldNotStart(t *testing.T) {
	tests := map[string]func(sys *fakeSystem){
		"start fails": func(sys *fakeSystem) {
			sys.failures["start swap.img.swap"] = errors.New("unit swap.img.swap failed")
		},
		"device never returns": func(sys *fakeSystem) {
			sys.after = func(call string) {
				if call == "stop swap.img.swap" {
					sys.stuck["swap.img.swap"] = true
				}
			}
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			_, sys := drainHost(t)
			setup(sys)

			_, err := sys.executor(nil).drainSwap(t.Context())
			if err == nil || !strings.Contains(err.Error(), "is off") || !strings.Contains(err.Error(), "swap.img.swap") {
				t.Fatalf("err = %v, want the device reported off with its unit", err)
			}
			if slices.Contains(sys.calls, "stop "+partitionUnit) {
				t.Fatalf("calls = %v, want the drain stopped at the first device", sys.calls)
			}
		})
	}
}

// A swap file whose fstab unit systemd does not report active could not be
// stopped and started again through that unit: the drain refuses at that
// device, and a device drained before it stays drained.
func TestDrainSwapRefusesASwapFileItsUnitDoesNotRun(t *testing.T) {
	f, sys := drainHost(t)
	f.write(etcFstab, "/swap.img none swap sw 0 0\n/data/swap2 none swap sw 0 0\n")
	f.write("/data/swap2", "")
	const second = "/data/swap2                             file\t\t1048572\t\t131072\t\t-3\n"
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+second)
	sys.devices["data-swap2.swap"] = second
	sys.activeStates["data-swap2.swap"] = "inactive"

	_, err := sys.executor(nil).drainSwap(t.Context())
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "data-swap2.swap") || !strings.Contains(err.Error(), "stay drained") {
		t.Fatalf("err = %v, want a conflict naming the unit and the device drained before it", err)
	}
	assertCalls(t, sys.calls, []string{"stop swap.img.swap", "start swap.img.swap"})
}

// A partition's fstab unit is named after its /dev/disk/by-* link, an alias
// systemd marks active from /proc/swaps; that is not confirmed on a host, so
// its state does not hold the stop back and the wait decides.
func TestDrainSwapStopsAPartitionWhateverItsAliasUnitReads(t *testing.T) {
	_, sys := drainHost(t)
	sys.activeStates[partitionUnit] = "inactive"

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "stop "+partitionUnit) {
		t.Fatalf("calls = %v, want the partition drained", sys.calls)
	}
}
