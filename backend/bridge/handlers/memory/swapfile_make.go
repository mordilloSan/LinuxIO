package memory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// makeSwapFile makes a swap file of size bytes at path and returns its
// identity: through the handle of its folder, reached from / through folders
// only root can change (openRootOnlyFolder), it creates the name with
// O_CREAT|O_EXCL|O_NOFOLLOW and mode 0600, on btrfs sets NOCOW on the empty
// file, fallocates it, fsyncs it and runs mkswap on it. When path is
// ownSwapFilePath it first makes ownSwapDir, 0755, as a subvolume on btrfs
// (openOwnSwapDir). It refuses, wrapping bridgeipc.ErrConflict, a filesystem
// other than ext4, xfs and btrfs, a read-only one, a btrfs layout that cannot
// hold a swap file, a folder that is not root-only, a name that is taken and
// a disk without room for the file. On any failure it removes the file it
// made.
func (h hostRoot) makeSwapFile(ctx context.Context, path string, size uint64) (fileID, error) {
	if err := checkSwapFilePath(path); err != nil {
		return fileID{}, err
	}
	if err := checkSwapFileSize(size); err != nil {
		return fileID{}, err
	}
	if err := ctx.Err(); err != nil {
		return fileID{}, err
	}
	var dir *os.File
	var kind string
	var err error
	if path == ownSwapFilePath {
		dir, kind, err = h.openOwnSwapDir()
	} else {
		dir, kind, err = h.openSwapFileFolder(filepath.Dir(path))
	}
	if err != nil {
		return fileID{}, err
	}
	defer dir.Close()
	return h.newSwapFile(ctx, dir, kind, path, size)
}

// openSwapFileFolder opens the root-only folder dir and checks that its
// filesystem can hold a swap file (swapFileFS), whose kind it returns.
func (h hostRoot) openSwapFileFolder(dir string) (*os.File, string, error) {
	f, err := h.openRootOnlyFolder(dir)
	if err != nil {
		return nil, "", err
	}
	kind, err := h.swapFileFS(f, dir)
	if err != nil {
		_ = f.Close()
		return nil, "", err
	}
	return f, kind, nil
}

// swapFileFS returns the kind of the filesystem holding the folder open at
// dir, the host path hostDir, and refuses one LinuxIO does not make swap
// files on: the mount's type in mountinfo must be ext4, xfs or btrfs and
// agree with statfs on the handle (ext2 and ext3 share ext4's magic, and
// mountinfo names a path that could have changed since), the filesystem must
// be writable, and btrfs must have the layout of btrfs.SwapfileLayout.
func (h hostRoot) swapFileFS(dir *os.File, hostDir string) (string, error) {
	ffs, err := statFolderFS(dir)
	if err != nil {
		return "", err
	}
	if ffs.kind == "" {
		return "", swapFileConflict(errUnsupportedFilesystem, "%s", hostDir)
	}
	mounts, err := h.readMounts()
	if err != nil {
		return "", err
	}
	mount, ok := mountHolding(mounts, hostDir)
	if !ok || mount.fsType != ffs.kind {
		return "", swapFileConflict(errUnsupportedFilesystem, "%s is on %s", hostDir, mount.fsType)
	}
	if ffs.readOnly {
		return "", swapFileConflict(errReadOnlyFilesystem, "%s", hostDir)
	}
	if ffs.kind == "btrfs" && !ffs.btrfsLayout {
		return "", swapFileConflict(errBtrfsLayout, "%s", hostDir)
	}
	return ffs.kind, nil
}

// openOwnSwapDir makes or opens ownSwapDir through the handle of the
// root-only folder above it, and returns its handle and its filesystem's
// kind. On btrfs it is a subvolume of its own, so that a snapshot of the
// subvolume holding /var/lib leaves the swap file out: an empty plain folder
// at the name is replaced by one, and a plain folder that holds anything is
// refused as a taken name. Anything there that is not a folder only root can
// change is refused too. The folder is set 0755, so that a session without
// sudo can see the file in it.
func (h hostRoot) openOwnSwapDir() (*os.File, string, error) {
	parentPath, name := filepath.Dir(ownSwapDir), filepath.Base(ownSwapDir)
	parent, kind, err := h.openSwapFileFolder(parentPath)
	if err != nil {
		return nil, "", err
	}
	defer parent.Close()
	dir, err := h.openOwnSwapDirIn(parent, name, kind)
	created := false
	if errors.Is(err, unix.ENOENT) {
		created = true
		if kind == "btrfs" {
			err = createSubvolume(parent, name)
		} else {
			err = unix.Mkdirat(int(parent.Fd()), name, 0o755)
		}
		if err != nil {
			return nil, "", &os.PathError{Op: "create", Path: ownSwapDir, Err: err}
		}
		dir, err = h.openOwnSwapDirIn(parent, name, kind)
	}
	if err != nil {
		return nil, "", err
	}
	if kind == "btrfs" {
		if err = checkOwnSwapSubvolume(parent, dir, created); err != nil {
			_ = dir.Close()
			return nil, "", err
		}
	}
	if err = unix.Fchmod(int(dir.Fd()), 0o755); err != nil {
		_ = dir.Close()
		return nil, "", &os.PathError{Op: "chmod", Path: ownSwapDir, Err: err}
	}
	// Another filesystem mounted on the folder is where the file would go.
	if kind, err = h.swapFileFS(dir, ownSwapDir); err != nil {
		_ = dir.Close()
		return nil, "", err
	}
	return dir, kind, nil
}

// checkOwnSwapSubvolume refuses the folder open at dir, ownSwapDir on btrfs,
// unless it is a subvolume: the kernel made one, or openOwnSwapDirIn found
// one, and what is there now must still be one. A subvolume this call made
// is given the SELinux context of parent: a new subvolume has none of its
// own, and what is made in it reads unlabeled_t. It swaps all the same, so a
// label that cannot be set is not a failure.
func checkOwnSwapSubvolume(parent, dir *os.File, created bool) error {
	root, err := isSubvolumeRoot(dir)
	if err != nil {
		return err
	}
	if !root {
		return swapFileConflict(errSwapNameTaken, "%s is not a subvolume", ownSwapDir)
	}
	if created {
		if err = labelLikeParent(parent, dir); err != nil {
			slog.Warn("label the swap subvolume like its folder failed", "path", ownSwapDir, "error", err)
		}
	}
	return nil
}

// openOwnSwapDirIn opens the folder name in parent, as openOwnSwapDir needs
// it: unix.ENOENT when nothing is there, or when on btrfs an empty plain
// folder was there and has been removed to make room for a subvolume.
func (h hostRoot) openOwnSwapDirIn(parent *os.File, name, kind string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, err
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return nil, swapFileConflict(errSwapNameTaken, "%s is a link or not a folder", ownSwapDir)
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: ownSwapDir, Err: err}
	}
	if err = h.checkRootOnlyFolder(fd, ownSwapDir); err != nil {
		unix.Close(fd)
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), ownSwapDir)
	if kind != "btrfs" {
		return dir, nil
	}
	root, err := isSubvolumeRoot(dir)
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	if root {
		return dir, nil
	}
	names, err := dir.Readdirnames(1)
	_ = dir.Close()
	if len(names) > 0 {
		return nil, swapFileConflict(errSwapNameTaken, "%s is a folder with files in it, not a subvolume", ownSwapDir)
	}
	if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read %s: %w", ownSwapDir, err)
	}
	// Only root can change the folder above, so the name still holds the
	// empty folder just read, and the kernel refuses to remove one that is
	// not empty.
	if err = unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		return nil, &os.PathError{Op: "remove", Path: ownSwapDir, Err: err}
	}
	return nil, unix.ENOENT
}

// newSwapFile makes the swap file name of path in the folder open at dir, on
// a filesystem of kind, as makeSwapFile describes.
func (h hostRoot) newSwapFile(ctx context.Context, dir *os.File, kind, path string, size uint64) (fileID, error) {
	name := filepath.Base(path)
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		return fileID{}, swapFileConflict(errSwapNameTaken, "%s", path)
	}
	if err != nil {
		return fileID{}, &os.PathError{Op: "create", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		err = &os.PathError{Op: "stat", Path: path, Err: err}
		// Without its identity the file cannot be told from one put at its
		// name since, which in a root-only folder nobody could; it is
		// removed by name.
		if rmErr := unix.Unlinkat(int(dir.Fd()), name, 0); rmErr != nil {
			return fileID{}, fmt.Errorf("%w; %s is left, since removing it failed: %w", err, path, rmErr)
		}
		return fileID{}, err
	}
	id := fileIDOf(&st)
	if err = h.fillSwapFile(ctx, fd, kind, path, size); err != nil {
		if rmErr := removeSwapFileAt(dir, name, id); rmErr != nil {
			return fileID{}, fmt.Errorf("%w; %s is left, since removing it failed: %w", err, path, rmErr)
		}
		return fileID{}, err
	}
	return id, nil
}

// fillSwapFile turns the new, empty file open at fd into a swap file of size
// bytes.
func (h hostRoot) fillSwapFile(ctx context.Context, fd int, kind, path string, size uint64) error {
	// The umask may have taken bits from the mode, never added any; this
	// states it.
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return &os.PathError{Op: "chmod", Path: path, Err: err}
	}
	if kind == "btrfs" {
		if err := setNoCow(fd); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := fallocateFile(fd, int64(size)); err != nil {
		if errors.Is(err, unix.ENOSPC) {
			return fmt.Errorf("%w: %w: %s needs %d bytes: %w", bridgeipc.ErrConflict, errNoSpaceForSwap, path, size, err)
		}
		return &os.PathError{Op: "allocate", Path: path, Err: err}
	}
	if err := unix.Fsync(fd); err != nil {
		return &os.PathError{Op: "sync", Path: path, Err: err}
	}
	return runSwapTool(ctx, mkswapCommand, "--", h.path(path))
}

// removeSwapFileAt unlinks name from the folder open at dir when it still
// holds the file id names. A name that is gone is not an error.
func removeSwapFileAt(dir *os.File, name string, id fileID) error {
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return &os.PathError{Op: "stat", Path: name, Err: err}
	}
	if fileIDOf(&st) != id {
		return swapFileConflict(errSwapFileChanged, "%s", name)
	}
	if err = unix.Unlinkat(int(dir.Fd()), name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return &os.PathError{Op: "remove", Path: name, Err: err}
	}
	return nil
}

// selinuxXattr holds a file's SELinux context.
const selinuxXattr = "security.selinux"

// labelLikeParent gives the folder open at dir the SELinux context of the
// folder open at parent, through both handles. A parent without one, or a
// filesystem or kernel without SELinux, leaves dir as it is. A variable so
// that a test records it.
var labelLikeParent = func(parent, dir *os.File) error {
	label := make([]byte, 256)
	n, err := unix.Fgetxattr(int(parent.Fd()), selinuxXattr, label)
	if errors.Is(err, unix.ERANGE) {
		if n, err = unix.Fgetxattr(int(parent.Fd()), selinuxXattr, nil); err == nil {
			label = make([]byte, n)
			n, err = unix.Fgetxattr(int(parent.Fd()), selinuxXattr, label)
		}
	}
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the SELinux context of %s: %w", parent.Name(), err)
	}
	if err = unix.Fsetxattr(int(dir.Fd()), selinuxXattr, label[:n], 0); err != nil {
		return fmt.Errorf("set the SELinux context of %s: %w", dir.Name(), err)
	}
	return nil
}
