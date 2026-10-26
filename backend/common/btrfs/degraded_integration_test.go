package btrfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

// systemdEscapePath names the unit of a path the way systemd-escape --path
// does.
func systemdEscapePath(p string) string {
	var b strings.Builder
	for i, c := range []byte(strings.Trim(p, "/")) {
		switch {
		case c == '/':
			b.WriteByte('-')
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == ':', c == '_', c == '.' && i > 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

// itMirror is the two-disk raid1 pool the degraded test loses disks of.
type itMirror struct {
	t       *testing.T
	ctx     context.Context
	uuid    string
	target  string
	options []string
	a, b    *loopDisk
	// Digests of the files written before and while a was gone.
	before, during [32]byte
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
// restores redundancy, so that disk alone then holds every file. Under a
// running systemd it also checks that a pending fstab-style mount job does
// not undo the degraded mount. Run it with `make test-btrfs-integration`.
func TestBtrfsDegradedIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	dir := t.TempDir()
	stubStatePaths(t, dir)
	m := &itMirror{t: t, ctx: context.Background(), target: filepath.Join(dir, "mnt"), options: []string{"defaults", "noatime", "nofail"}}
	t.Cleanup(func() { _ = unix.Unmount(m.target, 0) })
	m.a = newLoopDisk(t, filepath.Join(dir, "a.img"), 2<<30)
	m.b = newLoopDisk(t, filepath.Join(dir, "b.img"), 2<<30)
	uuid, err := CreatePool(m.ctx, "linuxio-degraded", "raid1", "raid1", []string{m.a.dev, m.b.dev}, true, nil)
	m.must("create pool", err)
	m.uuid = uuid
	writeFstab(t, fmt.Sprintf("UUID=%s %s btrfs %s 0 0\n", uuid, m.target, strings.Join(m.options, ",")))
	m.must("mount", MountPool(m.ctx, m.a.dev, m.target, m.options, false))
	m.before = writeRandom(t, filepath.Join(m.target, "before.bin"), 8<<20)
	m.unmount()

	m.bootMountsTheSurvivor()
	m.returnedDiskIsNotUsed()
	m.staleDiskAloneIsRefused()
	m.resyncRepairsTheReturnedDisk()
	m.returnedDiskHoldsEverything()
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
	// More than the raid1 data chunk has left, so new chunks are allocated
	// while only one disk can take them.
	m.during = writeRandom(t, filepath.Join(m.target, "during.bin"), 400<<20)
	fs := m.filesystem()
	t.Logf("allocation while degraded: %+v", fs.Allocation)
	if !slices.ContainsFunc(fs.Allocation, func(x BtrfsAllocation) bool { return x.Kind == "data" && x.Profile == "single" }) {
		t.Fatal("no single data chunk was allocated on one disk; the convert step would have nothing to do")
	}
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
// is systemd's to mount; then the scrub rewrites a's stale copies and the
// convert gives the single chunks their second copy.
func (m *itMirror) resyncRepairsTheReturnedDisk() {
	t := m.t
	m.b.plug()
	m.must("boot check on a complete pool", MountDegradedPools(m.ctx, 0))
	if m.mounted() {
		t.Fatal("the boot check mounted a complete pool")
	}
	pools, err := FindOfflinePools(m.ctx, []string{m.a.dev, m.b.dev})
	m.must("offline pools", err)
	if len(pools) != 1 || len(pools[0].Devices) != 2 {
		t.Fatalf("offline pools = %+v", pools)
	}
	m.must("mount complete", MountOfflinePool(m.ctx, pools[0], m.target, m.options, false))
	if m.filesystem().Degraded || !m.state().Degraded {
		t.Fatalf("complete pool: state %+v", m.state())
	}

	m.must("resync", Resync(m.ctx, m.uuid))
	if scrub, ok := loadScrubRecord(m.uuid); !ok || scrub.CorrectedErrors == 0 || scrub.UncorrectableErrors != 0 {
		t.Fatalf("resync scrub = %+v; want stale copies corrected", scrub)
	}
	fs := m.filesystem()
	for _, x := range fs.Allocation {
		if x.Profile != "raid1" {
			t.Fatalf("after resync %s is %s, want raid1: %+v", x.Kind, x.Profile, fs.Allocation)
		}
	}
	if m.state().Degraded {
		t.Fatalf("state after resync = %+v", m.state())
	}
	if !m.hasAlert("resynced-"+m.uuid, "whole again") || m.hasAlert("resync-"+m.uuid, "") {
		t.Fatal("the alerts do not say the pool is whole again")
	}
	m.unmount()
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
	if digest(t, filepath.Join(m.target, "before.bin")) != m.before || digest(t, filepath.Join(m.target, "during.bin")) != m.during {
		t.Fatal("a alone does not hold the files written before and while it was gone")
	}
	status, err := Scrub(m.ctx, m.uuid, func(BtrfsScrubStatus) {})
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
	unit := systemdEscapePath(where) + ".mount"
	device := systemdEscapePath("/dev/disk/by-uuid/"+uuid) + ".device"
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
