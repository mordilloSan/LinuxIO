package services

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// The trash and its .trashinfo files are the session user's to edit, so the
// Path= a .trashinfo holds cannot tell a root bridge where to put an item back
// (backend audit item 132). A root bridge acting for that user therefore also
// records where it trashed each item from, in a folder only root can write:
// one file per item, <trashRecordsDir>/<uid>/<dev>-<ino>.json, named by the
// trashed item's device and inode, which a move into the trash keeps and a
// rename inside it does not change. Two users' records never share a folder.
// A restore uses the record and ignores Path=; the record goes when the item
// is restored or purged, and a purge also drops the records of items no
// longer in the trash (emptied by another program, say).

// trashRecordsDir holds the records, one folder per user. Tests point it
// elsewhere.
var trashRecordsDir = filepath.Join(version.DataDir, "trash")

const (
	// maxTrashRecords bounds one user's records. Past it stale records are
	// dropped, and if that does not free room an item is trashed without one:
	// restoring it into a system folder then asks first, as for an item
	// another program trashed.
	maxTrashRecords     = 10000
	maxTrashRecordBytes = 16 << 10
	trashRecordSuffix   = ".json"
)

var errTrashRecordsFull = errors.New("too many trash records")

// trashRecord is where a root bridge trashed an item from, and which item.
type trashRecord struct {
	Path  string `json:"path"`  // where the item was, absolute
	Trash string `json:"trash"` // the trash folder it went to
	Type  uint32 `json:"type"`  // its file type (S_IFMT bits)
	UID   uint32 `json:"uid"`   // its owner
}

// matches reports whether r was written for the item st describes, sitting
// in trash.
func (r trashRecord) matches(st *unix.Stat_t, trash string) bool {
	return r.Trash == trash && r.Type == st.Mode&unix.S_IFMT && r.UID == st.Uid && filepath.IsAbs(r.Path)
}

func trashRecordName(st *unix.Stat_t) string {
	return strconv.FormatUint(st.Dev, 10) + "-" + strconv.FormatUint(st.Ino, 10) + trashRecordSuffix
}

// trashRecordFolder returns uid's record folder, made with mode 0700 when
// create is set. The folder and the one above it must be real folders the
// bridge owns that no group or everyone may write.
func trashRecordFolder(uid int, create bool) (string, error) {
	dir := filepath.Join(trashRecordsDir, strconv.Itoa(uid))
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	for _, path := range []string{trashRecordsDir, dir} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || int(st.Uid) != os.Geteuid() || info.Mode().Perm()&0o022 != 0 {
			return "", fmt.Errorf("trash record folder %s is not the bridge's own", path)
		}
	}
	return dir, nil
}

// recordTrashedAt records, for a root bridge acting for owner (owner set),
// where name in dir, the item about to move from original into trash, came
// from, and returns the item as fstatat saw it, or nil when there is no
// record. Without a record a restore into a system folder asks first, so a
// failure here is logged, not fatal.
func recordTrashedAt(ctx context.Context, root *fsroot.FSRoot, home string, owner Owner, dir *os.File, name, original, trash string) *unix.Stat_t {
	if !owner.Set {
		return nil
	}
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		err = recordTrashed(ctx, root, home, int(owner.UID), &st, original, trash)
	}
	if err != nil {
		slog.Warn("failed to record where a trashed item came from", "component", "filebrowser", "subsystem", "trash", "path", original, "error", err)
		return nil
	}
	return &st
}

// recordTrashed records, for a root bridge acting for uid, that the item st
// describes came from original and goes to trash. It drops stale records
// first when uid has too many.
func recordTrashed(ctx context.Context, root *fsroot.FSRoot, home string, uid int, st *unix.Stat_t, original, trash string) error {
	dir, err := trashRecordFolder(uid, true)
	if err != nil {
		return err
	}
	n, err := countTrashRecords(dir)
	if err == nil && n >= maxTrashRecords {
		pruneTrashRecords(ctx, root, home, uid)
		n, err = countTrashRecords(dir)
	}
	if err != nil {
		return err
	}
	if n >= maxTrashRecords {
		return errTrashRecordsFull
	}
	data, err := json.Marshal(trashRecord{Path: original, Trash: trash, Type: st.Mode & unix.S_IFMT, UID: st.Uid})
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, trashRecordName(st)), data, 0o600)
}

func countTrashRecords(dir string) (int, error) {
	f, err := os.Open(dir)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	names, err := f.Readdirnames(maxTrashRecords)
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	return len(names), err
}

// trashRecordOf returns the record of the item st describes, in trash, when
// there is one written for that item. A bridge that is not root cannot read
// the records and gets none.
func trashRecordOf(uid int, st *unix.Stat_t, trash string) (trashRecord, bool) {
	dir, err := trashRecordFolder(uid, false)
	if err != nil {
		return trashRecord{}, false
	}
	record, err := readTrashRecord(filepath.Join(dir, trashRecordName(st)))
	if err != nil || !record.matches(st, trash) {
		return trashRecord{}, false
	}
	return record, true
}

func readTrashRecord(path string) (trashRecord, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return trashRecord{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTrashRecordBytes))
	if err != nil {
		return trashRecord{}, err
	}
	var record trashRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return trashRecord{}, err
	}
	return record, nil
}

// dropTrashRecord removes the record of the item st describes, if any; a nil
// st is no item.
func dropTrashRecord(uid int, st *unix.Stat_t) {
	if st == nil {
		return
	}
	dir, err := trashRecordFolder(uid, false)
	if err != nil {
		return
	}
	path := filepath.Join(dir, trashRecordName(st))
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Debug("failed to remove trash record", "component", "filebrowser", "subsystem", "trash", "path", path, "error", err)
	}
}

// PruneTrashRecords drops uid's records whose item is no longer in the trash
// the record names, such as items another program purged when it emptied the
// trash. Each trash is reached as ListTrash reaches it.
func PruneTrashRecords(ctx context.Context, home string, uid int) {
	root, err := fsroot.Open()
	if err != nil {
		slog.Debug("failed to open filesystem root to prune trash records", "component", "filebrowser", "subsystem", "trash", "error", err)
		return
	}
	defer root.Close()
	pruneTrashRecords(ctx, root, home, uid)
}

func pruneTrashRecords(ctx context.Context, root *fsroot.FSRoot, home string, uid int) {
	dir, err := trashRecordFolder(uid, false)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("failed to read trash records", "component", "filebrowser", "subsystem", "trash", "path", dir, "error", err)
		return
	}
	stale := staleTrashRecords{root: root, home: home, uid: uid, trashes: trashDirs(root, filepath.Clean(home), uid), held: map[string]map[string]bool{}}
	removed := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		name := entry.Name()
		if !strings.HasSuffix(name, trashRecordSuffix) {
			continue
		}
		path := filepath.Join(dir, name)
		if !stale.is(path, name) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Debug("failed to remove trash record", "component", "filebrowser", "subsystem", "trash", "path", path, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Debug("pruned trash records", "component", "filebrowser", "subsystem", "trash", "uid", uid, "count", removed)
	}
}

// staleTrashRecords tells which of uid's records no longer have their item
// in the trash they name.
type staleTrashRecords struct {
	root    *fsroot.FSRoot
	home    string
	uid     int
	trashes []string                   // uid's trashes that exist
	held    map[string]map[string]bool // trash -> record names of what it holds, nil when it cannot be read
}

// is reports whether the record at path, called name, is stale: unreadable,
// naming a trash that no longer exists, or naming an item its trash no longer
// holds. A record whose trash cannot be read is kept.
func (s *staleTrashRecords) is(path, name string) bool {
	record, err := readTrashRecord(path)
	if err != nil || !slices.Contains(s.trashes, record.Trash) {
		return true
	}
	held, seen := s.held[record.Trash]
	if !seen {
		held = trashedRecordNames(s.root, record.Trash, s.home, s.uid)
		s.held[record.Trash] = held
	}
	return held != nil && !held[name]
}

// trashedRecordNames returns the record names of the items trash holds, or
// nil when it cannot be opened safely or read.
func trashedRecordNames(root *fsroot.FSRoot, trash, home string, uid int) map[string]bool {
	files, info, err := openTrash(root, trash, home, Owner{UID: uint32(uid), Set: os.Geteuid() == 0}, false)
	if err != nil {
		return nil
	}
	defer files.Close()
	if info != nil {
		defer info.Close()
	}
	entries, err := files.Readdirnames(-1)
	if err != nil {
		return nil
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		var st unix.Stat_t
		if unix.Fstatat(int(files.Fd()), entry, &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
			names[trashRecordName(&st)] = true
		}
	}
	return names
}
