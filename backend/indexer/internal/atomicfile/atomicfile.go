// Package atomicfile writes files atomically via a temp file and rename.
// Writes follow symlinks to their target and preserve the mode and (when
// running as root) ownership of an existing destination.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WriteFile atomically replaces path with data. defaultMode applies only when
// the destination does not already exist.
func WriteFile(path string, data []byte, defaultMode os.FileMode) (err error) {
	path, err = resolveWritePath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	attrs, err := writeAttrs(path, defaultMode)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temp file %s: %w", tmpName, removeErr))
			}
		}
	}()

	if err := prepareTempFile(tmp, tmpName, attrs, data); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file %s to %s: %w", tmpName, path, err)
	}
	removeTmp = false

	return syncDir(dir)
}

type fileAttrs struct {
	mode os.FileMode
	uid  int
	gid  int
}

func resolveWritePath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve symlink %s: %w", path, err)
	}
	return target, nil
}

func writeAttrs(path string, defaultMode os.FileMode) (fileAttrs, error) {
	attrs := fileAttrs{mode: defaultMode, uid: -1, gid: -1}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return attrs, nil
		}
		return attrs, fmt.Errorf("stat %s: %w", path, err)
	}
	attrs.mode = info.Mode().Perm()
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		attrs.uid = int(stat.Uid)
		attrs.gid = int(stat.Gid)
	}
	return attrs, nil
}

func prepareTempFile(tmp *os.File, tmpName string, attrs fileAttrs, data []byte) error {
	if err := tmp.Chmod(attrs.mode); err != nil {
		return closeTempAfterError(tmp, tmpName, fmt.Errorf("chmod temp file %s: %w", tmpName, err))
	}
	if attrs.uid >= 0 && attrs.gid >= 0 {
		if err := tmp.Chown(attrs.uid, attrs.gid); err != nil {
			return closeTempAfterError(tmp, tmpName, fmt.Errorf("chown temp file %s: %w", tmpName, err))
		}
	}
	if _, err := tmp.Write(data); err != nil {
		return closeTempAfterError(tmp, tmpName, fmt.Errorf("write temp file %s: %w", tmpName, err))
	}
	if err := tmp.Sync(); err != nil {
		return closeTempAfterError(tmp, tmpName, fmt.Errorf("sync temp file %s: %w", tmpName, err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpName, err)
	}
	return nil
}

func closeTempAfterError(tmp *os.File, tmpName string, primary error) error {
	if closeErr := tmp.Close(); closeErr != nil {
		return errors.Join(primary, fmt.Errorf("close temp file %s after error: %w", tmpName, closeErr))
	}
	return primary
}

func syncDir(dir string) (err error) {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory %s: %w", dir, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", dir, err)
	}
	return nil
}
