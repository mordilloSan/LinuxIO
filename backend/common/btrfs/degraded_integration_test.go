package btrfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// loopDisk is a loop device over an image file that a test can unplug and
// plug back in, like a disk that drops off the bus and returns.
type loopDisk struct {
	t    *testing.T
	file string
	dev  string
	fd   int
}

func newLoopDisk(t *testing.T, file string, size int64) *loopDisk {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	d := &loopDisk{t: t, file: file}
	d.dev, d.fd = attachLoop(t, f)
	t.Cleanup(func() {
		if d.fd >= 0 {
			detachLoop(d.fd)
		}
	})
	return d
}

// unplug makes the kernel forget the device, which it allows only while
// the filesystem is unmounted, and detaches it.
func (d *loopDisk) unplug() {
	d.t.Helper()
	if d.dev == "" {
		// Unplugged already. An empty path would make the kernel forget
		// every unmounted btrfs device of the host.
		d.t.Fatal("unplug of a disk that is not plugged in")
	}
	if err := controlIoctl(iocForgetDev, d.dev); err != nil && !errors.Is(err, unix.ENOENT) {
		d.t.Fatalf("forget %s: %v", d.dev, err)
	}
	detachLoop(d.fd)
	d.dev, d.fd = "", -1
}

// plug attaches the image again, maybe as another loop device; udev and
// the caller register it as they would a hotplugged disk.
func (d *loopDisk) plug() {
	d.t.Helper()
	f, err := os.OpenFile(d.file, os.O_RDWR, 0)
	if err != nil {
		d.t.Fatal(err)
	}
	defer f.Close()
	d.dev, d.fd = attachLoop(d.t, f)
}

// writeRandom writes size random bytes to path, syncs them and returns
// their digest.
func writeRandom(t *testing.T, path string, size int64) [32]byte {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Allocated first, in extents of the file's own size class. Written
	// through the page cache alone, a file is split wherever writeback
	// starts while it is being written (a sync by anything else on the
	// host), and the kernel allocates a new chunk for a piece of another
	// size class before it puts it beside the others (extent-tree.c
	// find_free_extent: LOOP_ALLOC_CHUNK comes before
	// LOOP_WRONG_SIZE_CLASS), so the chunks these tests count on would
	// depend on the host.
	if err := unix.Fallocate(int(f.Fd()), 0, 0, size); err != nil {
		t.Fatalf("allocate %s: %v", path, err)
	}
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(f, h), rand.Reader, size); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	return [32]byte(h.Sum(nil))
}

func digest(t *testing.T, path string) [32]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return [32]byte(h.Sum(nil))
}

// itMirror is the two-disk raid1 pool the degraded test loses disks of.
type itMirror struct {
	t       *testing.T
	ctx     context.Context
	uuid    string
	target  string
	options []string
	// shelf holds the disk images and the state folder, frozen for the
	// power-cut copy, which goes to dir.
	shelf, dir string
	a, b       *loopDisk
	// The digest of each file the pool must hold, written before and while
	// a was gone.
	files map[string][32]byte
}

// write writes a file of size random bytes to the pool and notes its digest.
func (m *itMirror) write(name string, size int64) {
	m.t.Helper()
	m.files[name] = writeRandom(m.t, filepath.Join(m.target, name), size)
}

func (m *itMirror) must(what string, err error) {
	m.t.Helper()
	if err != nil {
		m.t.Fatalf("%s: %v", what, err)
	}
}

func (m *itMirror) mounted() bool {
	m.t.Helper()
	mounts, err := ListMounts(m.ctx)
	m.must("list mounts", err)
	return slices.ContainsFunc(mounts, func(x Mount) bool { return x.Mountpoint == m.target })
}

func (m *itMirror) unmount() {
	m.t.Helper()
	m.must("unmount", unix.Unmount(m.target, 0))
}

func (m *itMirror) filesystem() BtrfsFilesystem {
	m.t.Helper()
	fs, err := FindFilesystem(m.ctx, m.uuid)
	m.must("inventory", err)
	return fs
}

// hasAlert reports whether the alert with key is up and its title or
// message contains text.
func (m *itMirror) hasAlert(key, text string) bool {
	m.t.Helper()
	doc, err := alerts.Load()
	m.must("alerts", err)
	return slices.ContainsFunc(doc.Alerts, func(x alerts.Alert) bool {
		return x.Key == key && strings.Contains(x.Title+" "+x.Message, text)
	})
}

func (m *itMirror) state() poolState {
	m.t.Helper()
	states, err := loadPoolStates()
	m.must("pool state", err)
	return states[m.uuid]
}

// TestBtrfsDegradedIntegration loses and returns disks of a two-disk raid1
// pool on loop devices and checks every step of the policy against the real
// kernel: the boot check mounts the pool on the disk that is left, a disk
// that comes back while mounted is not used, a stale disk alone is refused,
// and once both are back the resync rewrites what the stale disk missed and
// restores redundancy, so that disk alone then holds every file. The resync
// is cut by a power loss mid-scrub and continued from what the disks and the
// state kept. Under a running systemd it also checks that a pending
// fstab-style mount job does not undo the degraded mount. Run it with
// `make test-btrfs-integration`.
func TestBtrfsDegradedIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	t.Setenv("TMPDIR", "/var/tmp") // the images and their power-cut copies; /tmp may be a small tmpfs
	dir := t.TempDir()
	shelf := mountShelf(t, dir, 8<<30)
	state := filepath.Join(shelf, "state")
	if err := os.Mkdir(state, 0o755); err != nil {
		t.Fatal(err)
	}
	stubStatePaths(t, state)
	m := &itMirror{
		t: t, ctx: context.Background(), target: filepath.Join(dir, "mnt"), options: []string{"defaults", "noatime", "nofail"},
		shelf: shelf, dir: dir, files: map[string][32]byte{},
	}
	m.a = newLoopDisk(t, filepath.Join(shelf, "a.img"), 3<<30)
	m.b = newLoopDisk(t, filepath.Join(shelf, "b.img"), 3<<30)
	// Registered after the disks, so it runs before they are detached.
	t.Cleanup(func() { _ = unix.Unmount(m.target, 0) })
	var uts unix.Utsname
	m.must("uname", unix.Uname(&uts))
	t.Logf("kernel %s", unix.ByteSliceToString(uts.Release[:]))
	uuid, err := CreatePool(m.ctx, "linuxio-degraded", "raid1", "raid1", []string{m.a.dev, m.b.dev}, true, nil, nil)
	m.must("create pool", err)
	m.uuid = uuid
	writeFstab(t, fmt.Sprintf("UUID=%s %s btrfs %s 0 0\n", uuid, m.target, strings.Join(m.options, ",")))
	m.must("mount", MountPool(m.ctx, m.a.dev, m.target, m.options, false))
	m.writeWithHoles()
	m.unmount()

	m.bootMountsTheSurvivor()
	m.returnedDiskIsNotUsed()
	m.staleDiskAloneIsRefused()
	m.resyncRepairsTheReturnedDisk()
	m.returnedDiskHoldsEverything()
	writeErrorsFailTheResync(t, dir)
	mountedDropoutKeepsItsCounters(t, dir)
}

// writeWithHoles fills the data chunk mkfs made, then writes files into
// 256 MiB chunks, the smallest the kernel lets a pool set (sysfs.c
// btrfs_chunk_size_store), and deletes every other one. The writes made
// while a disk is missing fill those holes first, so the returned disk's
// stale copies lie in dev extents above the first, on both sides of where
// the resync is cut, and the continued scrub can skip whole extents.
func (m *itMirror) writeWithHoles() {
	t := m.t
	chunk := filepath.Join("/sys/fs/btrfs", m.uuid, "allocation", "data", "chunk_size")
	m.must("data chunk size", os.WriteFile(chunk, []byte(strconv.Itoa(256<<20)), 0o644))
	for _, x := range m.filesystem().Allocation {
		if x.Kind == "data" && x.TotalBytes > x.UsedBytes+2<<20 {
			m.write("base.bin", int64(x.TotalBytes-x.UsedBytes-2<<20))
			break
		}
	}
	// One file short of full: a write into allocated space still reserves
	// its size until it is written back (file.c btrfs_buffered_write), and a
	// reservation that does not fit allocates a chunk.
	for i := range 15 {
		m.write(fmt.Sprintf("before-%02d.bin", i), 32<<20)
	}
	for i := 1; i < 15; i += 2 {
		name := fmt.Sprintf("before-%02d.bin", i)
		m.must("delete "+name, os.Remove(filepath.Join(m.target, name)))
		delete(m.files, name)
	}
	unix.Sync()
	t.Logf("allocation with the holes: %+v", m.filesystem().Allocation)
}

// bootMountsTheSurvivor: a drops out and the boot check mounts the pool on b.
func (m *itMirror) bootMountsTheSurvivor() {
	t := m.t
	m.a.unplug()
	stopUnit := fstabStyleMountUnit(t, m.uuid, m.target, m.b.dev)
	m.must("boot check", MountDegradedPools(m.ctx, 0))
	if fs := m.filesystem(); !fs.Degraded || fs.Mountpoints[0].Mountpoint != m.target {
		t.Fatalf("after the boot check: degraded %v at %+v", fs.Degraded, fs.Mountpoints)
	}
	stopUnit()
	if !m.mounted() {
		t.Fatal("systemd undid the degraded mount when its own mount job gave up")
	}
	if !m.state().Degraded {
		t.Fatalf("state = %+v, want degraded", m.state())
	}
	if !m.hasAlert("health-"+m.uuid, "running without 1 of 2 disks") {
		t.Fatal("the degraded mount raised no alert")
	}
	// The holes in the raid1 chunks first, then more than they hold, so new
	// chunks are allocated while only one disk can take them. 16 MiB files
	// are in the size class of the files the holes were (block-group.c
	// btrfs_calc_block_group_size_class), so the allocator takes the holes
	// before it allocates a chunk.
	for i := range 24 {
		m.write(fmt.Sprintf("during-%02d.bin", i), 16<<20)
	}
	// Four single chunks or more, so a convert paused after one has some
	// left for the kernel to continue.
	for i := range 64 {
		m.write(fmt.Sprintf("single-%02d.bin", i), 16<<20)
	}
	fs := m.filesystem()
	t.Logf("allocation while degraded: %+v", fs.Allocation)
	if !slices.ContainsFunc(fs.Allocation, func(x BtrfsAllocation) bool {
		return x.Kind == "data" && x.Profile == "single" && x.TotalBytes >= 4*256<<20
	}) {
		t.Fatal("fewer than four single data chunks were allocated on one disk; the convert step would have too little to do")
	}
	// The chunk tree puts those single chunks on b, the disk that stayed,
	// and none on a: the automatic rebuild of a would not be refused for
	// them (oneCopyChunks).
	one, err := oneCopyChunks(m.target)
	m.must("chunk tree", err)
	t.Logf("chunks keeping one copy, by devid: %v", one)
	if one[2] == "" || one[1] != "" {
		t.Fatalf("chunks keeping one copy = %v, want them on device 2 alone", one)
	}
	// btrfs-progs reads the same from the disk: each device's first chunk
	// that keeps one copy, in logical order.
	unix.Sync()
	dump, err := exec.CommandContext(m.ctx, "btrfs", "inspect-internal", "dump-tree", "-t", "chunk", m.b.dev).CombinedOutput()
	m.must("dump the chunk tree", err)
	if want := progsOneCopyChunks(string(dump)); !maps.Equal(one, want) {
		t.Fatalf("chunks keeping one copy = %v, btrfs-progs reads %v", one, want)
	}
}

// progsOneCopyChunks is what oneCopyChunks returns, read from the chunk tree
// as btrfs-progs prints it (inspect-internal dump-tree -t chunk): its
// "length … type DATA|RAID1" line and its "stripe N devid D" lines.
func progsOneCopyChunks(dump string) map[uint64]string {
	found := map[uint64]string{}
	what := ""
	for line := range strings.Lines(dump) {
		f := strings.Fields(line)
		switch {
		case len(f) > 0 && f[0] == "item":
			what = ""
		case len(f) >= 8 && f[0] == "length" && f[6] == "type":
			what = progsOneCopyType(f[7])
		case len(f) >= 4 && f[0] == "stripe" && f[2] == "devid" && what != "":
			id, err := strconv.ParseUint(f[3], 10, 64)
			if _, seen := found[id]; err == nil && !seen {
				found[id] = what
			}
		}
	}
	return found
}

// progsOneCopyType words btrfs-progs' chunk type as oneCopyChunks does
// ("data single"), or "" for a profile that keeps more than one copy.
func progsOneCopyType(t string) string {
	var kinds []string
	profile := "single"
	for part := range strings.SplitSeq(t, "|") {
		switch p := strings.ToLower(part); p {
		case "data", "metadata", "system":
			kinds = append(kinds, p)
		default:
			profile = p
		}
	}
	if profile != "single" && profile != "dup" && profile != "raid0" {
		return ""
	}
	return strings.Join(kinds, "+") + " " + profile
}

// returnedDiskIsNotUsed: a comes back while mounted; the kernel lists it but
// never opens it, so it still counts as missing.
func (m *itMirror) returnedDiskIsNotUsed() {
	m.a.plug()
	m.must("register returned disk", RegisterDevice(m.a.dev))
	fs := m.filesystem()
	if !fs.Degraded || !slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool { return d.Path == m.a.dev && d.Missing }) {
		m.t.Fatalf("returned disk: degraded %v, devices %+v", fs.Degraded, fs.Devices)
	}
	m.unmount()
}

// staleDiskAloneIsRefused: b drops out too; a alone missed the writes, so
// the boot check leaves the pool to a person and says why.
func (m *itMirror) staleDiskAloneIsRefused() {
	m.b.unplug()
	m.must("boot check on the stale disk", MountDegradedPools(m.ctx, 0))
	if m.mounted() {
		m.t.Fatal("mounted the stale disk alone")
	}
	if !m.hasAlert("offline-"+m.uuid, "newest data is on a missing disk") {
		m.t.Fatal("the refusal raised no alert explaining it")
	}
}

// resyncRepairsTheReturnedDisk: with both back the pool is complete, which
// is systemd's to mount. The resync is cut by a power loss mid-scrub, and
// the pool booted from what its disks and the state kept, where the next
// resync continues the scrub: it rewrites a's stale copies on both sides of
// the cut, and the convert gives the single chunks their second copy.
func (m *itMirror) resyncRepairsTheReturnedDisk() {
	t := m.t
	m.b.plug()
	m.mountComplete("a complete pool")
	if !m.state().Degraded {
		t.Fatalf("complete pool: state %+v", m.state())
	}
	m.cutResync()
	m.mountComplete("the power-cut copies")

	cut, ok := loadScrubRecord(m.uuid)
	run := m.state().ResyncRun
	before := cut.total()
	if !ok || cut.Status != "running" || run == "" || cut.Run != run || cut.StartedAt == nil || before.CorrectedErrors == 0 {
		t.Fatalf("the cut left scrub record %+v for the resync run %q; want it running with corrections", cut, run)
	}
	used := m.filesystem().UsedBytes
	t.Logf("cut at %d of %d MiB with %d corrections, devices %+v", before.BytesScrubbed>>20, used>>20, before.CorrectedErrors, cut.Devices)

	calls := (&itPool{t: t, ctx: m.ctx, uuid: m.uuid, mnt: m.target}).scrubCalls()
	m.resyncWithItsConvertCut()
	// Each device was continued from one byte below its saved offset, not
	// scanned again from 0.
	starts := calls()
	for id, d := range cut.Devices {
		if got := starts[uint64(id)]; d.Offset == 0 || len(got) == 0 || got[0] != d.Offset-1 {
			t.Fatalf("device %d's scrub calls started at %v; want the first at %d, below its saved offset", id, got, d.Offset-1)
		}
	}
	final, _ := loadScrubRecord(m.uuid)
	own := final.BytesScrubbed - before.BytesScrubbed
	t.Logf("the continued scrub read %d MiB itself and corrected %d, %d MiB and %d corrections in all; final devices %+v",
		own>>20, final.CorrectedErrors-before.CorrectedErrors, final.BytesScrubbed>>20, final.CorrectedErrors, final.Devices)
	switch {
	case final.Run != run || final.Status != "finished" || final.StartedAt == nil || !final.StartedAt.Equal(*cut.StartedAt):
		t.Fatalf("resync scrub = %+v; want the cut scrub of run %s continued to the end", final, run)
	// The cut lies past each device's first data extent, which the
	// continuation skips whole: it reads under nine tenths of a full scrub.
	case own*10 >= used*9 || final.BytesScrubbed*100 < used*99:
		t.Fatalf("the continued scrub read %d bytes itself and %d in all of %d used; want under nine tenths itself, and all with the cut part", own, final.BytesScrubbed, used)
	case final.CorrectedErrors <= before.CorrectedErrors || final.UncorrectableErrors != 0:
		t.Fatalf("resync scrub = %+v; want stale copies corrected after the cut as well, none uncorrectable", final)
	}
	m.followTheKernelsBalance()
	fs := m.filesystem()
	for _, x := range fs.Allocation {
		if x.Profile != "raid1" {
			t.Fatalf("after resync %s is %s, want raid1: %+v", x.Kind, x.Profile, fs.Allocation)
		}
	}
	if s := m.state(); s.Degraded || s.ResyncRun != "" {
		t.Fatalf("state after resync = %+v", s)
	}
	// The notice counts the corrections of both parts.
	if !m.hasAlert("resynced-"+m.uuid, fmt.Sprintf("corrected %d ", final.CorrectedErrors)) || m.hasAlert("resync-"+m.uuid, "") {
		t.Fatal("the alerts do not say the pool is whole again with every correction")
	}
	m.resyncResetTheReturnedDisksCounters()
	m.unmount()
}

// device returns the pool's device at path dev, with its error counters as
// the kernel reads them.
func (m *itMirror) device(dev string) BtrfsDevice {
	m.t.Helper()
	fs := m.filesystem()
	i := slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.Path == dev })
	if i < 0 {
		m.t.Fatalf("no device %s in %+v", dev, fs.Devices)
	}
	return fs.Devices[i]
}

// resyncResetTheReturnedDisksCounters: the scrub counted the stale copies it
// rewrote on a as errors of a (7.0 scrub.c scrub_stripe_report_errors), and
// the resync that made the pool whole reset them there, not on b, so no
// health alert is left. The resync commits the reset by setting the label
// to itself, which keeps the label: with no write in between, a mount then
// reads the counters as zero from the device tree.
func (m *itMirror) resyncResetTheReturnedDisksCounters() {
	t := m.t
	doc, err := alerts.Load()
	m.must("alerts", err)
	var notice string
	for _, x := range doc.Alerts {
		if x.Key == "resynced-"+m.uuid {
			notice = x.Message
		}
	}
	a, b := m.device(m.a.dev), m.device(m.b.dev)
	t.Logf("after the resync %s (devid %d) counts %+v and %s (devid %d) %+v; the notice: %q", m.a.dev, a.ID, a.Errors, m.b.dev, b.ID, b.Errors, notice)
	switch {
	case a.Errors != (BtrfsDeviceErrors{}):
		t.Fatalf("the returned disk's error counters after the resync = %+v, want them reset", a.Errors)
	case strings.Contains(notice, m.b.dev+" ("):
		t.Fatalf("the notice names %s, which stayed present: %q", m.b.dev, notice)
	case m.hasAlert("health-"+m.uuid, ""):
		t.Fatal("the health alert is still up after the resync")
	case m.filesystem().Label != "linuxio-degraded":
		t.Fatalf("label after the resync = %q, want it kept", m.filesystem().Label)
	}

	m.unmount()
	m.mountComplete("the resynced pool")
	if got := m.device(m.a.dev).Errors; got != (BtrfsDeviceErrors{}) {
		t.Fatalf("with no write since the resync, the mount read %s's error counters as %+v from the disk, want the committed reset", m.a.dev, got)
	}
	// The health run's check of this pool.
	RaiseAlerts(m.ctx, m.filesystem())
	if m.hasAlert("health-"+m.uuid, "") {
		t.Fatal("a health check after the mount raised the health alert again")
	}
}

// resyncWithItsConvertCut runs the continued resync until its convert has
// moved a chunk, then pauses the convert, as a shutdown leaves it: the
// kernel keeps the balance item and continues it at the next read-write
// mount, as it does after a power cut. The scrub already counts.
func (m *itMirror) resyncWithItsConvertCut() {
	t := m.t
	live := &itPool{t: t, ctx: m.ctx, uuid: m.uuid, mnt: m.target}
	done := make(chan error, 1)
	go func() { done <- Resync(m.ctx, m.uuid, nil) }()
	live.waitFor("convert", func() bool { return live.exclusiveOp() == "balance" && live.balanceMoved() }, done)
	paused, err := PauseBalance(m.ctx, m.uuid)
	m.must("pause the convert", err)
	err = <-done
	t.Logf("the resync whose convert was paused: %v", err)
	if !paused || err == nil || !strings.Contains(err.Error(), "balance paused") {
		t.Fatalf("pause %v, resync %v; want the convert paused with chunks left: write more while a disk is missing", paused, err)
	}
	if s := m.state(); !s.Scrubbed || !s.Degraded {
		t.Fatalf("state after the convert's cut = %+v, want scrubbed and still degraded", s)
	}
}

// followTheKernelsBalance mounts the pool again, where the kernel continues
// the cut convert, and the next resync must recognise it as its own and
// follow it to the end, issuing no balance of its own, instead of failing
// on the new one the kernel refuses.
func (m *itMirror) followTheKernelsBalance() {
	t := m.t
	m.unmount()
	m.mountComplete("the pool with its convert cut")
	// The kernel's thread takes the paused balance up just after the mount.
	deadline := time.Now().Add(10 * time.Second)
	running, err := balanceStatusOf(m.target)
	for err == nil && running.State&balanceStateRunning == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		running, err = balanceStatusOf(m.target)
	}
	if err != nil || running.State&balanceStateRunning == 0 {
		t.Fatalf("no running balance after the mount (state %#x, %v): the kernel did not continue the convert, or finished it first", running.State, err)
	}
	t.Logf("the kernel continued the convert at the mount: state %#x, flags %#x, %d of %d chunks", running.State, running.Flags, running.Stat.Completed, running.Stat.Expected)
	var issued atomic.Int32
	issue := fsIoctl
	fsIoctl = func(mnt string, req uintptr, arg unsafe.Pointer) error {
		if req == iocBalanceV2 {
			issued.Add(1)
		}
		return issue(mnt, req, arg)
	}
	defer func() { fsIoctl = issue }()
	m.must("resync after the mount", Resync(m.ctx, m.uuid, nil))
	if n := issued.Load(); n != 0 {
		t.Fatalf("the resync issued %d balances of its own; want it to follow the kernel's", n)
	}
}

// mountComplete runs the boot check, which leaves a complete pool to
// systemd, then mounts the pool as systemd would.
func (m *itMirror) mountComplete(what string) {
	t := m.t
	m.must("boot check on "+what, MountDegradedPools(m.ctx, 0))
	if m.mounted() {
		t.Fatalf("the boot check mounted %s", what)
	}
	pools, err := FindOfflinePools(m.ctx, []string{m.a.dev, m.b.dev})
	m.must("offline pools", err)
	if len(pools) != 1 || len(pools[0].Devices) != 2 {
		t.Fatalf("offline pools of %s = %+v", what, pools)
	}
	m.must("mount "+what, MountOfflinePool(m.ctx, pools[0], m.target, m.options, false))
	if m.filesystem().Degraded {
		t.Fatalf("%s mounted degraded", what)
	}
}

// cutResync runs the resync in a child that saves its scrub every second,
// with the scrub slowed down, and cuts it as a power loss does once its
// record shows corrections in the holes: the shelf is frozen and the disk
// images and the state copied, then the child is killed. The originals are
// unplugged, the copies plugged in their place, and the state paths pointed
// at the copied state. It fails when the resync ends before those
// corrections were saved.
func (m *itMirror) cutResync() {
	t := m.t
	live := &itPool{t: t, ctx: m.ctx, uuid: m.uuid, mnt: m.target}
	live.scrubSpeed(32 << 20)
	child := live.runChild("resync", "LINUXIO_BTRFS_IT_STATE="+filepath.Dir(ScrubRecordPath))
	ended := make(chan error, 1)
	go func() { ended <- child.Wait() }()
	// 4096 corrected sectors, 16 MiB of stale copies, are more than the base
	// file's leftover and the metadata hold: a's scrub is in the holes, past
	// the first data extent, so the continuation can skip whole extents.
	// Mainline before 6.11 moves last_physical only at the end of a dev
	// extent, where the record would show them at the end of the first
	// chunk with holes; the noble VM's 6.8.0-149 saved the same offset
	// mid-extent as 7.0 did, so it seems to carry the 6.11 change, and no
	// VM run cuts at an extent end while that extent's repairs are queued.
	// That case rests on the source and the unit tests.
	live.waitFor("resync", func() bool {
		states, err := loadPoolStates()
		rec, ok := loadScrubRecord(m.uuid)
		return err == nil && ok && rec.Run != "" && rec.Run == states[m.uuid].ResyncRun &&
			rec.Status == "running" && rec.total().CorrectedErrors >= 4096
	}, ended)
	stateCopy := filepath.Join(m.dir, "state")
	copies := map[string]string{
		m.a.file: filepath.Join(m.dir, "a.img"), m.b.file: filepath.Join(m.dir, "b.img"),
		filepath.Dir(ScrubRecordPath): stateCopy,
	}
	copyFrozen(t, m.shelf, copies)
	logFreeSpace(t, m.dir, "with the power-cut copies")
	_ = child.Process.Kill()
	// Before 6.19 the kernel scrubs on to the end of each device, and the
	// child ends only then.
	live.scrubSpeed(0)
	t.Logf("the resync child after the kill: %v", <-ended)
	live.waitScrubIdle()
	m.unmount()

	m.a.unplug()
	m.b.unplug()
	m.a = &loopDisk{t: t, file: copies[m.a.file], fd: -1}
	m.b = &loopDisk{t: t, file: copies[m.b.file], fd: -1}
	for _, d := range []*loopDisk{m.a, m.b} {
		d.plug()
		t.Cleanup(func() {
			if d.fd >= 0 {
				detachLoop(d.fd)
			}
		})
	}
	// Registered after the copies, so it runs before they are detached.
	t.Cleanup(func() { _ = unix.Unmount(m.target, 0) })
	stubStatePaths(t, stateCopy)
}

// returnedDiskHoldsEverything: a alone now holds every file, and every copy
// on it checks out.
func (m *itMirror) returnedDiskHoldsEverything() {
	t := m.t
	m.b.unplug()
	pools, err := FindOfflinePools(m.ctx, []string{m.a.dev})
	m.must("offline pools", err)
	if len(pools) != 1 {
		t.Fatalf("offline pools = %+v", pools)
	}
	m.must("mount a alone", MountOfflinePool(m.ctx, pools[0], m.target, m.options, true))
	for name, want := range m.files {
		if digest(t, filepath.Join(m.target, name)) != want {
			t.Fatalf("a alone does not hold %s as written", name)
		}
	}
	status, err := ScrubFrom(m.ctx, m.uuid, "", func(BtrfsScrubStatus) {})
	m.must("scrub a alone", err)
	if status.ErrorCount != 0 {
		t.Fatalf("scrub of a alone = %+v, want no errors", status)
	}
	m.unmount()
}

// fstabStyleMountUnit stands in for the unit systemd-fstab-generator makes
// from the pool's fstab line, with a 5 s device timeout, and starts it. As
// at boot, its job waits for a device udev does not mark ready: present is
// re-evaluated now that its pool lacks a disk. The returned func waits past
// that timeout, then removes the unit. Without a running systemd it does
// nothing.
func fstabStyleMountUnit(t *testing.T, uuid, where, present string) func() {
	t.Helper()
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Log("systemd is not running; skipping the mount-job check")
		return func() {}
	}
	if out, err := exec.Command("udevadm", "trigger", "--action=change", "--settle", present).CombinedOutput(); err != nil {
		t.Fatalf("udevadm trigger %s: %v: %s", present, err, out)
	}
	ctx := context.Background()
	unit, err := systemd.EscapePath(where)
	if err != nil {
		t.Fatal(err)
	}
	unit += ".mount"
	device, err := systemd.EscapePath("/dev/disk/by-uuid/" + uuid)
	if err != nil {
		t.Fatal(err)
	}
	device += ".device"
	files := map[string]string{
		"/run/systemd/system/" + unit:                          "[Mount]\nWhat=/dev/disk/by-uuid/" + uuid + "\nWhere=" + where + "\nType=btrfs\nOptions=noatime\n",
		"/run/systemd/system/" + device + ".d/linuxio-it.conf": "[Unit]\nJobRunningTimeoutSec=5s\n",
	}
	remove := func() {
		for path := range files {
			_ = os.Remove(path)
		}
		_ = os.Remove("/run/systemd/system/" + device + ".d")
		_ = systemd.DaemonReload(ctx)
		_ = systemd.ResetFailedUnit(ctx, unit)
	}
	t.Cleanup(remove)
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := systemd.DaemonReload(ctx); err != nil {
		t.Fatalf("daemon-reload: %v", err)
	}
	if err := systemd.StartUnit(ctx, unit); err != nil {
		t.Fatalf("start %s: %v", unit, err)
	}
	return func() {
		time.Sleep(8 * time.Second)
		remove()
	}
}

// writeErrorsFailTheResync resyncs a pool of its own whose second disk, a
// device-mapper device, missed writes while the pool ran without it, and
// fails that disk's reads, writes and flushes for a moment while the pool
// stays mounted, as when a cable comes loose and the disk stays attached:
// the kernel marks no disk missing then (it does so at mount, and since 7.1
// when a block device goes away), only the disk's error counters rise. It
// fails the disk once during the scrub, with a file written meanwhile that
// reaches the other disk alone, and once during the convert, which writes
// the second copies onto it. Each time the resync must fail, naming the
// disk, rather than declare the pool whole; once the disk works again the
// next resync scrubs it again, after which that disk alone holds every file.
func writeErrorsFailTheResync(t *testing.T, dir string) {
	f := newFlakyMirror(t, dir)
	// The health run saw the pool whole, with no errors counted.
	_, err := recordFilesystems(f.pool.ctx, []BtrfsFilesystem{f.pool.filesystem()})
	f.pool.must("record the pool whole", err)
	f.writeWithoutTheFlakyDisk()
	p := f.pool
	devices := p.filesystem().Devices
	i := slices.IndexFunc(devices, func(d BtrfsDevice) bool { return canonicalPath(d.Path) == f.flaky.dev })
	if i < 0 {
		t.Fatalf("no member on %s: %+v", f.flaky.dev, devices)
	}
	want := fmt.Sprintf("device %d (%s) failed writes", devices[i].ID, devices[i].Path)

	p.scrubSpeed(16 << 20)
	done := make(chan error, 1)
	go func() { done <- Resync(p.ctx, p.uuid, nil) }()
	p.waitFor("resync", p.scrubbedPast(8), done)
	f.flaky.fail()
	f.files["during-failure.bin"] = writeRandom(t, filepath.Join(p.mnt, "during-failure.bin"), 16<<20)
	unix.Sync()
	f.flaky.heal()
	p.scrubSpeed(0)
	f.wantFailedAttempt("its scrub", <-done, want, 1)

	go func() { done <- Resync(p.ctx, p.uuid, nil) }()
	p.waitFor("convert", p.holds("balance"), done)
	f.flaky.fail()
	failed := <-done
	f.flaky.heal()
	f.wantFailedAttempt("its convert", failed, want, 2)

	p.must("the resync once the disk works again", Resync(p.ctx, p.uuid, nil))
	if states, _ := loadPoolStates(); states[p.uuid].Degraded {
		t.Fatalf("state after the resync = %+v", states[p.uuid])
	}
	// The writes that failed during the earlier attempts are faults of the
	// disk while it was back, not of its absence: the resync that ended
	// whole keeps them, and the health alert stays.
	flaky := p.filesystem().Devices[i]
	doc, err := alerts.Load()
	p.must("alerts", err)
	health := slices.ContainsFunc(doc.Alerts, func(x alerts.Alert) bool { return x.Key == "health-"+p.uuid })
	t.Logf("after the resync that ended whole %s counts %+v; health alert up: %v", flaky.Path, flaky.Errors, health)
	if flaky.Errors.Write == 0 || !health {
		t.Fatalf("the failed writes of the earlier attempts were reset (%+v, health alert %v); want them kept", flaky.Errors, health)
	}
	f.flakyDiskHoldsEverything()
}

// wantFailedAttempt checks that a resync during part of which the flaky
// disk failed ended as failed attempt number n, saying want, with its scrub
// no longer counted.
func (f *flakyMirror) wantFailedAttempt(part string, err error, want string, n int) {
	t := f.pool.t
	t.Helper()
	t.Logf("the resync while the disk failed during %s: %v", part, err)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Resync = %v; want it to fail with %q", err, want)
	}
	states, loadErr := loadPoolStates()
	if s := states[f.pool.uuid]; loadErr != nil || !s.Degraded || s.Scrubbed || s.ResyncFailures != n {
		t.Fatalf("state after the attempt that failed during %s = %+v, %v", part, s, loadErr)
	}
}

// writeWithoutTheFlakyDisk runs the pool on its first disk alone and writes
// more than its raid1 data chunk holds, so the stale disk misses writes and
// a single chunk is allocated for the resync to convert; then it mounts the
// pool whole again, as after the disk came back.
func (f *flakyMirror) writeWithoutTheFlakyDisk() {
	p := f.pool
	p.must("unmount", unix.Unmount(p.mnt, 0))
	p.must("forget the flaky disk", controlIoctl(iocForgetDev, f.flaky.dev))
	pools, err := FindOfflinePools(p.ctx, []string{f.a.dev})
	if err != nil || len(pools) != 1 {
		p.t.Fatalf("offline pools = %+v, %v", pools, err)
	}
	p.must("mount degraded", MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, true))
	for i := range 48 {
		name := fmt.Sprintf("degraded-%02d.bin", i)
		f.files[name] = writeRandom(p.t, filepath.Join(p.mnt, name), 16<<20)
	}
	p.must("unmount", unix.Unmount(p.mnt, 0))
	p.must("register the flaky disk", RegisterDevice(f.flaky.dev))
	pools, err = FindOfflinePools(p.ctx, []string{f.a.dev, f.flaky.dev})
	if err != nil || len(pools) != 1 || len(pools[0].Devices) != 2 {
		p.t.Fatalf("offline pools = %+v, %v", pools, err)
	}
	p.must("mount whole", MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, false))
	if fs := p.filesystem(); !slices.ContainsFunc(fs.Allocation, func(x BtrfsAllocation) bool { return x.Kind == "data" && x.Profile == "single" }) {
		p.t.Fatalf("no single data chunk to convert: %+v", fs.Allocation)
	}
	if s, _ := loadPoolStates(); !s[p.uuid].Degraded {
		p.t.Fatalf("the degraded mount recorded no episode: %+v", s[p.uuid])
	}
}

// flakyMirror is a two-disk raid1 pool whose second disk is a device-mapper
// device that can fail every I/O, with the files written to it.
type flakyMirror struct {
	pool  *itPool
	a     *loopDisk
	flaky *dmDisk
	files map[string][32]byte
}

func newFlakyMirror(t *testing.T, dir string) *flakyMirror {
	a := newLoopDisk(t, filepath.Join(dir, "wa.img"), 3<<30)
	b := newLoopDisk(t, filepath.Join(dir, "wb.img"), 3<<30)
	f := &flakyMirror{a: a, flaky: newDMDisk(t, "linuxio-it-flaky", b.dev), files: map[string][32]byte{}}
	p := &itPool{t: t, ctx: t.Context(), mnt: filepath.Join(dir, "wmnt")}
	f.pool = p
	// Registered after the disks, so it runs before they are removed.
	t.Cleanup(func() { _ = unix.Unmount(p.mnt, 0) })
	uuid, err := CreatePool(p.ctx, "linuxio-flaky", "raid1", "raid1", []string{a.dev, f.flaky.dev}, true, nil, nil)
	p.must("create pool", err)
	p.uuid = uuid
	t.Cleanup(func() {
		for _, dev := range []string{a.dev, f.flaky.dev} {
			// a is unplugged by then (flakyDiskHoldsEverything), which
			// forgot it and left its path empty; an empty path would make
			// the kernel forget every unmounted btrfs device of the host.
			if dev != "" {
				_ = controlIoctl(iocForgetDev, dev)
			}
		}
	})
	p.must("mount", MountPool(p.ctx, a.dev, p.mnt, []string{"noatime"}, false))
	for i := range 8 {
		name := fmt.Sprintf("file-%d.bin", i)
		f.files[name] = writeRandom(t, filepath.Join(p.mnt, name), 16<<20)
	}
	return f
}

// flakyDiskHoldsEverything checks that the device-mapper disk alone holds
// every file, and that every copy on it checks out.
func (f *flakyMirror) flakyDiskHoldsEverything() {
	p := f.pool
	p.must("unmount", unix.Unmount(p.mnt, 0))
	f.a.unplug()
	pools, err := FindOfflinePools(p.ctx, []string{f.flaky.dev})
	if err != nil || len(pools) != 1 {
		p.t.Fatalf("offline pools = %+v, %v", pools, err)
	}
	p.must("mount the disk whose writes failed alone", MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, true))
	for name, want := range f.files {
		if digest(p.t, filepath.Join(p.mnt, name)) != want {
			p.t.Fatalf("the disk whose writes failed does not hold %s as written", name)
		}
	}
	status, err := ScrubFrom(p.ctx, p.uuid, "", func(BtrfsScrubStatus) {})
	if err != nil || status.ErrorCount != 0 {
		p.t.Fatalf("scrub of the disk alone = %+v, %v; want no errors", status, err)
	}
	p.must("unmount", unix.Unmount(p.mnt, 0))
}

// mountedDropoutKeepsItsCounters: a disk of a pool of its own fails while
// the pool is mounted, as a disk that drops out does, and counts the reads,
// writes and flushes that fail as it goes. 7.0 as Ubuntu builds it marks no
// disk missing while it stays attached, so the episode begins at the next
// mount, without the disk, as at a boot. The disk comes back and the resync
// makes the pool whole. LinuxIO never resets a disk that counts a failed
// read, write or flush: its counters, the failed writes included, and the
// health alert stay, and the notice is a warning that says why.
func mountedDropoutKeepsItsCounters(t *testing.T, dir string) {
	a := newLoopDisk(t, filepath.Join(dir, "da.img"), 3<<30)
	b := newLoopDisk(t, filepath.Join(dir, "db.img"), 3<<30)
	drop := newDMDisk(t, "linuxio-it-dropout", b.dev)
	p := &itPool{t: t, ctx: t.Context(), mnt: filepath.Join(dir, "dmnt")}
	// Registered after the disks, so it runs before they are removed.
	t.Cleanup(func() { _ = unix.Unmount(p.mnt, 0) })
	uuid, err := CreatePool(p.ctx, "linuxio-dropout", "raid1", "raid1", []string{a.dev, drop.dev}, true, nil, nil)
	p.must("create pool", err)
	p.uuid = uuid
	t.Cleanup(func() {
		for _, dev := range []string{a.dev, drop.dev} {
			_ = controlIoctl(iocForgetDev, dev)
		}
	})
	dropped := func() BtrfsDevice {
		t.Helper()
		devices := p.filesystem().Devices
		i := slices.IndexFunc(devices, func(d BtrfsDevice) bool { return canonicalPath(d.Path) == drop.dev })
		if i < 0 {
			t.Fatalf("no member on %s: %+v", drop.dev, devices)
		}
		return devices[i]
	}
	write := func(prefix string, n int) {
		for i := range n {
			writeRandom(t, filepath.Join(p.mnt, fmt.Sprintf("%s-%02d.bin", prefix, i)), 16<<20)
		}
		unix.Sync()
	}
	mountWhole := func(what string) {
		t.Helper()
		pools, findErr := FindOfflinePools(p.ctx, []string{a.dev, drop.dev})
		if findErr != nil || len(pools) != 1 || len(pools[0].Devices) != 2 {
			t.Fatalf("offline pools of %s = %+v, %v", what, pools, findErr)
		}
		p.must("mount "+what, MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, false))
	}
	p.must("mount", MountPool(p.ctx, a.dev, p.mnt, []string{"noatime"}, false))
	write("before", 8)
	// The health run sees the pool whole and saves its counters.
	_, err = recordFilesystems(p.ctx, []BtrfsFilesystem{p.filesystem()})
	p.must("record the pool whole", err)

	drop.fail()
	write("dropout", 4)
	gone := dropped()
	t.Logf("%s (devid %d) after its dropout counts %+v", drop.dev, gone.ID, gone.Errors)
	if gone.Errors.Write == 0 {
		t.Fatalf("the dropout counted no failed write on %s: %+v", drop.dev, gone.Errors)
	}
	p.must("unmount", unix.Unmount(p.mnt, 0))

	p.must("forget the dropped disk", controlIoctl(iocForgetDev, drop.dev))
	pools, err := FindOfflinePools(p.ctx, []string{a.dev})
	if err != nil || len(pools) != 1 {
		t.Fatalf("offline pools without the disk = %+v, %v", pools, err)
	}
	p.must("mount degraded", MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, true))
	states, err := loadPoolStates()
	p.must("pool state", err)
	if want := []missingDisk{{ID: uint64(gone.ID), UUID: gone.UUID, Whole: true}}; !slices.Equal(withoutAt(states[p.uuid].Missing), want) {
		t.Fatalf("missing disks of the episode = %+v, want %+v, with the counters of the pool whole", states[p.uuid].Missing, want)
	}
	write("degraded", 8)
	p.must("unmount", unix.Unmount(p.mnt, 0))

	drop.heal()
	p.must("register the returned disk", RegisterDevice(drop.dev))
	mountWhole("the pool with its disk back")
	p.must("resync", Resync(p.ctx, p.uuid, nil))
	back := dropped()
	doc, err := alerts.Load()
	p.must("alerts", err)
	var notice alerts.Alert
	health := false
	for _, x := range doc.Alerts {
		switch x.Key {
		case "resynced-" + p.uuid:
			notice = x
		case "health-" + p.uuid:
			health = true
		}
	}
	t.Logf("after the resync %s counts %+v; health alert up: %v; the notice (%s): %q", drop.dev, back.Errors, health, notice.Severity, notice.Message)
	if back.Errors.Write == 0 || !health || notice.Severity != alerts.SeverityWarning || !strings.Contains(notice.Message, "kept its error counters") || !strings.Contains(notice.Message, "it counts a failed read, write or flush") {
		t.Fatalf("after the resync %s counts %+v, health alert %v, notice %+v; want the failed writes kept, the alert up and a warning saying why", drop.dev, back.Errors, health, notice)
	}
	p.must("unmount", unix.Unmount(p.mnt, 0))
}
