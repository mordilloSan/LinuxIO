package memory

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

const (
	// ownSwapDir is the folder LinuxIO makes its swap file in, a subvolume of
	// its own on btrfs, and ownSwapFilePath the one path Create makes a swap
	// file at.
	ownSwapDir      = "/var/lib/linuxio/swap"
	ownSwapFilePath = ownSwapDir + "/swapfile"
	// resizeSuffix ends the name of the temporary swap file a resize makes
	// beside the file it resizes: <path>.linuxio-resize.
	resizeSuffix = ".linuxio-resize"
)

// fileID names one file by its device and inode, as stat reports them, so
// that a later step can tell that a name still holds the file that was
// checked.
type fileID struct{ dev, ino uint64 }

func fileIDOf(st *unix.Stat_t) fileID {
	return fileID{dev: st.Dev, ino: st.Ino}
}

// errNoSwapHeader marks a resizeSwapFile failure that may have left the file
// without a swap header: mkswap failed twice, after a grow, or before a
// shrink cut the file, or a grow that failed could not be cut back. Any
// other failure of resizeSwapFile leaves the file at its old length with its
// old header. Whether the old header survived shows when the file is turned
// on again, so the text says only what failed.
var errNoSwapHeader = errors.New("its new swap header could not be written")

// The refusals of the swap file blocks. Each is returned wrapped with
// bridgeipc.ErrConflict, beside errOthersCanRedirect for a folder on the
// path that someone other than root can change.
var (
	errUnsupportedFilesystem = errors.New("LinuxIO makes swap files only on ext4, xfs and btrfs")
	errReadOnlyFilesystem    = errors.New("the filesystem is read-only")
	errBtrfsLayout           = errors.New("btrfs holds a swap file only on a filesystem of one device whose data is single")
	errSwapNameTaken         = errors.New("the name is taken")
	errNotRootFile           = errors.New("it is not a regular file root owns")
	errSwapFileChanged       = errors.New("the name no longer holds the file that was checked")
	errSwapFileActive        = errors.New("the file is swapping")
	errNoCow                 = errors.New("the file lost its no-copy-on-write flag, which btrfs needs of a swap file")
	errNoSpaceForSwap        = errors.New("not enough free space on the filesystem")
)

// swapFileConflict wraps one of the refusals above, with what it is about.
func swapFileConflict(reason error, format string, a ...any) error {
	return fmt.Errorf("%w: %w (%s)", bridgeipc.ErrConflict, reason, fmt.Sprintf(format, a...))
}

// checkSwapFilePath refuses a path the blocks would not act on: one that is
// not absolute and clean, or that names a folder rather than a file in one.
func checkSwapFilePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, 0) {
		return fmt.Errorf("%w: %q is not the absolute path of a swap file", bridgeipc.ErrInvalidArgs, path)
	}
	return nil
}

// checkSwapFileSize refuses a size no file can have.
func checkSwapFileSize(size uint64) error {
	if size == 0 || size > math.MaxInt64 {
		return fmt.Errorf("%w: %d bytes is not a size for a swap file", bridgeipc.ErrInvalidArgs, size)
	}
	return nil
}

// The inode flags btrfs needs of a swap file (linux/fs.h), read and written
// with FS_IOC_GETFLAGS and FS_IOC_SETFLAGS.
const (
	fsComprFl  = 0x00000004 // FS_COMPR_FL: compress the file
	fsNoCompFl = 0x00000400 // FS_NOCOMP_FL: do not compress the file
	fsNoCowFl  = 0x00800000 // FS_NOCOW_FL: write in place, without copy-on-write
)

// inodeFlagsIoctl reads or writes the inode flags of the file open at fd. The
// kernel takes an int, whatever size the request number encodes. A variable
// so that a test on a filesystem without the flags stands in for it.
var inodeFlagsIoctl = func(fd int, req uintptr, flags *uint32) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(flags)))
	if errno != 0 {
		return errno
	}
	return nil
}

// setNoCow sets NOCOW and NOCOMP on the empty file open at fd, without
// COMPR, and reads the flags back: btrfs refuses swap on a file that copies
// on write or is compressed. A new file inherits COMPR from a folder that
// has it, and btrfs refuses a change of NOCOW, with EINVAL, while the old
// flags hold COMPR or NOCOMP, so those two are cleared by a call of their
// own first. btrfs takes NOCOW only on an empty file and otherwise sets
// nothing without an error, which the read back catches.
func setNoCow(fd int) error {
	var flags uint32
	if err := inodeFlagsIoctl(fd, unix.FS_IOC_GETFLAGS, &flags); err != nil {
		return fmt.Errorf("read the file's flags: %w", err)
	}
	if flags&(fsComprFl|fsNoCompFl) != 0 {
		flags &^= fsComprFl | fsNoCompFl
		if err := inodeFlagsIoctl(fd, unix.FS_IOC_SETFLAGS, &flags); err != nil {
			return fmt.Errorf("clear compression on the file: %w", err)
		}
	}
	flags |= fsNoCowFl | fsNoCompFl
	if err := inodeFlagsIoctl(fd, unix.FS_IOC_SETFLAGS, &flags); err != nil {
		return fmt.Errorf("set no copy-on-write on the file: %w", err)
	}
	return checkNoCow(fd)
}

// checkNoCow refuses the file open at fd unless its NOCOW flag is set.
func checkNoCow(fd int) error {
	var flags uint32
	if err := inodeFlagsIoctl(fd, unix.FS_IOC_GETFLAGS, &flags); err != nil {
		return fmt.Errorf("read the file's flags: %w", err)
	}
	if flags&fsNoCowFl == 0 {
		return fmt.Errorf("%w: %w", bridgeipc.ErrConflict, errNoCow)
	}
	return nil
}

// fallocateFile gives the file open at fd blocks for its first size bytes and
// makes that its length when it was shorter. A large allocation can be
// interrupted by a signal, the Go runtime's own among them, and allocating
// what is already allocated changes nothing, so it is asked again. A
// variable so that a test can have the disk run out.
var fallocateFile = func(fd int, size int64) error {
	for {
		err := unix.Fallocate(fd, 0, 0, size)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
