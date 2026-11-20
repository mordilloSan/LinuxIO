package btrfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// What the automatic rebuild must never erase or overwrite, and the checks
// that could not be made, which are retried rather than taken as refusals.

// sbDisk is the identity of member 2's disk, sdb, serial SB.
var sbDisk = BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}

// toSecondSighting drops member 2 out, brings its disk back as sdc and runs
// the two health runs that start the rebuild onto it.
func (h *rebuildHost) toSecondSighting() string {
	h.t.Helper()
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	if a := h.health(); !rebuilds(a) {
		h.t.Fatal("the second sighting did not start the rebuild")
	}
	return back
}

// onLock runs fn as a disk lock is taken, right before the exclusive open.
func (h *rebuildHost) onLock(fn func(device string)) {
	lock := lockTargetDisk
	lockTargetDisk = func(ctx context.Context, device string) (func(), uint64, error) {
		fn(device)
		return lock(ctx, device)
	}
}

// setUdev replaces the udev database entry of disk name.
func (h *rebuildHost) setUdev(name, entry string) {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.sys, name, "dev"))
	if err != nil {
		h.t.Fatal(err)
	}
	h.write(filepath.Join(h.udev, "b"+strings.TrimSpace(string(b))), entry)
}

// content is what an open of device reads.
func (h *rebuildHost) content(device string) []byte {
	h.t.Helper()
	h.mu.Lock()
	name := h.files[device]
	h.mu.Unlock()
	b, err := os.ReadFile(name)
	if err != nil {
		h.t.Fatal(err)
	}
	return b
}

// eraseMagic zeroes the magic of device's superblock, and only it, as
// LinuxIO's erase and the kernel's cancel do: the scan no longer reads it.
func (h *rebuildHost) eraseMagic(device string) {
	h.t.Helper()
	h.mu.Lock()
	name := h.files[device]
	delete(h.supers, device)
	h.mu.Unlock()
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt(make([]byte, 8), superOffset+superMagicAt); err != nil {
		h.t.Fatal(err)
	}
}

// withLVM makes the disk an open of device reaches a 2 MiB disk carrying an
// LVM2 label in its second sector, beside its superblock.
func (h *rebuildHost) withLVM(device string) {
	h.t.Helper()
	h.mu.Lock()
	name := h.files[device]
	h.mu.Unlock()
	if err := os.Truncate(name, 2<<20); err != nil {
		h.t.Fatal(err)
	}
	writeAt(h.t, name, 512, lvm2Label(1, strings.Repeat("p", 32)))
}

// setRecord puts rec on record for the pool.
func (h *rebuildHost) setRecord(rec rebuildRecord) {
	h.t.Helper()
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
		st := s[dataUUID]
		st.Rebuild = &rec
		s[dataUUID] = st
	}); err != nil {
		h.t.Fatal(err)
	}
}

// nothingWritten fails the test when the kernel was asked to replace, scan
// or forget anything, or when any of devices lost its magic.
func (h *rebuildHost) nothingWritten(devices ...string) {
	h.t.Helper()
	if calls := h.k.Calls(); slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, "REPLACE_START") }) || len(*h.control) != 0 {
		h.t.Fatalf("calls %v control %v, want nothing written", calls, *h.control)
	}
	for _, d := range devices {
		if h.erased(d) {
			h.t.Fatalf("%s was erased", d)
		}
	}
}

// mountOffline mounts pool as the pool details do, degraded, and returns the
// mounts made.
func (h *rebuildHost) mountOffline(pool BtrfsOfflinePool) ([]string, error) {
	h.t.Helper()
	stubMemberSignatures(h.t, nil)
	var mounted []string
	old := mountFS
	mountFS = func(source, _, _ string, _ uintptr, data string) error {
		mounted = append(mounted, source+" "+data)
		return nil
	}
	h.t.Cleanup(func() { mountFS = old })
	err := MountOfflinePool(context.Background(), pool, h.t.TempDir(), nil, true)
	return mounted, err
}

// targetSuper is the superblock of the pool's replace target at gen.
func targetSuper(gen uint64) Superblock {
	sb := memberSuper(0, gen)
	sb.DevUUID = devUUID(9)
	return sb
}

// super3 is the superblock of devid id of a three-disk pool at gen.
func super3(id, gen uint64) Superblock {
	sb := memberSuper(id, gen)
	sb.NumDevices = 3
	if id == replaceTargetDevID {
		sb.DevUUID = devUUID(9)
	}
	return sb
}

// M1, M2: LinuxIO's mount erases the replace target of an unverified
// rebuild unless it is newer than every device that stays, and a disk
// carrying the member's devid only when every other member is present and
// none is older; any other such disk is left as it is and the pool mounts
// as without a record. It erases only the disk the plan was made on: what
// the scan read must still be on the disk the lock and the exclusive open
// reach. It refuses only what it cannot tell, saying how to get out of it.
func TestMountErasesOnlyWhatThePoolCanLose(t *testing.T) { //nolint:gocognit,maintidx // a table of mounts, each checked the same way
	pair := func(gens ...uint64) BtrfsOfflinePool {
		return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sda", "/dev/sdb"}, devids: []uint64{1, 0}, generations: gens}
	}
	for _, c := range []struct {
		name    string
		setup   func(h *rebuildHost) BtrfsOfflinePool
		want    string // in the error; "" mounts
		mounted string
		erased  []string
		kept    []string // byte for byte
	}{
		{"the member's disk alone", func(h *rebuildHost) BtrfsOfflinePool {
			h.pull("sda")
			h.setSuper("/dev/sdb", memberSuper(2, 100))
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sdb"}, devids: []uint64{2}, generations: []uint64{100}}
		}, "", "/dev/sdb degraded", nil, []string{"/dev/sdb"}},
		{"the member's disk newer than the other member", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sda", memberSuper(1, 90))
			h.setSuper("/dev/sdb", memberSuper(2, 100))
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sda", "/dev/sdb"}, devids: []uint64{1, 2}, generations: []uint64{90, 100}}
		}, "", "/dev/sda degraded", nil, []string{"/dev/sda", "/dev/sdb"}},
		{"the member's disk with another member missing", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sda", super3(1, 100))
			h.setSuper("/dev/sdb", super3(2, 100))
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 3, Devices: []string{"/dev/sda", "/dev/sdb"}, devids: []uint64{1, 2}, generations: []uint64{100, 100}}
		}, "", "/dev/sda degraded", nil, []string{"/dev/sda", "/dev/sdb"}},
		{"the member's disk with every other member current", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", memberSuper(2, 100))
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sda", "/dev/sdb"}, devids: []uint64{1, 2}, generations: []uint64{100, 100}}
		}, "", "/dev/sda degraded", []string{"/dev/sdb"}, []string{"/dev/sda"}},
		{"the replace target", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			return pair(100, 99)
		}, "", "/dev/sda degraded", []string{"/dev/sdb"}, []string{"/dev/sda"}},
		{"the replace target beside a stale disk", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sda", super3(1, 100))
			h.setSuper("/dev/sdb", super3(0, 99))
			h.plug("sdc", "SC", super3(3, 90))
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 3, Devices: []string{"/dev/sda", "/dev/sdb", "/dev/sdc"}, devids: []uint64{1, 0, 3}, generations: []uint64{100, 99, 90}}
		}, "", "/dev/sda degraded", []string{"/dev/sdb"}, []string{"/dev/sda", "/dev/sdc"}},
		{"the replace target newer than every disk left", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sda", memberSuper(1, 90))
			h.setSuper("/dev/sdb", targetSuper(99))
			return pair(90, 99)
		}, "", "/dev/sda degraded", nil, []string{"/dev/sda", "/dev/sdb"}},
		{"the replace target alone", func(h *rebuildHost) BtrfsOfflinePool {
			h.pull("sda")
			h.setSuper("/dev/sdb", targetSuper(99))
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sdb"}, devids: []uint64{0}, generations: []uint64{99}}
		}, "/dev/sdb, the replace target of the unverified rebuild of device 2 onto /dev/sdb, is the only device of the pool present. Reconnect the pool's other disks", "", nil, []string{"/dev/sdb"}},
		{"another disk at the open", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			// By the lock, the name reaches member 1's disk.
			h.onLock(func(string) {
				h.mu.Lock()
				h.files["/dev/sdb"] = h.files["/dev/sda"]
				h.mu.Unlock()
			})
			return pair(100, 99)
		}, "/dev/sdb names another disk than the one its lock holds", "", nil, []string{"/dev/sda"}},
		{"another identity at the open", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			h.onLock(func(string) { h.setUdev("sdb", "E:ID_SERIAL=SX\n") })
			return pair(100, 99)
		}, "/dev/sdb is no longer the disk of the rebuild", "", nil, []string{"/dev/sdb"}},
		{"another superblock at the open", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			h.onLock(func(string) { h.setSuper("/dev/sdb", memberSuper(1, 100)) })
			return pair(100, 99)
		}, "/dev/sdb no longer carries the superblock LinuxIO read from it", "", nil, nil},
		{"an LVM2 signature", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			h.withLVM("/dev/sdb")
			return pair(100, 99)
		}, "carries an LVM2 signature", "", nil, []string{"/dev/sdb"}},
		{"a target udev has not reported", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			b, _ := os.ReadFile(filepath.Join(h.sys, "sdb", "dev"))
			if err := os.Remove(filepath.Join(h.udev, "b"+strings.TrimSpace(string(b)))); err != nil {
				h.t.Fatal(err)
			}
			return pair(100, 99)
		}, "udev reports no identity for /dev/sdb, a replace target of the pool, so LinuxIO cannot tell whether it is the disk of the unverified rebuild of device 2 onto /dev/sdb, which it erases first: mounted with it, the kernel would continue that replace past what it never copied. Disconnect /dev/sdb", "", nil, []string{"/dev/sdb"}},
		{"a target udev reports without a serial", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			h.setUdev("sdb", "E:ID_MODEL=enclosure\n")
			return pair(100, 99)
		}, "udev reports no identity for /dev/sdb, a replace target of the pool", "", nil, []string{"/dev/sdb"}},
		{"a member's disk udev has not reported, which it would erase", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", memberSuper(2, 100))
			h.setUdev("sdb", "")
			return BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sda", "/dev/sdb"}, devids: []uint64{1, 2}, generations: []uint64{100, 100}}
		}, "udev reports no identity for /dev/sdb, which carries device 2's superblock, so LinuxIO cannot tell whether it is the disk of the unverified rebuild of device 2 onto /dev/sdb, which it would erase first. Wait until udev reports /dev/sdb, or disconnect it", "", nil, []string{"/dev/sdb"}},
		{"two disks with its identity", func(h *rebuildHost) BtrfsOfflinePool {
			h.setSuper("/dev/sdb", targetSuper(99))
			h.plug("sdd", "SB", Superblock{})
			return pair(100, 99)
		}, "2 connected disks have the WWN, serial number and size of a disk of the unverified rebuild of device 2 onto /dev/sdb, so LinuxIO cannot tell which one to erase first. Disconnect the disks with that serial number that are not the pool's", "", nil, []string{"/dev/sdb"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdb", Started: 1, Boot: "boot-0", Failed: "x"})
			pool := c.setup(h)
			before := map[string][]byte{}
			for _, d := range c.kept {
				before[d] = h.content(d)
			}
			*h.control = nil
			mounted, err := h.mountOffline(pool)
			for _, d := range c.kept {
				if !bytes.Equal(h.content(d), before[d]) {
					t.Errorf("%s was written", d)
				}
			}
			for _, d := range c.erased {
				if !h.erased(d) {
					t.Errorf("%s was not erased", d)
				}
			}
			if c.want == "" {
				if err != nil || !slices.Equal(mounted, []string{c.mounted}) {
					t.Fatalf("mount = %v, mounts %v; want %q", err, mounted, c.mounted)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) || len(mounted) != 0 {
				t.Fatalf("mount = %v, mounts %v; want it refused saying %q", err, mounted, c.want)
			}
			for _, d := range pool.Devices {
				if h.erased(d) {
					t.Errorf("%s was erased", d)
				}
			}
			if slices.ContainsFunc(*h.control, func(c string) bool { return strings.HasPrefix(c, "forget") }) {
				t.Fatalf("control = %v, want nothing forgotten", *h.control)
			}
		})
	}
}

// bootCheck runs the boot check over the disks plugged into h, for the pool
// fstab mounts, and returns the mounts it made.
func (h *rebuildHost) bootCheck() ([]string, error) {
	h.t.Helper()
	stubMounts(h.t, nil)
	stubMemberSignatures(h.t, nil)
	stubPeekSignatures(h.t, nil)
	mnt := filepath.Join(h.t.TempDir(), "data")
	h.write(FstabPath, "UUID="+dataUUID+" "+mnt+" btrfs defaults,nofail 0 0\n")
	var mounted []string
	oldMount := mountFS
	mountFS = func(source, _, _ string, _ uintptr, data string) error {
		mounted = append(mounted, source+" "+data)
		return nil
	}
	h.t.Cleanup(func() { mountFS = oldMount })
	err := MountDegradedPools(context.Background(), 0)
	return mounted, err
}

// M1, 3: the boot check judges the pool before it erases anything, by the
// same plan: a member's disk with another member missing is kept and the
// pool mounts degraded; a pool the ordinary rules refuse, such as one
// whose late disk took the writes of its last degraded run, keeps the
// replace target as it is; a target alone is refused with a way out.
func TestBootJudgesBeforeItErases(t *testing.T) { //nolint:gocognit // a table of boots, each checked the same way
	for _, c := range []struct {
		name    string
		setup   func(h *rebuildHost) string // returns the rebuild's disk
		mounted string                      // "" leaves the pool unmounted
		alert   string
	}{
		{"the member's disk with another member missing", func(h *rebuildHost) string {
			h.plug("sda", "SA", super3(1, 100))
			h.plug("sdc", "SB", super3(2, 100))
			return "/dev/sdc"
		}, "/dev/sda degraded", ""},
		{"a late disk that took the last writes", func(h *rebuildHost) string {
			h.plug("sda", "SA", super3(1, 100))
			h.plug("sdc", "SB", super3(0, 99))
			if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
				st := s[dataUUID]
				st.Degraded, st.Present = true, []uint64{1, 2}
				s[dataUUID] = st
			}); err != nil {
				h.t.Fatal(err)
			}
			return "/dev/sdc"
		}, "", "the present disks missed writes"},
		{"the replace target alone", func(h *rebuildHost) string {
			h.plug("sdc", "SB", targetSuper(99))
			return "/dev/sdc"
		}, "", "is the only device of the pool present. Reconnect the pool's other disks, then mount it."},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.pull("sda")
			h.pull("sdb")
			disk := c.setup(h)
			h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: disk, Started: 1, Boot: "boot-0", Failed: "x"})
			before := h.content(disk)
			mounted, err := h.bootCheck()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(h.content(disk), before) {
				t.Fatalf("%s was written", disk)
			}
			if c.mounted != "" {
				if !slices.Equal(mounted, []string{c.mounted}) {
					t.Fatalf("mounts %v, want %q", mounted, c.mounted)
				}
				return
			}
			if len(mounted) != 0 {
				t.Fatalf("mounts %v, want the pool left unmounted", mounted)
			}
			if a, ok := h.alert("offline-" + dataUUID); !ok || !strings.Contains(a.Message, c.alert) {
				t.Fatalf("offline alert = %+v, %v; want %q", a, ok, c.alert)
			}
		})
	}
}

// The spare that replaces the disk a failed rebuild left untrusted carries
// that disk in its record, and LinuxIO's mount erases both: the spare, the
// replace target, and the untrusted disk in the member's slot.
func TestMountErasesTheUntrustedDiskTheSpareReplaces(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.plug("sdd", "SD", targetSuper(100))
	h.setSuper("/dev/sdb", memberSuper(2, 100))
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SD", SizeBytes: rebuildDiskBytes}, Device: "/dev/sdd", Spare: true, Started: 1, Boot: "boot-0", Untrusted: sbDisk})
	h.replace.Store(replaceStateSuspended)
	pool := BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sda", "/dev/sdb", "/dev/sdd"}, devids: []uint64{1, 2, 0}, generations: []uint64{100, 100, 100}}
	mounted, err := h.mountOffline(pool)
	if err != nil || !slices.Equal(mounted, []string{"/dev/sda degraded"}) {
		t.Fatalf("mount = %v, mounts %v; want a degraded mount of the trusted disk", err, mounted)
	}
	if !h.erased("/dev/sdb") || !h.erased("/dev/sdd") || h.erased("/dev/sda") {
		t.Fatalf("erased sda %v sdb %v sdd %v, want the untrusted disk and the spare erased", h.erased("/dev/sda"), h.erased("/dev/sdb"), h.erased("/dev/sdd"))
	}
}

// M2: a rebuild to start again onto its disk takes the disk only as the cut
// left it, blank as LinuxIO's erase or the kernel's cancel leave it (the
// pool's superblock with its magic zeroed) or a replace target of the pool;
// one put to another use since, whatever is left of the superblock, fails
// the rebuild with nothing written. With the switch off it is not started
// again at all.
func TestRebuildRestartTakesOnlyTheDiskAsTheCutLeftIt(t *testing.T) { //nolint:gocognit // a table of restarts, each checked the same way
	for _, c := range []struct {
		name  string
		setup func(h *rebuildHost, back string)
		want  string // "" restarts
	}{
		{"erased", func(*rebuildHost, string) {}, ""},
		{"a filesystem made since", func(h *rebuildHost, _ string) {
			h.setUdev("sdc", "E:ID_SERIAL=SB\nE:ID_FS_TYPE=xfs\nE:ID_FS_UUID=0f0f0f0f-0000-4000-8000-000000000000\n")
		}, "is no longer blank: it carries a xfs filesystem"},
		{"wiped", func(h *rebuildHost, back string) {
			h.setSuper(back, Superblock{})
		}, "is no longer blank: it holds something other than the pool's erased superblock"},
		{"another pool's erased superblock", func(h *rebuildHost, back string) {
			other := memberSuper(1, 5)
			other.UUID = "11111111-2222-4333-8444-555555555555"
			h.setSuper(back, other)
			h.eraseMagic(back)
		}, "is no longer blank"},
		{"the switch off", func(h *rebuildHost, _ string) {
			if err := SetAutoRebuild(context.Background(), dataUUID, false); err != nil {
				h.t.Fatal(err)
			}
		}, "the automatic rebuild is off for the pool, so LinuxIO does not start the rebuild of device 2 onto /dev/sdc again; replace device 2"},
		{"smaller than the member", func(h *rebuildHost, _ string) { h.k.devices[2].total = 2 * rebuildDiskBytes }, "/dev/sdc is smaller than device 2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.dropOut()
			back := h.returnAs("sdc")
			h.eraseMagic(back)
			h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: back, Started: time.Now().Unix(), Boot: "boot-0", Restart: true})
			c.setup(h, back)
			before := h.content(back)
			// The switch off fails the rebuild at the health run itself.
			if a := h.health(); !rebuilds(a) && c.name != "the switch off" {
				t.Fatal("the rebuild to start again was not started")
			}
			err := Rebuild(context.Background(), dataUUID, func(float64) {})
			if c.want == "" {
				if err != nil || !slices.Equal(h.k.Calls(), []string{"REPLACE_START 2 " + back}) {
					t.Fatalf("Rebuild = %v, calls %v; want it started again onto %s", err, h.k.Calls(), back)
				}
				return
			}
			if !scheduledrun.AllReported(err) {
				t.Fatalf("Rebuild = %v, want it failed saying %q", err, c.want)
			}
			if rec := h.state().Rebuild; rec == nil || !strings.Contains(rec.Failed, c.want) {
				t.Fatalf("record = %+v, want it failed", rec)
			}
			if !bytes.Equal(h.content(back), before) {
				t.Fatalf("%s was written", back)
			}
			h.nothingWritten()
			if a := h.health(); rebuilds(a) {
				t.Fatal("the failed rebuild is started again")
			}
		})
	}
}

// chunkItem is a chunk item of the chunk tree at logical offset, with
// block-group flags and a stripe on each of devids.
func chunkItem(offset, flags uint64, devids ...uint64) treeItem {
	data := make([]byte, chunkStripes+len(devids)*chunkStripeSize)
	binary.LittleEndian.PutUint64(data[chunkType:], flags)
	binary.LittleEndian.PutUint16(data[chunkNumStripes:], uint16(len(devids)))
	for i, id := range devids {
		binary.LittleEndian.PutUint64(data[chunkStripes+i*chunkStripeSize:], id)
	}
	return treeItem{tree: chunkTreeObjectID, objectid: firstChunkTreeObjectID, typ: chunkItemKey, offset: offset, data: data}
}

// The chunk item is read at the offsets of the packed struct btrfs_chunk
// and struct btrfs_stripe (btrfs_tree.h), which structs of the same fields
// lay out alike.
func TestChunkItemLayout(t *testing.T) {
	type stripe struct {
		DevID, Offset uint64
		DevUUID       [16]byte
	}
	type chunk struct {
		Length, Owner, StripeLen, Type uint64
		IOAlign, IOWidth, SectorSize   uint32
		NumStripes, SubStripes         uint16
		Stripe                         stripe
	}
	var c chunk
	if unsafe.Offsetof(c.Type) != chunkType || unsafe.Offsetof(c.NumStripes) != chunkNumStripes || unsafe.Offsetof(c.Stripe) != chunkStripes || unsafe.Sizeof(c.Stripe) != chunkStripeSize {
		t.Fatalf("chunk offsets type %d stripes %d first stripe %d stripe size %d", unsafe.Offsetof(c.Type), unsafe.Offsetof(c.NumStripes), unsafe.Offsetof(c.Stripe), unsafe.Sizeof(c.Stripe))
	}
	if chunkTreeObjectID != 3 || firstChunkTreeObjectID != 256 || chunkItemKey != 228 {
		t.Fatal("chunk tree keys differ from btrfs_tree.h")
	}
}

// M3: a pool whose profile is raid1 may still hold chunks that keep one
// copy. One with a stripe on the missing member keeps its only copy on that
// disk, so the member is not rebuilt; those a degraded mount allocated on
// the disks that stayed are no reason to refuse.
func TestRebuildRefusesAMemberHoldingTheOnlyCopy(t *testing.T) { //nolint:gocognit // a table of chunk layouts, each checked the same way
	for _, c := range []struct {
		name   string
		chunks []treeItem
		want   string // "" rebuilds
	}{
		{"single data on the member", []treeItem{chunkItem(1<<30, bgData|bgRaid1, 1, 2), chunkItem(2<<30, bgData, 2)}, "Device 2 holds data single chunks, which keep one copy"},
		{"dup metadata on the member", []treeItem{chunkItem(1<<30, bgMetadata|bgDup, 2, 2)}, "Device 2 holds metadata dup chunks"},
		{"raid0 across both", []treeItem{chunkItem(1<<30, bgData|bgRaid0, 1, 2)}, "Device 2 holds data raid0 chunks"},
		{"single chunks on the disk that stayed", []treeItem{chunkItem(1<<30, bgData|bgRaid1, 1, 2), chunkItem(2<<30, bgData, 1), chunkItem(3<<30, bgMetadata, 1)}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.k.trees.items = c.chunks
			h.health()
			h.dropOut()
			back := h.returnAs("sdc")
			h.health()
			started := rebuilds(h.health())
			if c.want == "" {
				if !started {
					t.Fatal("the rebuild was not started")
				}
				if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
					t.Fatalf("Rebuild: %v", err)
				}
				return
			}
			if started {
				t.Fatal("the rebuild was started")
			}
			if a, ok := h.alert("rebuild-" + dataUUID); !ok || !strings.Contains(a.Message, c.want) || !strings.Contains(a.Message, "Reconnect that disk") {
				t.Fatalf("alert = %+v, %v; want %q", a, ok, c.want)
			}
			if r := h.filesystem().Rebuild; r == nil || !strings.Contains(r.Advice, "Reconnect that disk") {
				t.Fatalf("rebuild = %+v, want the refusal with its advice for the page", r)
			}
			if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) {
				t.Fatalf("Rebuild = %v, want a reported refusal", err)
			}
			h.nothingWritten(back)
		})
	}
}

// S1: whether a replace is the rebuild's own is told by the kernel's start
// time against the one it reported when the record was written, never by
// the clock: a replace the rebuild started after a backward step of the
// clock is still verified, and the kernel's last replace from before the
// record is not taken for the rebuild's.
func TestRebuildTellsItsReplaceWithoutTheClock(t *testing.T) {
	// The clock stepped back between the record and the start: the dead
	// target is caught, not dropped as a rebuild that never ran.
	h := newRebuildHost(t)
	back := h.toSecondSighting()
	now := time.Now()
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: back, Started: now.Unix(), Boot: "boot-1", Prior: 1000})
	h.k.replaceStarted = uint64(now.Add(-time.Hour).Unix())
	h.install(2, back)
	h.replace.Store(replaceStateFinished)
	h.k.readOnly = true
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the finished replace was not settled")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Rebuild = %v, want the replace checked and failed", err)
	}
	if rec := h.state().Rebuild; rec == nil || !strings.Contains(rec.Failed, "read-only") {
		t.Fatalf("record = %+v, want it failed", rec)
	}

	// The kernel's last replace is the one from before the record: the pool
	// took the disk back before the rebuild erased it.
	// Its start time reads later than the record's, as after a backward step
	// of the clock between that replace and the record.
	h = newRebuildHost(t)
	h.health()
	later := time.Now().Add(time.Hour).Unix()
	h.k.replaceStarted = uint64(later)
	h.replace.Store(replaceStateFinished)
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdc", Started: time.Now().Unix(), Boot: "boot-1", Prior: later})
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the record was not settled")
	}
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if !errors.Is(err, ErrNothingToRebuild) || !strings.Contains(err.Error(), "took device 2 back") {
		t.Fatalf("Rebuild = %v, want the record dropped as never erased", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Rebuilt) != 0 {
		t.Fatalf("state = %+v, want no record and nothing noted rebuilt", s)
	}
}

// S3, S13: a check that could not be made is retried, not taken as a
// refusal: the identity of a disk udev cannot report for a moment, and a
// rebuild to start again whose disk is not connected, which the health run
// does not start the unit for.
func TestRebuildRetriesWhatItCouldNotCheck(t *testing.T) {
	h := newRebuildHost(t)
	back := h.toSecondSighting()
	b, _ := os.ReadFile(filepath.Join(h.sys, "sdc", "dev"))
	entry := filepath.Join(h.udev, "b"+strings.TrimSpace(string(b)))
	h.onLock(func(string) { _ = os.Remove(entry) })
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if err == nil || scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "the identity of "+back+" could not be read") {
		t.Fatalf("Rebuild = %v, want an unreported error", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Refused) != 0 {
		t.Fatalf("state = %+v, want no record and no refusal", s)
	}
	h.nothingWritten(back)

	// A rebuild to start again whose disk is gone waits for it.
	h = newRebuildHost(t)
	h.health()
	h.dropOut()
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdc", Started: time.Now().Unix(), Boot: "boot-0", Restart: true})
	if a := h.health(); rebuilds(a) {
		t.Fatal("the unit was started for a disk that is not connected")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !errors.Is(err, ErrNothingToRebuild) {
		t.Fatalf("Rebuild = %v, want nothing to do", err)
	}
	if rec := h.state().Rebuild; rec == nil || rec.Failed != "" {
		t.Fatalf("record = %+v, want it kept to start again", rec)
	}
	// The runner, sent to start it again as the disk went, waits too.
	fs := h.filesystem()
	m := fs.Devices[slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == 2 })]
	if err := resumeRebuild(context.Background(), fs, *h.state().Rebuild, m, func(float64) {}); !errors.Is(err, ErrNothingToRebuild) {
		t.Fatalf("resumeRebuild = %v, want nothing to do", err)
	}
	if rec := h.state().Rebuild; rec == nil || rec.Failed != "" {
		t.Fatalf("record = %+v, want it kept to start again", rec)
	}
}

// S4: a stop right as the replace finishes does not cut the check: the
// record is cleared, so the next boot does not take the good disk for an
// unverified one.
func TestRebuildFinishesItsCheckAfterAStop(t *testing.T) {
	h := newRebuildHost(t)
	back := h.toSecondSighting()
	rec := rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: back, Started: time.Now().Unix(), Boot: "boot-1"}
	h.setRecord(rec)
	h.install(2, back)
	h.replace.Store(replaceStateFinished)
	// findmnt, which the check runs, ends with the context.
	mounts := listMounts
	listMounts = func(ctx context.Context) ([]Mount, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return mounts(ctx)
	}
	t.Cleanup(func() { listMounts = mounts })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := finishRebuild(ctx, h.filesystem(), rec); err != nil {
		t.Fatalf("finishRebuild after a stop: %v", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Rebuilt) != 1 {
		t.Fatalf("state = %+v, want the rebuild verified and cleared", s)
	}
}

// S5: a rebuild whose record a person's replace cleared while it ran does
// not write its failure back over it, and the hot spare replaces a member
// for a failed rebuild only while the slot holds the disk that rebuild
// wrote.
func TestRebuildFailureKeepsToItsOwnRecord(t *testing.T) {
	h := newRebuildHost(t)
	h.toSecondSighting()
	h.onReplace = func(string) error {
		if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
			st := s[dataUUID]
			st.Rebuild = nil
			s[dataUUID] = st
		}); err != nil {
			t.Error(err)
		}
		return unix.EIO
	}
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if err == nil || scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "its record was cleared meanwhile") {
		t.Fatalf("Rebuild = %v, want the failure returned alone", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Refused) != 0 {
		t.Fatalf("state = %+v, want the cleared record left cleared", s)
	}
	if a, ok := h.alert("rebuild-" + dataUUID); ok && a.Severity == alerts.SeverityError {
		t.Fatalf("alert = %+v, want no failure raised", a)
	}

	// The slot holds another disk than the failed rebuild wrote.
	h = newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 0); err != nil {
		t.Fatal(err)
	}
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SX", SizeBytes: rebuildDiskBytes}, Device: "/dev/sdx", Started: 1, Boot: "boot-0", Failed: "x"})
	if a := h.health(); rebuilds(a) {
		t.Fatal("the spare replaces a member the failed rebuild did not write")
	}
}

// S6: a hardware identity that does not tell disks apart, as behind USB
// bridges that report one serial number for every disk, is no identity: a
// returned disk recognised by it is refused.
func TestRebuildRefusesAnIdentityDisksShare(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(h *rebuildHost)
		want  string
	}{
		{"another member's", func(h *rebuildHost) { h.setUdev("sda", "E:ID_SERIAL=SB\n") }, "Device 2's disk has the WWN, serial number and size of device 1's"},
		{"another connected disk's", func(h *rebuildHost) { h.plug("sdd", "SB", Superblock{}) }, "2 connected disks have the WWN, serial number and size of device 2's disk"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			if c.name == "another member's" {
				c.setup(h)
			}
			h.health()
			h.dropOut()
			back := h.returnAs("sdc")
			if c.name != "another member's" {
				c.setup(h)
			}
			for range 3 {
				if a := h.health(); rebuilds(a) {
					t.Fatal("the rebuild was started")
				}
			}
			if a, ok := h.alert("rebuild-" + dataUUID); !ok || !strings.Contains(a.Message, c.want) {
				t.Fatalf("alert = %+v, %v; want %q", a, ok, c.want)
			}
			h.nothingWritten(back)
		})
	}
}

// S7: a refusal is one occurrence while its reason stands, whatever the
// pool's generation does; a pool with nothing to rebuild onto raises
// nothing, read-only or not; profiles that cannot be read are waited for;
// and a health run that read the state before the rebuild unit wrote its
// record does not resolve the unit's alert.
func TestRebuildAlertsOnlyWhatWaitsOnAPerson(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.plug("sdc", "", memberSuper(2, 500))
	h.health()
	first, _ := h.alert("rebuild-" + dataUUID)
	h.k.generation = 120
	h.health()
	if again, _ := h.alert("rebuild-" + dataUUID); first.OccurrenceID == "" || again.OccurrenceID != first.OccurrenceID {
		t.Fatalf("occurrences %q then %q, want one", first.OccurrenceID, again.OccurrenceID)
	}

	// Read-only, the switch off and no spare: nothing to rebuild onto.
	h = newRebuildHost(t)
	h.health()
	if err := SetAutoRebuild(context.Background(), dataUUID, false); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	h.returnAs("sdc")
	h.k.readOnly = true
	h.health()
	if a, ok := h.alert("rebuild-" + dataUUID); ok {
		t.Fatalf("alert %+v with nothing to rebuild onto", a)
	}

	// Profiles that cannot be read.
	h = newRebuildHost(t)
	h.health()
	h.dropOut()
	h.returnAs("sdc")
	spaces := h.k.spaces
	h.k.spaces = nil
	for range 3 {
		if a := h.health(); rebuilds(a) {
			t.Fatal("rebuild started without the pool's profiles")
		}
	}
	if a, ok := h.alert("rebuild-" + dataUUID); ok || h.state().RebuildRefusal != "" {
		t.Fatalf("alert %+v, refusal %q; want a wait", a, h.state().RebuildRefusal)
	}
	h.k.spaces = spaces
	h.health()
	if a := h.health(); !rebuilds(a) {
		t.Fatal("no rebuild once the profiles are read")
	}

	// The unit raised its alert after the health run read the state.
	h = newRebuildHost(t)
	h.health()
	stale, err := loadPoolStates()
	if err != nil {
		t.Fatal(err)
	}
	rec := rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdc", Started: time.Now().Unix(), Boot: "boot-1"}
	h.setRecord(rec)
	fs := h.filesystem()
	raise(context.Background(), rebuildRunningObservation(fs, rec))
	checkRebuilds(context.Background(), []BtrfsFilesystem{fs}, stale)
	if _, ok := h.alert("rebuild-" + dataUUID); !ok {
		t.Fatal("the health run resolved the running rebuild's alert")
	}
}

// S8, S9: a sysfs read that fails, and a name that reaches another disk at
// the lock than at the open, or for the hot spare another disk than the one
// it was chosen as, are errors the next run retries; nothing is written.
func TestRebuildChecksTheDiskItHolds(t *testing.T) {
	h := newRebuildHost(t)
	back := h.toSecondSighting()
	holders := filepath.Join(h.sys, "sdc", "holders")
	if err := os.Remove(holders); err != nil {
		t.Fatal(err)
	}
	h.write(holders, "")
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err == nil || scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "read the holders of "+back) {
		t.Fatalf("Rebuild = %v, want an unreported error", err)
	}
	h.nothingWritten(back)

	h = newRebuildHost(t)
	back = h.toSecondSighting()
	h.onLock(func(string) {
		h.mu.Lock()
		h.files[back] = h.files["/dev/sda"]
		h.mu.Unlock()
	})
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err == nil || scheduledrun.AllReported(err) || !strings.Contains(err.Error(), back+" names another disk than the one its lock holds") {
		t.Fatalf("Rebuild = %v, want an unreported error", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Refused) != 0 {
		t.Fatalf("state = %+v, want no record and no refusal", s)
	}
	h.nothingWritten("/dev/sda", back)

	// The spare's lock holds another disk.
	h = newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 0); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	if a := h.health(); !rebuilds(a) {
		t.Fatal("spare not used")
	}
	lock := lockTargetDisk
	lockTargetDisk = func(ctx context.Context, device string) (func(), uint64, error) {
		unlock, _, err := lock(ctx, device)
		other, _ := h.number("sda")
		return unlock, other, err
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err == nil || scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "/dev/sdd no longer names the disk its lock holds") {
		t.Fatalf("Rebuild = %v, want an unreported error", err)
	}
	h.nothingWritten()
}

// S10: while a member that stayed counts read, corruption or generation
// errors, the disk that came back may hold the only good copy of a block:
// it is not erased, and the alert says to have the pool take it back. The
// rule is evaluated at every run: once the counters are reset, the rebuild
// starts.
func TestRebuildKeepsTheReturnedDiskWhileTheOthersCountErrors(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.k.setCounters(1, BtrfsDeviceErrors{Corruption: 3})
	h.dropOut()
	back := h.returnAs("sdc")
	for range 3 {
		if a := h.health(); rebuilds(a) {
			t.Fatal("the rebuild was started")
		}
	}
	a, ok := h.alert("rebuild-" + dataUUID)
	if !ok || !strings.Contains(a.Message, "Device 1 counts 0 read, 3 corruption and 0 generation errors") || !strings.Contains(a.Message, "so btrfs takes the disk back") || !strings.Contains(a.Message, "btrfs device stats --reset /data") {
		t.Fatalf("alert = %+v, %v", a, ok)
	}
	if r := h.filesystem().Rebuild; r == nil || r.State != RebuildRefused || !strings.Contains(r.Advice, "so btrfs takes the disk back") {
		t.Fatalf("rebuild = %+v, want the refusal with its advice for the page", r)
	}
	// A count that changes is the same refusal.
	h.k.setCounters(1, BtrfsDeviceErrors{Corruption: 5})
	h.health()
	if again, _ := h.alert("rebuild-" + dataUUID); again.OccurrenceID != a.OccurrenceID {
		t.Fatalf("occurrences %q then %q, want one", a.OccurrenceID, again.OccurrenceID)
	}
	if s := h.state(); len(s.Refused) != 0 {
		t.Fatalf("refused = %+v, want no disk refused for the episode", s.Refused)
	}
	h.nothingWritten(back)
	h.k.setCounters(1, BtrfsDeviceErrors{})
	h.health()
	if a := h.health(); !rebuilds(a) {
		t.Fatal("no rebuild once the counters are reset")
	}
}

// S11: the erase of the returned disk is a host mutation of its own, said
// at Info.
func TestRebuildSaysItErasedTheReturnedDisk(t *testing.T) {
	h := newRebuildHost(t)
	h.toSecondSighting()
	logs := captureLogs(t)
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if lines := logLines(logs, "erased the returned disk of a missing btrfs device to rebuild onto it"); len(lines) != 1 || !strings.Contains(lines[0], "level=INFO") {
		t.Fatalf("erase lines = %q, want one at Info", lines)
	}
}

// S13: a replace the rebuild did not start runs on without the health run
// starting the unit at every run.
func TestRebuildIsNotStartedForAReplaceItLeaves(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.k.replaceStarted = 1_700_000_000
	h.replace.Store(replaceStateStarted)
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdc", Started: time.Now().Unix(), Boot: "boot-1", Prior: 1_700_000_000})
	for range 2 {
		if a := h.health(); rebuilds(a) {
			t.Fatal("the unit was started for a replace it leaves to run")
		}
	}
}

// S14: a check of the new disk that could not be made is an error, not a
// failed rebuild: the record stays in flight and the next run checks again.
func TestRebuildRetriesACheckOfTheNewDisk(t *testing.T) {
	h := newRebuildHost(t)
	h.toSecondSighting()
	notBusy := deviceNotBusy
	// Refused permission, as an SELinux denial would: the dropout check
	// reads that as not gone, and the hold of the new disk cannot be told.
	deviceNotBusy = func(device string) error { return fmt.Errorf("open %s: %w", device, unix.EACCES) }
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	deviceNotBusy = notBusy
	if err == nil || scheduledrun.AllReported(err) || errors.Is(err, ErrReplaceUnverified) {
		t.Fatalf("Rebuild = %v, want an unreported error", err)
	}
	if rec := h.state().Rebuild; rec == nil || rec.Failed != "" {
		t.Fatalf("record = %+v, want it kept in flight", rec)
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the record in flight was not settled")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("Rebuild once the disk can be checked: %v", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Rebuilt) != 1 {
		t.Fatalf("state = %+v, want the rebuild verified", s)
	}
}

// Two sightings are of one attachment of the disk in one boot: a reboot, or
// the disk pulled and plugged in again under its name, starts the count
// over.
func TestRebuildSightingsAreOfOneAttachment(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.returnAs("sdc")
	h.health()
	h.boot = "boot-2"
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild started on sightings either side of a reboot")
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatal("no rebuild at the second sighting in the boot")
	}

	h = newRebuildHost(t)
	h.health()
	h.dropOut()
	h.returnAs("sdc")
	h.health()
	h.pull("sdc")
	h.returnAs("sdc")
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild started on sightings either side of a replug")
	}
}

// The missing tests of rule 1 and rule 6: a disk smaller than the member, a
// member recorded without an identity, an md or LVM2 signature or a newer
// generation found only through the exclusive open, and a replace that
// installed another disk than the one the rebuild wrote.
func TestRebuildRefusesWhatOnlyItsChecksFind(t *testing.T) { //nolint:gocognit // a table of refusals, each checked the same way
	for _, c := range []struct {
		name   string
		before func(h *rebuildHost) // before the health runs
		atOpen func(h *rebuildHost, back string)
		want   string
	}{
		{"smaller than the member", func(h *rebuildHost) { h.k.devices[2].total = 2 * rebuildDiskBytes }, nil, "/dev/sdc is smaller than device 2"},
		{"no identity recorded", func(h *rebuildHost) {
			if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
				st := s[dataUUID]
				st.Identities[1].Disk = BtrfsSpareDisk{}
				s[dataUUID] = st
			}); err != nil {
				h.t.Fatal(err)
			}
		}, nil, "Device 2's disk had no WWN or serial number"},
		{"an LVM2 signature", nil, func(h *rebuildHost, back string) { h.withLVM(back) }, "/dev/sdc carries an LVM2 signature"},
		{"a newer generation at the open", nil, func(h *rebuildHost, back string) {
			h.setSuper(back, memberSuper(2, 101))
			h.mu.Lock()
			h.supers[back] = memberSuper(2, 95)
			h.mu.Unlock()
		}, "/dev/sdc carries a newer generation (101) than the pool"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.dropOut()
			if c.before != nil {
				c.before(h)
			}
			back := h.returnAs("sdc")
			h.health()
			started := rebuilds(h.health())
			if c.atOpen == nil {
				if started {
					t.Fatal("the rebuild was started")
				}
				if a, ok := h.alert("rebuild-" + dataUUID); !ok || !strings.Contains(a.Message, c.want) {
					t.Fatalf("alert = %+v, %v; want %q", a, ok, c.want)
				}
				h.nothingWritten(back)
				return
			}
			c.atOpen(h, back)
			if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Rebuild = %v, want it refused saying %q", err, c.want)
			}
			if h.state().Rebuild != nil {
				t.Fatal("a record was written")
			}
			h.nothingWritten(back)
		})
	}

	// Rule 6: the disk in the slot after the replace is not the one written.
	h := newRebuildHost(t)
	h.toSecondSighting()
	h.onReplace = func(target string) error {
		h.install(2, target)
		h.replace.Store(replaceStateFinished)
		h.setUdev(filepath.Base(target), "E:ID_SERIAL=SX\n")
		return nil
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "which is not the disk the rebuild wrote") {
		t.Fatalf("Rebuild = %v, want the replace failed", err)
	}
	if rec := h.state().Rebuild; rec == nil || rec.Failed == "" {
		t.Fatalf("record = %+v, want it failed", rec)
	}
}

// 5: a rebuild cut whose disk stays away waits for it, and says so, not that
// it copies; after restartWaitsFor it fails, saying that the disk coming
// back does not resume it, and the hot spare takes the member.
func TestRebuildCutWaitsForItsDiskThenLetsTheSpareIn(t *testing.T) {
	h := newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 0); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdc", Started: time.Now().Unix(), Boot: "boot-1", Restart: true})
	if a := h.health(); rebuilds(a) {
		t.Fatal("started while its disk is away")
	}
	if r := h.filesystem().Rebuild; r == nil || r.State != RebuildWaiting || !strings.Contains(r.Reason, "starts again once /dev/sdc, its disk, is connected") {
		t.Fatalf("rebuild = %+v, want it waiting for its disk", r)
	}
	if a, ok := h.alert("rebuild-" + dataUUID); !ok || !strings.Contains(a.Title, "waits") || !strings.Contains(a.Message, "Nothing is copied meanwhile") {
		t.Fatalf("alert = %+v, %v; want the wait", a, ok)
	}
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
		st := s[dataUUID]
		st.Rebuild.Started = time.Now().Add(-restartWaitsFor - time.Minute).Unix()
		s[dataUUID] = st
	}); err != nil {
		t.Fatal(err)
	}
	h.health()
	rec := h.state().Rebuild
	if rec == nil || !strings.Contains(rec.Failed, "/dev/sdc, the disk LinuxIO was rebuilding device 2 onto, has not been connected since the rebuild was cut; connecting it again does not resume the rebuild: replace device 2") {
		t.Fatalf("record = %+v, want it failed", rec)
	}
	if a, ok := h.alert("rebuild-" + dataUUID); !ok || a.Severity != alerts.SeverityError {
		t.Fatalf("alert = %+v, %v; want the failure", a, ok)
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the hot spare was not let in")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("Rebuild onto the spare: %v", err)
	}
	if calls := h.k.Calls(); !slices.Equal(calls, []string{"REPLACE_START 2 /dev/sdd"}) {
		t.Fatalf("calls = %v, want the spare taking the member", calls)
	}
}

// 6: a rebuild onto the hot spare to start again, once the person removed
// the spare, fails, rather than being started at every health run.
func TestRebuildSpareRestartFailsWithoutTheSpare(t *testing.T) {
	h := newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 0); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SD", SizeBytes: rebuildDiskBytes}, Device: "/dev/sdd", Spare: true, Started: time.Now().Unix(), Boot: "boot-1", Restart: true})
	if _, err := ClearSpare(context.Background(), dataUUID); err != nil {
		t.Fatal(err)
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the rebuild to start again was not started")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) {
		t.Fatalf("Rebuild = %v, want a reported failure", err)
	}
	if rec := h.state().Rebuild; rec == nil || !strings.Contains(rec.Failed, "the pool has no hot spare any more") {
		t.Fatalf("record = %+v, want it failed", rec)
	}
	if a := h.health(); rebuilds(a) {
		t.Fatal("the failed rebuild is started again")
	}
	h.nothingWritten()
}

// 7: a rebuild to start again obeys the pool's checks. One whose disk still
// carries the member's own superblock, never erased, is set aside, not
// failed, when the checks refuse that disk or the switch is off: btrfs takes
// the disk back at a mount, and LinuxIO's next mount does not erase it. Any
// waits while the pool runs another operation.
func TestRebuildRestartObeysThePool(t *testing.T) {
	for _, c := range []struct {
		name   string
		erased bool
		setup  func(h *rebuildHost)
		kept   bool   // the record stays
		want   string // the refusal, or what it waits for
	}{
		{"errors on a member that stayed", false, func(h *rebuildHost) { h.k.setCounters(1, BtrfsDeviceErrors{Corruption: 3}) }, false, "device 1 counts 0 read, 3 corruption"},
		{"the only copy on the member", false, func(h *rebuildHost) { h.k.trees.items = []treeItem{chunkItem(1<<30, bgData, 2)} }, false, "device 2 holds data single chunks"},
		{"the switch off", false, func(h *rebuildHost) {
			if err := SetAutoRebuild(context.Background(), dataUUID, false); err != nil {
				h.t.Fatal(err)
			}
		}, false, ""},
		{"a balance, the disk as it was", false, func(h *rebuildHost) {
			h.k.balanceProgress = func() (balanceArgs, error) { return balanceArgs{State: balanceStateRunning}, nil }
		}, true, "a balance runs on the pool"},
		{"a balance, the disk erased", true, func(h *rebuildHost) {
			h.k.balanceProgress = func() (balanceArgs, error) { return balanceArgs{State: balanceStateRunning}, nil }
		}, true, "a balance runs on the pool"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.dropOut()
			back := h.returnAs("sdc")
			if c.erased {
				h.eraseMagic(back)
			}
			h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: back, Started: time.Now().Unix(), Boot: "boot-1", Restart: true})
			c.setup(h)
			before := h.content(back)
			if a := h.health(); rebuilds(a) {
				t.Fatal("the rebuild was started again")
			}
			s := h.state()
			switch {
			case c.kept && (s.Rebuild == nil || s.Rebuild.Failed != "" || !strings.Contains(s.RebuildWait, c.want)):
				t.Fatalf("state = %+v, want the record kept, waiting for %q", s, c.want)
			case !c.kept && (s.Rebuild != nil || len(s.Refused) != 0 || !strings.Contains(s.RebuildRefusal, c.want)):
				t.Fatalf("state = %+v, want the record set aside, refused for %q", s, c.want)
			}
			if !bytes.Equal(h.content(back), before) {
				t.Fatalf("%s was written", back)
			}
			h.nothingWritten()
		})
	}
}

// 8: a rebuild onto the hot spare to start again takes the spare only as
// SpareOf finds it empty: one udev reports a filesystem on fails the
// rebuild. One with a foreign superblock udev has not reported, no md, LVM2
// or btrfs signature, is taken (docs/btrfs.md, Hot spare, Known limits).
func TestRebuildSpareRestartOntoAForeignDisk(t *testing.T) { //nolint:gocognit // the same restart onto two kinds of foreign disk
	for _, foreign := range []string{"udev", "unreported"} {
		t.Run(foreign, func(t *testing.T) {
			h := newRebuildHost(t)
			h.plug("sdd", "SD", Superblock{})
			h.health()
			if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 0); err != nil {
				t.Fatal(err)
			}
			h.dropOut()
			h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SD", SizeBytes: rebuildDiskBytes}, Device: "/dev/sdd", Spare: true, Started: time.Now().Unix(), Boot: "boot-1", Restart: true})
			if foreign == "udev" {
				h.setUdev("sdd", "E:ID_SERIAL=SD\nE:ID_FS_TYPE=xfs\n")
			} else {
				h.mu.Lock()
				name := h.files["/dev/sdd"]
				h.mu.Unlock()
				writeAt(t, name, 0, []byte("XFSB"))
			}
			if a := h.health(); !rebuilds(a) {
				t.Fatal("the rebuild to start again was not started")
			}
			err := Rebuild(context.Background(), dataUUID, func(float64) {})
			if foreign == "unreported" {
				if err != nil || !slices.Equal(h.k.Calls(), []string{"REPLACE_START 2 /dev/sdd"}) {
					t.Fatalf("Rebuild = %v, calls %v; want the disk taken", err, h.k.Calls())
				}
				return
			}
			if !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "the hot spare /dev/sdd is not ready: it carries a xfs signature") {
				t.Fatalf("Rebuild = %v, want the rebuild failed", err)
			}
			h.nothingWritten()
		})
	}
}

// S1: a finished replace whose slot holds the recorded disk is checked
// before its record is cleared, also when its start time is the one on
// record from before: a disk that is not working fails the rebuild.
func TestRebuildChecksAFinishedReplaceBeforeItClears(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.k.replaceStarted = 1_700_000_000
	h.replace.Store(replaceStateFinished)
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: "/dev/sdb", Started: time.Now().Unix(), Boot: "boot-1", Prior: 1_700_000_000})
	h.k.readOnly = true
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the record was not settled")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Rebuild = %v, want the disk checked and the rebuild failed", err)
	}
	if rec := h.state().Rebuild; rec == nil || !strings.Contains(rec.Failed, "read-only") {
		t.Fatalf("record = %+v, want it failed", rec)
	}
}

// A rebuild started again writes its record only over its own: one a
// person's replace cleared meanwhile stays cleared, and nothing is written.
func TestRebuildRestartWritesOnlyOverItsOwnRecord(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.eraseMagic(back)
	h.setRecord(rebuildRecord{ID: 2, UUID: devUUID(2), Disk: sbDisk, Device: back, Started: time.Now().Unix(), Boot: "boot-1", Restart: true})
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the rebuild to start again was not started")
	}
	h.onLock(func(string) {
		if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
			st := s[dataUUID]
			st.Rebuild = nil
			s[dataUUID] = st
		}); err != nil {
			t.Error(err)
		}
	})
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !errors.Is(err, ErrNothingToRebuild) {
		t.Fatalf("Rebuild = %v, want nothing done", err)
	}
	if rec := h.state().Rebuild; rec != nil {
		t.Fatalf("record = %+v, want it left cleared", rec)
	}
	h.nothingWritten()
}
