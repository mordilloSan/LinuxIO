package btrfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A path from the top level names folders and subvolumes of the pool, and
// LinuxIO acts on it as root. Anyone who can write a folder on the way can
// put a symbolic link there, and a path opened by name would take root
// wherever the link points: a subvolume created in /etc, or another one of
// the same name deleted. So every operation on such a path reaches it from
// the top-level mount without following a link, checks that it is still on
// the pool, and then works through that one handle.

// ErrLinkOnPath is a top-level path that is, or goes through, a symbolic
// link.
var ErrLinkOnPath = errors.New("a subvolume path may not be or go through a symbolic link")

// openBelowTries bounds the retries of an open the kernel asks to repeat
// because a rename ran beside it.
const openBelowTries = 8

// openBelow opens the folder at rel, a path below the top-level mount top
// ("." for top itself), and returns its descriptor. It follows no symbolic
// link, on the way or at rel, and never climbs out of top: the kernel
// resolves the whole path under RESOLVE_BENEATH and RESOLVE_NO_SYMLINKS
// (openat2, Linux 5.6). Another filesystem mounted on a folder below top is
// refused by onFilesystemOf. The way to top itself is root's and resolves as
// usual.
func openBelow(top, rel string) (int, error) {
	topFD, err := unix.Open(top, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, &fs.PathError{Op: "open", Path: top, Err: err}
	}
	defer unix.Close(topFD)
	fd, err := openAtBelow(topFD, rel)
	if err != nil && !errors.Is(err, ErrLinkOnPath) {
		return -1, &fs.PathError{Op: "open", Path: filepath.Join(top, rel), Err: err}
	}
	if err != nil {
		return -1, err
	}
	if err = onFilesystemOf(topFD, fd, rel); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

// onFilesystemOf checks that the folder open at fd, reached as rel, is on
// the btrfs filesystem of the top-level mount open at topFD. A user's own
// top-level mount can have another filesystem mounted on a folder below it,
// which no resolve flag tells from a subvolume of the pool mounted in
// place, and the subvolumes of one pool have device numbers of their own:
// so the fsid the kernel reports for each is compared. A top that is not
// btrfs, such as a mountpoint a tmpfs was mounted over, fails every
// operation: that is the host's state, not a bad path, so it does not carry
// ErrNotOnFilesystem. The check does not tell the pool from another btrfs
// mounted over its top-level mountpoint.
func onFilesystemOf(topFD, fd int, rel string) error {
	topID, err := fsidOf(topFD)
	if errors.Is(err, ErrNotOnFilesystem) {
		return errors.New("the top-level mount is not a btrfs filesystem")
	}
	if err != nil {
		return fmt.Errorf("top level: %w", err)
	}
	gotID, err := fsidOf(fd)
	if err != nil {
		return fmt.Errorf("%w: %s", err, rel)
	}
	if gotID != topID {
		return fmt.Errorf("%w: %s", ErrNotOnFilesystem, rel)
	}
	return nil
}

// fsidOf returns the fsid of the btrfs filesystem the folder open at fd is
// on, and ErrNotOnFilesystem when it is on no btrfs. A var so unit tests,
// whose folders are on whatever holds the temp dir, can name a filesystem.
var fsidOf = func(fd int) ([16]byte, error) {
	var sfs unix.Statfs_t
	if err := unix.Fstatfs(fd, &sfs); err != nil {
		return [16]byte{}, fmt.Errorf("statfs: %w", err)
	}
	if sfs.Type != unix.BTRFS_SUPER_MAGIC {
		return [16]byte{}, ErrNotOnFilesystem
	}
	var info fsInfoArgs
	if err := rawIoctl(fd, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		return [16]byte{}, fmt.Errorf("fs info: %w", err)
	}
	return info.FSID, nil
}

// openAtBelow is openBelow from the open folder dirFD.
func openAtBelow(dirFD int, rel string) (int, error) {
	if rel == "" {
		rel = "."
	}
	how := unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	for try := 1; ; try++ {
		fd, err := unix.Openat2(dirFD, rel, &how)
		switch {
		case err == nil:
			return fd, nil
		case errors.Is(err, unix.ELOOP):
			return -1, fmt.Errorf("%w: %s", ErrLinkOnPath, rel)
		case (errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR)) && try < openBelowTries:
			continue
		default:
			return -1, err
		}
	}
}

// mkdirAllBelow makes the folders of rel below top that are missing, each
// through the handle of the one above it, and refuses a symbolic link or
// another filesystem at any of them. They get mode 0755 less the bridge's
// umask, as os.MkdirAll would give them.
func mkdirAllBelow(top, rel string) error {
	topFD, err := openBelow(top, ".")
	if err != nil {
		return err
	}
	defer unix.Close(topFD)
	fd := topFD
	defer func() {
		if fd != topFD {
			unix.Close(fd)
		}
	}()
	for name := range strings.SplitSeq(rel, "/") {
		if name == "" || name == "." {
			continue
		}
		if err = unix.Mkdirat(fd, name, 0o755); err != nil && !errors.Is(err, unix.EEXIST) {
			return &fs.PathError{Op: "mkdir", Path: name, Err: err}
		}
		next, err := openAtBelow(fd, name)
		if err != nil && !errors.Is(err, ErrLinkOnPath) {
			return &fs.PathError{Op: "open", Path: name, Err: err}
		}
		if err != nil {
			return err
		}
		if fd != topFD {
			unix.Close(fd)
		}
		fd = next
		if err = onFilesystemOf(topFD, fd, name); err != nil {
			return err
		}
	}
	return nil
}

// existsBelow reports whether anything, a link included, is at rel below
// top. The folders on the way are reached as openBelow reaches them; a
// missing one means nothing is there.
func existsBelow(top, rel string) (bool, error) {
	dirFD, err := openBelow(top, filepath.Dir(rel))
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(dirFD)
	var st unix.Stat_t
	err = unix.Fstatat(dirFD, filepath.Base(rel), &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, &fs.PathError{Op: "stat", Path: rel, Err: err}
	}
	return true, nil
}

// renameBelow renames from to to, both paths below top, through the handles
// of the folders they sit in, and never onto a name that is taken, as
// os.Rename refuses a folder there (RENAME_NOREPLACE). rename(2) itself
// never follows a link at either name.
func renameBelow(top, from, to string) error {
	fromDir, err := openBelow(top, filepath.Dir(from))
	if err != nil {
		return err
	}
	defer unix.Close(fromDir)
	toDir, err := openBelow(top, filepath.Dir(to))
	if err != nil {
		return err
	}
	defer unix.Close(toDir)
	if err = unix.Renameat2(fromDir, filepath.Base(from), toDir, filepath.Base(to), unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
