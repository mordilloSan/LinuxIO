package btrfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TestBtrfsResumeIntegration interrupts long btrfs operations against a real
// kernel and checks what is left: a two-disk raid1 pool on 8 GiB loop disks,
// plus a third blank disk the steps share and leave blank for the next one.
// The disks are sparse and far bigger than the data: every interrupted
// balance leaves chunks half filled, and on 4 GiB disks the later balances
// sometimes found no unallocated room (ENOSPC).
// Run it with `make vm-test-btrfs-integration`.
func TestBtrfsResumeIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs resume integration test needs root")
	}
	t.Setenv("TMPDIR", "/var/tmp") // the disks fill up; /tmp may be a small tmpfs
	dir := t.TempDir()
	stubStatePaths(t, dir)
	p := &itPool{t: t, ctx: context.Background(), mnt: filepath.Join(dir, "mnt")}
	for i := range 3 {
		p.disks = append(p.disks, loopDevice(t, filepath.Join(dir, fmt.Sprintf("disk%d.img", i)), 8<<30))
	}
	uuid, err := CreatePool(p.ctx, "linuxio-resume", "raid1", "raid1", p.disks[:2], true, nil)
	p.must("create pool", err)
	p.uuid = uuid
	p.must("mount pool", MountPool(p.ctx, p.disks[0], p.mnt, []string{"noatime"}, false))
	t.Cleanup(func() { _ = unix.Unmount(p.mnt, 0) })
	began := time.Now()
	t.Cleanup(func() {
		if t.Failed() {
			logKernelBtrfs(t, began)
		}
	})
	var uts unix.Utsname
	p.must("uname", unix.Uname(&uts))
	t.Logf("kernel %s", unix.ByteSliceToString(uts.Release[:]))

	for _, step := range []struct {
		name string
		run  func(*itPool)
	}{
		{"signals", (*itPool).signals},
		{"removeCancel", (*itPool).removeCancel},
		{"removeKill", (*itPool).removeKill},
		{"rollback", (*itPool).rollback},
		{"balancePauseRemount", (*itPool).balancePauseRemount},
		{"balancePauseSameMount", (*itPool).balancePauseSameMount},
		{"refusedWhilePaused", (*itPool).refusedWhilePaused},
		{"balanceKill", (*itPool).balanceKill},
		{"balanceCancel", (*itPool).balanceCancel},
		{"scrubResume", (*itPool).scrubResume},
		{"scrubKill", (*itPool).scrubKill},
		{"replaceKill", (*itPool).replaceKill},
		{"replacePowerCut", (*itPool).replacePowerCut},
	} {
		if !t.Run(step.name, func(t *testing.T) {
			s := *p
			s.t = t
			step.run(&s)
		}) {
			t.FailNow() // the steps share the pool
		}
	}
}

// logKernelBtrfs logs what the kernel said about btrfs since a failed test
// began: that is where it explains an aborted transaction or a refusal.
func logKernelBtrfs(t *testing.T, since time.Time) {
	out, err := exec.Command("journalctl", "--dmesg", "--no-pager", "--output=short-monotonic", "--since", since.Format("2006-01-02 15:04:05")).CombinedOutput()
	if err != nil {
		t.Logf("kernel log not read: %v: %s", err, out)
		return
	}
	var lines []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(strings.ToLower(line), "btrfs") {
			lines = append(lines, line)
		}
	}
	t.Logf("kernel btrfs messages since the test began (last %d of %d):\n%s", min(len(lines), 80), len(lines), strings.Join(lines[max(len(lines)-80, 0):], "\n"))
}

// logAllocation logs how much of each disk is allocated. A relocation writes
// new chunks, so it needs unallocated room on the disks.
func (p *itPool) logAllocation(when string) {
	p.t.Helper()
	fs := p.filesystem()
	var disks []string
	for _, d := range fs.Devices {
		disks = append(disks, fmt.Sprintf("device %d has %d of %d MiB allocated", d.ID, d.UsedBytes>>20, d.SizeBytes>>20))
	}
	p.t.Logf("%s: %s; %+v", when, strings.Join(disks, ", "), fs.Allocation)
}

// TestBtrfsIntegrationChild runs one operation for runChild, so a step can
// kill the process that issued the ioctl: scrub, balance, remove:<id> or
// replace:<id>:<path>.
func TestBtrfsIntegrationChild(t *testing.T) {
	op := os.Getenv("LINUXIO_BTRFS_IT_CHILD")
	if op == "" {
		t.Skip("run by runChild")
	}
	stubStatePaths(t, t.TempDir())
	ctx, uuid := context.Background(), os.Getenv("LINUXIO_BTRFS_IT_UUID")
	kind, rest, _ := strings.Cut(op, ":")
	idText, path, _ := strings.Cut(rest, ":")
	var err error
	switch kind {
	case "scrub":
		_, err = Scrub(ctx, uuid, func(BtrfsScrubStatus) {})
	case "balance":
		err = Balance(ctx, uuid, BalanceOptions{}, func(float64) {})
	case "remove", "replace":
		id, convErr := strconv.Atoi(idText)
		if convErr != nil {
			t.Fatalf("operation %q: %v", op, convErr)
		}
		if kind == "remove" {
			err = RemoveDevice(ctx, uuid, id, func(float64) {})
		} else {
			err = Replace(ctx, uuid, id, path, func(float64) {})
		}
	default:
		t.Fatalf("unknown operation %q", op)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// runChild starts TestBtrfsIntegrationChild running op on the pool in a new
// process. The step kills or waits for it; it is killed at the latest when
// the step ends, and its output is logged if the step failed.
func (p *itPool) runChild(op string) *exec.Cmd {
	p.t.Helper()
	exe, err := os.Executable()
	p.must("test binary", err)
	cmd := exec.Command(exe, "-test.run=^TestBtrfsIntegrationChild$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "LINUXIO_BTRFS_IT_CHILD="+op, "LINUXIO_BTRFS_IT_UUID="+p.uuid)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	p.must("start child "+op, cmd.Start())
	p.t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if p.t.Failed() {
			p.t.Logf("child %s:\n%s", op, out.String())
		}
	})
	return cmd
}

// fill writes mib MiB of random data to a new file in the pool and commits
// it, so scrub and relocation have that much to move.
func (p *itPool) fill(mib int) {
	p.t.Helper()
	f, err := os.CreateTemp(p.mnt, "fill-*")
	p.must("create fill file", err)
	defer f.Close()
	_, err = io.CopyN(f, rand.Reader, int64(mib)<<20)
	p.must("fill", err)
	unix.Sync()
}

// exclusiveOp is the operation holding the pool: none, balance, balance
// paused, device remove, device replace and so on.
func (p *itPool) exclusiveOp() string {
	p.t.Helper()
	op, err := os.ReadFile(filepath.Join("/sys/fs/btrfs", p.uuid, "exclusive_operation"))
	p.must("exclusive operation", err)
	return strings.TrimSpace(string(op))
}

// sigurgStorm sends SIGURG to every thread of the process until stop is
// called, which returns how many it sent. The runtime ignores a SIGURG it
// did not ask for, so only an ioctl the signal interrupts notices.
func sigurgStorm() (stop func() int) {
	quit, sent := make(chan struct{}), make(chan int)
	go func() {
		n := 0
		for {
			select {
			case <-quit:
				sent <- n
				return
			default:
			}
			tasks, _ := os.ReadDir("/proc/self/task")
			for _, task := range tasks {
				if tid, err := strconv.Atoi(task.Name()); err == nil && unix.Tgkill(os.Getpid(), tid, unix.SIGURG) == nil {
					n++
				}
			}
		}
	}()
	return func() int {
		close(quit)
		return <-sent
	}
}

// signals scrubs the pool while every thread takes SIGURG. Since 6.19 a
// signal pending on the ioctl thread aborts a scrub with EINTR, which is
// what the unmasked scrub of one device shows first; Scrub must finish.
// Then a child process scrubs too, as the later steps' children do.
func (p *itPool) signals() {
	p.fill(512)
	stop := sigurgStorm()
	args := scrubArgs{DevID: 1, End: ^uint64(0)}
	unmasked := fsIoctl(p.mnt, iocScrub, unsafe.Pointer(&args))
	s, err := Scrub(p.ctx, p.uuid, func(BtrfsScrubStatus) {})
	sent := stop()
	p.t.Logf("unmasked scrub of device 1 under SIGURG: %v; %d signals sent", unmasked, sent)
	p.must("scrub under SIGURG", err)
	if sent == 0 || s.Status != "finished" || s.ErrorCount != 0 || s.BytesScrubbed == 0 {
		p.t.Fatalf("scrub = %+v after %d signals", s, sent)
	}
	p.must("child scrub", p.runChild("scrub").Wait())
	if op := p.exclusiveOp(); op != "none" {
		p.t.Fatalf("exclusive operation = %q after the scrubs", op)
	}
}

// addThird adds the blank third disk and balances the pool onto it, so its
// removal has data to move: new writes would fill free space in the old
// disks' chunks first. It returns the disk's id.
func (p *itPool) addThird() int {
	p.t.Helper()
	p.must("add device", AddDevice(p.ctx, p.uuid, p.disks[2], true))
	p.must("balance onto the third disk", Balance(p.ctx, p.uuid, BalanceOptions{}, func(float64) {}))
	fs := p.filesystem()
	i := slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.Path == p.disks[2] })
	if i < 0 {
		p.t.Fatalf("added disk not a member: %v", devicePaths(fs))
	}
	p.t.Logf("device %d holds %d MiB", fs.Devices[i].ID, fs.Devices[i].UsedBytes>>20)
	return fs.Devices[i].ID
}

// waitFor waits until ready, failing when ended reports first: operation op
// finished before it could be interrupted.
func (p *itPool) waitFor(op string, ready func() bool, ended <-chan error) {
	p.t.Helper()
	for !ready() {
		select {
		case err := <-ended:
			p.t.Fatalf("the %s ended before it was interrupted (%v); raise the fill size", op, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// holds reports whether op holds the pool, for waitFor.
func (p *itPool) holds(op string) func() bool {
	return func() bool { return p.exclusiveOp() == op }
}

// waitExclusive waits until op holds the pool, as waitFor does.
func (p *itPool) waitExclusive(op string, ended <-chan error) {
	p.t.Helper()
	p.waitFor(op, p.holds(op), ended)
}

// balanceMoved reports whether a balance runs and has relocated a chunk.
func (p *itPool) balanceMoved() bool {
	var args balanceArgs
	moved := fsIoctl(p.mnt, iocBalanceProgress, unsafe.Pointer(&args)) == nil &&
		args.State&balanceStateRunning != 0 && args.Stat.Completed > 0
	if moved {
		p.t.Logf("balance relocated %d of %d chunks", args.Stat.Completed, args.Stat.Expected)
	}
	return moved
}

// killWhile runs op in a child and SIGKILLs it once ready.
func (p *itPool) killWhile(op string, ready func() bool) {
	p.t.Helper()
	child := p.runChild(op)
	ended := make(chan error, 1)
	start := time.Now()
	go func() { ended <- child.Wait() }()
	p.waitFor(op, ready, ended)
	_ = child.Process.Kill()
	err := <-ended
	p.t.Logf("killed after %v: %v", time.Since(start), err)
	var exit *exec.ExitError
	switch {
	case err == nil:
		p.t.Fatalf("the %s finished before the kill; raise the fill size", op)
	case !errors.As(err, &exit) || exit.ExitCode() != -1: // -1: ended by a signal
		p.t.Fatalf("child: %v", err)
	}
}

// resumeInterrupted checks what an interrupted removal of device id left:
// nothing holds the pool, the device is still a member and a scrub finds no
// errors. Then ResumeRemove must take the device out.
func (p *itPool) resumeInterrupted(id int) {
	p.t.Helper()
	if op := p.exclusiveOp(); op != "none" {
		p.t.Fatalf("exclusive operation = %q after the interruption", op)
	}
	if !hasDevice(p.filesystem(), id) {
		p.t.Fatalf("device %d left the pool despite the interruption", id)
	}
	p.scrubClean("the interruption")
	// The kernel frees the device's allocation chunk by chunk, which is the
	// only progress a removal has.
	interval := progressInterval
	progressInterval = 50 * time.Millisecond
	defer func() { progressInterval = interval }()
	start := time.Now()
	var reported []float64
	p.must("resume removal", ResumeRemove(p.ctx, p.uuid, id, func(pct float64) { reported = append(reported, pct) }))
	reported = slices.Compact(reported)
	p.t.Logf("resumed removal took %v, reporting %v", time.Since(start), reported)
	if !slices.IsSorted(reported) || !slices.ContainsFunc(reported, func(pct float64) bool { return pct > 0 && pct < 100 }) {
		p.t.Fatalf("removal progress = %v, want a rise between 0 and 100", reported)
	}
	if hasDevice(p.filesystem(), id) {
		p.t.Fatalf("device %d still a member after ResumeRemove", id)
	}
}

// removeCancel cancels a removal while it relocates data off the third
// disk. The kernel must stop it, leaving the disk a member.
func (p *itPool) removeCancel() {
	p.fill(1024)
	id := p.addThird()
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	ended := make(chan error, 1)
	start := time.Now()
	go func() { ended <- RemoveDevice(ctx, p.uuid, id, func(float64) {}) }()
	p.waitExclusive("device remove", ended)
	cancel()
	err := <-ended
	p.t.Logf("canceled after %v: %v", time.Since(start), err)
	if err == nil {
		p.t.Fatal("the removal finished before the cancel landed; raise the fill size")
	}
	if want := fmt.Sprintf("removal of device %d canceled", id); err.Error() != want {
		p.t.Fatalf("removal: %v, want %s", err, want)
	}
	p.resumeInterrupted(id)
}

// removeKill SIGKILLs a process while its removal relocates data off the
// third disk. The kernel must release the pool, leaving the disk a member.
// The pool keeps removeCancel's data, which addThird moves back.
func (p *itPool) removeKill() {
	id := p.addThird()
	p.killWhile(fmt.Sprintf("remove:%d", id), p.holds("device remove"))
	p.resumeInterrupted(id)
}

// rollback undoes a pool create on a new partition of the third disk the way
// a failed create does: the pool's superblocks first, since their offsets
// are relative to the partition, then the partition. It blanks the disk again.
func (p *itPool) rollback() {
	disk := p.disks[2]
	// Held throughout, as CreateBtrfsPool holds it until its undo is done:
	// otherwise udev re-reads the new table itself and the re-read is busy.
	unlock, err := LockDisk(p.ctx, disk)
	p.must("lock disk", err)
	defer unlock()
	part, err := PartitionDisk(p.ctx, disk)
	p.must("partition", err)
	uuid, err := CreatePool(p.ctx, "rollback", "single", "dup", []string{part}, true, nil)
	p.must("create on partition", err)
	parts, err := kernelPartitions(disk)
	p.must("kernel partitions", err)
	if len(parts) != 1 {
		p.t.Fatalf("partitions = %+v, want one", parts)
	}
	wiped, err := WipePoolSuperblocks(part, uuid)
	p.must("wipe superblocks", err)
	if !wiped {
		p.t.Fatal("no superblock of the new pool found")
	}
	p.must("delete partition", DeletePartition(p.ctx, disk, 1))

	if left, listErr := kernelPartitions(disk); listErr != nil || len(left) != 0 {
		p.t.Fatalf("partitions after the delete = %+v, %v", left, listErr)
	}
	f, err := os.Open(disk)
	p.must("open disk", err)
	defer f.Close()
	magic := make([]byte, 8)
	for _, off := range superblockOffsets(parts[0].length) {
		_, err = f.ReadAt(magic, int64(parts[0].start)+off+superMagicAt)
		p.must("read superblock", err)
		if !bytes.Equal(magic, make([]byte, 8)) {
			p.t.Fatalf("superblock at partition offset %d kept its magic % X", off, magic)
		}
	}
	fss, err := ListFilesystems(p.ctx)
	p.must("list filesystems", err)
	if slices.ContainsFunc(fss, func(fs BtrfsFilesystem) bool { return fs.UUID == uuid }) {
		p.t.Fatalf("pool %s still listed", uuid)
	}
	p.must("blank the disk", wipeDeviceEdges(disk))
}

// pauseBalance starts a full balance and pauses it while it relocates, as
// the job host's Suspend does: Balance ends errBalancePaused and the paused
// balance keeps the pool.
func (p *itPool) pauseBalance() {
	p.t.Helper()
	ended := make(chan error, 1)
	go func() { ended <- Balance(p.ctx, p.uuid, BalanceOptions{}, func(float64) {}) }()
	p.waitExclusive("balance", ended)
	if paused, err := PauseBalance(p.ctx, p.uuid); err != nil || !paused {
		p.t.Fatalf("pause balance: paused=%v err=%v", paused, err)
	}
	switch err := <-ended; {
	case err == nil:
		p.t.Fatal("the balance finished before the pause; raise the fill size")
	case !errors.Is(err, errBalancePaused):
		p.t.Fatalf("balance: %v, want %v", err, errBalancePaused)
	}
	if op := p.exclusiveOp(); op != "balance paused" {
		p.t.Fatalf("exclusive operation = %q after the pause", op)
	}
}

// resumeBalance resumes the balance pauseBalance paused and checks it ran to
// the end.
func (p *itPool) resumeBalance() {
	p.t.Helper()
	start := time.Now()
	p.must("resume balance", ResumeBalance(p.ctx, p.uuid, BalanceOptions{}, true, func(float64) {}))
	p.t.Logf("resumed balance took %v", time.Since(start))
	p.noBalance()
}

// noBalance checks that no balance is left, running or paused.
func (p *itPool) noBalance() {
	p.t.Helper()
	if _, _, err := balanceProgressOf(p.mnt); !errors.Is(err, unix.ENOTCONN) {
		p.t.Fatalf("balance progress: %v, want ENOTCONN", err)
	}
	if op := p.exclusiveOp(); op != "none" {
		p.t.Fatalf("exclusive operation = %q", op)
	}
}

// balancePauseRemount pauses a balance and remounts the pool, as a shutdown
// and the next boot do: the kernel resumes it at mount and ResumeBalance
// follows it. The earlier steps' files go first, so the balance has room.
func (p *itPool) balancePauseRemount() {
	old, err := filepath.Glob(filepath.Join(p.mnt, "fill-*"))
	p.must("list fill files", err)
	for _, f := range old {
		p.must("remove fill file", os.Remove(f))
	}
	unix.Sync() // commit the deletes, so their space is free for the fill
	p.fill(1500)
	p.pauseBalance()
	p.must("unmount", unix.Unmount(p.mnt, 0))
	p.must("mount", MountPool(p.ctx, p.disks[0], p.mnt, []string{"noatime"}, false))
	p.resumeBalance()
}

// balancePauseSameMount pauses a balance and resumes it on the same mount,
// through BALANCE_V2's resume flag.
func (p *itPool) balancePauseSameMount() {
	p.pauseBalance()
	p.resumeBalance()
}

// refusedWhilePaused removes a device while a paused balance holds the pool:
// the kernel refuses with code 8, which must not read as success. It leaves
// the third disk removed and blank.
func (p *itPool) refusedWhilePaused() {
	id := p.addThird()
	p.pauseBalance()
	if err := RemoveDevice(p.ctx, p.uuid, id, func(float64) {}); !errors.Is(err, kernelCode(8)) {
		p.t.Fatalf("removal under a paused balance: %v, want %v", err, kernelCode(8))
	}
	p.resumeBalance()
	p.must("remove the third disk", RemoveDevice(p.ctx, p.uuid, id, func(float64) {}))
	p.must("blank the disk", wipeDeviceEdges(p.disks[2]))
}

// balanceKill SIGKILLs a process once its balance has relocated a chunk.
// The kernel cancels the balance and deletes it, so ResumeBalance runs it
// again, and the pool must scrub clean.
func (p *itPool) balanceKill() {
	p.logAllocation("before the balance")
	p.killWhile("balance", p.balanceMoved)
	p.logAllocation("after the kill")
	p.noBalance()
	start := time.Now()
	p.must("run the balance again", ResumeBalance(p.ctx, p.uuid, BalanceOptions{}, false, func(float64) {}))
	p.t.Logf("balance run again in %v", time.Since(start))
	p.noBalance()
	p.scrubClean("the re-run")
}

// scrubClean scrubs the pool and fails unless it finished with no errors.
func (p *itPool) scrubClean(after string) {
	p.t.Helper()
	s, err := Scrub(p.ctx, p.uuid, func(BtrfsScrubStatus) {})
	p.must("scrub after "+after, err)
	if s.Status != "finished" || s.ErrorCount != 0 {
		p.t.Fatalf("scrub after %s = %+v", after, s)
	}
}

// balanceCancel cancels a running balance both ways: the cancel route's
// BalanceCancel ends the job's Balance canceled, and the job's own context
// cancels it in the kernel. Neither leaves a balance behind.
func (p *itPool) balanceCancel() {
	ended := make(chan error, 1)
	go func() { ended <- Balance(p.ctx, p.uuid, BalanceOptions{}, func(float64) {}) }()
	p.waitExclusive("balance", ended)
	p.must("cancel balance", BalanceCancel(p.ctx, p.uuid))
	if err := <-ended; !errors.Is(err, context.Canceled) {
		p.t.Fatalf("balance canceled by the route: %v, want one wrapping context.Canceled", err)
	}
	p.noBalance()

	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	go func() { ended <- Balance(ctx, p.uuid, BalanceOptions{}, func(float64) {}) }()
	p.waitExclusive("balance", ended)
	cancel()
	if err := <-ended; err == nil || err.Error() != "balance canceled" {
		p.t.Fatalf("balance canceled by its context: %v, want balance canceled", err)
	}
	p.noBalance()
}

// scrubbedPast reports whether a running scrub has read more than mib MiB of
// some device.
func (p *itPool) scrubbedPast(mib uint64) func() bool {
	return func() bool {
		for _, d := range p.filesystem().Devices {
			args := scrubArgs{DevID: uint64(d.ID)}
			if fsIoctl(p.mnt, iocScrubProgress, unsafe.Pointer(&args)) == nil &&
				args.Progress.DataBytesScrubbed+args.Progress.TreeBytesScrubbed > mib<<20 {
				return true
			}
		}
		return false
	}
}

// scrubResume cancels a scrub as Suspend does, positions first, and resumes
// it from them with ScrubFrom. The cancel waits until a device has read more
// than any one dev extent holds (1 GiB at most), so a whole extent lies below
// its position: the resumed run skips it and reads less than a full scrub.
func (p *itPool) scrubResume() {
	full, err := Scrub(p.ctx, p.uuid, func(BtrfsScrubStatus) {})
	p.must("full scrub", err)
	type outcome struct {
		status BtrfsScrubStatus
		err    error
	}
	done, ended := make(chan outcome, 1), make(chan error, 1)
	go func() {
		s, scrubErr := Scrub(p.ctx, p.uuid, func(BtrfsScrubStatus) {})
		done <- outcome{s, scrubErr}
		ended <- scrubErr
	}()
	p.waitFor("scrub", p.scrubbedPast(1024), ended)
	positions, err := ScrubPositions(p.ctx, p.uuid)
	p.must("scrub positions", err)
	p.must("cancel scrub", ScrubCancel(p.ctx, p.uuid))
	first := <-done
	if first.err != nil || first.status.Status != "canceled" || len(positions) == 0 {
		p.t.Fatalf("canceled scrub = %+v err=%v, positions %v", first.status, first.err, positions)
	}
	resumed, err := ScrubFrom(p.ctx, p.uuid, positions, func(BtrfsScrubStatus) {})
	p.must("resume scrub", err)
	own := resumed.BytesScrubbed - first.status.BytesScrubbed
	p.t.Logf("positions %v; full scrub read %d MiB, the canceled one %d MiB, the resumed one %d MiB", positions, full.BytesScrubbed>>20, first.status.BytesScrubbed>>20, own>>20)
	if resumed.Status != "finished" || resumed.ErrorCount != 0 || !resumed.StartedAt.Equal(*first.status.StartedAt) || own >= full.BytesScrubbed {
		p.t.Fatalf("resumed scrub = %+v, read %d of a full %d bytes", resumed, own, full.BytesScrubbed)
	}
}

// scrubKill SIGKILLs a process while it scrubs: the kernel ends the scrub
// with the process and the pool must scrub clean.
func (p *itPool) scrubKill() {
	p.killWhile("scrub", p.scrubbedPast(0))
	p.scrubClean("the kill")
}

// replaceState is the state the pool's replace STATUS reports.
func (p *itPool) replaceState() uint64 {
	p.t.Helper()
	state, _, _, err := replaceStatusOf(p.mnt)
	p.must("replace status", err)
	return state
}

// replaceKill SIGKILLs a process while it replaces device 2 with the blank
// third disk. Since 6.19 the kill aborts the replace in the process's thread:
// it ends canceled, the target destroyed and device 2 kept, and ResumeReplace
// must refuse it. Older kernels ignore the signal until the replace finished;
// ResumeReplace accepts that one, which started after the step did, and the
// old disk is put back. Either way the third disk is left blank.
func (p *itPool) replaceKill() {
	var uts unix.Utsname
	p.must("uname", unix.Uname(&uts))
	var major, minor int
	_, err := fmt.Sscanf(unix.ByteSliceToString(uts.Release[:]), "%d.%d", &major, &minor)
	p.must("kernel version", err)
	signalsAbort := major > 6 || major == 6 && minor >= 19

	start := time.Now()
	p.killWhile("replace:2:"+p.disks[2], func() bool {
		return p.exclusiveOp() == "device replace" && p.replaceState() == replaceStateStarted
	})
	if op := p.exclusiveOp(); op != "none" {
		p.t.Fatalf("exclusive operation = %q after the kill", op)
	}
	resumed := ResumeReplace(p.ctx, p.uuid, 2, start, func(float64) {})
	fs := p.filesystem()
	i := slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == 2 })
	if i < 0 {
		p.t.Fatalf("device 2 gone after the kill: %v", devicePaths(fs))
	}
	state, path := p.replaceState(), fs.Devices[i].Path
	p.t.Logf("replace state %d after the kill; device 2 is %s; ResumeReplace: %v", state, path, resumed)
	switch stale := "the replace of device 2 did not survive the interruption; start it again"; {
	case signalsAbort:
		if state != replaceStateCanceled || path != p.disks[1] || resumed == nil || resumed.Error() != stale {
			p.t.Fatalf("since 6.19 the kill must cancel the replace, keep %s and be refused: state %d, device 2 %s, ResumeReplace %v", p.disks[1], state, path, resumed)
		}
	case state == replaceStateFinished && path == p.disks[2] && resumed == nil:
		// The replace ran to the end: put the old disk back.
		p.must("replace back", Replace(p.ctx, p.uuid, 2, p.disks[1], func(float64) {}))
	default:
		p.t.Fatalf("before 6.19 the replace must finish and be accepted: state %d, device 2 %s, ResumeReplace %v", state, path, resumed)
	}
	p.scrubClean("the kill")
	p.must("blank the disk", wipeDeviceEdges(p.disks[2]))
}

// replacePowerCut cuts the power of a replace and boots what its disks kept.
// The disk images of a pool of its own sit on an ext4 the step freezes
// mid-replace, and the copy it then takes holds exactly the writes that
// reached the disks, as a power cut leaves them. udev holds that copy back:
// the kernel counts the replace target, devid 0, as a third disk of two
// (DEVICES_READY). The boot check must mount it as fstab says, not degraded,
// and the kernel then continues the replace for ResumeReplace to follow.
func (p *itPool) replacePowerCut() {
	t, dir := p.t, p.t.TempDir()
	run := func(name string, args ...string) {
		t.Helper()
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
	}
	shelfDev, shelf := loopDevice(t, filepath.Join(dir, "shelf.img"), 16<<30), filepath.Join(dir, "shelf")
	run("mkfs.ext4", "-q", shelfDev)
	p.must("shelf mountpoint", os.Mkdir(shelf, 0o755))
	p.must("mount the shelf", unix.Mount(shelfDev, shelf, "ext4", 0, ""))
	t.Cleanup(func() { _ = unix.Unmount(shelf, 0) })
	var disks []*loopDisk
	for i := range 3 {
		disks = append(disks, newLoopDisk(t, filepath.Join(shelf, fmt.Sprintf("disk%d.img", i)), 6<<30))
	}
	cut := &itPool{t: t, ctx: p.ctx, mnt: filepath.Join(dir, "mnt")}
	uuid, err := CreatePool(p.ctx, "linuxio-cut", "raid1", "raid1", []string{disks[0].dev, disks[1].dev}, true, nil)
	p.must("create pool", err)
	cut.uuid = uuid
	p.must("mount pool", MountPool(p.ctx, disks[0].dev, cut.mnt, []string{"noatime"}, false))
	t.Cleanup(func() { _ = unix.Unmount(cut.mnt, 0) })
	cut.fill(1024)

	start := time.Now()
	child := cut.runChild("replace:2:" + disks[2].dev)
	ended := make(chan error, 1)
	go func() { ended <- child.Wait() }()
	// Progress means the commit that recorded the replace is on the disks.
	cut.waitFor("replace", func() bool {
		state, pct, _, statusErr := replaceStatusOf(cut.mnt)
		return statusErr == nil && state == replaceStateStarted && pct > 0
	}, ended)
	run("fsfreeze", "--freeze", shelf)
	t.Cleanup(func() { _ = exec.Command("fsfreeze", "--unfreeze", shelf).Run() })
	copies := make([]*loopDisk, len(disks))
	for i, d := range disks {
		copies[i] = &loopDisk{t: t, file: filepath.Join(dir, filepath.Base(d.file)), fd: -1}
		run("cp", "--sparse=always", d.file, copies[i].file)
	}
	run("fsfreeze", "--unfreeze", shelf)
	_ = child.Process.Kill()
	t.Logf("the live pool's replace child after the kill: %v", <-ended)
	p.must("unmount pool", unix.Unmount(cut.mnt, 0))
	for _, d := range disks {
		d.unplug()
	}
	for _, c := range copies {
		c.plug()
		t.Cleanup(func() {
			if c.fd >= 0 {
				detachLoop(c.fd)
			}
		})
	}
	t.Cleanup(func() { _ = unix.Unmount(cut.mnt, 0) })
	// udev's view, logged rather than asserted: a kernel that stops counting
	// the target leaves the boot check nothing to do here, which is not wrong.
	_ = exec.Command("udevadm", "settle").Run()
	for _, c := range copies {
		out, _ := exec.Command("udevadm", "info", "--query=property", "--property=ID_BTRFS_READY", c.dev).CombinedOutput()
		t.Logf("%s (copy of %s): %s", c.dev, filepath.Base(c.file), strings.TrimSpace(string(out)))
	}

	writeFstab(t, fmt.Sprintf("UUID=%s %s btrfs noatime 0 0\n", uuid, cut.mnt))
	p.must("boot check", MountDegradedPools(p.ctx, 0))
	fs, err := FindFilesystem(p.ctx, uuid)
	if err != nil || fs.Degraded {
		t.Fatalf("after the boot check: %v, degraded %v", err, fs.Degraded)
	}
	// The kernel names the source of a running replace, so the job of another
	// device's replace must not follow this one. Once it finished there is no
	// source left to compare.
	other := ResumeReplace(p.ctx, uuid, 1, start, func(float64) { t.Error("device 1's job followed the replace of device 2") })
	if want := fmt.Sprintf("pool %s is replacing device 2, not device 1", uuid); other != nil && other.Error() != want {
		t.Fatalf("ResumeReplace for device 1: %v, want %s", other, want)
	}
	t.Logf("ResumeReplace for device 1 during the replace of device 2: %v", other)
	followed := false
	p.must("follow the replace", ResumeReplace(p.ctx, uuid, 2, start, func(float64) { followed = true }))
	fs = cut.filesystem()
	if i := slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == 2 }); i < 0 || fs.Devices[i].Path != copies[2].dev {
		t.Fatalf("devices after the replace: %v, want device 2 on %s", devicePaths(fs), copies[2].dev)
	}
	t.Logf("ResumeReplace followed a running replace: %v", followed)
	cut.scrubClean("the resumed replace")
}
