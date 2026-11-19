package btrfs

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A device-mapper device over a loop disk, for the root tests that need a
// disk whose writes fail while the pool stays mounted, as a loose cable
// does. It is driven through the device-mapper ioctls on
// /dev/mapper/control, as dmsetup does, since the test VMs install no
// dmsetup. The linear and error targets are built into dm-mod.

// dmIoctl is struct dm_ioctl (include/uapi/linux/dm-ioctl.h), 312 bytes.
type dmIoctl struct {
	Version     [3]uint32
	DataSize    uint32
	DataStart   uint32
	TargetCount uint32
	OpenCount   int32
	Flags       uint32
	EventNr     uint32
	Padding     uint32
	Dev         uint64
	Name        [128]byte
	UUID        [129]byte
	Data        [7]byte
}

// dmTargetSpec is struct dm_target_spec, 40 bytes; the target's parameters
// follow it as a NUL-terminated string.
type dmTargetSpec struct {
	SectorStart uint64
	Length      uint64
	Status      int32
	Next        uint32
	TargetType  [16]byte
}

// The commands of dm-ioctl.h, _IOWR(DM_IOCTL, cmd, struct dm_ioctl).
const (
	dmDevCreate      = 3
	dmDevRemove      = 4
	dmDevSuspend     = 6 // with no DM_SUSPEND_FLAG it resumes, swapping in a loaded table
	dmTableLoad      = 9
	dmSkipLockfsFlag = 1 << 10 // DM_SKIP_LOCKFS_FLAG: do not freeze the filesystem on top
	dmBufferSize     = 16 << 10
)

func dmRequest(cmd uintptr) uintptr {
	return ioRW | unsafe.Sizeof(dmIoctl{})<<16 | 0xfd<<8 | cmd
}

// dmCall issues dm command cmd on device name with flags and, when target
// is set, one target of sectors with params, and returns the header the
// kernel wrote back.
func dmCall(cmd uintptr, name string, flags uint32, target, params string, sectors uint64) (dmIoctl, error) {
	words := make([]uint64, dmBufferSize/8) // 8-byte aligned, as the structs need
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), dmBufferSize)
	hdr := (*dmIoctl)(unsafe.Pointer(&buf[0]))
	hdr.Version = [3]uint32{4, 0, 0}
	hdr.DataSize, hdr.DataStart, hdr.Flags = dmBufferSize, uint32(unsafe.Sizeof(dmIoctl{})), flags
	copy(hdr.Name[:len(hdr.Name)-1], name)
	if target != "" {
		hdr.TargetCount = 1
		spec := (*dmTargetSpec)(unsafe.Pointer(&buf[hdr.DataStart]))
		spec.Length = sectors
		copy(spec.TargetType[:len(spec.TargetType)-1], target)
		paramStart := int(hdr.DataStart) + int(unsafe.Sizeof(dmTargetSpec{}))
		copy(buf[paramStart:], params) // the zeroed buffer ends the string
		spec.Next = uint32(unsafe.Sizeof(dmTargetSpec{})) + uint32(len(params)+8)&^7
	}
	fd, err := unix.Open("/dev/mapper/control", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return dmIoctl{}, err
	}
	defer unix.Close(fd)
	if err := rawIoctl(fd, dmRequest(cmd), unsafe.Pointer(&buf[0])); err != nil {
		return dmIoctl{}, err
	}
	return *hdr, nil
}

// dmDisk is a device-mapper device over a loop disk that can be made to
// fail every read, write and flush, and healed again.
type dmDisk struct {
	t       *testing.T
	name    string
	dev     string // /dev/dm-N
	under   string // the loop disk under it
	sectors uint64
}

// newDMDisk maps a device named name linearly over the loop disk under and
// removes it when the test ends.
func newDMDisk(t *testing.T, name, under string) *dmDisk {
	t.Helper()
	runTool(t, "modprobe", "dm-mod")
	size, err := blockDeviceSize(under)
	if err != nil {
		t.Fatal(err)
	}
	d := &dmDisk{t: t, name: name, under: under, sectors: size >> 9}
	created, err := dmCall(dmDevCreate, name, 0, "", "", 0)
	if err != nil {
		t.Fatalf("create device-mapper device %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := dmCall(dmDevRemove, name, 0, "", "", 0); err != nil {
			t.Logf("remove device-mapper device %s: %v", name, err)
		}
	})
	d.heal()
	d.dev = fmt.Sprintf("/dev/dm-%d", unix.Minor(created.Dev))
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(d.dev); err == nil {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared for device-mapper device %s", d.dev, name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// swap loads a table of one target over the whole disk and swaps it in.
func (d *dmDisk) swap(target, params string) {
	d.t.Helper()
	if _, err := dmCall(dmTableLoad, d.name, 0, target, params, d.sectors); err != nil {
		d.t.Fatalf("load the %s table of %s: %v", target, d.name, err)
	}
	if _, err := dmCall(dmDevSuspend, d.name, dmSkipLockfsFlag, "", "", 0); err != nil {
		d.t.Fatalf("swap in the %s table of %s: %v", target, d.name, err)
	}
}

// fail makes every read, write and flush of the disk fail.
func (d *dmDisk) fail() { d.swap("error", "") }

// heal maps the disk over its loop disk again, with what that holds.
func (d *dmDisk) heal() {
	var st unix.Stat_t
	if err := unix.Stat(d.under, &st); err != nil {
		d.t.Fatal(err)
	}
	d.swap("linear", fmt.Sprintf("%d:%d 0", unix.Major(st.Rdev), unix.Minor(st.Rdev)))
}
