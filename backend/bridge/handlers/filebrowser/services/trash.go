package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

const maxTrashNameAttempts = 10000

// MoveToTrash moves absPath into the FreeDesktop.org trash that applies to its
// filesystem: the user's home trash when absPath lives on the same device as
// home, otherwise a per-filesystem trash in the device's top directory. Other
// filesystems are never copied across; the caller gets an error instead.
func MoveToTrash(ctx context.Context, absPath, home string, uid, gid int, chownNew bool) (string, error) {
	absPath = fsroot.CleanAbsPath(absPath)
	if absPath == "/" {
		return "", errors.New("cannot trash root")
	}
	if home == "" {
		return "", errors.New("home directory unknown")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	root, err := fsroot.Open()
	if err != nil {
		return "", err
	}
	defer root.Close()

	trash, infoBase, err := chooseTrashDir(root, absPath, filepath.Clean(home), uid)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}

	for _, sub := range []string{"files", "info"} {
		if mkErr := mkdirAllOwned(root, filepath.Join(trash, sub), uid, gid, chownNew); mkErr != nil {
			return "", fmt.Errorf("trash %s: %w", absPath, mkErr)
		}
	}

	infoPath := filepath.Join(trash, "info")
	filesPath := filepath.Join(trash, "files")
	name, infoFile, err := reserveTrashName(root, filepath.Base(absPath), infoPath, filesPath)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}
	infoName := filepath.Join(infoPath, name+".trashinfo")

	recorded := absPath
	if infoBase != "" {
		if recorded, err = filepath.Rel(infoBase, absPath); err != nil {
			infoFile.Close()
			_ = root.Root.Remove(relPath(infoName))
			return "", fmt.Errorf("trash %s: %w", absPath, err)
		}
	}
	content := "[Trash Info]\nPath=" + (&url.URL{Path: recorded}).EscapedPath() +
		"\nDeletionDate=" + time.Now().Format("2006-01-02T15:04:05") + "\n"
	_, writeErr := infoFile.WriteString(content)
	if closeErr := infoFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = root.Root.Remove(relPath(infoName))
		return "", fmt.Errorf("trash %s: write info: %w", absPath, writeErr)
	}
	if chownNew {
		if err := root.Root.Chown(relPath(infoName), uid, gid); err != nil {
			_ = root.Root.Remove(relPath(infoName))
			return "", fmt.Errorf("trash %s: chown info: %w", absPath, err)
		}
	}

	dest := filepath.Join(filesPath, name)
	if err := root.Root.Rename(relPath(absPath), relPath(dest)); err != nil {
		_ = root.Root.Remove(relPath(infoName))
		if errors.Is(err, syscall.EXDEV) {
			return "", fmt.Errorf("trash %s: cannot move to trash across filesystems: %w", absPath, err)
		}
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}
	return dest, nil
}

// chooseTrashDir returns the trash directory for absPath and, for a top
// directory trash, the directory recorded paths are relative to ("" for the
// home trash, which records absolute paths).
func chooseTrashDir(root *fsroot.FSRoot, absPath, home string, uid int) (trash, infoBase string, err error) {
	parent := filepath.Dir(absPath)
	parentDev, err := deviceOf(root, parent)
	if err != nil {
		return "", "", err
	}
	if _, statErr := root.Root.Lstat(relPath(absPath)); statErr != nil {
		return "", "", statErr
	}
	homeDev, err := deviceOf(root, home)
	if err != nil {
		return "", "", fmt.Errorf("home directory: %w", err)
	}
	if parentDev == homeDev {
		return filepath.Join(home, ".local/share/Trash"), "", nil
	}

	top := parent
	for top != "/" {
		dev, err := deviceOf(root, filepath.Dir(top))
		if err != nil || dev != parentDev {
			break
		}
		top = filepath.Dir(top)
	}

	shared := filepath.Join(top, ".Trash")
	if info, err := root.Root.Lstat(relPath(shared)); err == nil && info.IsDir() && info.Mode()&os.ModeSticky != 0 {
		return filepath.Join(shared, strconv.Itoa(uid)), top, nil
	}
	return filepath.Join(top, ".Trash-"+strconv.Itoa(uid)), top, nil
}

func deviceOf(root *fsroot.FSRoot, path string) (uint64, error) {
	info, err := root.Root.Stat(relPath(path))
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("no device information for %s", path)
	}
	return st.Dev, nil
}

// mkdirAllOwned creates dir and any missing parents with mode 0700, chowning
// only the directories it created.
func mkdirAllOwned(root *fsroot.FSRoot, dir string, uid, gid int, chownNew bool) error {
	var missing []string
	for p := dir; p != "/"; p = filepath.Dir(p) {
		if _, err := root.Root.Stat(relPath(p)); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, p)
	}
	for _, m := range slices.Backward(missing) {
		rel := relPath(m)
		if err := root.Root.Mkdir(rel, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if chownNew {
			if err := root.Root.Chown(rel, uid, gid); err != nil {
				return err
			}
		}
	}
	return nil
}

// reserveTrashName claims the first free name by exclusively creating its info
// file, and returns that file open for writing.
func reserveTrashName(root *fsroot.FSRoot, base, infoDir, filesDir string) (string, *os.File, error) {
	for i := 1; i <= maxTrashNameAttempts; i++ {
		name := base
		if i > 1 {
			name = base + "." + strconv.Itoa(i)
		}
		// An entry without an info file (orphan) still owns its name.
		if _, err := root.Root.Lstat(relPath(filepath.Join(filesDir, name))); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
		f, err := root.Root.OpenFile(relPath(filepath.Join(infoDir, name+".trashinfo")), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return name, f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("no free trash name")
}
