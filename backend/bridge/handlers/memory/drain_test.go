package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"syscall"
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
		"swapoff /swap.img",
		"start swap.img.swap",
		"swapoff /dev/sda3",
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
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "start swap.img.swap"})
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
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "start swap.img.swap"})
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
		if call == "swapoff /swap.img" {
			cancel()
		}
	}

	if _, err := sys.executor(nil).drainSwap(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancel", err)
	}
	if !slices.Contains(sys.calls, "start swap.img.swap") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want swap.img.swap started again", sys.calls)
	}
	if slices.Contains(sys.calls, "swapoff /dev/sda3") {
		t.Fatalf("calls = %v, want the next device left alone", sys.calls)
	}
}

// A swapoff the kernel refuses leaves the device swapping: nothing is
// started, the error names the device and the next device is not touched.
func TestDrainSwapStopsAtASwapoffThatFails(t *testing.T) {
	f, sys := drainHost(t)
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	_, err := sys.executor(nil).drainSwap(t.Context())
	if !errors.Is(err, syscall.EBUSY) || !strings.Contains(err.Error(), "swapoff /swap.img") {
		t.Fatalf("err = %v, want the kernel's refusal naming the device", err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img"})
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatal("the device is off")
	}
}

// A swapoff that reports an error after the device left still counts: the
// device is started again.
func TestDrainSwapStartsADeviceThatLeftWhateverSwapoffReturned(t *testing.T) {
	f, sys := drainHost(t)
	sys.failAfter["swapoff /swap.img"] = syscall.EIO

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "start swap.img.swap") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want swap.img.swap started again", sys.calls)
	}
}

// A request cancelled while swapoff runs ends the swapoff with the device
// still swapping: nothing is started, and a device drained before it stays
// drained.
func TestDrainSwapLeavesTheDeviceOnWhenCancelledDuringSwapoff(t *testing.T) {
	f, sys := drainHost(t)
	sys.blocked["/dev/sda3"] = true
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := sys.executor(nil).drainSwap(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "swapoff /dev/sda3") || !strings.Contains(err.Error(), "stay drained") {
		t.Fatalf("err = %v, want the cancelled swapoff and the device drained before it", err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "start swap.img.swap", "swapoff /dev/sda3"})
	if swaps := f.read(procSwaps); !strings.Contains(swaps, "/dev/sda3") || !strings.Contains(swaps, "/swap.img") {
		t.Fatalf("swaps = %q, want both devices on", swaps)
	}
}

// systemd learns from /proc/swaps that the device left; a start it gets
// while it still holds the unit active does nothing. The drain waits for
// the unit to read inactive before it starts it.
func TestDrainSwapWaitsForSystemdToSeeTheDeviceLeft(t *testing.T) {
	f, sys := drainHost(t)
	sys.unseen["swap.img.swap"] = 5

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatal("the device stayed off")
	}
}

// A start systemd did nothing for is sent again until the device is back.
func TestDrainSwapStartsAgainWhenTheStartDidNothing(t *testing.T) {
	f, sys := drainHost(t)
	sys.ignoredStarts["swap.img.swap"] = 2
	e := sys.executor(nil)
	e.waits.stopGrace = 10 * time.Millisecond
	e.waits.swapOn = 5 * time.Second

	if _, err := e.drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatal("the device stayed off")
	}
}

// A unit systemd never sees inactive is started all the same after the
// grace, and the drain reports the device off when that brings nothing back.
func TestDrainSwapReportsADeviceSystemdNeverSawLeave(t *testing.T) {
	_, sys := drainHost(t)
	sys.unseen["swap.img.swap"] = 1 << 30

	_, err := sys.executor(nil).drainSwap(t.Context())
	if err == nil || !strings.Contains(err.Error(), "/swap.img is off") {
		t.Fatalf("err = %v, want the device reported off", err)
	}
	if !slices.Contains(sys.calls, "start swap.img.swap") || slices.Contains(sys.calls, "swapoff /dev/sda3") {
		t.Fatalf("calls = %v, want the start tried and the drain stopped there", sys.calls)
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
				if call == "swapoff /swap.img" {
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
			if slices.Contains(sys.calls, "swapoff /dev/sda3") {
				t.Fatalf("calls = %v, want the drain stopped at the first device", sys.calls)
			}
		})
	}
}

// swapoff takes a device away whatever systemd says of its unit: a swap
// file systemd tracks under another unit name, or a partition whose fstab
// unit is an alias named after its /dev/disk/by-* link, is drained and
// started again through the unit fstab gives it.
func TestDrainSwapDrainsADeviceWhateverItsUnitReads(t *testing.T) {
	f, sys := drainHost(t)
	sys.activeStates["swap.img.swap"] = "inactive"
	sys.activeStates[partitionUnit] = "failed"

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"swapoff /swap.img", "start swap.img.swap",
		"swapoff /dev/sda3", "start " + partitionUnit,
	})
	if swaps := f.read(procSwaps); !strings.Contains(swaps, "/swap.img") || !strings.Contains(swaps, "/dev/sda3") {
		t.Fatalf("swaps = %q, want both devices back", swaps)
	}
}
