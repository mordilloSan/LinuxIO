package btrfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
// xattr; tests replace them to record calls. A path is given as the
// top-level mount and the path below it, and reached without following a
// symbolic link (see openBelow).
var subvolOps = struct {
	create      func(top, path string) error
	createIn    func(dir *os.File, name string) error
	snapshot    func(top, src, dst string, readOnly bool) error
	destroy     func(top, path string) error
	setDefault  func(top string, id int) error
	readOnly    func(top, path string, ro bool) error
	compression func(top, path, value string) error
	quota       func(top string, enable bool) error
	limit       func(top string, id int, max *uint64) error
}{createSubvolumeAt, createSubvolumeIn, snapshotSubvolumeTo, destroySubvolume, setDefaultSubvolumeID, setSubvolumeReadOnly, setCompressionProperty, quotaCtl, qgroupLimit}

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

// createSubvolumeAt creates an empty subvolume at path below top. The kernel
// applies the bridge's 077 umask to the new root, so it is chmodded 0755 like
// `btrfs subvolume create` run from a root shell, on what isNewSubvolume
// accepts (see createSubvolumeIn); snapshots keep their source's mode.
func createSubvolumeAt(top, path string) error {
	dirFD, err := openBelow(top, filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	return createSubvolumeOn(dirFD, filepath.Base(path))
}

// errSubvolumeReplaced is a new subvolume that was no longer at its name
// when createSubvolumeIn came back to set its mode.
var errSubvolumeReplaced = errors.New("the new subvolume was moved or replaced before its mode was set")

// newSubvolumeSlack is how far before the create a new subvolume's birth
// time may lie: the kernel stamps it from a coarser clock than time.Now.
const newSubvolumeSlack = 2 * time.Second

// createSubvolumeIn creates an empty subvolume called name in the open
// folder dir and chmods it 0755 as createSubvolumeAt does, through the
// folder's handle and the new subvolume's own, never by path. The kernel
// hands back no handle, so the subvolume is reopened by name, and whoever
// can write dir can put another folder at that name first: the mode is set
// only on what isNewSubvolume accepts.
func createSubvolumeIn(dir *os.File, name string) error {
	// dir must outlive its descriptor's use below.
	defer runtime.KeepAlive(dir)
	return createSubvolumeOn(int(dir.Fd()), name)
}

// createSubvolumeOn creates the subvolume name in the folder open at dirFD
// and sets its mode, both through that one handle.
func createSubvolumeOn(dirFD int, name string) error {
	args, err := nameArgs(name)
	if err != nil {
		return err
	}
	notBefore := time.Now().Add(-newSubvolumeSlack)
	if err = ioctlFD(dirFD, iocSubvolCreate, unsafe.Pointer(&args)); err != nil {
		return err
	}
	if err = setNewSubvolumeMode(dirFD, name, notBefore); err != nil {
		return fmt.Errorf("the subvolume was created, but its mode was not set: %w", err)
	}
	return nil
}

// setNewSubvolumeMode chmods the subvolume just created at name in the
// folder open at dirFD to 0755, when isNewSubvolume accepts what is there.
func setNewSubvolumeMode(dirFD int, name string, notBefore time.Time) error {
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer unix.Close(fd)
	var stx unix.Statx_t
	if err = unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_BASIC_STATS|unix.STATX_BTIME, &stx); err != nil {
		return fmt.Errorf("stat %s: %w", name, err)
	}
	if !isNewSubvolume(&stx, uint32(os.Geteuid()), notBefore) {
		return errSubvolumeReplaced
	}
	if err = unix.Fchmod(fd, 0o755); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

// isNewSubvolume reports whether stx is a subvolume this process, running as
// uid, created at notBefore or later: the root folder of a subvolume, owned
// by uid and born since. Nobody else can make such a folder for uid, so one
// swapped in at the new subvolume's name fails this.
//
// A var so a test on a filesystem without subvolumes can stand in for it.
var isNewSubvolume = func(stx *unix.Statx_t, uid uint32, notBefore time.Time) bool {
	if stx.Ino != firstFreeObjectID || stx.Uid != uid || stx.Mask&unix.STATX_BTIME == 0 {
		return false
	}
	return !time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec)).Before(notBefore)
}

// locateFolder returns where the open folder dir is: the uuid of its btrfs
// filesystem, the id of the subvolume holding it and its path inside that
// subvolume, "" for the subvolume itself; a var so tests can fake the kernel.
var locateFolder = func(dir *os.File) (fsUUID string, subvolID uint64, rel string, err error) {
	defer runtime.KeepAlive(dir)
	fd := int(dir.Fd())
	fsid, err := fsidOf(fd)
	if err != nil {
		return "", 0, "", err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return "", 0, "", fmt.Errorf("stat: %w", err)
	}
	// A zero tree id looks the inode up in the subvolume of fd and returns
	// that subvolume's id.
	lookup := &inoLookupArgs{ObjectID: st.Ino}
	if err = ioctlFD(fd, iocInoLookup, unsafe.Pointer(lookup)); err != nil {
		return "", 0, "", fmt.Errorf("resolve inode %d: %w", st.Ino, err)
	}
	return formatUUID(fsid[:]), lookup.TreeID, strings.Trim(cstring(lookup.Name[:]), "/"), nil
}

func destroySubvolume(top, path string) error {
	args, err := nameArgs(path)
	if err != nil {
		return err
	}
	dirFD, err := openBelow(top, filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	return ioctlFD(dirFD, iocSnapDestroy, unsafe.Pointer(&args))
}

func snapshotSubvolumeTo(top, src, dst string, readOnly bool) error {
	name := filepath.Base(dst)
	if len(name) > 255 {
		return fmt.Errorf("subvolume name too long: %s", name)
	}
	fd, err := openBelow(top, src)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	args := volArgsV2{Fd: int64(fd)}
	if readOnly {
		args.Flags = subvolRdonly
	}
	copy(args.Name[:], name)
	dirFD, err := openBelow(top, filepath.Dir(dst))
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	return ioctlFD(dirFD, iocSnapCreateV2, unsafe.Pointer(&args))
}

func setDefaultSubvolumeID(top string, id int) error {
	v := uint64(id)
	return fsIoctl(top, iocDefaultSubvol, unsafe.Pointer(&v))
}

// setSubvolumeReadOnly sets or clears the read-only flag of the subvolume
// at path below top, keeping its other flags.
func setSubvolumeReadOnly(top, path string, ro bool) error {
	fd, err := openBelow(top, path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var flags uint64
	if err = ioctlFD(fd, iocSubvolGetFlags, unsafe.Pointer(&flags)); err != nil {
		return err
	}
	if ro {
		flags |= subvolRdonly
	} else {
		flags &^= subvolRdonly
	}
	return ioctlFD(fd, iocSubvolSetFlags, unsafe.Pointer(&flags))
}

// setCompressionProperty sets the btrfs.compression xattr, which is the
// compression property, on the subvolume at path below top; an empty value
// clears it. A plain folder put at that name since the subvolume was looked
// up is refused, as the kernel refuses one for the read-only flag.
func setCompressionProperty(top, path, value string) error {
	fd, err := openBelow(top, path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if st.Ino != firstFreeObjectID {
		return fmt.Errorf("%s is not a subvolume", path)
	}
	return unix.Fsetxattr(fd, "btrfs.compression", []byte(value), 0)
}

func quotaCtl(top string, enable bool) error {
	args := quotaCtlArgs{Cmd: quotaCtlDisable}
	if enable {
		args.Cmd = quotaCtlEnable
	}
	return fsIoctl(top, iocQuotaCtl, unsafe.Pointer(&args))
}

// qgroupLimit limits the referenced size of subvolume id, whose own qgroup
// has that id, on the filesystem mounted at top; nil removes the limit. It
// goes by id, not by path: a zero id would limit whichever subvolume holds
// the folder found at the path, the parent's if a plain folder took the name.
func qgroupLimit(top string, id int, max *uint64) error {
	args := qgroupLimitArgs{QgroupID: uint64(id), Flags: qgroupLimitMaxRfer, MaxRfer: qgroupClear}
	if max != nil {
		args.MaxRfer = *max
	}
	return fsIoctl(top, iocQgroupLimit, unsafe.Pointer(&args))
}

// subvolumeID returns the id of the subvolume containing path below top.
func subvolumeID(top, path string) (uint64, error) {
	fd, err := openBelow(top, path)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	args := &inoLookupArgs{ObjectID: firstFreeObjectID}
	if err = ioctlFD(fd, iocInoLookup, unsafe.Pointer(args)); err != nil {
		return 0, fmt.Errorf("subvolume id of %s: %w", path, err)
	}
	return args.TreeID, nil
}

// sendStream writes the send stream of the read-only snapshot at snap below
// top to w, incremental from the snapshot at parent when it is set; a var so
// tests can fake the kernel.
var sendStream = func(top, snap, parent string, w *os.File) error {
	args := sendArgs{SendFD: int64(w.Fd())}
	var clones []uint64
	if parent != "" {
		id, err := subvolumeID(top, parent)
		if err != nil {
			return err
		}
		clones = []uint64{id}
		args.ParentRoot, args.CloneSourcesCount = id, 1
		args.CloneSources = uint64(uintptr(unsafe.Pointer(&clones[0])))
	}
	fd, err := openBelow(top, snap)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	err = ioctlFD(fd, iocSend, unsafe.Pointer(&args))
	runtime.KeepAlive(clones)
	runtime.KeepAlive(w)
	if err != nil {
		return fmt.Errorf("send %s: %w", snap, err)
	}
	return nil
}
