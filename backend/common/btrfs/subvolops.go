package btrfs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// quotaCtlArgs mirrors struct btrfs_ioctl_quota_ctl_args.
type quotaCtlArgs struct {
	Cmd    uint64
	Status uint64
}

// qgroupLimitArgs mirrors struct btrfs_ioctl_qgroup_limit_args.
type qgroupLimitArgs struct {
	QgroupID uint64
	Flags    uint64
	MaxRfer  uint64
	MaxExcl  uint64
	RsvRfer  uint64
	RsvExcl  uint64
}

// sendArgs mirrors struct btrfs_ioctl_send_args.
type sendArgs struct {
	SendFD            int64
	CloneSourcesCount uint64
	CloneSources      uint64 // user pointer to the clone root ids
	ParentRoot        uint64
	Flags             uint64
	Version           uint32
	Reserved          [28]byte
}

const (
	quotaCtlEnable  = 1
	quotaCtlDisable = 2
	// qgroupClear as a limit value removes the limit.
	qgroupClear = ^uint64(0)
)

// subvolOps are the subvolume and quota mutations, each one ioctl or
// xattr; tests replace them to record calls.
var subvolOps = struct {
	create      func(path string) error
	snapshot    func(src, dst string, readOnly bool) error
	destroy     func(path string) error
	setDefault  func(top string, id int) error
	readOnly    func(path string, ro bool) error
	compression func(path, value string) error
	quota       func(top string, enable bool) error
	limit       func(path string, max *uint64) error
}{createSubvolumeAt, snapshotSubvolumeTo, destroySubvolume, setDefaultSubvolumeID, setSubvolumeReadOnly, setCompressionProperty, quotaCtl, qgroupLimit}

// nameArgs fills a vol_args with the last element of path, the name the
// ioctls take relative to the parent directory they are issued on.
func nameArgs(path string) (volArgs, error) {
	var args volArgs
	name := filepath.Base(path)
	if len(name) > 255 {
		return args, fmt.Errorf("subvolume name too long: %s", name)
	}
	copy(args.Name[:], name)
	return args, nil
}

func createSubvolumeAt(path string) error {
	args, err := nameArgs(path)
	if err != nil {
		return err
	}
	return fsIoctl(filepath.Dir(path), iocSubvolCreate, unsafe.Pointer(&args))
}

func destroySubvolume(path string) error {
	args, err := nameArgs(path)
	if err != nil {
		return err
	}
	return fsIoctl(filepath.Dir(path), iocSnapDestroy, unsafe.Pointer(&args))
}

func snapshotSubvolumeTo(src, dst string, readOnly bool) error {
	name := filepath.Base(dst)
	if len(name) > 255 {
		return fmt.Errorf("subvolume name too long: %s", name)
	}
	fd, err := unix.Open(src, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer unix.Close(fd)
	args := volArgsV2{Fd: int64(fd)}
	if readOnly {
		args.Flags = subvolRdonly
	}
	copy(args.Name[:], name)
	return fsIoctl(filepath.Dir(dst), iocSnapCreateV2, unsafe.Pointer(&args))
}

func setDefaultSubvolumeID(top string, id int) error {
	v := uint64(id)
	return fsIoctl(top, iocDefaultSubvol, unsafe.Pointer(&v))
}

// setSubvolumeReadOnly sets or clears the read-only flag of the subvolume
// at path, keeping its other flags.
func setSubvolumeReadOnly(path string, ro bool) error {
	var flags uint64
	if err := fsIoctl(path, iocSubvolGetFlags, unsafe.Pointer(&flags)); err != nil {
		return err
	}
	if ro {
		flags |= subvolRdonly
	} else {
		flags &^= subvolRdonly
	}
	return fsIoctl(path, iocSubvolSetFlags, unsafe.Pointer(&flags))
}

// setCompressionProperty sets the btrfs.compression xattr, which is the
// compression property; an empty value clears it.
func setCompressionProperty(path, value string) error {
	return unix.Setxattr(path, "btrfs.compression", []byte(value), 0)
}

func quotaCtl(top string, enable bool) error {
	args := quotaCtlArgs{Cmd: quotaCtlDisable}
	if enable {
		args.Cmd = quotaCtlEnable
	}
	return fsIoctl(top, iocQuotaCtl, unsafe.Pointer(&args))
}

// qgroupLimit limits the referenced size of the subvolume at path, whose
// own qgroup a zero id selects; nil removes the limit.
func qgroupLimit(path string, max *uint64) error {
	args := qgroupLimitArgs{Flags: qgroupLimitMaxRfer, MaxRfer: qgroupClear}
	if max != nil {
		args.MaxRfer = *max
	}
	return fsIoctl(path, iocQgroupLimit, unsafe.Pointer(&args))
}

// subvolumeID returns the id of the subvolume containing path.
func subvolumeID(path string) (uint64, error) {
	args := &inoLookupArgs{ObjectID: firstFreeObjectID}
	if err := fsIoctl(path, iocInoLookup, unsafe.Pointer(args)); err != nil {
		return 0, fmt.Errorf("subvolume id of %s: %w", path, err)
	}
	return args.TreeID, nil
}

// sendStream writes the send stream of the read-only snapshot at snap to w,
// incremental from the snapshot at parent when it is set; a var so tests can
// fake the kernel.
var sendStream = func(snap, parent string, w *os.File) error {
	args := sendArgs{SendFD: int64(w.Fd())}
	var clones []uint64
	if parent != "" {
		id, err := subvolumeID(parent)
		if err != nil {
			return err
		}
		clones = []uint64{id}
		args.ParentRoot, args.CloneSourcesCount = id, 1
		args.CloneSources = uint64(uintptr(unsafe.Pointer(&clones[0])))
	}
	err := fsIoctl(snap, iocSend, unsafe.Pointer(&args))
	runtime.KeepAlive(clones)
	runtime.KeepAlive(w)
	if err != nil {
		return fmt.Errorf("send %s: %w", snap, err)
	}
	return nil
}
