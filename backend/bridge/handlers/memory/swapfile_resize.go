package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// resizeSwapFile changes the length of the swap file at path, which must be
// off and still be the file id names, to size bytes and writes a new swap
// header, fchmod 0600 first, through the file's handle. A grow fallocates
// and fsyncs, then runs mkswap on the path. A shrink runs mkswap with the
// new size while the file still has its old length, then ftruncates and
// fsyncs: a header smaller than its file swaps at the header's size, while
// one larger than its file, as a cut between the two steps would leave the
// other way round, is refused by swapon at every boot. On btrfs it refuses a
// file that lost its NOCOW flag. The file is reached through folders only
// root can change and opened with O_NOFOLLOW; its filesystem must still be
// one swapFileFS accepts. A grow the disk has no room for (ENOSPC) is
// refused, wrapping bridgeipc.ErrConflict, with the file cut back to its old
// length. See errNoSwapHeader for what a failure leaves.
func (h hostRoot) resizeSwapFile(ctx context.Context, path string, id fileID, size uint64) error {
	if err := checkSwapFilePath(path); err != nil {
		return err
	}
	if err := checkSwapFileSize(size); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, kind, err := h.openSwapFileFolder(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	file, err := h.openSwapFileToChange(dir, path, id)
	if err != nil {
		return err
	}
	defer file.Close()
	fd := int(file.Fd())
	if kind == "btrfs" {
		if err := checkNoCow(fd); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return &os.PathError{Op: "chmod", Path: path, Err: err}
	}
	if int64(size) < st.Size {
		if err := h.writeSwapHeader(ctx, path, size); err != nil {
			return err
		}
		cutSwapFile(fd, path, int64(size))
		return nil
	}
	if err := growSwapFile(fd, path, st.Size, int64(size)); err != nil {
		return err
	}
	return h.writeSwapHeader(ctx, path, 0)
}

// writeSwapHeader runs mkswap on the swap file at path, for an area of size
// bytes, or of the whole file when size is 0, and runs it once more when it
// fails. mkswap takes the size in KiB after the path; for a file it then
// leaves the length as it is. A failure wraps errNoSwapHeader: mkswap may
// have wiped the old header before it failed.
func (h hostRoot) writeSwapHeader(ctx context.Context, path string, size uint64) error {
	args := []string{"--", h.path(path)}
	if size > 0 {
		args = append(args, strconv.FormatUint(size>>10, 10))
	}
	if err := runSwapTool(ctx, mkswapCommand, args...); err != nil {
		// A second try, for a failure that passes; a cancel does not.
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", errNoSwapHeader, err)
		}
		if again := runSwapTool(ctx, mkswapCommand, args...); again != nil {
			return fmt.Errorf("%w: %w", errNoSwapHeader, again)
		}
	}
	return nil
}

// openSwapFileToChange opens the swap file name of path in the folder open
// at dir for writing, without following a link, and refuses it unless it is
// a regular file the owner owns, is the file id names, and is off: listed in
// a fresh /proc/swaps the kernel would refuse to write it anyway.
func (h hostRoot) openSwapFileToChange(dir *os.File, path string, id fileID) (*os.File, error) {
	if err := h.checkSwapFileOff(path); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ELOOP) {
		return nil, swapFileConflict(errNotRootFile, "%s is a link", path)
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		_ = file.Close()
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != h.owner:
		err = swapFileConflict(errNotRootFile, "%s", path)
	case fileIDOf(&st) != id:
		err = swapFileConflict(errSwapFileChanged, "%s", path)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// checkSwapFileOff refuses path while a fresh /proc/swaps lists it.
func (h hostRoot) checkSwapFileOff(path string) error {
	devices, err := h.readSwaps()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Path == path }) {
		return swapFileConflict(errSwapFileActive, "%s", path)
	}
	return nil
}

// growSwapFile makes the file open at fd, of length old, size bytes long and
// syncs it. A grow that fails, the disk running out included, cuts the file
// back to old, so that its old swap header fits it again.
func growSwapFile(fd int, path string, old, size int64) error {
	err := fallocateFile(fd, size)
	if err == nil {
		err = unix.Fsync(fd)
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOSPC) {
		err = fmt.Errorf("%w: %w: %s needs %d bytes more: %w", bridgeipc.ErrConflict, errNoSpaceForSwap, path, size-old, err)
	} else {
		err = &os.PathError{Op: "grow", Path: path, Err: err}
	}
	if cutErr := unix.Ftruncate(fd, old); cutErr != nil {
		return fmt.Errorf("%w: %w; cutting it back to %d bytes failed: %w", errNoSwapHeader, err, old, cutErr)
	}
	return err
}

// cutSwapFile cuts the file open at fd, whose new swap header is already
// written for size bytes, to that length and syncs it. A cut that fails
// leaves a file that swaps at its new size and keeps its old length on
// disk, which wastes space and nothing else: it is logged, not returned.
func cutSwapFile(fd int, path string, size int64) {
	err := unix.Fsync(fd)
	if err == nil {
		err = unix.Ftruncate(fd, size)
	}
	if err == nil {
		err = unix.Fsync(fd)
	}
	if err != nil {
		slog.Warn("cut swap file to its new size failed", "path", path, "size", size, "error", err)
	}
}
