package btrfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The rebuild proof: a member of a mounted raid1 pool drops off the bus, the
// pool is written to, the same disk comes back, and the pool is rebuilt onto
// it by a device replace of the member's devid, without unmounting. The test
// VM's data disks are virtio disks on PCIe root ports of their own, so a
// disk is pulled for real (PCI remove: virtio_blk's remove runs del_gendisk)
// and comes back on the same backing file (a rescan of its port), which no
// loop device or device-mapper target can stage. It runs only in the test VM
// and only on blank disks, since a PCI remove on a host would pull a real
// disk.

const (
	// The rig VM's SMBIOS serial (packaging/vm/linuxio-vm.sh define_domain).
	rigSerialPrefix = "ds=nocloud;s=http://192.168.122.1:"
	// Where the reboot phases keep what they wrote, on the VM's root disk.
	proofStatePath = "/var/lib/linuxio-rebuild-proof.json"
	nocowFlag      = 0x00800000 // FS_NOCOW_FL
	scrubReadOnly  = 1          // BTRFS_SCRUB_READONLY
)

var pciAddress = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)

// pciDisk is a virtio disk of the VM that the test pulls from its PCI bus
// and plugs back in.
type pciDisk struct {
	t    *testing.T
	fn   string // the disk's PCI function
	port string // the PCIe root port above it, which a rescan brings it back under
	dev  string // its /dev path while plugged in, "" while pulled
}

func newPCIDisk(t *testing.T, name string) *pciDisk {
	t.Helper()
	target, err := filepath.EvalSymlinks(filepath.Join("/sys/block", name, "device"))
	if err != nil {
		t.Skipf("no virtio disk %s: %v", name, err)
	}
	fn := filepath.Dir(target) // .../0000:00:01.5/0000:06:00.0/virtio4
	d := &pciDisk{t: t, fn: filepath.Base(fn), port: filepath.Base(filepath.Dir(fn)), dev: "/dev/" + name}
	if !pciAddress.MatchString(d.fn) || !pciAddress.MatchString(d.port) || !strings.HasPrefix(filepath.Base(target), "virtio") {
		t.Skipf("%s is not a virtio disk on a PCI port of its own: %s", name, target)
	}
	return d
}

func sysWrite(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString(value)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func settle() { _ = exec.Command("udevadm", "settle", "--timeout=60").Run() }

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// pull removes the disk's PCI function, as a disk that drops off the bus.
func (d *pciDisk) pull() {
	d.t.Helper()
	if d.dev == "" {
		d.t.Fatal("pull of a disk that is out")
	}
	if err := sysWrite(filepath.Join("/sys/bus/pci/devices", d.fn, "remove"), "1"); err != nil {
		d.t.Fatalf("remove %s: %v", d.fn, err)
	}
	waitFor(d.t, d.fn+" to go", time.Minute, func() bool {
		_, err := os.Stat(filepath.Join("/sys/bus/pci/devices", d.fn))
		return errors.Is(err, os.ErrNotExist)
	})
	settle()
	d.dev = ""
}

// plug rescans the port, which finds the same disk on the same backing file,
// and returns the name the kernel gave it.
func (d *pciDisk) plug() string {
	d.t.Helper()
	if err := d.rescan(); err != nil {
		d.t.Fatal(err)
	}
	return d.dev
}

func (d *pciDisk) rescan() error {
	if err := sysWrite(filepath.Join("/sys/bus/pci/devices", d.port, "rescan"), "1"); err != nil {
		return fmt.Errorf("rescan %s: %w", d.port, err)
	}
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		m, _ := filepath.Glob(filepath.Join("/sys/bus/pci/devices", d.fn, "virtio*", "block", "*"))
		if len(m) == 1 {
			if _, err := os.Stat("/dev/" + filepath.Base(m[0])); err == nil {
				settle()
				d.dev = "/dev/" + filepath.Base(m[0])
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s did not come back under %s", d.fn, d.port)
}

// diskIdentity is what a disk's own primary superblock says it is.
type diskIdentity struct {
	FSID       string
	DevID      uint64
	DevUUID    string
	Generation uint64
	Copies     []int64 // offsets of the superblock copies with the btrfs magic
}

func readIdentity(dev string) (diskIdentity, error) {
	f, err := os.Open(dev)
	if err != nil {
		return diskIdentity{}, err
	}
	defer f.Close()
	size, err := f.Seek(0, 2)
	if err != nil {
		return diskIdentity{}, err
	}
	var id diskIdentity
	sb := make([]byte, superSize)
	for _, off := range superblockOffsets(uint64(size)) {
		if _, err = f.ReadAt(sb, off); err != nil {
			return diskIdentity{}, err
		}
		if binary.LittleEndian.Uint64(sb[superMagicAt:]) != superMagic {
			continue
		}
		id.Copies = append(id.Copies, off)
		if off == superOffset {
			id.FSID = formatUUID(sb[superFSID : superFSID+16])
			id.DevID = binary.LittleEndian.Uint64(sb[superDevID:])
			id.DevUUID = formatUUID(sb[superDevUUID : superDevUUID+16])
			id.Generation = binary.LittleEndian.Uint64(sb[superGeneration:])
		}
	}
	return id, nil
}

// zeroSignatures zeroes the first MiB and every superblock copy of dev,
// whatever filesystem they hold, and makes the kernel forget it.
func zeroSignatures(dev string) error {
	f, err := openExclusive(dev, os.O_RDWR)
	if err != nil {
		return err
	}
	size, err := f.Seek(0, 2)
	if err == nil {
		_, err = f.WriteAt(make([]byte, 1<<20), 0)
	}
	for _, off := range superblockOffsets(uint64(size)) {
		if err == nil {
			_, err = f.WriteAt(make([]byte, superSize), off)
		}
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if forgetErr := controlIoctl(iocForgetDev, dev); err == nil && forgetErr != nil && !errors.Is(forgetErr, unix.ENOENT) {
		err = forgetErr
	}
	return err
}

// blank reports whether dev holds nothing: its first MiB and its superblock
// copies are zero. The open is exclusive, so a disk in use is not blank.
func blank(dev string) (bool, error) {
	f, err := openExclusive(dev, os.O_RDONLY)
	if err != nil {
		return false, err
	}
	defer f.Close()
	size, err := f.Seek(0, 2)
	if err != nil {
		return false, err
	}
	head := make([]byte, 1<<20)
	if _, err = f.ReadAt(head, 0); err != nil {
		return false, err
	}
	if slices.ContainsFunc(head, func(b byte) bool { return b != 0 }) {
		return false, nil
	}
	sb := make([]byte, superSize)
	for _, off := range superblockOffsets(uint64(size)) {
		if _, err = f.ReadAt(sb, off); err != nil {
			return false, err
		}
		if slices.ContainsFunc(sb, func(b byte) bool { return b != 0 }) {
			return false, nil
		}
	}
	return true, nil
}

// proofState is what the reboot phases keep between boots.
type proofState struct {
	UUID   string            `json:"uuid"`
	Files  map[string]string `json:"files"` // name: hex sha256
	Member uint64            `json:"member"`
}

type rebuildRig struct {
	t      *testing.T
	ctx    context.Context
	dir    string
	mnt    string
	uuid   string
	disks  map[string]*pciDisk // by the name each had when the test started
	files  map[string][32]byte
	nocow  map[string][]byte
	keep   bool // leave the pool on the disks for the next boot
	logged int  // kernel log lines already shown
	marks  bool // the kernel marked a pulled member missing itself (7.1)
}

func newRebuildRig(t *testing.T, wantBlank bool) *rebuildRig {
	t.Helper()
	serial, err := os.ReadFile("/sys/class/dmi/id/product_serial")
	if err != nil || !strings.HasPrefix(string(serial), rigSerialPrefix) {
		t.Skip("runs only in the LinuxIO test VM (make vm-test-btrfs-integration): it pulls virtio disks off the PCI bus")
	}
	t.Setenv("TMPDIR", "/var/tmp")
	dir := t.TempDir()
	stubStatePaths(t, dir)
	r := &rebuildRig{
		t: t, ctx: context.Background(), dir: dir, mnt: filepath.Join(dir, "mnt"),
		disks: map[string]*pciDisk{}, files: map[string][32]byte{}, nocow: map[string][]byte{},
	}
	for _, name := range []string{"vdc", "vdd", "vde"} {
		d := newPCIDisk(t, name)
		if wantBlank {
			if ok, err := blank(d.dev); err != nil || !ok {
				t.Skipf("%s is not blank (%v); run make vm-test-btrfs-integration without VM= so the VM is reverted first", d.dev, err)
			}
		}
		r.disks[name] = d
	}
	r.must("mountpoint", os.MkdirAll(r.mnt, 0o755))
	r.kernelLog("before the test")
	t.Cleanup(r.restore)
	return r
}

// restore leaves the VM as the test found it: the pool unmounted, every
// disk plugged in and, unless a reboot phase keeps it, blank again.
func (r *rebuildRig) restore() {
	t := r.t
	if r.mounted() {
		_, _ = cancelReplace(r.mnt)
		var err error
		for range 600 {
			if err = unix.Unmount(r.mnt, 0); err == nil || errors.Is(err, unix.EINVAL) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil && !errors.Is(err, unix.EINVAL) {
			t.Errorf("cleanup: unmount %s: %v", r.mnt, err)
		}
	}
	for name, d := range r.disks {
		if d.dev == "" {
			if err := d.rescan(); err != nil {
				t.Errorf("cleanup: plug %s back: %v", name, err)
				continue
			}
		}
		if r.keep {
			continue
		}
		if err := zeroSignatures(d.dev); err != nil {
			t.Errorf("cleanup: wipe %s (was %s): %v", d.dev, name, err)
		}
	}
	settle()
}

func (r *rebuildRig) must(what string, err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatalf("%s: %v", what, err)
	}
}

func (r *rebuildRig) mounted() bool {
	mounts, err := ListMounts(r.ctx)
	return err == nil && slices.ContainsFunc(mounts, func(x Mount) bool { return x.Mountpoint == r.mnt })
}

func (r *rebuildRig) filesystem() BtrfsFilesystem {
	r.t.Helper()
	fs, err := FindFilesystem(r.ctx, r.uuid)
	r.must("inventory", err)
	return fs
}

// kernelLog shows the kernel's btrfs and disk lines since the last call.
func (r *rebuildRig) kernelLog(label string) {
	r.t.Helper()
	out, err := exec.Command("dmesg").Output()
	if err != nil {
		r.t.Logf("dmesg: %v", err)
		return
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if r.logged > len(lines) {
		r.logged = 0
	}
	var shown []string
	for _, l := range lines[r.logged:] {
		if strings.Contains(l, "BTRFS") || strings.Contains(l, "virtio_blk") || strings.Contains(l, "btrfs") {
			shown = append(shown, "    "+l)
		}
	}
	r.logged = len(lines)
	if label != "before the test" {
		r.t.Logf("kernel log, %s:\n%s", label, strings.Join(shown, "\n"))
	}
}

func (r *rebuildRig) devInfo(id uint64) (devInfoArgs, error) {
	di := devInfoArgs{DevID: id}
	err := fsIoctl(r.mnt, iocDevInfo, unsafe.Pointer(&di))
	return di, err
}

func (r *rebuildRig) generation() uint64 {
	r.t.Helper()
	info := fsInfoArgs{Flags: fsInfoFlagGeneration}
	r.must("fs info", fsIoctl(r.mnt, iocFSInfo, unsafe.Pointer(&info)))
	return info.Generation
}

// observe logs what the kernel and LinuxIO's inventory say about each
// device slot: DEV_INFO, the error counters, sysfs devinfo and devices/.
func (r *rebuildRig) observe(label string) {
	r.t.Helper()
	var b strings.Builder
	info := fsInfoArgs{Flags: fsInfoFlagGeneration}
	if err := fsIoctl(r.mnt, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		fmt.Fprintf(&b, "  FS_INFO: %v\n", err)
	} else {
		fmt.Fprintf(&b, "  FS_INFO num_devices %d max_id %d generation %d\n", info.NumDevices, info.MaxID, info.Generation)
	}
	sys := filepath.Join(sysFSBtrfs, r.uuid)
	for id := uint64(0); id <= max(info.MaxID, 3); id++ {
		r.observeSlot(&b, id)
	}
	if entries, err := os.ReadDir(filepath.Join(sys, "devices")); err == nil {
		for _, e := range entries {
			link, _ := os.Readlink(filepath.Join(sys, "devices", e.Name()))
			_, statErr := os.Stat(filepath.Join(sys, "devices", e.Name()))
			fmt.Fprintf(&b, "  sysfs devices/%s -> %s (resolves: %v)\n", e.Name(), link, statErr == nil)
		}
	}
	if fs, err := FindFilesystem(r.ctx, r.uuid); err == nil {
		for _, d := range fs.Devices {
			fmt.Fprintf(&b, "  inventory device %d: path %q missing %v errors %+v\n", d.ID, d.Path, d.Missing, d.Errors)
		}
		fmt.Fprintf(&b, "  inventory degraded %v allocation %+v\n", fs.Degraded, fs.Allocation)
	}
	if state, pct, _, err := replaceStatusOf(r.mnt); err == nil {
		fmt.Fprintf(&b, "  replace status: state %d at %.1f%%\n", state, pct)
	}
	r.t.Logf("%s:\n%s", label, b.String())
}

// observeSlot writes what DEV_INFO, GET_DEV_STATS and sysfs devinfo say
// about devid id, nothing when the pool has no such device.
func (r *rebuildRig) observeSlot(b *strings.Builder, id uint64) {
	di, err := r.devInfo(id)
	switch {
	case errors.Is(err, unix.ENODEV):
		return
	case err != nil:
		fmt.Fprintf(b, "  DEV_INFO %d: %v\n", id, err)
		return
	}
	path := cstring(di.Path[:])
	alive := "no block device at that path"
	if _, statErr := os.Stat(path); statErr == nil {
		alive = "path exists"
	}
	fmt.Fprintf(b, "  DEV_INFO %d: path %q (%s) uuid %s total %d used %d\n", id, path, alive, formatUUID(di.UUID[:]), di.TotalBytes, di.BytesUsed)
	stats := devStatsArgs{DevID: id, NrItems: 5}
	if err := fsIoctl(r.mnt, iocGetDevStats, unsafe.Pointer(&stats)); err != nil {
		fmt.Fprintf(b, "    GET_DEV_STATS: %v\n", err)
	} else {
		fmt.Fprintf(b, "    GET_DEV_STATS write %d read %d flush %d corruption %d generation %d\n", stats.Values[0], stats.Values[1], stats.Values[2], stats.Values[3], stats.Values[4])
	}
	var attrs []string
	for _, a := range []string{"missing", "writeable", "in_fs_metadata", "replace_target"} {
		v, err := os.ReadFile(filepath.Join(sysFSBtrfs, r.uuid, "devinfo", strconv.FormatUint(id, 10), a))
		if err != nil {
			attrs = append(attrs, a+"=?")
			continue
		}
		attrs = append(attrs, a+"="+strings.TrimSpace(string(v)))
	}
	fmt.Fprintf(b, "    sysfs devinfo/%d: %s\n", id, strings.Join(attrs, " "))
}

// write writes n files of size random bytes and syncs.
func (r *rebuildRig) write(prefix string, n int, size int64) {
	r.t.Helper()
	for i := range n {
		name := fmt.Sprintf("%s-%02d.bin", prefix, i)
		r.files[name] = writeRandom(r.t, filepath.Join(r.mnt, name), size)
	}
	unix.Sync()
}

// writeNocow writes a nodatacow file: no checksums, overwritten in place.
func (r *rebuildRig) writeNocow(name string, size int) {
	r.t.Helper()
	f, err := os.OpenFile(filepath.Join(r.mnt, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	r.must("create "+name, err)
	defer f.Close()
	flags, err := unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	r.must("flags of "+name, err)
	r.must("nodatacow "+name, unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, int(flags|nocowFlag)))
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	_, err = f.WriteAt(buf, 0)
	r.must("write "+name, err)
	r.must("sync "+name, f.Sync())
	if flags, err = unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS); err != nil || flags&nocowFlag == 0 {
		r.t.Fatalf("%s is not nodatacow: flags %#x, %v", name, flags, err)
	}
	r.nocow[name] = buf
}

// rewriteNocow overwrites n bytes at off of a nodatacow file in place.
func (r *rebuildRig) rewriteNocow(name string, off, n int) {
	r.t.Helper()
	buf := r.nocow[name]
	_, _ = rand.Read(buf[off : off+n])
	f, err := os.OpenFile(filepath.Join(r.mnt, name), os.O_RDWR, 0)
	r.must("open "+name, err)
	defer f.Close()
	_, err = f.WriteAt(buf[off:off+n], int64(off))
	r.must("rewrite "+name, err)
	r.must("sync "+name, f.Sync())
}

// verifyFiles reads every file back from the disks, not the page cache.
func (r *rebuildRig) verifyFiles(label string) {
	r.t.Helper()
	unix.Sync()
	r.must("drop caches", sysWrite("/proc/sys/vm/drop_caches", "3"))
	for name, want := range r.files {
		if got := digest(r.t, filepath.Join(r.mnt, name)); got != want {
			r.t.Fatalf("%s: %s reads back wrong", label, name)
		}
	}
	for name, buf := range r.nocow {
		if got := digest(r.t, filepath.Join(r.mnt, name)); got != sha256.Sum256(buf) {
			r.t.Fatalf("%s: nodatacow %s reads back wrong", label, name)
		}
	}
	r.t.Logf("%s: %d files and %d nodatacow files read back right", label, len(r.files), len(r.nocow))
}

// scrubReadOnly scrubs every device without repairing and returns the sum.
func (r *rebuildRig) scrubReadOnly() scrubProgress {
	r.t.Helper()
	var sum scrubProgress
	for _, d := range r.filesystem().Devices {
		if d.Missing {
			r.t.Fatalf("scrub with device %d missing", d.ID)
		}
		args := scrubArgs{DevID: uint64(d.ID), End: ^uint64(0), Flags: scrubReadOnly}
		r.must(fmt.Sprintf("read-only scrub of device %d", d.ID), blockingIoctl(r.mnt, iocScrub, unsafe.Pointer(&args)))
		p := args.Progress
		sum.DataBytesScrubbed += p.DataBytesScrubbed
		sum.TreeBytesScrubbed += p.TreeBytesScrubbed
		sum.ReadErrors += p.ReadErrors
		sum.CsumErrors += p.CsumErrors
		sum.VerifyErrors += p.VerifyErrors
		sum.NoCsum += p.NoCsum
		sum.SuperErrors += p.SuperErrors
		sum.UncorrectableErrors += p.UncorrectableErrors
		sum.CorrectedErrors += p.CorrectedErrors
		sum.UnverifiedErrors += p.UnverifiedErrors
	}
	return sum
}

// checkWhole checks that the pool is whole: every file reads back, a
// read-only scrub finds nothing, the devices are ids with none missing, and
// every chunk has the profile want gives its kind.
func (r *rebuildRig) checkWhole(label string, ids []int, want map[string]string) {
	r.t.Helper()
	r.verifyFiles(label)
	s := r.scrubReadOnly()
	r.t.Logf("%s: read-only scrub %+v", label, s)
	if s.DataBytesScrubbed == 0 || s.ReadErrors+s.CsumErrors+s.VerifyErrors+s.SuperErrors+s.UncorrectableErrors+s.CorrectedErrors+s.UnverifiedErrors != 0 {
		r.t.Fatalf("%s: read-only scrub found errors: %+v", label, s)
	}
	fs := r.filesystem()
	var got []int
	for _, d := range fs.Devices {
		if d.Missing {
			r.t.Fatalf("%s: device %d missing: %+v", label, d.ID, fs.Devices)
		}
		got = append(got, d.ID)
	}
	if !slices.Equal(got, ids) {
		r.t.Fatalf("%s: devids %v, want %v", label, got, ids)
	}
	for _, a := range fs.Allocation {
		if a.Profile != want[a.Kind] {
			r.t.Fatalf("%s: %s chunks are %s, want %s: %+v", label, a.Kind, a.Profile, want[a.Kind], fs.Allocation)
		}
	}
	r.observe(label)
}

// replaceRaw starts a replace of devid id onto target through the ioctl
// with the given source-reading mode and waits for it, as Replace does.
func (r *rebuildRig) replaceRaw(id int, target string, mode uint64) error {
	args := devReplaceArgs{Cmd: replaceCmdStart}
	putU64(args.Params[:], replaceSrcDevID, uint64(id))
	putU64(args.Params[:], replaceContReading, mode)
	copy(args.Params[replaceTgtDevName:], target)
	if err := blockingIoctl(r.mnt, iocDevReplace, unsafe.Pointer(&args)); err != nil {
		return err
	}
	if args.Result != 0 {
		return fmt.Errorf("replace result %d", args.Result)
	}
	return nil
}

// waitCopied waits until the running replace of devid id has copied at
// least n bytes, as its scrub of the source counts them. The status's
// percent is of the source's whole size and moves only when a dev extent
// is done, so it cannot say where a copy stands inside one.
func (r *rebuildRig) waitCopied(id int, n uint64, done <-chan error) {
	r.t.Helper()
	waitFor(r.t, fmt.Sprintf("the replace to copy %d MiB", n>>20), 5*time.Minute, func() bool {
		select {
		case err := <-done:
			r.t.Fatalf("the throttled replace ended before %d MiB were copied: %v", n>>20, err)
		default:
		}
		state, _, _, err := replaceStatusOf(r.mnt)
		if err != nil || state != replaceStateStarted {
			return false // the status says FINISHED, of the last replace, until this one starts
		}
		p, running, err := deviceScrubProgress(r.mnt, id)
		return err == nil && running && p.DataBytesScrubbed+p.TreeBytesScrubbed >= n
	})
}

// checkReturned checks the returned disk's own superblock against the
// member it claims to be, as a product would before erasing it.
func (r *rebuildRig) checkReturned(dev string, id uint64, devUUID string) diskIdentity {
	r.t.Helper()
	got, err := readIdentity(dev)
	r.must("identity of "+dev, err)
	gen := r.generation()
	r.t.Logf("returned disk %s: superblock fsid %s devid %d dev uuid %s generation %d, copies at %v; pool generation %d, member %d dev uuid %s",
		dev, got.FSID, got.DevID, got.DevUUID, got.Generation, got.Copies, gen, id, devUUID)
	if got.FSID != r.uuid || got.DevID != id || got.DevUUID != devUUID || got.Generation > gen {
		r.t.Fatalf("%s is not member %d of %s as it left: %+v", dev, id, r.uuid, got)
	}
	return got
}

func (r *rebuildRig) createPool(meta string, disks ...*pciDisk) {
	r.t.Helper()
	var devs []string
	for _, d := range disks {
		devs = append(devs, d.dev)
	}
	uuid, err := CreatePool(r.ctx, "linuxio-rebuild", "raid1", meta, devs, true, nil, nil)
	r.must("create pool", err)
	r.uuid = uuid
	r.must("mount", MountPool(r.ctx, devs[0], r.mnt, []string{"noatime"}, false))
	r.files, r.nocow = map[string][32]byte{}, map[string][]byte{}
}

func (r *rebuildRig) unmount() {
	r.t.Helper()
	r.must("unmount", unix.Unmount(r.mnt, 0))
}

// TestBtrfsRebuildProofIntegration pulls a member of a mounted raid1 pool off
// the bus, writes to the pool, plugs the same disk back and rebuilds the pool
// onto it with a device replace of the member's devid, without unmounting:
// on a kernel that does not call the pulled disk missing (one without
// btrfs_remove_bdev) and after a degraded mount that does, with each mode of
// reading the source, with two members and with three. It also stages what
// can go wrong: a smaller target, a clone of the member, the returned disk
// failing again mid-replace.
//
// LINUXIO_REBUILD_PROOF_PHASE splits a check across a reboot, run by hand on
// the VM N that a make vm-test-btrfs-integration run used (its home keeps
// btrfs.test), each phase with
//
//	make vm-ssh VM=N CMD='sudo env LINUXIO_RUN_BTRFS_INTEGRATION=1 LINUXIO_REBUILD_PROOF_PHASE=keep ./btrfs.test -test.run TestBtrfsRebuildProofIntegration -test.v'
//
// targetgone alone, on a kernel that marks a gone disk missing (7.1), which
// the run without a phase leaves out there: it pulls a replace's target,
// after which that kernel's memory is corrupt, so the VM is reverted
// afterwards (make vm-reset VM=N).
//
// keep then verify: vm-ssh shuts the VM down when it ends and the next one
// boots it. cut then resume: cut resets the VM with sysrq b mid-replace, so
// its vm-ssh fails, and the rig may not get the rebooting VM to shut down
// (it says so); resume runs on it as it is.
func TestBtrfsRebuildProofIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	phase := os.Getenv("LINUXIO_REBUILD_PROOF_PHASE")
	r := newRebuildRig(t, phase == "" || phase == "keep" || phase == "cut" || phase == "targetgone")
	var uts unix.Utsname
	r.must("uname", unix.Uname(&uts))
	t.Logf("kernel %s, phase %q", unix.ByteSliceToString(uts.Release[:]), phase)
	switch phase {
	case "":
		r.notMissing()
		if r.marks {
			// A kernel that marks a gone disk missing (7.1) finishes a
			// replace onto its pulled target and corrupts its own memory
			// from then on (docs/btrfs.md, Missing disks, Known limits):
			// whatever ran after these legs would run on that kernel.
			t.Log("skipping the legs that pull a replace's target: on this kernel they corrupt kernel memory; LINUXIO_REBUILD_PROOF_PHASE=targetgone runs them, on a VM to revert afterwards")
		} else {
			r.targetFailsMidReplace()
			r.targetFailsThenWiped()
		}
		r.rebuiltDiskAlone()
		r.missingState()
		r.threeMembers()
	case "targetgone":
		r.notMissing()
		r.targetFailsMidReplace()
	case "keep":
		r.keepForReboot()
	case "verify":
		r.verifyAfterReboot()
	case "cut":
		r.cutMidReplace()
	case "resume":
		r.resumeAfterCut()
	default:
		t.Fatalf("unknown LINUXIO_REBUILD_PROOF_PHASE %q", phase)
	}
}

var raid1 = map[string]string{"data": "raid1", "metadata": "raid1", "system": "raid1"}

// dropout pulls disk d, member id, from the mounted pool, writes to the pool
// while it is out (new files and an in-place rewrite of the nodatacow file)
// and plugs it back. It returns the member's device UUID and the disk's new
// path.
func (r *rebuildRig) dropout(d *pciDisk, id uint64, prefix string) (devUUID, returned string) {
	r.t.Helper()
	di, err := r.devInfo(id)
	r.must("dev info", err)
	devUUID = formatUUID(di.UUID[:])
	was := d.dev
	r.observe("before " + prefix + ": " + was + " is member " + strconv.FormatUint(id, 10))
	r.kernelLog("before " + prefix)
	d.pull()
	if data, err := os.ReadFile(filepath.Join(sysFSBtrfs, r.uuid, "devinfo", strconv.FormatUint(id, 10), "missing")); err == nil && strings.TrimSpace(string(data)) == "1" {
		r.marks = true
	}
	pulledAt := r.generation()
	r.write(prefix, 4, 32<<20)
	r.rewriteNocow("nocow.bin", 16<<20, 16<<20)
	r.observe(fmt.Sprintf("%s: %s pulled at generation %d, pool written", prefix, was, pulledAt))
	r.kernelLog(prefix + ": pulled and written")
	returned = d.plug()
	r.t.Logf("%s: %s came back as %s (devt %s)", prefix, was, returned, devt(returned))
	r.kernelLog(prefix + ": plugged back, udev settled")
	r.observe(prefix + ": after the return and udev's scan")
	return devUUID, returned
}

func devt(dev string) string {
	var st unix.Stat_t
	if err := unix.Stat(dev, &st); err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%d:%d", unix.Major(st.Rdev), unix.Minor(st.Rdev))
}

// notMissing: 7.0 without btrfs_remove_bdev keeps the pulled disk's dead
// block device and never calls it missing. The returned disk is refused by
// the kernel's scan; after its superblocks are wiped, a replace of the
// member's devid onto it with the dead source read always (_ALWAYS, which
// btrfs replace start uses; LinuxIO's Replace avoids a missing source)
// rebuilds the pool. Before that, the failure cases that need the disk out:
// a smaller target and clones of members. Since 7.1 the kernel calls the
// member missing at the pull, so what is written meanwhile goes to single
// chunks, which the replace leaves and the resync converts, as here.
func (r *rebuildRig) notMissing() {
	t := r.t
	vdc, vdd := r.disks["vdc"], r.disks["vdd"]
	r.createPool("raid1", vdc, vdd)
	r.write("before", 4, 32<<20)
	r.writeNocow("nocow.bin", 64<<20)
	devUUID, returned := r.dropout(vdd, 2, "dropout1")

	err := RegisterDevice(returned)
	t.Logf("RegisterDevice(%s) while member 2 holds its dead disk: %v", returned, err)
	r.kernelLog("after RegisterDevice")
	r.checkReturned(returned, 2, devUUID)
	r.clonesAreRefused(returned, vdc.dev)
	r.smallerTargetIsRefused()

	wiped, err := WipePoolSuperblocks(returned, r.uuid)
	t.Logf("WipePoolSuperblocks(%s): wiped %v, %v", returned, wiped, err)
	r.must("wipe the returned disk", err)
	if id, idErr := readIdentity(returned); idErr != nil || len(id.Copies) != 0 {
		t.Fatalf("after the wipe %s still has superblocks: %+v %v", returned, id, idErr)
	}
	start := time.Now()
	err = r.replaceRaw(2, returned, 0)
	t.Logf("replace(devid 2 -> %s, source read always) took %v: %v", returned, time.Since(start).Round(time.Millisecond), err)
	r.kernelLog("rebuild 1")
	r.must("rebuild onto the returned disk", err)
	_, err = ReplaceResult(r.ctx, r.uuid, 2, returned)
	r.must("replace result", err)
	r.convertIfMixed()
	r.checkWhole("after rebuild 1 (not missing, source read always)", []int{1, 2}, raid1)
	if di, _ := r.devInfo(2); formatUUID(di.UUID[:]) != devUUID {
		t.Logf("member 2's device UUID changed from %s to %s", devUUID, formatUUID(di.UUID[:]))
	} else {
		t.Logf("member 2 keeps device UUID %s across the replace", devUUID)
	}
	r.unmount()
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("register "+returned, RegisterDevice(returned))
	r.must("mount again", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("rebuild 1 after an unmount and mount", []int{1, 2}, raid1)
}

// clonesAreRefused: a disk that carries a member's superblock, copied from
// the pool's other member (same fsid, another devid) or from the returned
// disk itself (same devid and device UUID), on a loop device.
func (r *rebuildRig) clonesAreRefused(returned, other string) {
	t := r.t
	for _, src := range []string{other, returned} {
		img := filepath.Join(r.dir, "clone-"+filepath.Base(src)+".img")
		loop := loopDevice(t, img, 8<<30)
		in, err := os.Open(src)
		r.must("open "+src, err)
		out, err := os.OpenFile(loop, os.O_RDWR, 0)
		r.must("open "+loop, err)
		sb := make([]byte, superSize)
		for _, off := range []int64{superOffset, 64 << 20} {
			_, err = in.ReadAt(sb, off)
			r.must("read "+src, err)
			_, err = out.WriteAt(sb, off)
			r.must("write "+loop, err)
		}
		r.must("sync "+loop, out.Sync())
		in.Close()
		out.Close()
		id, err := readIdentity(loop)
		r.must("identity of the clone", err)
		regErr := RegisterDevice(loop)
		t.Logf("clone of %s on %s: superblock %+v; RegisterDevice: %v", src, loop, id, regErr)
		if regErr == nil {
			t.Errorf("the kernel took a clone of %s on %s into the mounted pool", src, loop)
		}
		_ = controlIoctl(iocForgetDev, loop)
	}
	r.kernelLog("clones")
}

// smallerTargetIsRefused: a target smaller than the member is refused by
// the kernel before anything is written, and the pool is left as it was.
func (r *rebuildRig) smallerTargetIsRefused() {
	t := r.t
	loop := loopDevice(t, filepath.Join(r.dir, "small.img"), 4<<30)
	err := Replace(r.ctx, r.uuid, 2, loop, func(float64) {})
	t.Logf("Replace(devid 2 -> %s, 4 GiB of the member's 8): %v", loop, err)
	r.kernelLog("smaller target")
	if !errors.Is(err, unix.EINVAL) {
		t.Fatalf("a smaller target: %v, want EINVAL", err)
	}
	if _, err := r.devInfo(0); !errors.Is(err, unix.ENODEV) {
		t.Fatalf("a refused replace left a target device: %v", err)
	}
}

// pullTargetMidReplace pulls member 2's disk, writes, plugs it back and,
// once wiped, replaces member 2 onto it, throttled, and pulls it again while
// the copy runs. The kernel copies on to the end without the target (the
// replace's num_write_errors, which would stop it, is read in scrub.c but
// raised nowhere in 6.8, 7.0 or 7.1) and installs the dead target. On 7.0
// the commit that finishes cannot write a superblock to either dead disk, so
// the transaction aborts and the pool goes read-only, but the surviving
// disk's superblock of that commit is written first, with the replace
// cursor past every extent; a new replace is then refused. Since 7.1 the
// kernel marks the pulled target missing (btrfs_remove_bdev), the commit
// skips it, and the replace finishes "to <missing disk>" with the pool
// writable and member 2 missing. The disk comes back still carrying the
// replace target's superblock (devid 0). It returns when the replace
// started, the disk's path, and whether the commit aborted (7.0).
func (r *rebuildRig) pullTargetMidReplace(prefix string) (time.Time, string, bool) {
	t := r.t
	vdd := r.disks["vdd"]
	devUUID, returned := r.dropout(vdd, 2, prefix)
	r.checkReturned(returned, 2, devUUID)
	_, err := WipePoolSuperblocks(returned, r.uuid)
	r.must("wipe the returned disk", err)
	// Paced by the source's scrub speed limit, so it still copies when the
	// target goes.
	speed := filepath.Join(sysFSBtrfs, r.uuid, "devinfo", "2", "scrub_speed_max")
	r.must("throttle the replace", sysWrite(speed, strconv.Itoa(16<<20)))
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- r.replaceRaw(2, returned, replaceReadAvoid) }()
	r.waitCopied(2, 96<<20, done)
	state, pct, _, _ := replaceStatusOf(r.mnt)
	p, _, _ := deviceScrubProgress(r.mnt, 2)
	t.Logf("%s: replace onto %s after %v: status %.1f%% (state %d), %d MiB copied; pulling the target",
		prefix, returned, time.Since(start).Round(time.Millisecond), pct, state, (p.DataBytesScrubbed+p.TreeBytesScrubbed)>>20)
	vdd.pull()
	r.must("speed limit off", sysWrite(speed, "0"))
	var replaceErr error
	select {
	case replaceErr = <-done:
	case <-time.After(10 * time.Minute):
		t.Fatal("the replace did not end after its target was pulled")
	}
	state, pct, _, _ = replaceStatusOf(r.mnt)
	var st unix.Statfs_t
	r.must("statfs", unix.Statfs(r.mnt, &st))
	aborted := st.Flags&unix.ST_RDONLY != 0
	t.Logf("%s: replace with its target pulled returned %v; status state %d (1 started, 2 finished, 3 canceled) at %.1f%%; pool read-only %v",
		prefix, replaceErr, state, pct, aborted)
	r.kernelLog(prefix + ": target pulled mid-replace")
	r.observe(prefix + ": after the target failed mid-replace")
	if !aborted {
		missing := slices.ContainsFunc(r.filesystem().Devices, func(d BtrfsDevice) bool { return d.ID == 2 && d.Missing })
		if state != replaceStateFinished || !missing {
			t.Fatalf("%s: writable pool after the target was pulled with replace state %d and member 2 missing %v, want finished onto the missing disk", prefix, state, missing)
		}
	}
	again := vdd.plug()
	id, err := readIdentity(again)
	t.Logf("%s: the twice-returned disk is %s (devt %s): superblock %+v, %v", prefix, again, devt(again), id, err)
	r.kernelLog(prefix + ": twice-returned disk plugged")
	if aborted {
		err = r.replaceRaw(2, again, replaceReadAvoid)
		t.Logf("%s: a new replace onto %s on the read-only pool: %v", prefix, again, err)
	}
	r.unmount()
	survivor, err := readIdentity(r.disks["vdc"].dev)
	t.Logf("%s: after the unmount the surviving disk's superblock is at generation %d (%v); the failed target's at %d",
		prefix, survivor.Generation, err, id.Generation)
	return start, again, aborted
}

// targetFailsMidReplace: after the failed finish, mounted again with the
// failed target present. The source is gone (7.0), or the member the finish
// installed is (since 7.1), so only a degraded mount is allowed. On 7.0 it
// resumes the replace from the cursor the last commit wrote, which is past
// every extent, so the replace finishes at once and the target keeps
// whatever it had: here, the disk's content from before this dropout. A
// scrub finds the stale copies of checksummed data; those of nodatacow data
// it cannot see. Since 7.1 the replace had finished onto the missing disk:
// the kernel refuses even the degraded mount while it knows the disk with
// the target's superblock, mounts once it forgets it, and nothing resumes.
// The stale member, or the target's superblock, is then
// erased and the pool rebuilt onto the disk from the start.
func (r *rebuildRig) targetFailsMidReplace() {
	t := r.t
	vdc := r.disks["vdc"]
	start, again, aborted := r.pullTargetMidReplace("dropout2")
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	err := RegisterDevice(again)
	t.Logf("RegisterDevice(%s), the replace target on disk: %v", again, err)
	err = MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false)
	t.Logf("plain mount after the failed finish: %v", err)
	r.kernelLog("plain mount after the failed finish")
	if err == nil {
		t.Fatal("the pool mounted whole with its replace source gone")
	}
	if !aborted {
		// The replace item says finished, and the kernel refuses any mount
		// while a device of the pool carries the target's devid 0
		// (btrfs_init_dev_replace: "replace without active item"), until
		// it forgets that device; LinuxIO's erase of the disk forgets it.
		err = MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, true)
		t.Logf("degraded mount with the finished replace's target registered: %v", err)
		r.kernelLog("degraded mount with the target registered")
		if !errors.Is(err, unix.EUCLEAN) {
			t.Fatalf("degraded mount with the finished replace's target registered = %v, want EUCLEAN", err)
		}
		r.must("forget "+again, controlIoctl(iocForgetDev, again))
	}
	r.must("degraded mount", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, true))
	r.kernelLog("degraded mount after the failed finish")
	r.observe("mounted degraded after the failed finish")
	if aborted {
		err = ResumeReplace(r.ctx, r.uuid, 2, start, func(float64) {})
		t.Logf("ResumeReplace after the failed finish: %v", err)
		r.kernelLog("resumed replace")
		r.must("resumed replace", err)
		s := r.scrubReadOnly()
		t.Logf("after the resumed replace, read-only scrub: %+v", s)
		if s.CsumErrors != 0 {
			t.Logf("TRAP: the resumed replace finished with %d stale checksummed sectors on the target; nodatacow sectors are unchecked", s.CsumErrors)
		}
		r.observe("after the resumed replace")
	} else {
		r.finishedOntoMissing("degraded mount after the finish onto the missing disk")
	}

	r.unmount()
	wiped, err := WipePoolSuperblocks(again, r.uuid)
	t.Logf("WipePoolSuperblocks(%s), the stale member: wiped %v, %v", again, wiped, err)
	r.must("wipe the stale member", err)
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("degraded mount", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, true))
	r.must("rebuild onto the erased member", Replace(r.ctx, r.uuid, 2, again, func(float64) {}))
	r.kernelLog("rebuild onto the erased member")
	r.convertIfMixed()
	r.checkWhole("after the rebuild onto the erased member", []int{1, 2}, raid1)
	r.unmount()
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("register "+again, RegisterDevice(again))
	r.must("mount whole", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("the rebuild onto the erased member after an unmount and mount", []int{1, 2}, raid1)
}

// finishedOntoMissing checks the pool as 7.1 and later leave it after a
// replace whose target was pulled: the replace finished, so nothing is left
// to resume or cancel, and member 2 is missing.
func (r *rebuildRig) finishedOntoMissing(label string) {
	r.t.Helper()
	state, pct, _, err := replaceStatusOf(r.mnt)
	r.t.Logf("%s: replace state %d at %.1f%%, %v", label, state, pct, err)
	missing := slices.ContainsFunc(r.filesystem().Devices, func(d BtrfsDevice) bool { return d.ID == 2 && d.Missing })
	if err != nil || state != replaceStateFinished || !missing {
		r.t.Fatalf("%s: replace state %d (%v) and member 2 missing %v, want the replace finished and the member missing", label, state, err, missing)
	}
}

// targetFailsThenWiped: the same failure, but the failed target is wiped
// before the pool is mounted again. On 7.0 the degraded mount then finds no
// target and leaves the replace suspended; a cancel ends it. Since 7.1 the
// replace had finished onto the missing disk and there is none to cancel.
// A new replace copies everything from the start.
func (r *rebuildRig) targetFailsThenWiped() {
	t := r.t
	vdc := r.disks["vdc"]
	_, again, aborted := r.pullTargetMidReplace("dropout3")
	wiped, err := WipePoolSuperblocks(again, r.uuid)
	t.Logf("WipePoolSuperblocks(%s), the failed target: wiped %v, %v", again, wiped, err)
	r.must("wipe the failed target", err)
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("degraded mount", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, true))
	r.kernelLog("degraded mount without the failed target")
	r.observe("mounted degraded without the failed target")
	if aborted {
		state, pct, _, err := replaceStatusOf(r.mnt)
		t.Logf("replace status after the degraded mount without the target: state %d (4 suspended) at %.1f%%, %v", state, pct, err)
		err = CancelReplace(r.ctx, r.uuid, 2)
		state, pct, _, _ = replaceStatusOf(r.mnt)
		t.Logf("CancelReplace: %v; status state %d at %.1f%%", err, state, pct)
		r.kernelLog("cancel")
		r.must("cancel the suspended replace", err)
	} else {
		r.finishedOntoMissing("degraded mount without the target the replace finished onto")
	}
	r.must("rebuild from the start", Replace(r.ctx, r.uuid, 2, again, func(float64) {}))
	r.kernelLog("rebuild after the cancel")
	r.convertIfMixed()
	r.checkWhole("after the rebuild that followed the cancel", []int{1, 2}, raid1)
	r.unmount()
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("register "+again, RegisterDevice(again))
	r.must("mount whole", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("the rebuild that followed the cancel after an unmount and mount", []int{1, 2}, raid1)
}

// convertIfMixed converts the chunks a degraded mount, or the kernel with a
// member missing, allocated with one copy back to raid1, as the resync does,
// logging the allocation before.
func (r *rebuildRig) convertIfMixed() { r.convertTo(raid1) }

// convertTo converts every chunk not in want's profile for its kind back to
// it, soft, as the resync does.
func (r *rebuildRig) convertTo(want map[string]string) {
	r.t.Helper()
	fs := r.filesystem()
	if !slices.ContainsFunc(fs.Allocation, func(a BtrfsAllocation) bool { return a.Profile != want[a.Kind] }) {
		r.t.Logf("no chunk to convert: %+v", fs.Allocation)
		return
	}
	r.t.Logf("converting to %v: %+v", want, fs.Allocation)
	data, meta := want["data"], want["metadata"]
	r.must("convert", Balance(r.ctx, r.uuid, BalanceOptions{DataProfile: &data, MetadataProfile: &meta, Soft: true}, func(float64) {}))
}

// rebuiltDiskAlone: the rebuilt disk alone, its partner forgotten, mounts
// read-only and holds every file.
func (r *rebuildRig) rebuiltDiskAlone() {
	t := r.t
	vdc, vdd := r.disks["vdc"], r.disks["vdd"]
	r.unmount()
	r.must("forget "+vdc.dev, controlIoctl(iocForgetDev, vdc.dev))
	r.must("register "+vdd.dev, RegisterDevice(vdd.dev))
	r.must("mount the rebuilt disk alone", MountPool(r.ctx, vdd.dev, r.mnt, []string{"ro"}, true))
	r.verifyFiles("the rebuilt disk alone")
	r.observe("the rebuilt disk alone")
	r.unmount()
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("mount whole", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false))
	t.Log("the rebuilt disk alone holds every file")
}

// missingState: after a degraded mount without the disk, the kernel has no
// block device for the member and calls it missing, the state every kernel
// reaches after a degraded boot. The disk returns; udev's scan gives the
// member its path back. Then wipe, LinuxIO's Replace and a convert of the
// chunks written while degraded.
func (r *rebuildRig) missingState() {
	t := r.t
	vdc, vdd := r.disks["vdc"], r.disks["vdd"]
	di, err := r.devInfo(2)
	r.must("dev info", err)
	devUUID := formatUUID(di.UUID[:])
	r.unmount()
	was := vdd.dev
	// As a boot without the disk: the kernel never saw it.
	r.must("forget "+was, controlIoctl(iocForgetDev, was))
	vdd.pull()
	r.must("degraded mount", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, true))
	r.write("degraded", 4, 32<<20)
	r.rewriteNocow("nocow.bin", 32<<20, 16<<20)
	r.observe("missing: mounted degraded without " + was + ", written")
	r.kernelLog("degraded mount")
	returned := vdd.plug()
	t.Logf("missing: %s came back as %s (devt %s)", was, returned, devt(returned))
	r.kernelLog("missing: plugged back, udev settled")
	r.observe("missing: after the return and udev's scan")
	err = RegisterDevice(returned)
	t.Logf("missing: RegisterDevice(%s): %v", returned, err)
	r.kernelLog("missing: after RegisterDevice")
	r.observe("missing: after RegisterDevice")
	r.checkReturned(returned, 2, devUUID)
	wiped, err := WipePoolSuperblocks(returned, r.uuid)
	t.Logf("missing: WipePoolSuperblocks(%s): wiped %v, %v", returned, wiped, err)
	if !wiped || (err != nil && !errors.Is(err, unix.EBUSY)) {
		t.Fatalf("missing: wipe of %s: wiped %v, %v", returned, wiped, err)
	}
	if id, idErr := readIdentity(returned); idErr != nil || len(id.Copies) != 0 {
		t.Fatalf("missing: after the wipe %s still has superblocks: %+v %v", returned, id, idErr)
	}
	r.observe("missing: after the wipe")
	start := time.Now()
	err = Replace(r.ctx, r.uuid, 2, returned, func(float64) {})
	t.Logf("missing: Replace(devid 2 -> %s) took %v: %v", returned, time.Since(start).Round(time.Millisecond), err)
	r.kernelLog("missing: rebuild")
	r.must("missing: rebuild onto the returned disk", err)
	_, err = ReplaceResult(r.ctx, r.uuid, 2, returned)
	r.must("missing: replace result", err)
	r.observe("missing: after the replace, before the convert")
	p := "raid1"
	r.must("convert the degraded chunks", Balance(r.ctx, r.uuid, BalanceOptions{DataProfile: &p, MetadataProfile: &p, Soft: true}, func(float64) {}))
	r.checkWhole("missing: after rebuild and convert", []int{1, 2}, raid1)
	r.unmount()
	r.must("register "+vdc.dev, RegisterDevice(vdc.dev))
	r.must("register "+returned, RegisterDevice(returned))
	r.must("mount whole", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("missing: after an unmount and mount", []int{1, 2}, raid1)
	r.unmount()
}

// threeMembers: three members, metadata raid1c3, the third pulled; the
// rebuild onto the returned disk, its old superblocks left on it, reads
// the source's other mirrors.
func (r *rebuildRig) threeMembers() {
	t := r.t
	vdc, vdd, vde := r.disks["vdc"], r.disks["vdd"], r.disks["vde"]
	for _, d := range []*pciDisk{vdc, vdd} {
		r.must("wipe "+d.dev, zeroSignatures(d.dev))
	}
	r.createPool("raid1c3", vdc, vdd, vde)
	want := map[string]string{"data": "raid1", "metadata": "raid1c3", "system": "raid1c3"}
	r.write("before", 4, 32<<20)
	r.writeNocow("nocow.bin", 64<<20)
	devUUID, returned := r.dropout(vde, 3, "three")
	r.checkReturned(returned, 3, devUUID)
	// Not wiped: the kernel never reads the target's own superblock.
	start := time.Now()
	err := r.replaceRaw(3, returned, replaceReadAvoid)
	t.Logf("three: replace(devid 3 -> %s, not wiped, source avoided) took %v: %v", returned, time.Since(start).Round(time.Millisecond), err)
	r.kernelLog("three: rebuild")
	r.must("three: rebuild", err)
	// Since 7.1 what was written while the member was out went to single
	// chunks, which the resync converts.
	r.convertTo(want)
	r.checkWhole("three: after the rebuild", []int{1, 2, 3}, want)
	r.unmount()
	for _, d := range []*pciDisk{vdc, vdd, vde} {
		r.must("register "+d.dev, RegisterDevice(d.dev))
	}
	r.must("mount whole", MountPool(r.ctx, vdc.dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("three: after an unmount and mount", []int{1, 2, 3}, want)
	r.unmount()
}

func (r *rebuildRig) saveState() {
	r.t.Helper()
	s := proofState{UUID: r.uuid, Files: map[string]string{}, Member: 2}
	for name, sum := range r.files {
		s.Files[name] = hex.EncodeToString(sum[:])
	}
	for name, buf := range r.nocow {
		sum := sha256.Sum256(buf)
		s.Files[name] = hex.EncodeToString(sum[:])
	}
	b, err := json.Marshal(s)
	r.must("encode the state", err)
	f, err := os.Create(proofStatePath)
	r.must("save the state", err)
	_, err = f.Write(b)
	r.must("save the state", err)
	r.must("sync the state", f.Sync())
	r.must("close the state", f.Close())
	d, err := os.Open(filepath.Dir(proofStatePath))
	r.must("open the state folder", err)
	r.must("sync the state folder", d.Sync())
	d.Close()
}

// loadState reads what the phase before the reboot kept, and registers the
// VM's disks that carry the pool.
func (r *rebuildRig) loadState() {
	r.t.Helper()
	b, err := os.ReadFile(proofStatePath)
	if errors.Is(err, os.ErrNotExist) {
		r.t.Skip("no state from a phase before the reboot")
	}
	r.must("read the state", err)
	var s proofState
	r.must("decode the state", json.Unmarshal(b, &s))
	r.uuid = s.UUID
	for name, h := range s.Files {
		raw, _ := hex.DecodeString(h)
		r.files[name] = [32]byte(raw)
	}
	r.t.Cleanup(func() { _ = os.Remove(proofStatePath) })
	for _, d := range r.disks {
		id, err := readIdentity(d.dev)
		r.t.Logf("after the reboot %s: %+v %v", d.dev, id, err)
		if err == nil && id.FSID == r.uuid {
			err = RegisterDevice(d.dev)
			r.t.Logf("RegisterDevice(%s): %v", d.dev, err)
		}
	}
	r.kernelLog("after the reboot, registered")
}

// keepForReboot: rebuild 1, then leave the pool on the disks, unmounted,
// for verify after a reboot.
func (r *rebuildRig) keepForReboot() {
	vdc, vdd := r.disks["vdc"], r.disks["vdd"]
	r.createPool("raid1", vdc, vdd)
	r.write("before", 4, 32<<20)
	r.writeNocow("nocow.bin", 64<<20)
	devUUID, returned := r.dropout(vdd, 2, "keep")
	r.checkReturned(returned, 2, devUUID)
	_, err := WipePoolSuperblocks(returned, r.uuid)
	r.must("wipe", err)
	r.must("rebuild", r.replaceRaw(2, returned, replaceReadAvoid))
	r.checkWhole("keep: after the rebuild", []int{1, 2}, raid1)
	r.unmount()
	r.saveState()
	r.keep = true
}

// verifyAfterReboot: the rebuilt pool mounts whole after a reboot.
func (r *rebuildRig) verifyAfterReboot() {
	r.loadState()
	r.must("mount", MountPool(r.ctx, r.disks["vdc"].dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("after the reboot", []int{1, 2}, raid1)
	r.unmount()
}

// cutMidReplace: rebuild 1 cut by a power loss (sysrq b, an immediate reset
// with nothing written back) while the replace copies.
func (r *rebuildRig) cutMidReplace() {
	t := r.t
	vdc, vdd := r.disks["vdc"], r.disks["vdd"]
	r.createPool("raid1", vdc, vdd)
	r.write("before", 4, 32<<20)
	r.writeNocow("nocow.bin", 64<<20)
	devUUID, returned := r.dropout(vdd, 2, "cut")
	r.checkReturned(returned, 2, devUUID)
	_, err := WipePoolSuperblocks(returned, r.uuid)
	r.must("wipe", err)
	r.must("throttle the replace", sysWrite(filepath.Join(sysFSBtrfs, r.uuid, "devinfo", "2", "scrub_speed_max"), strconv.Itoa(16<<20)))
	done := make(chan error, 1)
	go func() { done <- r.replaceRaw(2, returned, replaceReadAvoid) }()
	r.waitCopied(2, 128<<20, done)
	r.saveState()
	r.keep = true
	_, pct, _, _ := replaceStatusOf(r.mnt)
	t.Logf("cut: power loss at %.1f%% of the replace onto %s", pct, returned)
	r.kernelLog("cut: just before the power loss")
	_ = sysWrite("/proc/sysrq-trigger", "b")
	time.Sleep(time.Minute)
	t.Fatal("the VM did not reset")
}

// resumeAfterCut: the next boot after the power loss mid-replace.
func (r *rebuildRig) resumeAfterCut() {
	t := r.t
	r.loadState()
	pools, err := FindOfflinePools(r.ctx, []string{r.disks["vdc"].dev, r.disks["vdd"].dev, r.disks["vde"].dev})
	t.Logf("offline pools after the cut: %+v %v", pools, err)
	err = MountPool(r.ctx, r.disks["vdc"].dev, r.mnt, []string{"noatime"}, false)
	t.Logf("resume: plain mount: %v", err)
	r.kernelLog("resume: plain mount")
	if err == nil {
		r.observe("resume: mounted without degraded")
	} else {
		r.must("degraded mount", MountPool(r.ctx, r.disks["vdc"].dev, r.mnt, []string{"noatime"}, true))
		r.kernelLog("resume: degraded mount")
		r.observe("resume: mounted degraded")
	}
	err = ResumeReplace(r.ctx, r.uuid, 2, time.Unix(1, 0), func(float64) {})
	t.Logf("resume: ResumeReplace: %v", err)
	r.kernelLog("resume: replace followed")
	r.observe("resume: after the resumed replace")
	r.must("resumed replace", err)
	r.convertIfMixed()
	r.checkWhole("resume: after the resumed replace", []int{1, 2}, raid1)
	r.unmount()
	for _, d := range r.disks {
		if id, err := readIdentity(d.dev); err == nil && id.FSID == r.uuid {
			r.must("register "+d.dev, RegisterDevice(d.dev))
		}
	}
	r.must("mount whole", MountPool(r.ctx, r.disks["vdc"].dev, r.mnt, []string{"noatime"}, false))
	r.checkWhole("resume: after an unmount and mount", []int{1, 2}, raid1)
	r.unmount()
}
