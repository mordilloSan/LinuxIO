package services

import (
	"context"
	"crypto/rand"
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

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
)

const (
	maxTrashNameAttempts = 10000
	trashInfoTimeLayout  = "2006-01-02T15:04:05"
	maxTrashInfoBytes    = 64 << 10
)

var (
	// ErrNameTaken marks a restore refused because something is at the name
	// the item goes back under and the request chose neither replace nor
	// keep both for it. The item stays in the trash.
	ErrNameTaken = errors.New("already exists")
	// ErrNoTrash marks a move to the trash refused because the item's
	// filesystem cannot hold a trash for the session user: the trash there
	// cannot be made or opened, or the move into it is refused across
	// filesystems. The item stays where it is; nothing is copied.
	ErrNoTrash = errors.New("no trash on this filesystem")
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

	// The item is judged and moved through one handle on its folder, so a
	// folder swapped on the path in between cannot put another in its place.
	owner := Owner{UID: uint32(uid), GID: uint32(gid), Set: chownNew}
	src, err := linksafe.OpenDirNoUserLinks(root, filepath.Dir(absPath), owner)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}
	defer src.Close()
	base := filepath.Base(absPath)
	srcDev, err := checkNotProtectedAt(src, base, absPath)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}

	trash, infoBase, err := chooseTrashDir(root, absPath, srcDev, filepath.Clean(home), uid)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}
	filesDir, infoDir, err := openTrash(root, trash, home, owner, true)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, noTrash(err))
	}
	defer filesDir.Close()
	defer infoDir.Close()

	name, infoFile, err := reserveTrashName(base, infoDir, filesDir)
	if err != nil {
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}

	recorded := absPath
	if infoBase != "" {
		if recorded, err = filepath.Rel(infoBase, absPath); err != nil {
			infoFile.Close()
			removeTrashInfo(infoDir, name)
			return "", fmt.Errorf("trash %s: %w", absPath, err)
		}
	}
	content := "[Trash Info]\nPath=" + (&url.URL{Path: recorded}).EscapedPath() +
		"\nDeletionDate=" + time.Now().Format("2006-01-02T15:04:05") + "\n"
	_, writeErr := infoFile.WriteString(content)
	if writeErr == nil {
		writeErr = handOverTrashInfo(infoFile, infoDir, owner)
	}
	if closeErr := infoFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		removeTrashInfo(infoDir, name)
		return "", fmt.Errorf("trash %s: write info: %w", absPath, writeErr)
	}

	// Acting for the session user, a root bridge also records where the item
	// came from where only root can write (see trash_records.go).
	item := recordTrashedAt(ctx, root, home, owner, src, base, absPath, trash)

	if err := renameNoReplace(src, base, filesDir, name); err != nil {
		removeTrashInfo(infoDir, name)
		dropTrashRecord(uid, item)
		if errors.Is(err, syscall.EXDEV) {
			return "", fmt.Errorf("trash %s: %w: %w", absPath, ErrNoTrash, err)
		}
		return "", fmt.Errorf("trash %s: %w", absPath, err)
	}
	return filepath.Join(trash, "files", name), nil
}

// noTrash marks err, why the trash a move needs could not be made or opened,
// as ErrNoTrash when it says the place cannot hold one: the user may not
// write there, or something that is not a folder is where a folder of the
// trash goes. A refusal under the link and ownership rules (403: a trash
// folder another user owns, a link on the way to the trash's top folder)
// keeps its own meaning, and so does any other failure (a read-only
// filesystem, a full disk, an I/O error): the page offers to delete for good
// only where a retry could not help.
func noTrash(err error) error {
	if errors.Is(err, bridgeipc.ErrForbidden) {
		return err
	}
	for _, cause := range []error{unix.EACCES, unix.EPERM, unix.ENOTDIR, unix.ELOOP, unix.EEXIST} {
		if errors.Is(err, cause) {
			return fmt.Errorf("%w: %w", ErrNoTrash, err)
		}
	}
	return err
}

// checkNotProtectedAt is CheckNotProtected for name in dir, the open folder
// the item at path sits in, so the item judged is the one then moved out of
// dir. It returns dir's device.
func checkNotProtectedAt(dir *os.File, name, path string) (uint64, error) {
	var parent, item, top unix.Stat_t
	if err := unix.Fstat(int(dir.Fd()), &parent); err != nil {
		return 0, err
	}
	if err := unix.Fstatat(int(dir.Fd()), name, &item, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, &fs.PathError{Op: "lstat", Path: path, Err: err}
	}
	if err := unix.Stat("/", &top); err != nil {
		return 0, err
	}
	if parent.Dev == top.Dev && parent.Ino == top.Ino && protectedTopLevel[name] {
		return 0, fmt.Errorf("%s is a system folder", path)
	}
	if item.Mode&unix.S_IFMT == unix.S_IFDIR && item.Dev != parent.Dev {
		return 0, fmt.Errorf("%s is a mount point", path)
	}
	return parent.Dev, nil
}

// handOverTrashInfo gives the still-open .trashinfo to owner when the info
// folder, as fstat saw its handle, is theirs.
func handOverTrashInfo(f, infoDir *os.File, owner Owner) error {
	folder, err := infoDir.Stat()
	if err != nil || !HandsOverIn(folder, owner) {
		return err
	}
	return HandOver(f, owner, 0o600, folder.Mode()&os.ModeSetgid != 0)
}

// chooseTrashDir returns the trash directory for absPath, whose folder is on
// device parentDev, and, for a top directory trash, the directory recorded
// paths are relative to ("" for the home trash, which records absolute paths).
func chooseTrashDir(root *fsroot.FSRoot, absPath string, parentDev uint64, home string, uid int) (trash, infoBase string, err error) {
	homeDev, err := deviceOf(root, home)
	if err != nil {
		return "", "", fmt.Errorf("home directory: %w", err)
	}
	if parentDev == homeDev {
		return homeTrashDir(home), "", nil
	}

	top := topDirOf(root, filepath.Dir(absPath), parentDev)
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

// trashAnchor is the folder a trash hangs off: the user's home for the home
// trash, otherwise the top directory of the trash's filesystem.
func trashAnchor(trash, home string) string {
	if trash == homeTrashDir(home) {
		return home
	}
	if parent := filepath.Dir(trash); filepath.Base(parent) == ".Trash" {
		return filepath.Dir(parent)
	}
	return filepath.Dir(trash)
}

// openTrash opens trash's files and info folders, making them when create is
// set; without create a missing info folder comes back nil, as if every
// .trashinfo in it were missing.
//
// On a root bridge nothing below the trash's anchor (the user's home or the
// filesystem's top folder) is followed if it is a symlink: those folders are
// the user's, who could otherwise point the trash, and what root moves into,
// out of or deletes from it, at any folder on the system. An unprivileged
// bridge acts as the user and follows their links as any program of theirs
// would. Either way a files or info folder another user owns, say one made
// ahead of time in a shared .Trash, is refused.
func openTrash(root *fsroot.FSRoot, trash, home string, owner Owner, create bool) (files, info *os.File, err error) {
	var dir *os.File // the trash folder, walked to on a root bridge
	if os.Geteuid() == 0 {
		if dir, err = walkTrash(root, trash, home, owner, create); err != nil {
			return nil, nil, err
		}
		defer dir.Close()
		if err = refuseOtherUsersFolder(dir, owner); err != nil {
			return nil, nil, err
		}
	}
	if files, err = openTrashSub(root, dir, trash, "files", owner, create); err != nil {
		return nil, nil, err
	}
	info, err = openTrashSub(root, dir, trash, "info", owner, create)
	if !create && errors.Is(err, fs.ErrNotExist) {
		return files, nil, nil
	}
	if err != nil {
		_ = files.Close()
		return nil, nil, err
	}
	return files, info, nil
}

// walkTrash opens trash from its anchor one folder at a time without
// following a symlink, making missing folders with mode 0700 when create is
// set. The per-filesystem trash folder (.Trash-<uid>, .Trash/<uid>) this call
// makes goes to owner even in a root-owned top folder, as the XDG trash spec
// has it; any other only inside a folder owner owns (see MkdirAt).
func walkTrash(root *fsroot.FSRoot, trash, home string, owner Owner, create bool) (*os.File, error) {
	anchor := trashAnchor(trash, home)
	rel, err := filepath.Rel(anchor, trash)
	if err != nil {
		return nil, err
	}
	dir, err := linksafe.OpenDirNoUserLinks(root, anchor, owner)
	if err != nil {
		return nil, err
	}
	path := anchor
	for name := range strings.SplitSeq(rel, string(filepath.Separator)) {
		path = filepath.Join(path, name)
		var next *os.File
		switch {
		case create && path == trash && trash != homeTrashDir(home):
			next, err = mkdirAt(dir, name, 0o700, owner, owner.Set)
		case create:
			next, err = MkdirAt(dir, name, 0o700, owner)
		default:
			next, err = linksafe.OpenDirAt(dir, name)
		}
		_ = dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// openTrashSub opens sub of trash: in dir, the trash folder a root bridge
// walked to, without following a symlink, or by path, as the user would, on
// an unprivileged bridge (dir nil).
func openTrashSub(root *fsroot.FSRoot, dir *os.File, trash, sub string, owner Owner, create bool) (*os.File, error) {
	var f *os.File
	var err error
	switch {
	case dir != nil && create:
		f, err = MkdirAt(dir, sub, 0o700, owner)
	case dir != nil:
		f, err = linksafe.OpenDirAt(dir, sub)
	case create:
		var missing []string
		if f, missing, err = linksafe.OpenNearestDir(root, relPath(filepath.Join(trash, sub))); err == nil {
			f, err = MkdirAllAt(f, missing, 0o700, owner)
		}
	default:
		f, err = root.Root.OpenFile(relPath(filepath.Join(trash, sub)), os.O_RDONLY|syscall.O_DIRECTORY, 0)
	}
	if err != nil {
		return nil, err
	}
	if err = refuseOtherUsersFolder(f, owner); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// refuseOtherUsersFolder refuses a trash folder neither the session user nor
// the bridge owns: another user could have made it ahead of time, say
// .Trash/<uid> in a shared .Trash, to receive what root moves in.
func refuseOtherUsersFolder(f *os.File, owner Owner) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	if st.Uid != owner.UID && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%w: %s belongs to another user", bridgeipc.ErrForbidden, f.Name())
	}
	return nil
}

// reserveTrashName claims the first free name by exclusively creating its info
// file in infoDir, never through a symlink at that name, and returns that file
// open for writing.
func reserveTrashName(base string, infoDir, filesDir *os.File) (string, *os.File, error) {
	for i := 1; i <= maxTrashNameAttempts; i++ {
		name := base
		if i > 1 {
			name = base + "." + strconv.Itoa(i)
		}
		// An entry without an info file (orphan) still owns its name.
		var st unix.Stat_t
		if err := unix.Fstatat(int(filesDir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
			continue
		} else if !errors.Is(err, unix.ENOENT) {
			return "", nil, &fs.PathError{Op: "fstatat", Path: name, Err: err}
		}
		infoName := name + ".trashinfo"
		fd, err := unix.Openat(int(infoDir.Fd()), infoName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if err == nil {
			return name, os.NewFile(uintptr(fd), filepath.Join(infoDir.Name(), infoName)), nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return "", nil, &fs.PathError{Op: "create", Path: infoName, Err: err}
		}
	}
	return "", nil, errors.New("no free trash name")
}

// renameNoReplace moves oldName in oldDir to newName in newDir, failing with
// EEXIST rather than replacing what is there. Where the filesystem has no
// RENAME_NOREPLACE (NFS, SMB, some FUSE) it falls back to a check and a plain
// rename rather than refusing every move on that mount.
func renameNoReplace(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	oldFd, newFd := int(oldDir.Fd()), int(newDir.Fd())
	err := unix.Renameat2(oldFd, oldName, newFd, newName, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EINVAL) {
		var st unix.Stat_t
		if err = unix.Fstatat(newFd, newName, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
			err = unix.EEXIST
		} else if errors.Is(err, unix.ENOENT) {
			err = unix.Renameat(oldFd, oldName, newFd, newName)
		}
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: filepath.Join(oldDir.Name(), oldName), New: filepath.Join(newDir.Name(), newName), Err: err}
	}
	return nil
}

// TrashItem is one entry of a trash directory as the Trash view shows it.
type TrashItem struct {
	Path         string // where the item sits now, under a trash "files" dir
	Name         string
	OriginalPath string // empty when no .trashinfo records it
	DeletedAt    time.Time
	IsDir        bool
	Size         int64
	// ConfirmRestore is set when no record vouches for OriginalPath (see
	// trash_records.go) and restoring there goes into a folder the session
	// user does not own: RestoreFromTrash then needs it confirmed.
	ConfirmRestore bool
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

	owner := Owner{UID: uint32(uid), Set: os.Geteuid() == 0} // Set: reach the trash as a move does
	items := []TrashItem{}
	for _, trash := range trashDirs(root, filepath.Clean(home), uid) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		files, info, err := openTrash(root, trash, home, owner, false)
		if err != nil {
			// The other trashes still show.
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("skipped a trash folder that cannot be opened safely", "component", "filebrowser", "subsystem", "trash", "trash", trash, "error", err)
			}
			continue
		}
		err = listTrashItems(root, trash, files, info, owner, &items)
		_ = files.Close()
		_ = info.Close()
		if err != nil {
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

// listTrashItems appends what trash's files folder holds, as fstatat sees it
// there, with where each one came from: for a root bridge acting for owner
// the record of the move, when there is one, otherwise what its .trashinfo
// in info records.
func listTrashItems(root *fsroot.FSRoot, trash string, files, info *os.File, owner Owner, items *[]TrashItem) error {
	names, err := files.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(int(files.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
			continue // purged meanwhile
		} else if err != nil {
			return &fs.PathError{Op: "fstatat", Path: filepath.Join(files.Name(), name), Err: err}
		}
		original, deletedAt := readTrashInfo(root, trash, info, name)
		item := TrashItem{Path: filepath.Join(trash, "files", name), Name: name, OriginalPath: original, DeletedAt: deletedAt, IsDir: st.Mode&unix.S_IFMT == unix.S_IFDIR}
		if owner.Set {
			if record, ok := trashRecordOf(int(owner.UID), &st, trash); ok {
				item.OriginalPath = record.Path
			} else if original != "" && !userCouldWrite(&st, owner) {
				item.ConfirmRestore = restoreNeedsConfirmation(root, original, owner)
			}
		}
		if st.Mode&unix.S_IFMT == unix.S_IFREG {
			item.Size = st.Size
		}
		*items = append(*items, item)
	}
	return nil
}

// RestoreOptions are a restore request's choices for one item.
type RestoreOptions struct {
	// Confirmed lists the destination the request confirmed for the item,
	// which an item without a record needs to go back into a folder the
	// session user does not own.
	Confirmed []string
	// Into is an existing folder the user chose: the item goes into it as
	// Name, or under its name in the trash when Name is empty, instead of
	// back where it came from.
	Into, Name string
	// Replace puts the item over what is at its name, as a move with
	// overwrite does: a file replaces the one there and a folder merges into
	// the folder there. KeepBoth gives it the first free "name (copy)" name
	// instead. With neither, a taken name fails the restore with ErrNameTaken.
	Replace, KeepBoth bool
}

// RestoreFromTrash puts a trashed item back where its .trashinfo says it came
// from, recreating missing parent folders, or into the folder opts.Into, and
// drops the .trashinfo. It returns where the item is now.
//
// The trash and its .trashinfo are the session user's to edit, so for a root
// bridge acting for that user (chownNew) neither vouches for what goes back
// into a folder that user does not own: only an item the user could not have
// written (see userCouldWrite) is restored there, checked again inside that
// folder before it gets its name. Such a bridge restores an item it trashed
// itself to where its record says (see trash_records.go), whatever Path=
// says, and an item without a record, trashed by another program, into such
// a folder, or over anything with opts.Replace, only when opts.Confirmed
// lists that destination. A folder and a name the request gives in opts.Into
// and opts.Name are the user's choice, not the trash's claim, so none of that
// applies there (see restoreInto).
func RestoreFromTrash(ctx context.Context, trashedPath, home string, uid, gid int, chownNew bool, opts RestoreOptions) (string, error) {
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
	owner := Owner{UID: uint32(uid), GID: uint32(gid), Set: chownNew}
	files, info, err := openTrash(root, trash, home, owner, false)
	if err != nil {
		return "", fmt.Errorf("restore %s: %w", name, err)
	}
	defer files.Close()
	defer info.Close()
	var item unix.Stat_t
	if err = unix.Fstatat(int(files.Fd()), name, &item, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", fmt.Errorf("restore %s: %w", name, &fs.PathError{Op: "fstatat", Path: name, Err: err})
	}
	original, recorded := restoreDestination(root, trash, info, name, &item, owner)
	var restored string
	if opts.Into != "" {
		restored, err = restoreInto(ctx, root, files, name, &item, owner, opts)
	} else {
		restored, err = restoreToOriginal(ctx, root, files, name, item, original, recorded, owner, opts)
	}
	if err != nil {
		return "", fmt.Errorf("restore %s: %w", name, err)
	}
	removeTrashInfo(info, name)
	dropTrashRecord(uid, &item)
	return restored, nil
}

// restoreToOriginal moves name, the trashed item in files that item
// describes, back to original, where a record says it came from when
// recorded is set and its .trashinfo otherwise.
func restoreToOriginal(ctx context.Context, root *fsroot.FSRoot, files *os.File, name string, item unix.Stat_t, original string, recorded bool, owner Owner, opts RestoreOptions) (string, error) {
	if original == "" {
		return "", errors.New("its original location is not recorded")
	}
	userWritten := userCouldWrite(&item, owner)
	// Into a folder the session user does not own an item without a record
	// goes only where the request confirmed.
	checkSystem := func(dir *os.File) (bool, error) {
		system, checkErr := intoSystemFolder(dir, filepath.Dir(original), owner, userWritten)
		if checkErr == nil && system && !recorded && !slices.Contains(opts.Confirmed, original) {
			checkErr = fmt.Errorf("%w: LinuxIO has no record of moving %s to the trash; confirm restoring it to %s", bridgeipc.ErrForbidden, name, original)
		}
		return system, checkErr
	}

	// The nearest existing folder is checked before anything is made in it,
	// the destination again once it is open. It is reached without following
	// a link the session user could have planted on the way.
	dirPath := filepath.Dir(original)
	dir, missing, err := linksafe.OpenNearestDirNoUserLinks(root, dirPath, owner)
	if err != nil {
		return "", err
	}
	if _, err = checkSystem(dir); err != nil {
		dir.Close()
		return "", err
	}
	if dir, err = MkdirAllAt(dir, missing, PermDir, owner); err != nil {
		return "", err
	}
	defer dir.Close()
	system, err := checkSystem(dir)
	if err == nil {
		err = sameFilesystem(files, dir, dirPath)
	}
	if err != nil {
		return "", err
	}
	dst, replace, err := restoreTarget(dir, dirPath, filepath.Base(original), &item, opts)
	if err == nil && replace && owner.Set {
		err = checkReplace(name, &item, dst, recorded, system, owner, opts)
	}
	if err != nil {
		return "", err
	}
	if system {
		err = restoreIntoSystemFolder(ctx, files, name, item, dst, owner, replace)
	} else {
		err = placeTrashItem(ctx, files, name, dst, replace)
	}
	if err != nil {
		return "", err
	}
	return dst.path, nil
}

// checkReplace refuses, for a root bridge acting for the session user, a
// replace of what is at dst by name, the trashed item item describes, that
// the trash alone would decide: what a replace goes over must be fixed by a
// record or confirmed in the request, since Path= is the user's to rewrite
// after the page asked; and a folder merges neither into a folder the user
// does not own nor into one inside such a folder (system), since the
// could-have-written check vouches only for the top of the item, not for
// what a merge moves in, and a merge would put the user's entries where the
// user may not write.
func checkReplace(name string, item *unix.Stat_t, dst entry, recorded, system bool, owner Owner, opts RestoreOptions) error {
	if !recorded && !slices.Contains(opts.Confirmed, dst.path) {
		return fmt.Errorf("%w: LinuxIO has no record of moving %s to the trash; confirm replacing %s", bridgeipc.ErrForbidden, name, dst.path)
	}
	if item.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil
	}
	var there unix.Stat_t
	if err := unix.Fstatat(int(dst.dir.Fd()), dst.name, &there, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &fs.PathError{Op: "fstatat", Path: dst.path, Err: err}
	}
	if system || (there.Mode&unix.S_IFMT == unix.S_IFDIR && there.Uid != owner.UID) {
		return fmt.Errorf("%w: a folder is not merged into %s, which you do not own; keep both, or restore it into a folder you choose", bridgeipc.ErrForbidden, dst.path)
	}
	return nil
}

// restoreInto moves name, the trashed item in files that item describes,
// into the folder opts.Into as opts.Name, or under its name in the trash.
// Folder and name are the user's choice in this request, a move the user
// asks for now as filebrowser.move_batch is, not a claim read from the trash,
// which the session user could have edited: so neither the confirmation nor
// the could-have-written rule applies, as they do not to moving the item out
// of the trash by hand. The folder must exist and is reached as a move
// reaches it, without following a link anyone but root could have planted on
// the way, and the item goes in by rename only.
func restoreInto(ctx context.Context, root *fsroot.FSRoot, files *os.File, name string, item *unix.Stat_t, owner Owner, opts RestoreOptions) (string, error) {
	into, base := fsroot.CleanAbsPath(opts.Into), opts.Name
	if base == "" {
		base = name
	}
	if base == "." || base == ".." || strings.ContainsAny(base, "/\x00") {
		return "", fmt.Errorf("%w: %q is not a name", bridgeipc.ErrInvalidArgs, base)
	}
	dir, err := linksafe.OpenDirNoUserLinks(root, into, owner)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err = sameFilesystem(files, dir, into); err != nil {
		return "", err
	}
	dst, replace, err := restoreTarget(dir, into, base, item, opts)
	if err != nil {
		return "", err
	}
	if err = placeTrashItem(ctx, files, name, dst, replace); err != nil {
		return "", err
	}
	return dst.path, nil
}

// sameFilesystem refuses dir, the open folder at dirPath, when it is on
// another filesystem than files, the trash's files folder: a restore renames
// and does not copy an item to another filesystem.
func sameFilesystem(files, dir *os.File, dirPath string) error {
	var trash, dest unix.Stat_t
	if err := unix.Fstat(int(files.Fd()), &trash); err != nil {
		return &fs.PathError{Op: "fstat", Path: files.Name(), Err: err}
	}
	if err := unix.Fstat(int(dir.Fd()), &dest); err != nil {
		return &fs.PathError{Op: "fstat", Path: dirPath, Err: err}
	}
	if trash.Dev != dest.Dev {
		return fmt.Errorf("%s is on another filesystem than the trash, and a restore does not copy", dirPath)
	}
	return nil
}

// restoreTarget returns where in dir, the open folder at dirPath, the item
// item describes goes back as base, and whether something there is to be
// replaced: base when nothing is there; when something is, the first free
// "name (copy)" name with opts.KeepBoth, base with opts.Replace, and
// otherwise ErrNameTaken.
func restoreTarget(dir *os.File, dirPath, base string, item *unix.Stat_t, opts RestoreOptions) (entry, bool, error) {
	dst := entry{dir: dir, name: base, path: filepath.Join(dirPath, base)}
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), base, &st, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case errors.Is(err, unix.ENOENT):
		return dst, false, nil
	case err != nil:
		return entry{}, false, &fs.PathError{Op: "fstatat", Path: dst.path, Err: err}
	case opts.KeepBoth:
		if dst.name, err = freeCopyName(dir, dirPath, base, item.Mode&unix.S_IFMT == unix.S_IFDIR); err != nil {
			return entry{}, false, err
		}
		dst.path = filepath.Join(dirPath, dst.name)
		return dst, false, nil
	case opts.Replace:
		return dst, true, nil
	}
	return entry{}, false, fmt.Errorf("%w: %s %w", bridgeipc.ErrConflict, dst.path, ErrNameTaken)
}

// freeCopyName returns the first name for base, as CopyName counts them,
// that nothing in dir, the open folder at dirPath, has.
func freeCopyName(dir *os.File, dirPath, base string, isDir bool) (string, error) {
	for n := int64(1); n < 1000; n++ {
		name := CopyName(base, isDir, n)
		var st unix.Stat_t
		err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return name, nil
		}
		if err != nil {
			return "", &fs.PathError{Op: "fstatat", Path: filepath.Join(dirPath, name), Err: err}
		}
	}
	return CopyName(base, isDir, time.Now().Unix()), nil
}

// placeTrashItem moves name from the open folder from to dst. Without
// replace nothing at dst is ever replaced (RENAME_NOREPLACE); with it the
// move goes over what is there as a move with overwrite does, through
// handles: a file replaces the one there, which is set aside until the move
// is done, and a folder merges into the folder there entry by entry. A
// file and a folder never replace each other.
func placeTrashItem(ctx context.Context, from *os.File, name string, dst entry, replace bool) error {
	if !replace {
		return renameNoReplace(from, name, dst.dir, dst.name)
	}
	src := entry{dir: from, name: name, path: filepath.Join(from.Name(), name)}
	info, err := src.lstat()
	if err != nil {
		return err
	}
	state := newCopyState(ctx, true)
	state.dstBase, state.dstBasePath = dst.dir, filepath.Dir(dst.path)
	opts := &OperationCallbacks{Cancel: func() bool { return ctx.Err() != nil }}
	return transferEntry(src, dst, info, true, opts, state, MoveFileOptions{})
}

// restoreDestination returns where the trashed item name, which st
// describes, goes back to and whether a record says so: for a root bridge
// acting for owner the record of the move, when there is one, otherwise the
// Path= of its .trashinfo in info.
func restoreDestination(root *fsroot.FSRoot, trash string, info *os.File, name string, st *unix.Stat_t, owner Owner) (string, bool) {
	if owner.Set {
		if record, ok := trashRecordOf(int(owner.UID), st, trash); ok {
			return record.Path, true
		}
	}
	original, _ := readTrashInfo(root, trash, info, name)
	return original, false
}

// restoreNeedsConfirmation reports whether a restore to original, which no
// record vouches for, goes into a folder the session user does not own, as
// the restore judges the nearest folder of original that exists.
func restoreNeedsConfirmation(root *fsroot.FSRoot, original string, owner Owner) bool {
	dir, _, err := linksafe.OpenNearestDirNoUserLinks(root, filepath.Dir(original), owner)
	if err != nil {
		return false // the restore will say why
	}
	defer dir.Close()
	system, err := intoSystemFolder(dir, filepath.Dir(original), owner, false)
	return err == nil && system
}

// userCouldWrite reports whether the session user could have made or written
// the trashed item st describes: it is theirs, or a group or everyone may
// write it (whichever group: the user may be in it). A symlink's mode means
// nothing. An item they could not have written may still have been moved into
// the trash by them, but its content is not theirs (backend audit item 132).
func userCouldWrite(st *unix.Stat_t, owner Owner) bool {
	if st.Uid == owner.UID {
		return true
	}
	return st.Mode&unix.S_IFMT != unix.S_IFLNK && st.Mode&0o022 != 0
}

// intoSystemFolder reports whether dir, the open folder at dirPath, is one a
// root bridge restores into for the session user (owner set) although that
// user does not own it, and refuses it for an item the user could have
// written, which their own trash cannot vouch for.
func intoSystemFolder(dir *os.File, dirPath string, owner Owner, userWritten bool) (bool, error) {
	if !owner.Set {
		return false, nil
	}
	info, err := dir.Stat()
	if err != nil || HandsOverIn(info, owner) {
		return false, err
	}
	if userWritten {
		return true, fmt.Errorf("%w: restore into %s needs an item you neither own nor can write, as one you could have changed may have been planted in your trash; move it out of the trash by hand instead", bridgeipc.ErrForbidden, dirPath)
	}
	return true, nil
}

var errTrashItemReplaced = errors.New("trashed item was replaced")

// restoreIntoSystemFolder moves name from the trash's files folder to dst,
// in a folder the session user does not own, over what is there with
// replace (see placeTrashItem). The item is staged in a root-only folder
// inside the trash and checked there (see placeStaged), so nothing reaches
// that folder before it passes.
func restoreIntoSystemFolder(ctx context.Context, files *os.File, name string, item unix.Stat_t, dst entry, owner Owner, replace bool) error {
	stage, err := stageTrashItem(files, name)
	if err != nil {
		return err
	}
	defer stage.Close()
	return placeStaged(ctx, stage, files, name, item, dst, owner, replace)
}

// stageTrashItem moves name from files into a new folder only the bridge may
// write, made in files so it is on the same filesystem, and returns that
// folder open. The user owns files and may rename the folder there but not
// change what is in it, and the handle stays on it whatever its name.
func stageTrashItem(files *os.File, name string) (*os.File, error) {
	stageName := ".linuxio-restore-" + rand.Text()
	if err := unix.Mkdirat(int(files.Fd()), stageName, 0o700); err != nil {
		return nil, &fs.PathError{Op: "mkdirat", Path: filepath.Join(files.Name(), stageName), Err: err}
	}
	stage, err := openFreshStage(files, stageName)
	if err == nil {
		if err = renameNoReplace(files, name, stage, name); err != nil {
			_ = stage.Close()
		}
	}
	if err != nil {
		_ = unix.Unlinkat(int(files.Fd()), stageName, unix.AT_REMOVEDIR)
		return nil, err
	}
	return stage, nil
}

// openFreshStage opens the staging folder stageTrashItem just made, refusing
// any other: between mkdirat and openat the user, who owns files, could swap
// in another folder root owns, such as one root trashed earlier with content.
// Only a fresh one (the bridge's and empty, see IsFreshDir) that no group or
// everyone may write is taken.
func openFreshStage(files *os.File, stageName string) (*os.File, error) {
	stage, err := linksafe.OpenDirAt(files, stageName)
	if err != nil {
		return nil, err
	}
	info, err := stage.Stat()
	fresh := false
	if err == nil {
		fresh, err = IsFreshDir(stage, info)
	}
	if err == nil && (!fresh || info.Mode().Perm()&0o022 != 0) {
		err = fmt.Errorf("staging folder %s was replaced", stage.Name())
	}
	if err != nil {
		_ = stage.Close()
		return nil, err
	}
	return stage, nil
}

// placeStaged moves the staged item from stage straight to dst (see
// placeTrashItem) once it is checked there, out of the user's reach, to be
// item, the one checked in the trash, and still not one the user could have
// written. Otherwise, or with what a merge left of it, it goes back to files.
// If even that fails (the user took its name meanwhile) the staging folder,
// item and all, is handed to the user, so root vouches for nothing left in
// it. An emptied staging folder is removed.
func placeStaged(ctx context.Context, stage, files *os.File, name string, item unix.Stat_t, dst entry, owner Owner, replace bool) error {
	var st unix.Stat_t
	err := unix.Fstatat(int(stage.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case err != nil:
		err = &fs.PathError{Op: "fstatat", Path: filepath.Join(stage.Name(), name), Err: err}
	case st.Dev != item.Dev || st.Ino != item.Ino || userCouldWrite(&st, owner):
		err = errTrashItemReplaced
	default:
		err = placeTrashItem(ctx, stage, name, dst, replace)
	}
	if err != nil {
		if backErr := renameNoReplace(stage, name, files, name); backErr != nil {
			return errors.Join(err, fmt.Errorf("left in %s: %w", stage.Name(), backErr), HandOver(stage, owner, 0o700, false))
		}
	}
	_ = unix.Unlinkat(int(files.Fd()), filepath.Base(stage.Name()), unix.AT_REMOVEDIR)
	return err
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
	files, info, err := openTrash(root, trash, home, Owner{UID: uint32(uid), Set: os.Geteuid() == 0}, false)
	if err != nil {
		return 0, fmt.Errorf("purge %s: %w", name, err)
	}
	defer files.Close()
	defer info.Close()
	var item unix.Stat_t
	statErr := unix.Fstatat(int(files.Fd()), name, &item, unix.AT_SYMLINK_NOFOLLOW)
	var removed int64
	err = removeAllAt(ctx, files, name, func() {
		removed++
		if progress != nil {
			progress(removed)
		}
	})
	if err != nil {
		return removed, fmt.Errorf("purge %s: %w", name, err)
	}
	removeTrashInfo(info, name)
	if statErr == nil {
		dropTrashRecord(uid, &item)
	}
	return removed, nil
}

// removeAllAt deletes name in dir and everything below it through handles:
// each folder is opened relative to its parent without following a symlink,
// and a symlink is removed, never followed, so nothing outside dir is touched
// however the user rearranges the tree meanwhile. removed runs per entry.
func removeAllAt(ctx context.Context, dir *os.File, name string, removed func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := unix.Unlinkat(int(dir.Fd()), name, 0)
	if err == nil {
		removed()
		return nil
	}
	unlinkErr := &fs.PathError{Op: "unlinkat", Path: filepath.Join(dir.Name(), name), Err: err}
	if !errors.Is(err, unix.EISDIR) && !errors.Is(err, unix.EPERM) {
		return unlinkErr
	}
	sub, err := linksafe.OpenDirAt(dir, name)
	if errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
		return unlinkErr // not a folder, so the unlink failed for its own reason
	}
	if err != nil {
		return err
	}
	children, err := sub.Readdirnames(-1)
	for i := 0; err == nil && i < len(children); i++ {
		err = removeAllAt(ctx, sub, children[i], removed)
	}
	_ = sub.Close()
	if err != nil {
		return err
	}
	if err := unix.Unlinkat(int(dir.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		return &fs.PathError{Op: "rmdir", Path: filepath.Join(dir.Name(), name), Err: err}
	}
	removed()
	return nil
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

// readTrashInfo returns the original path and deletion time name's
// .trashinfo in info, trash's info folder, records; both are zero when info
// is nil or the file is missing, malformed or not a regular file. It is opened
// without following a symlink, or blocking on a FIFO, the user put at its
// name.
func readTrashInfo(root *fsroot.FSRoot, trash string, info *os.File, name string) (string, time.Time) {
	if info == nil {
		return "", time.Time{}
	}
	fd, err := unix.Openat(int(info.Fd()), name+".trashinfo", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", time.Time{}
	}
	// Only a regular file is read: a read on a FIFO with a writer would wait.
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return "", time.Time{}
	}
	f := os.NewFile(uintptr(fd), filepath.Join(info.Name(), name+".trashinfo"))
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

// removeTrashInfo drops name's .trashinfo from info, trash's info folder.
func removeTrashInfo(info *os.File, name string) {
	if info == nil {
		return
	}
	if err := unix.Unlinkat(int(info.Fd()), name+".trashinfo", 0); err != nil && !errors.Is(err, unix.ENOENT) {
		slog.Debug("failed to remove trash info", "component", "filebrowser", "subsystem", "trash", "path", filepath.Join(info.Name(), name+".trashinfo"), "error", err)
	}
}
