package btrfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// The recycle bin of a shared folder (docs/shared-folders.md, "Network"):
// a folder at its top that Samba moves what is deleted over SMB into, and
// that linuxio-worker empties of what has been there too long. Both are
// root working in a folder its users may write, so both go through handles
// and follow no link (below.go).

// MakeFolder makes the folder at path, a path from the top level of the
// filesystem uuid, in a parent that is there, through the parent's handle,
// and leaves one already there as it is. mode is the mode asked of the
// kernel: in a parent with a default ACL the folder starts with that ACL,
// as far as mode allows, so 0777 has it follow its parent. A link at the
// name is refused.
func MakeFolder(ctx context.Context, uuid, path string, mode uint32) error {
	if err := ValidateSubvolumePath(path); err != nil {
		return err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return err
	}
	parentFD, err := openBelow(top, filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	name := filepath.Base(path)
	if err = unix.Mkdirat(parentFD, name, mode); err != nil && !errors.Is(err, unix.EEXIST) {
		return &fs.PathError{Op: "mkdir", Path: filepath.Join(top, path), Err: err}
	}
	// What is at the name, made now or before, must be a folder.
	fd, err := openAtBelow(parentFD, name)
	if err != nil {
		if errors.Is(err, ErrLinkOnPath) {
			return err
		}
		return &fs.PathError{Op: "open", Path: filepath.Join(top, path), Err: err}
	}
	unix.Close(fd)
	return nil
}

// NotAFolder says err is of a path that holds a link or a file where a
// folder was expected. Whoever may write a shared folder can put either
// where its recycle bin was, so neither is a failure of root's work there.
func NotAFolder(err error) bool {
	return errors.Is(err, ErrLinkOnPath) || errors.Is(err, unix.ENOTDIR)
}

// removeMaxDepth bounds how deep RemoveOlderThan goes: it holds one handle
// for each folder on its way down, and a user can nest folders without end.
const removeMaxDepth = 64

// recycleStampAttr is where RemoveOlderThan notes, on a folder, when it
// first saw it there. The trusted namespace is root's alone to read and to
// write. A var so unit tests, which are not root, can keep it elsewhere.
var recycleStampAttr = "trusted.linuxio.recycled"

// recycleNow is the time a folder first seen is stamped with; a var so
// tests can choose it.
var recycleNow = time.Now

// RemoveOlderThan deletes, in the folder at path and the folders below it,
// what has been there since before cutoff, and returns how many files and
// links went. The folder at path stays.
//
// A file or link has been there since the last change to the entry itself
// (its ctime), which a move into the folder sets and no user can set back;
// a time a user can choose, such as when a file was last written, would let
// them have someone else's file deleted at once. A folder moved in keeps
// the times of everything in it, so a folder has been there since a pass
// first saw it: it is stamped then (recycleStampAttr), nothing in it is
// deleted until the stamp is before cutoff, and it is removed once it is
// empty and its stamp is. Without the stamp, whoever moves a folder into a
// recycle bin would have root delete, at the next pass, files in it that
// they could not delete themselves.
//
// It works through handles and follows no link (a link is removed, never
// what it points to), stays on the filesystem and subvolume of the folder
// at path, and goes no deeper than removeMaxDepth. A folder at path that is
// not there is nothing to do.
//
// A folder that cannot be noted (its pool or its limit is full, or its
// attributes are) is new at every pass: it is kept with what it holds, and
// the pass goes on to the rest, which is what frees the room. It fails no
// pass, since whoever may write the bin can fill a folder's attributes.
func RemoveOlderThan(ctx context.Context, uuid, path string, cutoff time.Time) (int, error) {
	if err := ValidateSubvolumePath(path); err != nil {
		return 0, err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return 0, err
	}
	fd, err := openBelow(top, path)
	if errors.Is(err, unix.ENOENT) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return 0, &fs.PathError{Op: "stat", Path: filepath.Join(top, path), Err: err}
	}
	sweep := recycleSweep{ctx: ctx, dev: st.Dev, cutoff: cutoff}
	_, err = sweep.folder(fd, st.Ino, 0, false)
	if sweep.unnoted > 0 {
		slog.Warn("recycle bin folders kept, the time they were first seen could not be noted",
			"uuid", uuid, "path", path, "folders", sweep.unnoted, "error", sweep.noteErr)
	}
	return sweep.removed, err
}

// recycleSweep is one pass of RemoveOlderThan: the device it stays on, the
// cut-off, how many files and links it removed so far, and how many folders
// it could not note, with the first reason.
type recycleSweep struct {
	ctx     context.Context
	dev     uint64
	cutoff  time.Time
	removed int
	unnoted int
	noteErr error
}

// folder sweeps the folder open at fd, whose inode is ino, and says whether
// it left it empty. kept says a folder above it is too new to the place for
// anything below it to go. The folder is read a part at a time, so one with
// more entries than memory holds is still swept.
func (s *recycleSweep) folder(fd int, ino uint64, depth int, kept bool) (empty bool, err error) {
	dup, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, &fs.PathError{Op: "open", Path: ".", Err: err}
	}
	dir := os.NewFile(uintptr(dup), ".")
	defer dir.Close()
	empty = true
	for {
		names, readErr := dir.Readdirnames(1024)
		for _, name := range names {
			gone, err := s.entry(fd, ino, name, depth, kept)
			if err != nil {
				return false, err
			}
			empty = empty && gone
		}
		if errors.Is(readErr, io.EOF) {
			return empty, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

// entry sweeps the entry name of the folder open at dirFD, whose inode is
// dirIno, and says whether it is gone.
func (s *recycleSweep) entry(dirFD int, dirIno uint64, name string, depth int, kept bool) (gone bool, err error) {
	if err = s.ctx.Err(); err != nil {
		return false, err
	}
	var st unix.Stat_t
	if err = unix.Fstatat(dirFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return true, nil
		}
		return false, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	if st.Mode&unix.S_IFMT == unix.S_IFDIR {
		return s.subfolder(dirFD, dirIno, name, st, depth, kept)
	}
	if kept || !time.Unix(st.Ctim.Unix()).Before(s.cutoff) {
		return false, nil
	}
	err = unix.Unlinkat(dirFD, name, 0)
	if errors.Is(err, unix.EISDIR) {
		// A folder took the name since its stat: the next pass's to see.
		return false, nil
	}
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return false, &fs.PathError{Op: "unlink", Path: name, Err: err}
	}
	s.removed++
	return true, nil
}

// subfolder sweeps the folder name of the folder open at dirFD, whose stat
// before it was opened is st, and removes it when that left it empty and it
// was first seen there before the cut-off; gone says it was removed. A
// folder of another filesystem or subvolume, one too deep, and one that
// became something else since its stat are left alone.
func (s *recycleSweep) subfolder(dirFD int, dirIno uint64, name string, st unix.Stat_t, depth int, kept bool) (gone bool, err error) {
	if st.Dev != s.dev || depth >= removeMaxDepth {
		return false, nil
	}
	fd, err := openAtBelow(dirFD, name)
	if err != nil {
		// Swapped for a link or removed since its stat: not ours to follow.
		if errors.Is(err, ErrLinkOnPath) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
			return false, nil
		}
		return false, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	defer unix.Close(fd)
	var opened unix.Stat_t
	if err = unix.Fstat(fd, &opened); err != nil {
		return false, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	if opened.Dev != s.dev {
		return false, nil
	}
	seen, err := firstSeen(fd, dirIno)
	if err != nil {
		// Not noted, so seen now: kept, and the pass goes on.
		if s.unnoted++; s.noteErr == nil {
			s.noteErr = fmt.Errorf("%s: %w", name, err)
		}
	}
	kept = kept || !seen.Before(s.cutoff)
	empty, err := s.folder(fd, opened.Ino, depth+1, kept)
	if err != nil || !empty || kept {
		return false, err
	}
	// A folder someone put something in meanwhile is not empty, and stays.
	return unix.Unlinkat(dirFD, name, unix.AT_REMOVEDIR) == nil, nil
}

// firstSeen returns when a pass first saw the folder open at fd in the
// folder whose inode is parent, and stamps it with now when it carries no
// stamp of that place: none at all, one written while it was in another
// folder, or one that cannot be read.
func firstSeen(fd int, parent uint64) (time.Time, error) {
	buf := make([]byte, 64)
	if n, err := unix.Fgetxattr(fd, recycleStampAttr, buf); err == nil {
		var seconds int64
		var where uint64
		if _, scanErr := fmt.Sscanf(string(buf[:n]), "%d:%d", &seconds, &where); scanErr == nil && where == parent {
			return time.Unix(seconds, 0), nil
		}
	}
	now := recycleNow()
	stamp := fmt.Sprintf("%d:%d", now.Unix(), parent)
	if err := unix.Fsetxattr(fd, recycleStampAttr, []byte(stamp), 0); err != nil {
		return now, err
	}
	return now, nil
}
