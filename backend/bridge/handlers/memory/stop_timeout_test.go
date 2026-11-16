package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var errNoBus = errors.New("no system bus")

const swapFileStopTimeout = "/run/systemd/system/swap.img.swap.d/90-linuxio-stop-timeout.conf"

func TestStopTimeoutPath(t *testing.T) {
	if got, err := stopTimeoutPath("swap.img.swap"); err != nil || got != swapFileStopTimeout {
		t.Errorf("path = %q, %v; want %q", got, err, swapFileStopTimeout)
	}
	// A partition's unit is named after its /dev/disk/by-* link.
	if got, err := stopTimeoutPath(`dev-disk-by\x2duuid-0a1b.swap`); err != nil || !strings.HasSuffix(got, `/dev-disk-by\x2duuid-0a1b.swap.d/90-linuxio-stop-timeout.conf`) {
		t.Errorf("path = %q, %v; want the drop-in of the escaped unit name", got, err)
	}
	for _, unit := range []string{"", "../etc.swap", "a/b.swap", "zramswap.service", ".."} {
		if got, err := stopTimeoutPath(unit); err == nil {
			t.Errorf("stopTimeoutPath(%q) = %q, want it refused", unit, got)
		}
	}
}

// systemd reads a timeout of 0 as none, so the drop-in never says 0.
func TestRenderStopTimeout(t *testing.T) {
	for timeout, seconds := range map[time.Duration]string{10 * time.Minute: "600", 1500 * time.Millisecond: "2", 200 * time.Millisecond: "1", 0: "1"} {
		want := managedHeader + "[Swap]\nTimeoutSec=" + seconds + "s\n"
		if got := string(renderStopTimeout(timeout)); got != want {
			t.Errorf("drop-in for %s = %q, want %q", timeout, got, want)
		}
	}
}

// The drop-in directory goes with the drop-in, unless something else is in
// it, and removing a drop-in that is not there is not an error.
func TestRemoveStopTimeout(t *testing.T) {
	f := newFixture(t)
	h := f.host()
	if removed, err := h.removeStopTimeout("swap.img.swap"); removed || err != nil {
		t.Fatalf("remove with no drop-in = %v, %v", removed, err)
	}
	if err := h.writeStopTimeout("swap.img.swap", time.Minute); err != nil {
		t.Fatal(err)
	}
	if removed, err := h.removeStopTimeout("swap.img.swap"); !removed || err != nil {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if f.exists("/run/systemd/system/swap.img.swap.d") {
		t.Error("empty drop-in directory left behind")
	}

	f.write("/run/systemd/system/swap.img.swap.d/admin.conf", "[Swap]\nOptions=discard\n")
	if err := h.writeStopTimeout("swap.img.swap", time.Minute); err != nil {
		t.Fatal(err)
	}
	if removed, err := h.removeStopTimeout("swap.img.swap"); !removed || err != nil {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if !f.exists("/run/systemd/system/swap.img.swap.d/admin.conf") || f.exists(swapFileStopTimeout) {
		t.Error("want the other drop-in kept and LinuxIO's removed")
	}

	// A write that failed after it made the directory leaves it empty.
	f.mkdir("/run/systemd/system/other.swap.d")
	if removed, err := h.removeStopTimeout("other.swap"); removed || err != nil {
		t.Fatalf("remove with an empty directory = %v, %v", removed, err)
	}
	if f.exists("/run/systemd/system/other.swap.d") {
		t.Error("empty drop-in directory left behind")
	}
}

// A mode change raises the swap file unit's timeout to its own wait before
// the stop and puts it back before the unit is masked.
func TestStopSwapUnitRaisesTheTimeoutForTheStop(t *testing.T) {
	f, sys := diskHost(t)
	var during string
	sys.after = func(call string) {
		if call == "stop swap.img.swap" {
			during = f.read(swapFileStopTimeout)
		}
	}

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err != nil {
		t.Fatal(err)
	}
	i := slices.Index(sys.calls, raiseTimeoutCall+"swap.img.swap")
	if i < 0 || !slices.Equal(sys.calls[i:], []string{
		raiseTimeoutCall + "swap.img.swap",
		"stop swap.img.swap",
		restoreTimeoutCall + "swap.img.swap",
		"mask swap.img.swap",
	}) {
		t.Fatalf("calls = %v, want the timeout raised around the stop", sys.calls)
	}
	// The fake executor waits 200ms for a swapoff.
	if want := managedHeader + "[Swap]\nTimeoutSec=1s\n"; during != want {
		t.Errorf("drop-in during the stop = %q, want %q", during, want)
	}
	if f.exists(swapFileStopTimeout) {
		t.Error("drop-in left behind")
	}
}

// A drain does the same for each device, and the timeout is put back when
// the stop fails or the request is cancelled.
func TestDrainSwapPutsTheStopTimeoutBack(t *testing.T) {
	tests := map[string]func(sys *fakeSystem, cancel context.CancelFunc){
		"drained":        func(*fakeSystem, context.CancelFunc) {},
		"the stop fails": func(sys *fakeSystem, _ context.CancelFunc) { sys.failures["stop swap.img.swap"] = errNoBus },
		"systemd gave up": func(sys *fakeSystem, _ context.CancelFunc) {
			sys.stops["swap.img.swap"] = &fakeStop{polls: 2, ends: "active"}
		},
		"cancelled during swapoff": func(sys *fakeSystem, cancel context.CancelFunc) {
			sys.stops["swap.img.swap"] = &fakeStop{polls: 1 << 30}
			sys.after = func(call string) {
				if call == "stop swap.img.swap" {
					cancel()
				}
			}
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f, sys := drainHost(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			setup(sys, cancel)

			_, _ = sys.executor(nil).drainSwap(ctx)
			raised := slices.Index(sys.calls, raiseTimeoutCall+"swap.img.swap")
			stopped := slices.Index(sys.calls, "stop swap.img.swap")
			restored := slices.Index(sys.calls, restoreTimeoutCall+"swap.img.swap")
			if raised < 0 || raised > stopped || stopped > restored {
				t.Fatalf("calls = %v, want the timeout raised before the stop and put back after it", sys.calls)
			}
			if f.exists(swapFileStopTimeout) {
				t.Error("drop-in left behind")
			}
		})
	}
}

// A timeout that cannot be raised does not hold the stop back: the unit
// stops within systemd's own, as it did before the timeout was raised.
func TestStopSwapUnitStopsWithoutTheRaisedTimeout(t *testing.T) {
	f, sys := drainHost(t)
	sys.failures[raiseTimeoutCall+"swap.img.swap"] = errNoBus

	if _, err := sys.executor(nil).drainSwap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "stop swap.img.swap") || !slices.Contains(sys.calls, restoreTimeoutCall+"swap.img.swap") {
		t.Fatalf("calls = %v, want the stop and the timeout put back", sys.calls)
	}
	if f.exists(swapFileStopTimeout) {
		t.Error("drop-in left behind")
	}
}

// A swap file that holds nothing leaves at once: its timeout is not raised,
// which would cost two systemd reloads.
func TestStopSwapUnitLeavesTheTimeoutOfAnEmptyDevice(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/swap.img                               file\t\t2097148\t\t0\t\t-2\n")
	sys.devices["swap.img.swap"] = "/swap.img                               file\t\t2097148\t\t0\t\t-2\n"

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "stop swap.img.swap") {
		t.Fatalf("calls = %v, want the swap file stopped", sys.calls)
	}
	for _, call := range sys.calls {
		if strings.HasPrefix(call, raiseTimeoutCall) || strings.HasPrefix(call, restoreTimeoutCall) {
			t.Fatalf("calls = %v, want no stop timeout raised", sys.calls)
		}
	}
}
