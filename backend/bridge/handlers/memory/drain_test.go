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
	partitionSwapsLine = "/dev/sda3                               partition\t8388604\t\t262144\t\t-3\n"
	unmanagedSwapsLine = "/home/extra                             file\t\t1048572\t\t131072\t\t-4\n"
	swap2SwapsLine     = "/swap2.img                              file\t\t1048572\t\t131072\t\t-3\n"
)

// drainHost has data parked on two swap files fstab names, /swap.img and
// /swap2.img with pri=5, and on a partition fstab names by UUID.
func drainHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(etcFstab, "/swap.img none swap sw 0 0\n/swap2.img none swap sw,pri=5 0 0\nUUID=7e8f none swap sw 0 0\n")
	f.writeSwapFile("/swap2.img", gib)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+swap2SwapsLine+partitionSwapsLine)
	return f, sys
}

// Each swap file that holds data is turned off and on again with the
// options of its fstab line, one at a time; the partition is never touched.
func TestDrainSwapTurnsEachSwapFileOffAndOnInTurn(t *testing.T) {
	f, sys := drainHost(t)
	logs := captureLogs(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).drainSwap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"swapoff /swap.img",
		"swapon /swap.img sw",
		"swapoff /swap2.img",
		"swapon /swap2.img sw,pri=5",
	})
	want := []apischema.SwapProgress{
		{Step: apischema.SwapStepDrainDevice, Index: 1, Total: 2, Device: "/swap.img"},
		{Step: apischema.SwapStepDrainDevice, Index: 2, Total: 2, Device: "/swap2.img"},
	}
	if !slices.Equal(progress, want) {
		t.Errorf("progress = %+v, want %+v", progress, want)
	}
	if len(got.Devices) != 3 || !strings.Contains(f.read(procSwaps), "/dev/sda3") {
		t.Errorf("devices = %+v, want all three on", got.Devices)
	}
	if !strings.Contains(logs.String(), `msg="swap drained" devices="[/swap.img /swap2.img]"`) {
		t.Errorf("logs = %s, want the drained files", logs)
	}
}

// The status after the drain is read again when a read fails: the files
// were drained and the request succeeds.
func TestDrainSwapReadsTheStatusAgainAfterTheDrain(t *testing.T) {
	_, sys := drainHost(t)
	sys.after = func(call string) {
		if call == "swapon /swap2.img sw,pri=5" {
			sys.statusFails = 2
		}
	}

	got, err := sys.executor(nil).drainSwap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 3 {
		t.Errorf("devices = %+v, want all back", got.Devices)
	}
}

// Room is checked per file on a fresh status; a file already drained stays
// drained when the next one no longer fits.
func TestDrainSwapRefusesAFileWithoutRoom(t *testing.T) {
	f, sys := drainHost(t)
	sys.after = func(call string) {
		if call == "swapon /swap.img sw" {
			f.write(procSwaps, swapsHeader+swapsLine("/swap.img", 2*gib, 2*gib-8*mib)+swap2SwapsLine)
			f.setMemAvailable(600 * mib)
		}
	}

	_, err := sys.executor(nil).drainSwap(t.Context())
	if !errors.Is(err, bridgeipc.ErrConflict) || !errors.Is(err, errNoRoom) || !strings.Contains(err.Error(), "stay drained") {
		t.Fatalf("err = %v, want a conflict for room that says the first file stays drained", err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "swapon /swap.img sw"})
}

func TestDrainSwapRefusesWithoutRoomForTheParkedData(t *testing.T) {
	f, sys := drainHost(t)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine)
	f.setMemAvailable(gib)

	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// Data parked only on a partition or a swap file fstab does not name is on
// swap LinuxIO does not turn off.
func TestDrainSwapRefusesOtherSwap(t *testing.T) {
	for name, swaps := range map[string]string{
		"partition":           partitionSwapsLine,
		"file not in fstab":   unmanagedSwapsLine,
		"partition and other": partitionSwapsLine + unmanagedSwapsLine,
	} {
		t.Run(name, func(t *testing.T) {
			f, sys := drainHost(t)
			f.write(procSwaps, swapsHeader+swaps)
			f.write("/home/extra", "")

			_, err := sys.executor(nil).drainSwap(t.Context())
			if !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
				t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
			}
			if want := drainRefusal(apischema.SwapDrainOtherSwap); !strings.Contains(err.Error(), want) {
				t.Errorf("err = %v, want it to say %q", err, want)
			}
		})
	}
}

// Beside a swap file it drains, a file fstab does not name is left alone.
func TestDrainSwapLeavesOtherSwapAlone(t *testing.T) {
	f, sys := drainHost(t)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+unmanagedSwapsLine+partitionSwapsLine)
	f.write("/home/extra", "")

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "swapon /swap.img sw"})
}

func TestDrainSwapRefusesWithNothingParked(t *testing.T) {
	f, sys := drainHost(t)
	f.write(procSwaps, swapsHeader+"/swap.img                               file\t\t2097148\t\t0\t\t-2\n")

	if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
}

// A request cancelled once a file has left /proc/swaps still turns it on
// again: the swapon runs on a context the cancel does not reach.
func TestDrainSwapTurnsAFileOnAfterACancel(t *testing.T) {
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
	if !slices.Contains(sys.calls, "swapon /swap.img sw") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want /swap.img on again", sys.calls)
	}
	if slices.Contains(sys.calls, "swapoff /swap2.img") {
		t.Fatalf("calls = %v, want the next file left alone", sys.calls)
	}
}

// A swapoff the kernel refuses leaves the file swapping: nothing is turned
// on, the error names the file and the next file is not touched.
func TestDrainSwapStopsAtASwapoffThatFails(t *testing.T) {
	f, sys := drainHost(t)
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	_, err := sys.executor(nil).drainSwap(t.Context())
	if !errors.Is(err, syscall.EBUSY) || !strings.Contains(err.Error(), "swapoff /swap.img") {
		t.Fatalf("err = %v, want the kernel's refusal naming the file", err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img"})
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatal("the file is off")
	}
}

// A swapoff that reports an error after the file left still counts: the
// file is turned on again.
func TestDrainSwapTurnsOnAFileThatLeftWhateverSwapoffReturned(t *testing.T) {
	f, sys := drainHost(t)
	sys.failAfter["swapoff /swap.img"] = syscall.EIO

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "swapon /swap.img sw") || !strings.Contains(f.read(procSwaps), "/swap.img ") {
		t.Fatalf("calls = %v, want /swap.img on again", sys.calls)
	}
}

// A request cancelled while swapoff runs ends the swapoff with the file
// still swapping: nothing is turned on, and a file drained before it stays
// drained.
func TestDrainSwapLeavesTheFileOnWhenCancelledDuringSwapoff(t *testing.T) {
	f, sys := drainHost(t)
	sys.blocked["/swap2.img"] = true
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := sys.executor(nil).drainSwap(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "swapoff /swap2.img") || !strings.Contains(err.Error(), "stay drained") {
		t.Fatalf("err = %v, want the cancelled swapoff and the file drained before it", err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "swapon /swap.img sw", "swapoff /swap2.img"})
	if swaps := f.read(procSwaps); !strings.Contains(swaps, "/swap2.img") || !strings.Contains(swaps, "/swap.img") {
		t.Fatalf("swaps = %q, want both files on", swaps)
	}
}

// A file that left /proc/swaps before its turn is skipped: the success line
// names only the files drained.
func TestDrainSwapSkipsAFileThatLeft(t *testing.T) {
	f, sys := drainHost(t)
	logs := captureLogs(t)
	sys.after = func(call string) {
		if call == "swapon /swap.img sw" {
			f.removeSwapsLine("/swap2.img")
		}
	}

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "swapon /swap.img sw"})
	if !strings.Contains(logs.String(), `devices=[/swap.img]`) {
		t.Errorf("logs = %s, want only /swap.img drained", logs)
	}
}

// A file that does not come back is reported off, and the drain stops there.
func TestDrainSwapReportsAFileItCouldNotTurnOn(t *testing.T) {
	tests := map[string]func(sys *fakeSystem){
		"swapon fails":       func(sys *fakeSystem) { sys.failures["swapon /swap.img sw"] = errors.New("swapon failed") },
		"file never returns": func(sys *fakeSystem) { sys.ignoredSwapOns["/swap.img"] = true },
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			_, sys := drainHost(t)
			setup(sys)

			_, err := sys.executor(nil).drainSwap(t.Context())
			if err == nil || !strings.Contains(err.Error(), "/swap.img is off") {
				t.Fatalf("err = %v, want the file reported off", err)
			}
			if slices.Contains(sys.calls, "swapoff /swap2.img") {
				t.Fatalf("calls = %v, want the drain stopped at the first file", sys.calls)
			}
		})
	}
}

// fstab options swapon would not take are refused before any file is
// turned off, which would otherwise stay off until the next boot.
func TestDrainSwapRefusesOptionsSwaponWouldNotTakeBeforeAnything(t *testing.T) {
	for _, options := range []string{"sw,pri=abc", "pri=40000", "pri=1,pri=2", "discard=foo"} {
		t.Run(options, func(t *testing.T) {
			f, sys := drainHost(t)
			f.write(etcFstab, "/swap.img none swap sw 0 0\n/swap2.img none swap "+options+" 0 0\n")
			if _, err := sys.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "/swap2.img") {
				t.Fatalf("err = %v, want a conflict naming /swap2.img", err)
			}
			assertCalls(t, sys.calls, nil)
		})
	}
}
