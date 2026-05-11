package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path atomically: a temp file in the same
// directory is written, fsynced, chmod-ed, closed, then renamed over path.
// The parent directory is fsynced after the rename so the rename itself
// survives a crash. The destination is validated upfront; refusing to clobber
// symlinks, directories, or special files.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	if err := validateDestination(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".linuxio-*")
	if err != nil {
		return fmt.Errorf("create temp in %q: %w", dir, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp %q: %w", tmp, err)
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return fmt.Errorf("chmod temp %q: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("fsync temp %q: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %q to %q: %w", tmp, path, err)
	}
	return fsyncDir(dir)
}

// validateDestination refuses to write to anything but a regular file (or a
// not-yet-existing path). os.Rename does not resolve symlinks at the
// destination, so without this check a symlink at path would be silently
// replaced by a regular file rather than the symlink target being updated.
// Adapted from moby/sys/atomicwriter.
func validateDestination(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat destination %q: %w", path, err)
	}
	switch mode := fi.Mode(); {
	case mode.IsRegular() && mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0:
		return nil
	case mode&os.ModeDir != 0:
		return fmt.Errorf("refusing to overwrite directory %q", path)
	case mode&os.ModeSymlink != 0:
		return fmt.Errorf("refusing to write through symlink %q", path)
	case mode&os.ModeNamedPipe != 0:
		return fmt.Errorf("refusing to overwrite named pipe %q", path)
	case mode&os.ModeSocket != 0:
		return fmt.Errorf("refusing to overwrite socket %q", path)
	case mode&os.ModeDevice != 0:
		return fmt.Errorf("refusing to overwrite device file %q", path)
	case mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0:
		return fmt.Errorf("refusing to overwrite file %q with special mode %s", path, mode)
	default:
		return fmt.Errorf("refusing to overwrite %q: unsupported file mode %s", path, mode)
	}
}

// fsyncDir fsyncs a directory so a preceding rename into it survives a crash.
// Best-effort: errors opening or syncing the directory are returned, but
// failures to close it are ignored (the data is already durable).
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir %q for fsync: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return fmt.Errorf("fsync dir %q: %w", dir, err)
	}
	_ = d.Close()
	return nil
}
