package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// The swap file the rig adds, and the sizes the resize test takes it to.
const (
	rigSwapFile    = "/swap.img"
	resizeITGrown  = 1536 * mib
	resizeITParked = 384
)

// onlyDiskSwap skips a test unless path is an active swap file LinuxIO can
// change and the host's only swap device, as the rig leaves /swap.img.
func onlyDiskSwap(t *testing.T, path string) apischema.SwapFileRow {
	t.Helper()
	status := freshStatus(t)
	row, ok := findRow(status, path)
	if !ok || !row.Active || row.LockedReason != "" || row.ResizeBlockedReason != "" {
		t.Skipf("%s is not an active swap file LinuxIO can resize (%+v): the rig adds it", path, row)
	}
	if len(status.Devices) != 1 {
		t.Skipf("swap devices %+v: the test needs %s as the only one", status.Devices, path)
	}
	return row
}

// TestSwapFileResizeIntegration resizes the rig's /swap.img in place on the
// real kernel, with data parked on it: up while it is the only swap, which
// makes the temporary file beside it, and down beside zram with room, which
// does not. Each time the path, the file and /etc/fstab stay as they were
// byte for byte, /proc/swaps lists the new size, no temporary file is left,
// and the parked data comes back unchanged. In between, a resize cancelled
// while the file is being turned off leaves it swapping at its old size
// with no temporary file left on or on disk. Cleanup puts the size and the
// host back.
func TestSwapFileResizeIntegration(t *testing.T) {
	needRoot(t)
	asJobHost(t)
	row := onlyDiskSwap(t, rigSwapFile)
	length := fileLength(t, rigSwapFile)
	e := itExecutor(t)
	view, err := e.readView(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := takeHostSnapshot(t, view.status)
	t.Cleanup(func() { snapshot.restore(t, e, view.status.ZramProvider) })
	fstab, id := readFile(t, etcFstab), fileIdentity(t, rigSwapFile)
	t.Logf("%s: %d MiB, %d MiB used", rigSwapFile, length>>20, row.Used>>20)

	enterDrainCgroup(t)
	holders := startDrainHolders(t, resizeITParked/drainHolders)
	parked := uint64(resizeITParked) << 20
	reclaimDrainCgroup(t, e, rigSwapFile, parked)

	t.Run("up as the only swap", func(t *testing.T) {
		steps := resizeIT(t, rigSwapFile, resizeITGrown)
		if !slices.Contains(steps, apischema.SwapStepMakeTemporarySwap) || !slices.Contains(steps, apischema.SwapStepRemoveTemporarySwap) {
			t.Errorf("steps %v, want the temporary file made and removed", steps)
		}
		assertResizedInPlace(t, rigSwapFile, resizeITGrown, fstab, id)
		checkHolders(t, holders)
	})

	reclaimDrainCgroup(t, e, rigSwapFile, parked)
	t.Run("cancelled while the file goes off", func(t *testing.T) {
		capDrainReads(t, rigSwapFile, 2<<20)
		cancelResizeMidway(t, e, rigSwapFile, length)
		assertResizedInPlace(t, rigSwapFile, resizeITGrown, fstab, id)
	})
	checkHolders(t, holders)

	startSmallZram(t, e)
	// zram, ahead of the file, takes up to its 64 MiB first: three quarters
	// of the data must reach the file, and what goes elsewhere stays under a
	// quarter of that.
	reclaimDrainCgroup(t, e, rigSwapFile, parked*3/4)
	t.Run("down beside zram with room", func(t *testing.T) {
		steps := resizeIT(t, rigSwapFile, length)
		if slices.Contains(steps, apischema.SwapStepMakeTemporarySwap) {
			t.Errorf("steps %v, want no temporary file beside zram with room", steps)
		}
		assertResizedInPlace(t, rigSwapFile, length, fstab, id)
		checkHolders(t, holders)
	})
}

// resizeIT resizes path to size through the route's function and returns
// the steps it reported.
func resizeIT(t *testing.T, path string, size uint64) []apischema.SwapStep {
	t.Helper()
	log := &stepLog{t: t}
	start := time.Now()
	if _, err := ResizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: path, Size: size}, log.report); err != nil {
		t.Fatalf("resize %s to %d MiB: %v", path, size>>20, err)
	}
	t.Logf("resized %s to %d MiB in %s", path, size>>20, time.Since(start).Round(time.Millisecond))
	return log.steps
}

// assertResizedInPlace checks that path is the same file, swapping at size,
// with /etc/fstab as it was and no temporary file beside it.
func assertResizedInPlace(t *testing.T, path string, size uint64, fstab string, id fileID) {
	t.Helper()
	assertSwapsAt(t, path, size)
	if got := fileLength(t, path); got != size {
		t.Errorf("%s is %d MiB long, want %d MiB", path, got>>20, size>>20)
	}
	if got := fileIdentity(t, path); got != id {
		t.Errorf("%s is %+v, want the same file %+v", path, got, id)
	}
	if got := readFile(t, etcFstab); got != fstab {
		t.Errorf("/etc/fstab = %q, want it unchanged: %q", got, fstab)
	}
	temporary := path + resizeSuffix
	if listed, _ := swapListed(host, temporary); listed {
		t.Errorf("%s still swaps", temporary)
	}
	if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is left on disk: %v", temporary, err)
	}
}

// cancelResizeMidway starts a resize of path back to size and cancels it
// once swapoff has read some of the parked data back: it must end with the
// cancel and leave the file swapping, not resized.
func cancelResizeMidway(t *testing.T, e *executor, path string, size uint64) {
	t.Helper()
	parked := swapUsed(t, e, path)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ResizeSwapFile(ctx, apischema.SwapFileResizeRequest{Path: path, Size: size}, logProgress(t))
		done <- err
	}()
	if err := waitUntil(t.Context(), 10*time.Minute, 20*time.Millisecond, func() (bool, error) {
		select {
		case err := <-done:
			return false, fmt.Errorf("the resize ended before it could be cancelled: %w", err)
		default:
			return swapUsed(t, e, path) < parked-parked/20, nil
		}
	}); err != nil {
		t.Fatalf("wait for the swapoff to read pages back: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resize: %v, want the cancel", err)
	}
	t.Logf("cancelled with %d of %d MiB still parked on %s", swapUsed(t, e, path)>>20, parked>>20, path)
}

func fileLength(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return uint64(info.Size())
}

func fileIdentity(t *testing.T, path string) fileID {
	t.Helper()
	id, err := host.fileIDOf(path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestSwapFileLeftoverTemporaryIntegration has a resize's temporary file
// left behind beside a swap file, as a crash leaves it. One that is off is
// deleted by the next Resize of its file, and by the next Remove; one that
// is on is a temporary row LinuxIO can remove, which Remove turns off and
// deletes without touching /etc/fstab, and the file beside it cannot be
// removed until it is gone.
func TestSwapFileLeftoverTemporaryIntegration(t *testing.T) {
	needRoot(t)
	asJobHost(t)
	onlyDiskSwap(t, rigSwapFile)
	length := fileLength(t, rigSwapFile)
	keepSwapFiles(t)
	temporary := rigSwapFile + resizeSuffix

	t.Run("off, then a resize", func(t *testing.T) {
		logs := captureLogs(t)
		makeLeftover(t, temporary)
		resizeIT(t, rigSwapFile, length+256*mib)
		assertResizedInPlace(t, rigSwapFile, length+256*mib, readFile(t, etcFstab), fileIdentity(t, rigSwapFile))
		if !strings.Contains(logs.String(), `msg="leftover temporary swap file deleted" path=`+temporary) {
			t.Errorf("logs = %s, want the leftover deleted", logs)
		}
		resizeIT(t, rigSwapFile, length)
	})

	t.Run("off, then a remove", func(t *testing.T) {
		second := "/linuxio-it-second.swap"
		makeHandMadeSwapFile(t, second, 256*mib)
		logs := captureLogs(t)
		makeLeftover(t, second+resizeSuffix)
		if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: second}, logProgress(t)); err != nil {
			t.Fatal(err)
		}
		assertSwapFileGone(t, second)
		if _, err := os.Lstat(second + resizeSuffix); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the leftover is still there: %v", err)
		}
		if !strings.Contains(logs.String(), `msg="leftover temporary swap file deleted" path=`+second+resizeSuffix) {
			t.Errorf("logs = %s, want the leftover deleted", logs)
		}
	})

	t.Run("on", func(t *testing.T) { leftoverOnIT(t, temporary, length) })
}

// leftoverOnIT has the temporary file left on beside /swap.img: it is a
// temporary row LinuxIO can remove, /swap.img cannot be removed beside it,
// and Remove turns it off and deletes it with /etc/fstab unchanged.
func leftoverOnIT(t *testing.T, temporary string, length uint64) {
	t.Helper()
	fstab := readFile(t, etcFstab)
	makeLeftover(t, temporary)
	if err := host.swapOn(t.Context(), temporary, ""); err != nil {
		t.Fatal(err)
	}
	waitSwapListed(t, host, temporary, true)
	row := mustRow(t, temporary)
	if !row.Temporary || !row.Active || row.LockedReason != "" || row.InFstab {
		t.Fatalf("row = %+v, want an active temporary row LinuxIO can remove", row)
	}
	if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: rigSwapFile}, logProgress(t)); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("remove %s with its temporary file on: %v, want a conflict", rigSwapFile, err)
	}
	if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: temporary}, logProgress(t)); err != nil {
		t.Fatal(err)
	}
	if listed, _ := swapListed(host, temporary); listed {
		t.Errorf("%s still swaps", temporary)
	}
	if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is still on disk: %v", temporary, err)
	}
	if got := readFile(t, etcFstab); got != fstab {
		t.Errorf("/etc/fstab = %q, want it unchanged: %q", got, fstab)
	}
	assertSwapsAt(t, rigSwapFile, length)
}

// makeLeftover makes a swap file that does not swap at path, as a resize
// that was cut short leaves its temporary file.
func makeLeftover(t *testing.T, path string) {
	t.Helper()
	if _, err := host.makeSwapFile(t.Context(), path, 256*mib); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if listed, _ := swapListed(host, path); listed {
			_ = swapOff(context.Background(), path)
		}
		_ = os.Remove(path)
	})
}

// makeHandMadeSwapFile makes a swap file at path the way an admin does by
// hand: the file, its fstab line and swapon. Cleanup takes it away again.
func makeHandMadeSwapFile(t *testing.T, path string, size uint64) {
	t.Helper()
	ctx := t.Context()
	t.Cleanup(func() {
		bg := context.Background()
		if listed, _ := swapListed(host, path); listed {
			_ = swapOff(bg, path)
		}
		_ = host.removeFstabSwap(bg, path)
		_ = os.Remove(path)
	})
	if _, err := host.makeSwapFile(ctx, path, size); err != nil {
		t.Fatal(err)
	}
	if err := host.addFstabSwap(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := host.swapOn(ctx, path, fstabSwapOptions); err != nil {
		t.Fatal(err)
	}
	waitSwapListed(t, host, path, true)
}

// TestSwapFileRemoveMissingIntegration removes a row whose file is missing,
// as a btrfs rollback leaves LinuxIO's: only its /etc/fstab line goes, and
// every other byte of the file stays.
func TestSwapFileRemoveMissingIntegration(t *testing.T) {
	needRoot(t)
	asJobHost(t)
	const missing = "/linuxio-it-missing.swap"
	fstab := readFile(t, etcFstab)
	t.Cleanup(func() { _ = host.removeFstabSwap(context.Background(), missing) })
	appendFstab(t, missing+" none swap sw,nofail 0 0\n")
	row := mustRow(t, missing)
	if row.Active || !row.InFstab || row.LockedReason != "" || row.ResizeBlockedReason != apischema.SwapFileResizeMissing {
		t.Fatalf("row = %+v, want a missing file LinuxIO can remove", row)
	}
	if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: missing}, logProgress(t)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, etcFstab); got != fstab {
		t.Errorf("/etc/fstab = %q, want %q", got, fstab)
	}
	if _, ok := findRow(freshStatus(t), missing); ok {
		t.Errorf("%s is still a row", missing)
	}
}

// TestSwapFileMoveIntegration moves the rig's /swap.img, which on a btrfs
// root is in the subvolume of /, into LinuxIO's own subvolume: before, a
// snapshot of that subvolume is refused while the file swaps; after, LinuxIO's
// file swaps at the same size, /swap.img and its line are gone, and the
// snapshot succeeds. On another filesystem the row must offer no move and
// block no snapshot, and the test stops there.
func TestSwapFileMoveIntegration(t *testing.T) {
	needRoot(t)
	asJobHost(t)
	row := mustRow(t, rigSwapFile)
	if !row.Active || row.LockedReason != "" {
		t.Skipf("%s is not an active swap file LinuxIO can change (%+v): the rig adds it", rigSwapFile, row)
	}
	if row.FSType != "btrfs" {
		if row.CanMove || row.BlocksSnapshots {
			t.Errorf("row on %s = %+v, want no move and no blocked snapshot", row.FSType, row)
		}
		t.Skipf("%s is on %s, not btrfs", rigSwapFile, row.FSType)
	}
	if !row.CanMove || !row.BlocksSnapshots {
		t.Fatalf("row = %+v, want it to block snapshots and offer the move", row)
	}
	length := fileLength(t, rigSwapFile)
	keepSwapFiles(t)
	subvolume := subvolumeHolding(t, filepath.Dir(rigSwapFile))
	if out, err := snapshotIT(t, subvolume); err == nil || !strings.Contains(out, "Text file busy") {
		t.Fatalf("snapshot of %s while %s swaps: %v, %s; want it refused as busy", subvolume, rigSwapFile, err, out)
	}
	if _, err := MoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: rigSwapFile}, logProgress(t)); err != nil {
		t.Fatal(err)
	}
	assertSwapFileGone(t, rigSwapFile)
	assertSwapsAt(t, ownSwapFilePath, moveSize(length))
	if out, err := snapshotIT(t, subvolume); err != nil {
		t.Fatalf("snapshot of %s after the move: %v, %s", subvolume, err, out)
	}
}

// subvolumeHolding is the root of the btrfs subvolume that holds dir: the
// nearest folder up from it whose inode is 256.
func subvolumeHolding(t *testing.T, dir string) string {
	t.Helper()
	for {
		var st unix.Stat_t
		if err := unix.Stat(dir, &st); err != nil {
			t.Fatal(err)
		}
		if st.Ino == 256 || dir == "/" {
			return dir
		}
		dir = filepath.Dir(dir)
	}
}

// snapshotIT snapshots the subvolume at dir into it and, when that worked,
// deletes the snapshot again.
func snapshotIT(t *testing.T, dir string) (string, error) {
	t.Helper()
	target := filepath.Join(dir, ".linuxio-it-snapshot")
	out, err := exec.CommandContext(t.Context(), "btrfs", "subvolume", "snapshot", dir, target).CombinedOutput()
	if err == nil {
		if del, delErr := exec.CommandContext(context.WithoutCancel(t.Context()), "btrfs", "subvolume", "delete", "--commit-after", target).CombinedOutput(); delErr != nil {
			t.Errorf("delete the snapshot %s: %v, %s", target, delErr, del)
		}
	}
	t.Logf("btrfs subvolume snapshot %s %s: %v\n%s", dir, target, err, out)
	return string(out), err
}

// TestSwapPartitionIntegration runs every operation beside a swap partition,
// LINUXIO_MEMORY_PARTITION (the rig's vdb, at priority 10): it is never a
// row, never turned off and its fstab line never changes. With data parked
// only on it, a drain is refused with other_swap. /swap.img is resized
// beside it, LinuxIO's file made and removed, and zram chosen beside it: the
// host then reads zram, not mixed, and goes back to disk on the partition
// without a swap file of LinuxIO's.
func TestSwapPartitionIntegration(t *testing.T) {
	needRoot(t)
	partition := os.Getenv("LINUXIO_MEMORY_PARTITION")
	if partition == "" {
		t.Skip("set LINUXIO_MEMORY_PARTITION to a swap partition the rig turned on")
	}
	asJobHost(t)
	e := itExecutor(t)
	view, err := e.readView(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.status.ZramProvider == "" {
		t.Skip("neither zram-tools nor zram-generator is installed")
	}
	i := slices.IndexFunc(view.status.Devices, func(d apischema.SwapDevice) bool { return d.Path == partition })
	if i < 0 || view.status.Devices[i].Kind != apischema.SwapDevicePartition {
		t.Fatalf("devices = %+v, want %s swapping as a partition", view.status.Devices, partition)
	}
	before := view.status.Devices[i]
	if _, ok := findRow(view.status, partition); ok {
		t.Fatalf("%s is a swap file row: %+v", partition, view.status.SwapFiles)
	}
	row := onlyFileBeside(t, view.status, rigSwapFile)
	length := fileLength(t, rigSwapFile)
	snapshot := takeHostSnapshot(t, view.status)
	t.Cleanup(func() { snapshot.restore(t, e, view.status.ZramProvider) })
	fstabLines := partitionLines(t, partition)
	untouched := func(after string) {
		t.Helper()
		dev := waitSwapListed(t, host, partition, true)
		if dev.Priority != before.Priority || dev.Size != before.Size {
			t.Errorf("after %s %s = %+v, want it as it was: %+v", after, partition, dev, before)
		}
		if got := partitionLines(t, partition); !slices.Equal(got, fstabLines) {
			t.Errorf("after %s the fstab lines of %s = %q, want %q", after, partition, got, fstabLines)
		}
	}

	holders := assertDrainRefusedOnPartition(t, e, partition, row)
	untouched("the refused drain")

	resizeIT(t, rigSwapFile, length+256*mib)
	untouched("a resize of " + rigSwapFile)
	resizeIT(t, rigSwapFile, length)
	untouched("a resize back")

	if _, err := CreateSwapFile(t.Context(), apischema.SwapFileCreateRequest{Size: 256 * mib}, logProgress(t)); err != nil {
		t.Fatal(err)
	}
	untouched("a create")
	if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: ownSwapFilePath}, logProgress(t)); err != nil {
		t.Fatal(err)
	}
	untouched("a remove")

	got := switchTo(t, apischema.SwapModeZram)
	if got.Mode != apischema.SwapModeZram || !hasDevice(got.Devices, apischema.SwapDeviceZram) {
		t.Errorf("after zram beside %s: mode %s, devices %+v; want zram", partition, got.Mode, got.Devices)
	}
	untouched("a switch to zram")
	got = switchTo(t, apischema.SwapModeDisk)
	if hasDevice(got.Devices, apischema.SwapDeviceZram) || slices.ContainsFunc(got.SwapFiles, func(r apischema.SwapFileRow) bool { return r.Path == ownSwapFilePath }) {
		t.Errorf("after disk on %s: devices %+v, rows %+v; want no zram and no swap file made", partition, got.Devices, got.SwapFiles)
	}
	untouched("a switch to disk")
	checkHolders(t, holders)
}

// assertDrainRefusedOnPartition parks data on the partition alone, the
// swap file's own brought back first, and checks that a drain is refused
// with other_swap. It returns the processes holding the data.
func assertDrainRefusedOnPartition(t *testing.T, e *executor, partition string, row apischema.SwapFileRow) []drainHolder {
	t.Helper()
	if row.Used > 0 {
		if _, err := DrainSwap(t.Context(), logProgress(t)); err != nil {
			t.Fatalf("drain %s first: %v", rigSwapFile, err)
		}
	}
	enterDrainCgroup(t)
	holders := startDrainHolders(t, 256/drainHolders)
	reclaimDrainCgroup(t, e, partition, 256<<20)
	status := freshStatus(t)
	if fileRow, _ := findRow(status, rigSwapFile); fileRow.Used != 0 {
		t.Fatalf("%s holds %d KiB: the data must sit only on %s", rigSwapFile, fileRow.Used>>10, partition)
	}
	if status.DrainBlockedReason != apischema.SwapDrainOtherSwap || status.CanDrain {
		t.Errorf("drain blocked %q, can drain %v; want other_swap", status.DrainBlockedReason, status.CanDrain)
	}
	if _, err := DrainSwap(t.Context(), logProgress(t)); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("drain with the data on %s: %v, want a conflict", partition, err)
	}
	return holders
}

// onlyFileBeside fails the test unless path is the one swap file, active and
// one LinuxIO can change, beside the partition.
func onlyFileBeside(t *testing.T, status apischema.SwapStatus, path string) apischema.SwapFileRow {
	t.Helper()
	row, ok := findRow(status, path)
	if !ok || !row.Active || row.LockedReason != "" || len(status.SwapFiles) != 1 || hasDevice(status.Devices, apischema.SwapDeviceZram) {
		t.Fatalf("swap files %+v, devices %+v; want %s alone beside the partition, no zram", status.SwapFiles, status.Devices, path)
	}
	return row
}

// partitionLines are the /etc/fstab lines whose first field is device.
func partitionLines(t *testing.T, device string) []string {
	t.Helper()
	var lines []string
	for line := range strings.Lines(readFile(t, etcFstab)) {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == device {
			lines = append(lines, line)
		}
	}
	return lines
}
