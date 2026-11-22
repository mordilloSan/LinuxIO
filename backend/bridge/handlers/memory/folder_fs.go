package memory

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// folderFS is what the filesystem holding a folder reports.
type folderFS struct {
	// free and total are bytes: f_bavail and f_blocks times the block size.
	free, total uint64
	readOnly    bool
	// kind is "ext4", "xfs" or "btrfs" by the f_type magic (ext2, ext3 and
	// ext4 share one and all read "ext4"), and empty for any other.
	kind string
	// btrfsLayout is, on btrfs, whether the filesystem can hold a swap file:
	// one device and every data block group single (btrfs.SwapfileLayout).
	btrfsLayout bool
	// subvolumeRoot is, on btrfs, whether the folder is the root of a
	// subvolume (inode 256).
	subvolumeRoot bool
}

// readFolderFS reads folderFS for the folder at dir, an absolute path on the
// machine the process runs on (a caller with a hostRoot passes h.path(dir)).
// It needs no privilege: statfs, and on btrfs BTRFS_IOC_FS_INFO and
// BTRFS_IOC_SPACE_INFO, answer anyone who can open the folder. A variable
// so that a test replaces it.
var readFolderFS = func(dir string) (folderFS, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return folderFS{}, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	f := os.NewFile(uintptr(fd), dir)
	defer f.Close()
	return statFolderFS(f)
}

// statFolderFS reads folderFS for the folder open at dir.
func statFolderFS(dir *os.File) (folderFS, error) {
	var sfs unix.Statfs_t
	if err := statfsFD(int(dir.Fd()), &sfs); err != nil {
		return folderFS{}, &os.PathError{Op: "statfs", Path: dir.Name(), Err: err}
	}
	bsize := uint64(sfs.Bsize)
	ffs := folderFS{
		free:     sfs.Bavail * bsize,
		total:    sfs.Blocks * bsize,
		readOnly: sfs.Flags&unix.ST_RDONLY != 0,
		kind:     fsKindOfMagic(sfs.Type),
	}
	if ffs.kind != "btrfs" {
		return ffs, nil
	}
	var err error
	if ffs.btrfsLayout, err = swapfileLayout(dir); err != nil {
		return folderFS{}, fmt.Errorf("read the btrfs layout of %s: %w", dir.Name(), err)
	}
	if ffs.subvolumeRoot, err = isSubvolumeRoot(dir); err != nil {
		return folderFS{}, err
	}
	return ffs, nil
}

// fsKindOfMagic names the filesystems LinuxIO makes swap files on by their
// statfs magic, and returns "" for any other.
func fsKindOfMagic(magic int64) string {
	switch magic {
	case unix.EXT4_SUPER_MAGIC:
		return "ext4"
	case unix.XFS_SUPER_MAGIC:
		return "xfs"
	case unix.BTRFS_SUPER_MAGIC:
		return "btrfs"
	}
	return ""
}

// The kernel calls of the swap file blocks that depend on the filesystem,
// variables so that a test on whatever filesystem holds its temporary folder
// can stand in for another.
var (
	statfsFD        = unix.Fstatfs
	swapfileLayout  = btrfs.SwapfileLayout
	createSubvolume = btrfs.CreateSubvolumeAt
	// isSubvolumeRoot reports whether the folder open at dir, on btrfs, is
	// the root of a subvolume.
	isSubvolumeRoot = func(dir *os.File) (bool, error) {
		var st unix.Stat_t
		if err := unix.Fstat(int(dir.Fd()), &st); err != nil {
			return false, &os.PathError{Op: "stat", Path: dir.Name(), Err: err}
		}
		return st.Ino == btrfs.SubvolumeRootInode && st.Mode&unix.S_IFMT == unix.S_IFDIR, nil
	}
)
