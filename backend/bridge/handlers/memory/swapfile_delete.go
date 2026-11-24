package memory

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
)

// deleteSwapFile unlinks the file at path through the handle of its folder,
// reached through folders only root can change, only when a fresh
// /proc/swaps does not list path and the name still holds the file id names
// (removeSwapFileAt checks device and inode right before the unlinkat);
// otherwise it refuses, wrapping bridgeipc.ErrConflict. A name that is
// already gone, or whose folder is, is not an error. The kernel refuses to
// unlink an active swap file as well.
func (h hostRoot) deleteSwapFile(ctx context.Context, path string, id fileID) error {
	if err := checkSwapFilePath(path); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := h.openRootOnlyFolder(filepath.Dir(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := h.checkSwapFileOff(path); err != nil {
		return err
	}
	return removeSwapFileAt(dir, filepath.Base(path), id)
}
