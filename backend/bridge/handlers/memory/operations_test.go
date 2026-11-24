package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

const tempPath = "/swap.img" + resizeSuffix

// fileLength is the length of a fixture file, or -1 when it is missing.
func (f fixture) fileLength(name string) int64 {
	f.t.Helper()
	info, err := os.Stat(filepath.Join(f.root, name))
	if err != nil {
		return -1
	}
	return info.Size()
}

// noSwapHost is diskHost with no swap at all and no swap file in fstab.
func noSwapHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader)
	f.write(etcFstab, "UUID=0a1b2c3d / ext4 defaults 0 1\n")
	f.remove("/swap.img")
	return f, sys
}

func TestCreateSwapFile(t *testing.T) {
	f, sys := noSwapHost(t)
	logs := captureLogs(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).createSwapFileRoute(t.Context(), apischema.SwapFileCreateRequest{Size: 2 * gib})
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"make " + ownSwapFilePath,
		"fstab add " + ownSwapFilePath,
		"reload",
		"swapon " + ownSwapFilePath + " sw,nofail",
	})
	if want := "UUID=0a1b2c3d / ext4 defaults 0 1\n" + ownSwapFilePath + " none swap sw,nofail 0 0\n"; f.read(etcFstab) != want {
		t.Errorf("fstab = %q, want %q", f.read(etcFstab), want)
	}
	row, ok := findRow(got, ownSwapFilePath)
	if !ok || !row.Active || !row.InFstab || row.LockedReason != "" || f.fileLength(ownSwapFilePath) != 2*gib {
		t.Errorf("row = %+v, length %d; want LinuxIO's file swapping at 2 GiB", row, f.fileLength(ownSwapFilePath))
	}
	if got.Create.BlockedReason != apischema.SwapFileCreateExists {
		t.Errorf("create = %+v, want exists", got.Create)
	}
	if want := []apischema.SwapProgress{{Step: apischema.SwapStepCreateSwapFile, Index: 1, Total: 1, Device: ownSwapFilePath}}; !slices.Equal(progress, want) {
		t.Errorf("progress = %+v, want %+v", progress, want)
	}
	if !strings.Contains(logs.String(), `level=INFO msg="swap file created" path=`+ownSwapFilePath+" size=2147483648") {
		t.Errorf("logs = %s, want the creation at Info", logs)
	}
}

func TestCreateSwapFileRefusals(t *testing.T) {
	tests := []struct {
		name  string
		size  uint64
		setup func(f fixture)
		want  error
	}{
		{name: "not whole MiB", size: gib + 1, want: bridgeipc.ErrInvalidArgs},
		{name: "below the smallest size", size: 255 * mib, want: bridgeipc.ErrInvalidArgs},
		{name: "above the room the disk has", size: 46 * gib, want: bridgeipc.ErrInvalidArgs},
		{name: "there already", size: gib, want: bridgeipc.ErrConflict, setup: func(f fixture) {
			f.write(etcFstab, ownSwapFilePath+" none swap sw,nofail 0 0\n")
			f.writeSwapFile(ownSwapFilePath, gib)
		}},
		{name: "on another filesystem", size: gib, want: bridgeipc.ErrConflict, setup: func(f fixture) {
			f.useFolderFS(folderFS{kind: "", total: 100 * gib, free: 50 * gib})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := noSwapHost(t)
			if test.setup != nil {
				test.setup(f)
			}
			_, err := sys.executor(nil).createSwapFileRoute(t.Context(), apischema.SwapFileCreateRequest{Size: test.size})
			if !errors.Is(err, test.want) || len(sys.calls) != 0 {
				t.Fatalf("err = %v, calls = %v; want %v and no change", err, sys.calls, test.want)
			}
		})
	}
}

// A swap file a cut-short create left at the path, with no fstab line, is
// deleted and made again.
func TestCreateSwapFileReplacesALeftover(t *testing.T) {
	f, sys := noSwapHost(t)
	f.writeSwapFile(ownSwapFilePath, gib)

	if _, err := sys.executor(nil).createSwapFileRoute(t.Context(), apischema.SwapFileCreateRequest{Size: 2 * gib}); err != nil {
		t.Fatal(err)
	}
	if len(sys.calls) < 2 || sys.calls[0] != "delete "+ownSwapFilePath || sys.calls[1] != "make "+ownSwapFilePath {
		t.Fatalf("calls = %v, want the leftover deleted before the make", sys.calls)
	}
}

// A failure before the file swaps takes the line, then the file, away
// again; a make that fails leaves nothing to undo.
func TestCreateSwapFileUndoesAFileThatDoesNotSwap(t *testing.T) {
	tests := map[string]struct {
		fail string
		err  error
		want []string
	}{
		"make, no room": {fail: "make " + ownSwapFilePath, err: syscall.ENOSPC, want: []string{"make " + ownSwapFilePath}},
		"fstab line": {fail: "fstab add " + ownSwapFilePath, err: errors.New("fstab is locked"), want: []string{
			"make " + ownSwapFilePath, "fstab add " + ownSwapFilePath, "fstab remove " + ownSwapFilePath, "reload", "delete " + ownSwapFilePath,
		}},
		"swapon": {fail: "swapon " + ownSwapFilePath + " sw,nofail", err: errors.New("swapon: Invalid argument"), want: []string{
			"make " + ownSwapFilePath, "fstab add " + ownSwapFilePath, "reload", "swapon " + ownSwapFilePath + " sw,nofail",
			"fstab remove " + ownSwapFilePath, "reload", "delete " + ownSwapFilePath,
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f, sys := noSwapHost(t)
			sys.failures[test.fail] = test.err

			_, err := sys.executor(nil).createSwapFileRoute(t.Context(), apischema.SwapFileCreateRequest{Size: 2 * gib})
			if !errors.Is(err, test.err) {
				t.Fatalf("err = %v, want %v", err, test.err)
			}
			assertCalls(t, sys.calls, test.want)
			if f.exists(ownSwapFilePath) || strings.Contains(f.read(etcFstab), ownSwapFilePath) {
				t.Fatalf("fstab = %q, want no file and no line", f.read(etcFstab))
			}
		})
	}
}

// The only swap device: a temporary file of the data plus a quarter stands
// in while the file is off, and goes once it is back. The fstab line is
// left byte for byte.
func TestResizeSwapFileWithATemporaryFile(t *testing.T) {
	f, sys := diskHost(t)
	fstab := f.read(etcFstab)
	logs := captureLogs(t)
	var progress []apischema.SwapProgress
	var tempLength int64
	sys.after = func(call string) {
		if call == "make "+tempPath {
			tempLength = f.fileLength(tempPath)
		}
	}

	got, err := sys.executor(&progress).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: 4 * gib})
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"make " + tempPath, "swapon " + tempPath + " ",
		"swapoff /swap.img", "resize /swap.img", "swapon /swap.img sw",
		"swapoff " + tempPath, "delete " + tempPath,
	})
	if tempLength != 640*mib {
		t.Errorf("temporary file = %d MiB, want 640 MiB for 512 MiB of data", tempLength>>20)
	}
	if f.read(etcFstab) != fstab || f.fileLength("/swap.img") != 4*gib || f.exists(tempPath) {
		t.Errorf("fstab = %q, length %d; want fstab unchanged, 4 GiB and no temporary file", f.read(etcFstab), f.fileLength("/swap.img"))
	}
	if row, _ := findRow(got, "/swap.img"); !row.Active || row.Size != 4*gib-pageSize() {
		t.Errorf("row = %+v, want it swapping at 4 GiB", row)
	}
	want := []apischema.SwapProgress{
		{Step: apischema.SwapStepMakeTemporarySwap, Index: 1, Total: 3, Device: tempPath},
		{Step: apischema.SwapStepResizeSwapFile, Index: 2, Total: 3, Device: "/swap.img"},
		{Step: apischema.SwapStepRemoveTemporarySwap, Index: 3, Total: 3, Device: tempPath},
	}
	if !slices.Equal(progress, want) {
		t.Errorf("progress = %+v, want %+v", progress, want)
	}
	if !strings.Contains(logs.String(), `msg="swap file resized" path=/swap.img size=4294967296 previous_size=2147483648`) {
		t.Errorf("logs = %s, want the resize at Info", logs)
	}
}

// Beside zram, with room for the data, no temporary file is made; without
// that room one is, and the room check counts its free space.
func TestResizeSwapFileTemporaryFileOnlyWhenNeeded(t *testing.T) {
	for name, test := range map[string]struct {
		memAvailable uint64
		temporary    bool
	}{
		"room in memory":       {memAvailable: 8 * gib},
		"room only beside one": {memAvailable: 700 * mib, temporary: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, sys := diskHost(t)
			f.write(procSwaps, f.read(procSwaps)+zramSwapsLine)
			f.setMemAvailable(test.memAvailable)

			if _, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib}); err != nil {
				t.Fatal(err)
			}
			if made := slices.Contains(sys.calls, "make "+tempPath); made != test.temporary {
				t.Fatalf("calls = %v, want a temporary file %v", sys.calls, test.temporary)
			}
			if f.fileLength("/swap.img") != gib {
				t.Errorf("length = %d, want 1 GiB", f.fileLength("/swap.img"))
			}
		})
	}
}

// Each failure leaves what the plan's table says: added swap is never
// undone, and the file is never left off when it can be turned on.
func TestResizeSwapFileFailures(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(f fixture, sys *fakeSystem)
		ctx        func(t *testing.T) context.Context
		wantErr    string
		fileOn     bool
		tempOn     bool
		wantLength int64
	}{
		{name: "making the temporary file", setup: func(_ fixture, sys *fakeSystem) {
			sys.failures["make "+tempPath] = syscall.ENOSPC
		}, wantErr: "no space", fileOn: true, wantLength: 2 * gib},
		{name: "turning the temporary file on", setup: func(_ fixture, sys *fakeSystem) {
			sys.failures["swapon "+tempPath+" "] = errors.New("swapon failed")
		}, wantErr: "swapon failed", fileOn: true, wantLength: 2 * gib},
		{name: "no room to turn the file off", setup: func(f fixture, _ *fakeSystem) {
			f.setMemAvailable(100 * mib)
		}, wantErr: errNoRoom.Error(), fileOn: true, wantLength: 2 * gib},
		{name: "cancelled while the file goes off", setup: func(_ fixture, sys *fakeSystem) {
			sys.blocked["/swap.img"] = true
		}, ctx: func(t *testing.T) context.Context {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			t.Cleanup(cancel)
			return ctx
		}, wantErr: "deadline", fileOn: true, wantLength: 2 * gib},
		{name: "growing, no room", setup: func(_ fixture, sys *fakeSystem) {
			sys.failures["resize /swap.img"] = syscall.ENOSPC
		}, wantErr: "swaps at its old size", fileOn: true, wantLength: 2 * gib},
		{name: "mkswap after a shrink", setup: func(_ fixture, sys *fakeSystem) {
			sys.noHeader["/swap.img"] = true
		}, wantErr: "/swap.img is off: turn on /swap.img: swapon /swap.img: read swap header failed; the host swaps on the temporary swap file", tempOn: true, wantLength: gib},
		{name: "mkswap after a grow", setup: func(_ fixture, sys *fakeSystem) {
			// The file keeps its old header, as a grow whose mkswap failed
			// twice leaves it, so it comes back at its old size.
			sys.failures["resize /swap.img"] = fmt.Errorf("%w: mkswap: something went wrong", errNoSwapHeader)
		}, wantErr: "swaps at its old size", fileOn: true, wantLength: 2 * gib},
		{name: "turning the file on", setup: func(_ fixture, sys *fakeSystem) {
			sys.failures["swapon /swap.img sw"] = errors.New("swapon failed")
		}, wantErr: "the host swaps on the temporary swap file", tempOn: true, wantLength: gib},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := diskHost(t)
			test.setup(f, sys)
			ctx := t.Context()
			if test.ctx != nil {
				ctx = test.ctx(t)
			}

			_, err := sys.executor(nil).resizeSwapFile(ctx, apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want it to say %q", err, test.wantErr)
			}
			assertResizeLeft(t, f, test.fileOn, test.tempOn, test.wantLength)
		})
	}
}

// assertResizeLeft checks what a resize that failed left: whether the file
// and the temporary file swap, that a temporary file that does not swap is
// gone, and the file's length.
func assertResizeLeft(t *testing.T, f fixture, fileOn, tempOn bool, length int64) {
	t.Helper()
	swaps := f.read(procSwaps)
	if on := strings.Contains(swaps, "/swap.img "); on != fileOn {
		t.Errorf("swaps = %q, want /swap.img on %v", swaps, fileOn)
	}
	if on := strings.Contains(swaps, tempPath); on != tempOn {
		t.Errorf("swaps = %q, want the temporary file on %v", swaps, tempOn)
	}
	if !tempOn && f.exists(tempPath) {
		t.Error("the temporary file is left on disk")
	}
	if f.fileLength("/swap.img") != length {
		t.Errorf("length = %d, want %d", f.fileLength("/swap.img"), length)
	}
}

// A file left without a header reads as not a swap file, with Remove.
func TestResizeSwapFileWithoutAHeaderReadsAsNotASwapFile(t *testing.T) {
	f, sys := diskHost(t)
	sys.noHeader["/swap.img"] = true
	if _, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib}); !errors.Is(err, errNoSwapHeader) {
		t.Fatalf("err = %v, want no header", err)
	}
	status, _, err := f.host().readSwapStatus(sys.provider)
	if err != nil {
		t.Fatal(err)
	}
	if row, _ := findRow(status, "/swap.img"); row.ResizeBlockedReason != apischema.SwapFileResizeNoHeader || row.LockedReason != "" {
		t.Fatalf("row = %+v, want not a swap file and removable", row)
	}
}

// A temporary file an earlier resize left off is deleted first; one it left
// swapping stands in for the file and is removed at the end.
func TestResizeSwapFileWithALeftoverTemporaryFile(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		f, sys := diskHost(t)
		f.writeSwapFile(tempPath, 256*mib)
		if _, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib}); err != nil {
			t.Fatal(err)
		}
		if sys.calls[0] != "delete "+tempPath || sys.calls[1] != "make "+tempPath {
			t.Fatalf("calls = %v, want the leftover deleted before a new one is made", sys.calls)
		}
	})
	t.Run("on", func(t *testing.T) {
		f, sys := diskHost(t)
		f.writeSwapFile(tempPath, 256*mib)
		f.write(procSwaps, f.read(procSwaps)+swapsLine(tempPath, 256*mib, 0))
		var progress []apischema.SwapProgress
		if _, err := sys.executor(&progress).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib}); err != nil {
			t.Fatal(err)
		}
		assertCalls(t, sys.calls, []string{
			"swapoff /swap.img", "resize /swap.img", "swapon /swap.img sw", "swapoff " + tempPath, "delete " + tempPath,
		})
		if len(progress) != 2 || progress[1].Step != apischema.SwapStepRemoveTemporarySwap {
			t.Errorf("progress = %+v, want resize and remove_temporary_swap", progress)
		}
	})
}

// Something at the temporary file's name that is not a swap file, as an
// admin or a root service may have put there, is never deleted: a resize
// and a remove of the file beside it refuse, and change nothing.
func TestLeftoverTemporaryThatIsNotOneIsKept(t *testing.T) {
	for name, setup := range map[string]func(f fixture){
		"a file without a swap signature": func(f fixture) { f.write(tempPath, "an admin's notes") },
		"a folder":                        func(f fixture) { f.mkdir(tempPath) },
		"a link to a swap file":           func(f fixture) { f.writeSwapFile("/other.img", 256*mib); f.symlink("other.img", tempPath) },
	} {
		t.Run(name, func(t *testing.T) {
			for op, run := range map[string]func(e *executor) error{
				"resize": func(e *executor) error {
					_, err := e.resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib})
					return err
				},
				"remove": func(e *executor) error {
					_, err := e.removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"})
					return err
				},
			} {
				t.Run(op, func(t *testing.T) { assertLeftoverKept(t, setup, run) })
			}
		})
	}
}

// fstab options swapon would not take are refused before anything is made
// or turned off: the file would otherwise stay off until the next boot.
func TestResizeSwapFileRefusesOptionsSwaponWouldNotTake(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/swap.img none swap sw,pri=abc 0 0\n")
	if _, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	assertCalls(t, sys.calls, nil)
}

// A file that went off between the status read and its turn-off is not
// resized and not turned on; the temporary file the run made goes again.
func TestResizeSwapFileRefusesAFileFoundOff(t *testing.T) {
	f, sys := diskHost(t)
	sys.after = func(call string) {
		if call == "swapon "+tempPath+" " {
			f.removeSwapsLine("/swap.img")
		}
	}
	_, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "does not swap") {
		t.Fatalf("err = %v, want a conflict saying the file does not swap", err)
	}
	assertCalls(t, sys.calls, []string{"make " + tempPath, "swapon " + tempPath + " ", "swapoff " + tempPath, "delete " + tempPath})
	if f.fileLength("/swap.img") != 2*gib {
		t.Errorf("length = %d, want the file left at 2 GiB", f.fileLength("/swap.img"))
	}
}

// A request that is refused deletes nothing, a leftover temporary file
// included: every refusal is decided before the leftover is deleted.
func TestResizeSwapFileRefusalKeepsALeftoverTemporaryFile(t *testing.T) {
	f, sys := diskHost(t)
	f.writeSwapFile(tempPath, 256*mib)
	if _, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: 2 * gib}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("err = %v, want the size it has refused", err)
	}
	assertCalls(t, sys.calls, nil)
	if !f.exists(tempPath) {
		t.Error("the leftover temporary file was deleted by a refused resize")
	}
}

// assertLeftoverKept runs an operation on diskHost with setup's thing at
// the temporary file's name and checks that it is refused and kept.
func assertLeftoverKept(t *testing.T, setup func(f fixture), run func(e *executor) error) {
	t.Helper()
	f, sys := diskHost(t)
	setup(f)
	fstab := f.read(etcFstab)
	if err := run(sys.executor(nil)); !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "in the way of the temporary swap file") {
		t.Fatalf("err = %v, want a conflict naming what is in the way", err)
	}
	assertCalls(t, sys.calls, nil)
	if _, err := os.Lstat(filepath.Join(f.root, tempPath)); err != nil {
		t.Errorf("what was at %s is gone: %v", tempPath, err)
	}
	if f.read(etcFstab) != fstab || !strings.Contains(f.read(procSwaps), "/swap.img ") {
		t.Errorf("fstab = %q, swaps = %q; want both unchanged", f.read(etcFstab), f.read(procSwaps))
	}
}

func TestResizeSwapFileRefusals(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		size  uint64
		setup func(f fixture)
		want  error
	}{
		{name: "no such row", path: "/other.img", size: gib, want: bridgeipc.ErrNotFound},
		{name: "a path that is not exact", path: "/swap.img/", size: gib, want: bridgeipc.ErrNotFound},
		{name: "not whole MiB", path: "/swap.img", size: gib + 4096, want: bridgeipc.ErrInvalidArgs},
		{name: "below the smallest", path: "/swap.img", size: 128 * mib, want: bridgeipc.ErrInvalidArgs},
		{name: "above the room", path: "/swap.img", size: 60 * gib, want: bridgeipc.ErrInvalidArgs},
		{name: "its own size", path: "/swap.img", size: 2 * gib, want: bridgeipc.ErrInvalidArgs},
		{name: "locked", path: "/swap.img", size: gib, want: bridgeipc.ErrConflict, setup: func(f fixture) {
			f.write(etcFstab, "")
		}},
		{name: "off", path: "/swap.img", size: gib, want: bridgeipc.ErrConflict, setup: func(f fixture) {
			f.write(procSwaps, swapsHeader)
		}},
		{name: "temporary", path: tempPath, size: gib, want: bridgeipc.ErrConflict, setup: func(f fixture) {
			f.writeSwapFile(tempPath, 256*mib)
			f.write(procSwaps, f.read(procSwaps)+swapsLine(tempPath, 256*mib, 0))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := diskHost(t)
			if test.setup != nil {
				test.setup(f)
			}
			_, err := sys.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: test.path, Size: test.size})
			if !errors.Is(err, test.want) || len(sys.calls) != 0 {
				t.Fatalf("err = %v, calls = %v; want %v and no change", err, sys.calls, test.want)
			}
		})
	}
}

// Remove turns the file off, then removes its fstab lines, then deletes it;
// every other line of fstab stays. Removing the last swap is allowed.
func TestRemoveSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "UUID=0a1b2c3d / ext4 defaults 0 1\n/swap.img none swap sw 0 0\n# a comment\n/swap.img none swap pri=2 0 0\n")
	logs := captureLogs(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"})
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "fstab remove /swap.img", "reload", "delete /swap.img"})
	if want := "UUID=0a1b2c3d / ext4 defaults 0 1\n# a comment\n"; f.read(etcFstab) != want || f.exists("/swap.img") {
		t.Errorf("fstab = %q, want %q and the file gone", f.read(etcFstab), want)
	}
	if len(got.SwapFiles) != 0 || got.Mode != apischema.SwapModeNone {
		t.Errorf("status = %+v, want no swap", got)
	}
	if want := []apischema.SwapProgress{{Step: apischema.SwapStepRemoveSwapFile, Index: 1, Total: 1, Device: "/swap.img"}}; !slices.Equal(progress, want) {
		t.Errorf("progress = %+v, want %+v", progress, want)
	}
	if !strings.Contains(logs.String(), `msg="swap file removed" path=/swap.img`) {
		t.Errorf("logs = %s, want the removal at Info", logs)
	}
}

// A line another program writes back into /etc/fstab right after LinuxIO
// removed it keeps the file, off: deleting it would leave a line naming a
// missing file at every boot.
func TestRemoveSwapFileKeepsTheFileWhenItsLineComesBack(t *testing.T) {
	f, sys := diskHost(t)
	sys.after = func(call string) {
		if call == "fstab remove /swap.img" {
			f.write(etcFstab, f.read(etcFstab)+"/swap.img none swap sw 0 0\n")
		}
	}
	_, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "came back") {
		t.Fatalf("err = %v, want a conflict saying the line came back", err)
	}
	assertCalls(t, sys.calls, []string{"swapoff /swap.img", "fstab remove /swap.img", "reload"})
	if !f.exists("/swap.img") {
		t.Error("the file was deleted although a line names it")
	}
}

// A line that another program took out of /etc/fstab right after LinuxIO
// added it is a failed add: the new file is taken away again.
func TestCreateSwapFileUndoesALineThatDidNotStay(t *testing.T) {
	f, sys := noSwapHost(t)
	fstab := f.read(etcFstab)
	sys.after = func(call string) {
		if call == "fstab add "+ownSwapFilePath {
			f.write(etcFstab, fstab)
		}
	}
	_, err := sys.executor(nil).createSwapFileRoute(t.Context(), apischema.SwapFileCreateRequest{Size: 2 * gib})
	if err == nil || !strings.Contains(err.Error(), "another program rewrote /etc/fstab") {
		t.Fatalf("err = %v, want the line that did not stay named", err)
	}
	if f.exists(ownSwapFilePath) || strings.Contains(f.read(procSwaps), ownSwapFilePath) {
		t.Errorf("calls = %v, want the file taken away and never turned on", sys.calls)
	}
}

// A file that is off skips the swapoff; a missing one only loses its line.
func TestRemoveSwapFileThatIsOff(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		f, sys := diskHost(t)
		f.write(procSwaps, swapsHeader)
		if _, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); err != nil {
			t.Fatal(err)
		}
		assertCalls(t, sys.calls, []string{"fstab remove /swap.img", "reload", "delete /swap.img"})
	})
	t.Run("missing", func(t *testing.T) {
		f, sys := diskHost(t)
		f.write(procSwaps, swapsHeader)
		f.remove("/swap.img")
		if _, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); err != nil {
			t.Fatal(err)
		}
		assertCalls(t, sys.calls, []string{"fstab remove /swap.img", "reload"})
	})
}

func TestRemoveSwapFileFailures(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(f fixture, sys *fakeSystem)
		want      error
		wantCalls []string
		inFstab   bool
		onDisk    bool
	}{
		{name: "no room", setup: func(f fixture, _ *fakeSystem) { f.setMemAvailable(600 * mib) },
			want: bridgeipc.ErrConflict, inFstab: true, onDisk: true},
		{name: "swapoff fails", setup: func(_ fixture, sys *fakeSystem) { sys.failures["swapoff /swap.img"] = syscall.EBUSY },
			want: syscall.EBUSY, wantCalls: []string{"swapoff /swap.img"}, inFstab: true, onDisk: true},
		{name: "the fstab line", setup: func(_ fixture, sys *fakeSystem) {
			sys.failures["fstab remove /swap.img"] = errors.New("fstab is locked")
		}, wantCalls: []string{"swapoff /swap.img", "fstab remove /swap.img"}, inFstab: true, onDisk: true},
		// Another file at the name between the check and the delete is never
		// deleted. It is renamed over the old one, since a file made after a
		// remove may get the freed inode number back (ext4 often does).
		{name: "the name holds another file", setup: func(f fixture, sys *fakeSystem) {
			sys.after = func(call string) {
				if call == "reload" {
					f.writeSwapFile("/swap.img.new", gib)
					if err := os.Rename(filepath.Join(f.root, "/swap.img.new"), filepath.Join(f.root, "/swap.img")); err != nil {
						f.t.Fatal(err)
					}
				}
			}
		}, want: bridgeipc.ErrConflict, wantCalls: []string{"swapoff /swap.img", "fstab remove /swap.img", "reload", "delete /swap.img"}, onDisk: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := diskHost(t)
			test.setup(f, sys)

			_, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"})
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			assertCalls(t, sys.calls, test.wantCalls)
			if in := strings.Contains(f.read(etcFstab), "/swap.img"); in != test.inFstab {
				t.Errorf("fstab = %q, want the line there %v", f.read(etcFstab), test.inFstab)
			}
			if f.exists("/swap.img") != test.onDisk {
				t.Errorf("file on disk = %v, want %v", f.exists("/swap.img"), test.onDisk)
			}
		})
	}
}

// A temporary file has no fstab line: Remove turns it off and deletes it.
// The file it stood in for is not removed while it swaps beside it.
func TestRemoveSwapFileTemporary(t *testing.T) {
	f, sys := diskHost(t)
	f.writeSwapFile(tempPath, 256*mib)
	f.write(procSwaps, f.read(procSwaps)+swapsLine(tempPath, 256*mib, 0))

	if _, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict while the temporary file swaps", err, sys.calls)
	}
	if _, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: tempPath}); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{"swapoff " + tempPath, "delete " + tempPath})
}

// A temporary file left off beside the file is deleted before the file.
func TestRemoveSwapFileDeletesALeftoverTemporaryFile(t *testing.T) {
	f, sys := diskHost(t)
	f.writeSwapFile(tempPath, 256*mib)
	if _, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); err != nil {
		t.Fatal(err)
	}
	if sys.calls[0] != "delete "+tempPath || f.exists(tempPath) {
		t.Fatalf("calls = %v, want the leftover deleted first", sys.calls)
	}
}

func TestRemoveSwapFileRefusals(t *testing.T) {
	for name, test := range map[string]struct {
		path  string
		setup func(f fixture)
		want  error
	}{
		"no such row": {path: "/other.img", want: bridgeipc.ErrNotFound},
		"locked":      {path: "/swap.img", setup: func(f fixture) { f.write(etcFstab, "") }, want: bridgeipc.ErrConflict},
	} {
		t.Run(name, func(t *testing.T) {
			f, sys := diskHost(t)
			if test.setup != nil {
				test.setup(f)
			}
			_, err := sys.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: test.path})
			if !errors.Is(err, test.want) || len(sys.calls) != 0 {
				t.Fatalf("err = %v, calls = %v; want %v and no change", err, sys.calls, test.want)
			}
		})
	}
}

// btrfsHost is diskHost on a btrfs root, where /swap.img blocks snapshots of
// the subvolume of /.
func btrfsHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(procMountinfo, "22 1 0:21 /@ / rw shared:1 - btrfs /dev/vda2 rw\n")
	f.useFolderFS(folderFS{kind: "btrfs", btrfsLayout: true, total: 100 * gib, free: 50 * gib})
	return f, sys
}

// Move makes LinuxIO's file at the old one's size, then removes the old
// one, so swap is added before any is removed.
func TestMoveSwapFile(t *testing.T) {
	f, sys := btrfsHost(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).moveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"})
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"make " + ownSwapFilePath, "fstab add " + ownSwapFilePath, "reload", "swapon " + ownSwapFilePath + " sw,nofail",
		"swapoff /swap.img", "fstab remove /swap.img", "reload", "delete /swap.img",
	})
	if f.fileLength(ownSwapFilePath) != 2*gib || len(got.SwapFiles) != 1 || got.SwapFiles[0].Path != ownSwapFilePath || got.SwapFiles[0].BlocksSnapshots {
		t.Errorf("swap files = %+v, want LinuxIO's alone at 2 GiB", got.SwapFiles)
	}
	want := []apischema.SwapProgress{
		{Step: apischema.SwapStepCreateSwapFile, Index: 1, Total: 2, Device: ownSwapFilePath},
		{Step: apischema.SwapStepRemoveSwapFile, Index: 2, Total: 2, Device: "/swap.img"},
	}
	if !slices.Equal(progress, want) {
		t.Errorf("progress = %+v, want %+v", progress, want)
	}
}

// A file Move is not offered for is refused before anything changes; a
// remove that fails after the create leaves both files, the new one on.
func TestMoveSwapFileFailures(t *testing.T) {
	t.Run("not offered", func(t *testing.T) {
		_, sys := diskHost(t)
		if _, err := sys.executor(nil).moveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
			t.Fatalf("err = %v, calls = %v; want a conflict on ext4 and no change", err, sys.calls)
		}
	})
	t.Run("remove fails", func(t *testing.T) {
		f, sys := btrfsHost(t)
		sys.failures["swapoff /swap.img"] = syscall.EBUSY
		_, err := sys.executor(nil).moveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"})
		if !errors.Is(err, syscall.EBUSY) || !strings.Contains(err.Error(), ownSwapFilePath+" swaps") {
			t.Fatalf("err = %v, want the failed removal and that the new file swaps", err)
		}
		if swaps := f.read(procSwaps); !strings.Contains(swaps, ownSwapFilePath) || !strings.Contains(swaps, "/swap.img ") {
			t.Fatalf("swaps = %q, want both files on", swaps)
		}
	})
}
