package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

const (
	maxTrashNameAttempts = 10000
	trashInfoTimeLayout  = "2006-01-02T15:04:05"
	maxTrashInfoBytes    = 64 << 10
)

func homeTrashDir(home string) string {
	return filepath.Join(home, ".local/share/Trash")
}

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
		if mkErr := mkdirAllOwned(root, filepath.Join(trash, sub), 0o700, uid, gid, chownNew); mkErr != nil {
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
		return homeTrashDir(home), "", nil
	}

	top := topDirOf(root, parent, parentDev)
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

// topDirOf walks up from dir to the highest directory on the same device: the
// mount point, where per-filesystem trashes live.
func topDirOf(root *fsroot.FSRoot, dir string, dev uint64) string {
	top := dir
	for top != "/" {
		parentDev, err := deviceOf(root, filepath.Dir(top))
		if err != nil || parentDev != dev {
			break
		}
		top = filepath.Dir(top)
	}
	return top
}

// mkdirAllOwned creates dir and any missing parents with mode, chowning only
// the directories it created.
func mkdirAllOwned(root *fsroot.FSRoot, dir string, mode os.FileMode, uid, gid int, chownNew bool) error {
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
		if err := root.Root.Mkdir(rel, mode); err != nil && !errors.Is(err, fs.ErrExist) {
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

// TrashItem is one entry of a trash directory as the Trash view shows it.
type TrashItem struct {
	Path         string // where the item sits now, under a trash "files" dir
	Name         string
	OriginalPath string // empty when no .trashinfo records it
	DeletedAt    time.Time
	IsDir        bool
	Size         int64
}

// ListTrash returns everything in the user's trash directories, newest first.
func ListTrash(ctx context.Context, home string, uid int) ([]TrashItem, error) {
	if home == "" {
		return nil, errors.New("home directory unknown")
	}
	root, err := fsroot.Open()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	items := []TrashItem{}
	for _, trash := range trashDirs(root, filepath.Clean(home), uid) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		filesDir := filepath.Join(trash, "files")
		err := forEachChild(root, filesDir, func(name string, info os.FileInfo) error {
			original, deletedAt := readTrashInfo(root, trash, name)
			item := TrashItem{Path: filepath.Join(filesDir, name), Name: name, OriginalPath: original, DeletedAt: deletedAt, IsDir: info.IsDir()}
			if info.Mode().IsRegular() {
				item.Size = info.Size()
			}
			items = append(items, item)
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("list trash %s: %w", trash, err)
		}
	}
	slices.SortFunc(items, func(a, b TrashItem) int {
		if c := b.DeletedAt.Compare(a.DeletedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return items, nil
}

// RestoreFromTrash puts a trashed item back where its .trashinfo says it came
// from, recreating missing parent folders, and drops the .trashinfo.
func RestoreFromTrash(ctx context.Context, trashedPath, home string, uid, gid int, chownNew bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := fsroot.Open()
	if err != nil {
		return "", err
	}
	defer root.Close()

	trash, name, err := trashedEntry(root, home, uid, trashedPath)
	if err != nil {
		return "", err
	}
	original, _ := readTrashInfo(root, trash, name)
	if original == "" {
		return "", fmt.Errorf("restore %s: its original location is not recorded", name)
	}
	if _, err := root.Root.Lstat(relPath(original)); err == nil {
		return "", fmt.Errorf("restore %s: %s already exists", name, original)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("restore %s: %w", name, err)
	}
	if err := mkdirAllOwned(root, filepath.Dir(original), PermDir, uid, gid, chownNew); err != nil {
		return "", fmt.Errorf("restore %s: %w", name, err)
	}
	if err := root.Root.Rename(relPath(filepath.Join(trash, "files", name)), relPath(original)); err != nil {
		return "", fmt.Errorf("restore %s: %w", name, err)
	}
	removeTrashInfo(root, trash, name)
	return original, nil
}

// PurgeFromTrash deletes a trashed item for good, along with its .trashinfo,
// and returns how many entries were removed.
func PurgeFromTrash(ctx context.Context, trashedPath, home string, uid int, progress func(processed int64)) (int64, error) {
	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	trash, name, err := trashedEntry(root, home, uid, trashedPath)
	if err != nil {
		return 0, err
	}
	removed, err := DeleteFilesWithProgress(ctx, filepath.Join(trash, "files", name), DeleteOptions{Progress: progress})
	if err != nil {
		return removed, fmt.Errorf("purge %s: %w", name, err)
	}
	removeTrashInfo(root, trash, name)
	return removed, nil
}

// trashDirs returns the trash directories that exist for uid: the home trash
// plus the per-filesystem trashes on every mount point, as the spec lays them
// out, so items trashed by other tools show up too.
func trashDirs(root *fsroot.FSRoot, home string, uid int) []string {
	dirs := []string{homeTrashDir(home)}
	for _, point := range mountPoints() {
		for _, candidate := range []string{
			filepath.Join(point, ".Trash", strconv.Itoa(uid)),
			filepath.Join(point, ".Trash-"+strconv.Itoa(uid)),
		} {
			if slices.Contains(dirs, candidate) {
				continue
			}
			if info, err := root.Root.Lstat(relPath(candidate)); err == nil && info.IsDir() {
				dirs = append(dirs, candidate)
			}
		}
	}
	return dirs
}

var mountinfoUnescape = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

// mountPoints lists this mount namespace's mount points. When mountinfo cannot
// be read only the home trash is used, which is still correct.
func mountPoints() []string {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	var points []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		points = append(points, mountinfoUnescape.Replace(fields[4]))
	}
	return points
}

// trashedEntry checks that path is an item directly inside one of uid's trash
// directories and returns that trash directory and the item's name.
func trashedEntry(root *fsroot.FSRoot, home string, uid int, path string) (string, string, error) {
	if home == "" {
		return "", "", errors.New("home directory unknown")
	}
	path = fsroot.CleanAbsPath(path)
	filesDir := filepath.Dir(path)
	trash := filepath.Dir(filesDir)
	if filepath.Base(filesDir) != "files" || !slices.Contains(trashDirs(root, filepath.Clean(home), uid), trash) {
		return "", "", fmt.Errorf("%s is not in the trash", path)
	}
	return trash, filepath.Base(path), nil
}

// readTrashInfo returns the original path and deletion time recorded for a
// trashed entry; both are zero when the .trashinfo is missing or malformed.
func readTrashInfo(root *fsroot.FSRoot, trash, name string) (string, time.Time) {
	f, err := root.Root.Open(relPath(filepath.Join(trash, "info", name+".trashinfo")))
	if err != nil {
		return "", time.Time{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTrashInfoBytes))
	if err != nil {
		return "", time.Time{}
	}

	var original string
	var deletedAt time.Time
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "Path="); ok && original == "" {
			if unescaped, err := url.PathUnescape(strings.TrimSpace(value)); err == nil {
				original = trashOriginalPath(root, trash, unescaped)
			}
		} else if value, ok := strings.CutPrefix(line, "DeletionDate="); ok {
			if at, err := time.ParseInLocation(trashInfoTimeLayout, strings.TrimSpace(value), time.Local); err == nil {
				deletedAt = at
			}
		}
	}
	return original, deletedAt
}

// trashOriginalPath resolves a recorded path: absolute as is, relative against
// the top directory of the trash's filesystem, as the spec defines it.
func trashOriginalPath(root *fsroot.FSRoot, trash, recorded string) string {
	if recorded == "" {
		return ""
	}
	if filepath.IsAbs(recorded) {
		return filepath.Clean(recorded)
	}
	dev, err := deviceOf(root, trash)
	if err != nil {
		return ""
	}
	return filepath.Join(topDirOf(root, trash, dev), recorded)
}

func removeTrashInfo(root *fsroot.FSRoot, trash, name string) {
	if err := root.Root.Remove(relPath(filepath.Join(trash, "info", name+".trashinfo"))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Debug("failed to remove trash info", "component", "filebrowser", "subsystem", "trash", "trash", trash, "name", name, "error", err)
	}
}
