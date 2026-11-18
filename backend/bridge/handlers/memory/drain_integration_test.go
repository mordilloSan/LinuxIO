package memory

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// drainHolders is how many processes hold the parked data between them. The
// kernel looks for a pending signal between one process's memory and the
// next, not inside one, so a cancelled swapoff ends only when the process it
// is reading back is done; with one process it would read everything.
const drainHolders = 8

// drainBound is how long the full drain may take: the capped run reads its
// data back in under three minutes.
const drainBound = 15 * time.Minute

// drainTestCgroup is the cgroup the test and its holders run in, so that it
// can be told to reclaim their memory and to cap their reads.
const drainTestCgroup = "/sys/fs/cgroup/linuxio-drain-test"

// TestMemoryDrainIntegration parks real data on a swap device and brings it
// back through the drain, on the real kernel and systemd: a drain cancelled
// while swapoff reads pages back must leave the device swapping with most
// of the data still on it, and a full drain must empty the device, start it
// again and leave the data as it was.
//
// LINUXIO_MEMORY_DRAIN_DEVICE names the device, which must be active and in
// /etc/fstab, and LINUXIO_MEMORY_DRAIN_MIB is how much to park.
// LINUXIO_MEMORY_DRAIN_READ_BPS caps the reads from the device, in bytes a
// second, once the data is parked, and LINUXIO_MEMORY_DRAIN_MIN_SECONDS is
// how long the full drain then has to take at least: together they show a
// swapoff that outlasts the 90 seconds systemd gives a swap unit. It needs
// root, so it runs only in the disposable test VM:
// `make vm-test-memory-integration`.
func TestMemoryDrainIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	device := os.Getenv("LINUXIO_MEMORY_DRAIN_DEVICE")
	if os.Geteuid() != 0 || device == "" {
		t.Skip("needs root and LINUXIO_MEMORY_DRAIN_DEVICE")
	}
	parkMiB := envInt(t, "LINUXIO_MEMORY_DRAIN_MIB", 256)
	if parkMiB < drainHolders {
		t.Fatalf("LINUXIO_MEMORY_DRAIN_MIB=%d, want at least 1 MiB for each of the %d holders", parkMiB, drainHolders)
	}
	readBPS := envInt(t, "LINUXIO_MEMORY_DRAIN_READ_BPS", 0)
	atLeast := time.Duration(envInt(t, "LINUXIO_MEMORY_DRAIN_MIN_SECONDS", 0)) * time.Second
	ctx := t.Context()
	e := newExecutor(func(p apischema.SwapProgress) { t.Logf("step %d/%d %s %s", p.Index, p.Total, p.Step, p.Device) })
	unit := drainUnit(t, e, device)
	stopSwapAheadOf(t, e, device)

	enterDrainCgroup(t)
	holders := startDrainHolders(t, parkMiB/drainHolders)
	reclaimDrainCgroup(t, e, device, uint64(parkMiB/drainHolders*drainHolders)<<20)
	parked := swapUsed(t, e, device)
	t.Logf("parked %d MiB on %s in %d processes", parked>>20, device, drainHolders)
	if readBPS > 0 {
		capDrainReads(t, device, readBPS)
	}

	left := cancelDrainMidway(t, e, device, parked)
	assertSwapUnitActive(t, e, device, unit)
	t.Logf("cancelled with %d MiB still parked on %s", left>>20, device)

	// Each wait in the test is bounded, the full drain too, so a host where
	// the data does not move fails rather than hangs.
	drainCtx, cancel := context.WithTimeout(ctx, drainBound)
	defer cancel()
	start := time.Now()
	if _, err := e.drainSwap(drainCtx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("drained %d MiB in %s", left>>20, elapsed.Round(time.Second))
	if after := swapUsed(t, e, device); after > parked/10 {
		t.Errorf("%d MiB on %s after the drain, want it empty", after>>20, device)
	}
	assertSwapUnitActive(t, e, device, unit)
	if elapsed < atLeast {
		t.Errorf("the drain took %s, want more than %s to show a swapoff that outlasts systemd's timeout", elapsed.Round(time.Second), atLeast)
	}
	for i, holder := range holders {
		if err := holder.check(); err != nil {
			t.Errorf("holder %d: the data that came back from swap is not the data that was parked: %v", i, err)
		}
	}
}

// TestMemoryDrainAliasIntegration drains the swap device
// LINUXIO_MEMORY_DRAIN_DEVICE, which /etc/fstab names by UUID=, through its
// unit, dev-disk-by\x2duuid-<uuid>.swap: an alias of the device's own unit.
// Once swapoff took the device away the alias must read inactive within the
// 15 seconds a drain waits for it, or the drain would wait them out and its
// start would do nothing; a start of the alias must bring the device back.
// The rig names the drain's data disk by UUID= for this test.
func TestMemoryDrainAliasIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	device := os.Getenv("LINUXIO_MEMORY_DRAIN_DEVICE")
	if os.Geteuid() != 0 || device == "" {
		t.Skip("needs root and LINUXIO_MEMORY_DRAIN_DEVICE")
	}
	ctx := t.Context()
	e := newExecutor(func(apischema.SwapProgress) {})
	unit := drainUnit(t, e, device)
	if !strings.HasPrefix(unit, `dev-disk-by\x2duuid-`) {
		t.Fatalf("/etc/fstab names %s with the unit %s, want one by UUID=", device, unit)
	}
	own, escapeErr := systemd.EscapePath(device)
	if escapeErr != nil {
		t.Fatal(escapeErr)
	}
	own += ".swap"
	assertSwapUnitActive(t, e, device, unit)
	if err := e.swapOffDevice(ctx, device); err != nil {
		t.Fatalf("swapoff %s: %v", device, err)
	}
	start := time.Now()
	if err := waitUntil(ctx, e.waits.stopGrace, e.waits.poll, func() (bool, error) {
		state, err := e.sys.activeState(ctx, unit)
		return state != "active" && state != "deactivating", err
	}); err != nil {
		t.Fatalf("%s still reads active %s after %s left: %v", unit, e.waits.stopGrace, device, err)
	}
	t.Logf("%s read off %s after swapoff took %s away", unit, time.Since(start).Round(time.Millisecond), device)
	if err := e.sys.startUnit(ctx, unit); err != nil {
		t.Fatal(err)
	}
	// One start must do: a drain sends it again only after stopGrace.
	if err := waitUntil(ctx, e.waits.stopGrace, e.waits.poll, e.deviceIs(device, true)); err != nil {
		t.Fatalf("a start of %s did not bring %s back: %v", unit, device, err)
	}
	assertSwapUnitActive(t, e, device, unit)
	assertSwapUnitActive(t, e, device, own)
	view, err := e.readView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(view.fstab, func(entry fstabSwap) bool { return entry.device == device })
	start = time.Now()
	if err := e.drainDevice(ctx, view.fstab[i], 0); err != nil {
		t.Fatalf("drain %s: %v", device, err)
	}
	if elapsed := time.Since(start); elapsed >= e.waits.stopGrace {
		t.Errorf("the drain of %s took %s, want less than the %s it waits for %s", device, elapsed.Round(time.Second), e.waits.stopGrace, unit)
	}
	assertSwapUnitActive(t, e, device, unit)
}

// TestMemoryDrainHolder is one of the processes TestMemoryDrainIntegration
// starts: it fills memory with data that does not compress, so zswap passes
// it on to the disk, asks the kernel to page it out, says it is ready, and
// when told to checks that the data is what it was.
func TestMemoryDrainHolder(t *testing.T) {
	size := envInt(t, "LINUXIO_MEMORY_DRAIN_HOLD_MIB", 0) << 20
	if size == 0 {
		t.Skip("started by TestMemoryDrainIntegration")
	}
	data, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatalf("map %d MiB: %v", size>>20, err)
	}
	seed := [32]byte{byte(envInt(t, "LINUXIO_MEMORY_DRAIN_HOLD_SEED", 0))}
	_, _ = rand.NewChaCha8(seed).Read(data)
	sum := sha256.Sum256(data)
	if err := unix.Madvise(data, unix.MADV_PAGEOUT); err != nil {
		t.Fatalf("page out: %v", err)
	}
	fmt.Println("ready")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatalf("wait to be told to check: %v", err)
	}
	if sha256.Sum256(data) != sum {
		t.Fatal("the data changed")
	}
}

// drainHolder is one running TestMemoryDrainHolder.
type drainHolder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

// check tells the holder to compare its data and waits for its verdict.
func (h drainHolder) check() error {
	if _, err := io.WriteString(h.stdin, "check\n"); err != nil {
		return err
	}
	return h.cmd.Wait()
}

// startDrainHolders starts the holders, each with mib MiB, and waits until
// every one has filled its memory.
func startDrainHolders(t *testing.T, mib int) []drainHolder {
	t.Helper()
	holders := make([]drainHolder, 0, drainHolders)
	for i := range drainHolders {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMemoryDrainHolder$", "-test.count=1")
		cmd.Env = append(os.Environ(), "LINUXIO_MEMORY_DRAIN_HOLD_MIB="+strconv.Itoa(mib), "LINUXIO_MEMORY_DRAIN_HOLD_SEED="+strconv.Itoa(i+1))
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start holder %d: %v", i, err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
			t.Fatalf("holder %d said %q, %v; want ready", i, line, err)
		}
		holders = append(holders, drainHolder{cmd: cmd, stdin: stdin})
	}
	return holders
}

// enterDrainCgroup moves the test into a cgroup of its own under the root,
// which has the memory and io controllers, and puts it back at the end.
func enterDrainCgroup(t *testing.T) {
	t.Helper()
	text, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(string(text)), "0::")
	if !ok {
		t.Fatalf("/proc/self/cgroup = %q, want a cgroup v2 path", text)
	}
	home := filepath.Join("/sys/fs/cgroup", name, "cgroup.procs")
	if err := os.Mkdir(drainTestCgroup, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	pid := []byte(strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(filepath.Join(drainTestCgroup, "cgroup.procs"), pid, 0); err != nil {
		t.Fatalf("enter %s: %v", drainTestCgroup, err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(home, pid, 0); err != nil {
			t.Errorf("leave %s: %v", drainTestCgroup, err)
		}
		// The holders are killed by their own cleanups, which ran first.
		for range 50 {
			if err := os.Remove(drainTestCgroup); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("%s could not be removed", drainTestCgroup)
	})
}

// stopSwapAheadOf makes device the swap the kernel fills first, so the data
// parks there. zram, which a zram-generator install starts beside the swap
// file at priority 100, is stopped through its provider's unit and started
// again when the test ends; any other device ahead of it is a reason to
// skip, since nothing here should stop it.
func stopSwapAheadOf(t *testing.T, e *executor, device string) {
	t.Helper()
	ctx := t.Context()
	devices, err := e.root.readSwaps()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(devices, func(d apischema.SwapDevice) bool { return d.Path == device })
	if i < 0 {
		t.Fatalf("%s does not swap", device)
	}
	zramAhead := false
	for _, d := range devices {
		switch {
		case d.Path == device || d.Priority < devices[i].Priority:
		case d.Kind == apischema.SwapDeviceZram:
			zramAhead = true
		default:
			t.Skipf("%s swaps at priority %d, ahead of %s at %d: the data would not park on %s", d.Path, d.Priority, device, devices[i].Priority, device)
		}
	}
	if !zramAhead {
		return
	}
	provider := host.zramProvider(ctx, systemd.GetActiveState)
	unit := zramService
	if provider == apischema.ZramProviderGenerator {
		unit = zramGeneratorSwapUnit
	}
	t.Logf("zram swaps ahead of %s: stopping %s until the test ends", device, unit)
	// Registered first: a stop that fails or is not waited out may still
	// take zram away.
	t.Cleanup(func() {
		restartCtx := context.WithoutCancel(ctx)
		if err := resetLeftZram(restartCtx, e, provider); err != nil {
			t.Errorf("start zram again: %v", err)
		} else if err := e.startUntilBack(restartCtx, unit, e.zramIs(true)); err != nil {
			t.Errorf("start zram again: %v", err)
		}
	})
	if err := e.sys.stopUnit(ctx, unit); err != nil {
		t.Fatalf("stop %s: %v", unit, err)
	}
	if err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, e.zramIs(false)); err != nil {
		t.Fatalf("wait for zram to stop swapping: %v", err)
	}
}

// reclaimDrainCgroup has the kernel move the cgroup's memory to swap until
// want bytes are on device. MADV_PAGEOUT in the holders starts the writes,
// but a page whose write is done stays in the swap cache until memory is
// short, and swapoff would then find it in memory and read nothing back from
// the disk. So the cgroup is told to reclaim, a little at a time since a
// request for more than it holds never returns, until the data is on the
// device and out of the swap cache.
func reclaimDrainCgroup(t *testing.T, e *executor, device string, want uint64) {
	t.Helper()
	const cached = 16 << 20
	deadline := time.Now().Add(10 * time.Minute)
	elsewhere := swapUsedElsewhere(t, e, device)
	for {
		// The write fails with EAGAIN when less than asked was reclaimed.
		err := writeReclaim("16M")
		used, inCache := swapUsed(t, e, device), cgroupStat(t, "swapcached")
		if used >= want*95/100 && inCache <= cached {
			return
		}
		// Data that goes to another swap device will not reach this one:
		// stop at once rather than at the deadline.
		if more := swapUsedElsewhere(t, e, device); more > elsewhere+want/4 {
			t.Fatalf("%d MiB went to other swap devices and %d MiB to %s: the data does not park there", (more-elsewhere)>>20, used>>20, device)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d MiB on %s and %d MiB in the swap cache after 10 minutes, want %d MiB parked (last reclaim: %v)", used>>20, device, inCache>>20, want>>20, err)
		}
		if err != nil {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// writeReclaim asks the test's cgroup to reclaim amount. It writes with
// system calls and not os.WriteFile: the kernel answers EAGAIN when it
// reclaimed less than asked, and Go's poller takes EAGAIN on a cgroup file,
// which it can poll, as "wait until writable", which never comes, so the
// test hung there.
func writeReclaim(amount string) error {
	fd, err := unix.Open(filepath.Join(drainTestCgroup, "memory.reclaim"), unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	_, err = unix.Write(fd, []byte(amount))
	return err
}

// cgroupStat reads one counter, in bytes, from the cgroup's memory.stat.
func cgroupStat(t *testing.T, key string) uint64 {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(drainTestCgroup, "memory.stat"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(text)) {
		if value, ok := strings.CutPrefix(line, key+" "); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				t.Fatalf("memory.stat %s: %v", key, err)
			}
			return n
		}
	}
	t.Fatalf("memory.stat has no %s", key)
	return 0
}

// capDrainReads caps what the cgroup reads from device, in bytes a second,
// until the test ends. swapoff runs in the cgroup, so its reads are capped.
func capDrainReads(t *testing.T, device string, bps int) {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Stat(device, &stat); err != nil {
		t.Fatal(err)
	}
	disk := fmt.Sprintf("%d:%d", unix.Major(stat.Rdev), unix.Minor(stat.Rdev))
	ioMax := filepath.Join(drainTestCgroup, "io.max")
	if err := os.WriteFile(ioMax, []byte(disk+" rbps="+strconv.Itoa(bps)), 0); err != nil {
		t.Fatalf("cap the reads from %s: %v", device, err)
	}
	t.Cleanup(func() { _ = os.WriteFile(ioMax, []byte(disk+" rbps=max"), 0) })
	t.Logf("reads from %s capped at %d bytes a second", device, bps)
}

// cancelDrainMidway starts a drain, cancels it once swapoff has read some
// of the parked bytes back, and returns what is still parked: most of it,
// on a device that still swaps. swapoff finishes the process it is reading
// back before it sees the cancel, so up to one holder more comes back.
func cancelDrainMidway(t *testing.T, e *executor, device string, parked uint64) uint64 {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	drained := make(chan error, 1)
	go func() {
		_, err := e.drainSwap(ctx)
		drained <- err
	}()
	if err := waitUntil(t.Context(), 10*time.Minute, 20*time.Millisecond, func() (bool, error) {
		select {
		case err := <-drained:
			return false, fmt.Errorf("the drain ended before it could be cancelled: %w", err)
		default:
			return swapUsed(t, e, device) < parked-parked/20, nil
		}
	}); err != nil {
		t.Fatalf("wait for the swapoff to read pages back: %v", err)
	}
	cancel()
	if err := <-drained; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled drain: %v, want the cancel", err)
	}
	left := swapUsed(t, e, device)
	if left < parked/2 || left >= parked {
		t.Fatalf("%d MiB on %s after the cancel, want some of the %d MiB read back and most of it still parked", left>>20, device, parked>>20)
	}
	return left
}

func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	text := os.Getenv(name)
	if text == "" {
		return fallback
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 {
		t.Fatalf("%s=%q is not a number", name, text)
	}
	return n
}

// drainUnit returns the unit of the fstab entry active as device.
func drainUnit(t *testing.T, e *executor, device string) string {
	t.Helper()
	view, err := e.readView(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range view.fstab {
		if entry.device == device {
			return entry.unit
		}
	}
	t.Fatalf("%s is not an active swap device /etc/fstab names: %+v", device, view.fstab)
	return ""
}

// swapUsedElsewhere is the bytes in use on the swap devices other than
// device.
func swapUsedElsewhere(t *testing.T, e *executor, device string) uint64 {
	t.Helper()
	devices, err := e.root.readSwaps()
	if err != nil {
		t.Fatal(err)
	}
	var used uint64
	for _, d := range devices {
		if d.Path != device {
			used += d.Used
		}
	}
	return used
}

// swapUsed is the bytes in use on device, or 0 when it does not swap.
func swapUsed(t *testing.T, e *executor, device string) uint64 {
	t.Helper()
	devices, err := e.root.readSwaps()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices {
		if d.Path == device {
			return d.Used
		}
	}
	return 0
}

func assertSwapUnitActive(t *testing.T, e *executor, device, unit string) {
	t.Helper()
	if on, err := e.deviceIs(device, true)(); err != nil || !on {
		t.Fatalf("%s active = %v, %v; want it swapping", device, on, err)
	}
	if state, err := e.sys.activeState(t.Context(), unit); err != nil || state != "active" {
		t.Errorf("%s = %q, %v; want active", unit, state, err)
	}
}
