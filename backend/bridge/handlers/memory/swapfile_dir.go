package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openRootOnlyFolder opens the folder dir, an absolute host path, from the
// host root one name at a time with O_DIRECTORY|O_NOFOLLOW, and checks on
// each handle, the root's included, that it is a folder the owner owns that
// no group or everyone may write. This is rootOnlyPath decided on handles
// rather than names: no folder on the way can be swapped for a link or
// another folder between the check and the use, and since no one but the
// owner can change any of them, no name below the returned handle can either.
// A name on the way that is a link or not a folder, or a folder others can
// change, is refused with errOthersCanRedirect, wrapped with
// bridgeipc.ErrConflict; a missing one is fs.ErrNotExist.
func (h hostRoot) openRootOnlyFolder(dir string) (*os.File, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, fmt.Errorf("%q is not the absolute path of a folder", dir)
	}
	fd, err := unix.Open(h.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: "/", Err: err}
	}
	at := "/"
	if err = h.checkRootOnlyFolder(fd, at); err != nil {
		unix.Close(fd)
		return nil, err
	}
	for name := range strings.SplitSeq(strings.TrimPrefix(dir, "/"), "/") {
		if name == "" {
			continue
		}
		at = filepath.Join(at, name)
		next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, swapFileConflict(errOthersCanRedirect, "%s is a link or not a folder", at)
		}
		if err != nil {
			return nil, &os.PathError{Op: "open", Path: at, Err: err}
		}
		fd = next
		if err = h.checkRootOnlyFolder(fd, at); err != nil {
			unix.Close(fd)
			return nil, err
		}
	}
	return os.NewFile(uintptr(fd), dir), nil
}

// checkRootOnlyFolder refuses the handle fd, open at the host path at, unless
// it is a folder the owner owns that no group or everyone may write.
func (h hostRoot) checkRootOnlyFolder(fd int, at string) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return &os.PathError{Op: "stat", Path: at, Err: err}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != h.owner || st.Mode&0o022 != 0 {
		return swapFileConflict(errOthersCanRedirect, "%s", at)
	}
	return nil
}

// statSwapFileAt reads the entry name in the folder open at dir without
// following a link, and refuses anything but a regular file the owner owns.
func (h hostRoot) statSwapFileAt(dir *os.File, name, path string) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return st, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != h.owner {
		return st, swapFileConflict(errNotRootFile, "%s", path)
	}
	return st, nil
}
