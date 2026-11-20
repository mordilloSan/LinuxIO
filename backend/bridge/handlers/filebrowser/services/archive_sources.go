package services

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	zip "github.com/klauspost/compress/zip"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
)

// ArchiveSources is how a compress reads the files it archives. Without
// Owner set they are read as the user's own programs would read them: a link
// to a file adds the file, a link to a folder is left out.
//
// Owner is the session user a root bridge compresses for. Then every source
// is reached as linksafe.OpenDirNoUserLinks reaches a folder and read through handles
// without following a symlink, so a folder swapped for a link mid-walk is
// never entered. A link inside a compressed folder is followed, as above,
// only where nobody but root can have planted it; one in any other folder
// is left out and passed to Skipped, so root never copies what it points at
// into an archive the user can read.
type ArchiveSources struct {
	Owner   Owner
	Skipped func(path string, err error)
}

// archiveReader adds the sources of one archive to its writer. ctx is the
// archive's, which the resolution of each source and link stops on.
type archiveReader struct {
	ctx     context.Context
	root    *fsroot.FSRoot
	tar     *tar.Writer
	zip     *zip.Writer
	opts    *OperationCallbacks
	sources ArchiveSources
	out     unix.Stat_t // the archive being written, never added to itself
}

func newArchiveReader(ctx context.Context, root *fsroot.FSRoot, out *os.File, tarWriter *tar.Writer, zipWriter *zip.Writer, opts *OperationCallbacks, sources ArchiveSources) (*archiveReader, error) {
	r := &archiveReader{ctx: ctx, root: root, tar: tarWriter, zip: zipWriter, opts: opts, sources: sources}
	if err := unix.Fstat(int(out.Fd()), &r.out); err != nil {
		return nil, &fs.PathError{Op: "fstat", Path: out.Name(), Err: err}
	}
	return r, nil
}

// add adds the source at path, a file or a folder and everything below it,
// under its own name. skipPath is the archive's own path.
func (r *archiveReader) add(path, skipPath string) error {
	if !r.sources.Owner.Set {
		return addFile(r.ctx, r.root, path, r.tar, r.zip, false, r.opts, skipPath)
	}
	path = fsroot.CleanAbsPath(path)
	at, err := entryFollowingNoUserLinks(r.ctx, path, r.sources.Owner)
	if err != nil {
		return err
	}
	defer at.dir.Close()
	var st unix.Stat_t
	if err = unix.Fstatat(int(at.dir.Fd()), at.name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &fs.PathError{Op: "fstatat", Path: at.path, Err: err}
	}
	base := filepath.Base(path)
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return r.addEntry(at.dir, at.name, base)
	}
	// The folder named in the request gets no entry of its own, as in a walk.
	dir, err := linksafe.OpenDirAt(at.dir, at.name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return r.addChildren(dir, base)
}

// addChildren adds what dir holds below archivePath, in name order. An entry
// that is gone or cannot be read is left out, as a walk leaves it out.
func (r *archiveReader) addChildren(dir *os.File, archivePath string) error {
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return &fs.PathError{Op: "readdir", Path: dir.Name(), Err: err}
	}
	slices.Sort(names)
	for _, name := range names {
		err := r.addEntry(dir, name, archivePath+"/"+name)
		if err != nil && !skipArchiveEntry(filepath.Join(dir.Name(), name), err) {
			return err
		}
	}
	return nil
}

// addEntry adds name in dir as archivePath, without following a symlink at
// name except as addLink does.
func (r *archiveReader) addEntry(dir *os.File, name, archivePath string) error {
	if r.opts.IsCancelled() {
		return ErrAborted
	}
	var st unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &fs.PathError{Op: "fstatat", Path: filepath.Join(dir.Name(), name), Err: err}
	}
	if st.Dev == r.out.Dev && st.Ino == r.out.Ino {
		return nil
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		sub, err := linksafe.OpenDirAt(dir, name)
		if err != nil {
			return err
		}
		defer sub.Close()
		if err := writeArchiveDirectoryHeader(archivePath, time.Unix(st.Mtim.Unix()), r.tar, r.zip); err != nil {
			return err
		}
		return r.addChildren(sub, archivePath)
	case unix.S_IFREG:
		return r.addRegular(dir, name, archivePath)
	case unix.S_IFLNK:
		return r.addLink(dir, name, archivePath)
	default:
		// A FIFO, socket or device holds no content to archive, and opening a
		// FIFO would wait for a writer.
		return nil
	}
}

// addRegular adds the regular file name in dir, opened without following a
// symlink there or waiting on a FIFO swapped in since it was seen.
func (r *archiveReader) addRegular(dir *os.File, name, archivePath string) error {
	path := filepath.Join(dir.Name(), name)
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return &fs.PathError{Op: "openat", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	return writeArchiveFile(file, info, archivePath, r.zip, r.tar, r.opts)
}

// addLink adds what the symlink name in dir points at as a walk does, the
// file's content for a link to a file and nothing for a link to a folder, but
// only when nobody but root can have planted the link or any link it
// leads through. Otherwise the link is left out and reported.
func (r *archiveReader) addLink(dir *os.File, name, archivePath string) error {
	path := filepath.Join(dir.Name(), name)
	target, isLink := linksafe.LinkTarget(dir, name)
	if !isLink {
		return nil // replaced since it was seen
	}
	planted, err := linksafe.Plantable(dir)
	if err != nil {
		return err
	}
	if planted {
		r.skip(path, fmt.Errorf("left out: a link to %s in a folder that is not root's alone", linksafe.LinkDestination(dir, target)))
		return nil
	}
	at, err := entryFollowingNoUserLinks(r.ctx, path, r.sources.Owner)
	if errors.Is(err, bridgeipc.ErrForbidden) {
		r.skip(path, fmt.Errorf("left out: a link to %s that leads through a link in a folder that is not root's alone", linksafe.LinkDestination(dir, target)))
		return nil
	}
	if err != nil {
		return err
	}
	defer at.dir.Close()
	var st unix.Stat_t
	if err := unix.Fstatat(int(at.dir.Fd()), at.name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &fs.PathError{Op: "fstatat", Path: at.path, Err: err}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || (st.Dev == r.out.Dev && st.Ino == r.out.Ino) {
		return nil
	}
	return r.addRegular(at.dir, at.name, archivePath)
}

// skip reports a source left out of the archive.
func (r *archiveReader) skip(path string, err error) {
	slog.Debug("left a link out of an archive", "component", "filebrowser", "subsystem", "archive", "path", path, "error", err)
	if r.sources.Skipped != nil {
		r.sources.Skipped(path, err)
	}
}

// estimateSourceSize adds up the regular files at and below path for a root
// bridge compressing for owner. It reaches path as archiveReader.add does and
// walks through handles without following a symlink, so a planted link is
// neither entered nor measured; a link the archive follows to a file is not
// counted. An entry that is gone or cannot be read counts nothing.
func estimateSourceSize(ctx context.Context, path string, owner Owner) (int64, error) {
	at, err := entryFollowingNoUserLinks(ctx, path, owner)
	if err != nil {
		return 0, err
	}
	defer at.dir.Close()
	walk := sizeWalk{ctx: ctx}
	if err = walk.add(at.dir, at.name); err != nil {
		return 0, err
	}
	return walk.total, ctx.Err()
}

// sizeWalk adds up files through handles and counts the folders and the other
// entries it passes. By default it adds the apparent size of regular files,
// what an archive of them holds. With diskUsage it adds what entries take on
// disk (st_blocks times 512), as the indexer sizes the folders of the
// listing: every entry but a folder, a file with several links once, keyed by
// device and inode, and every folder it lists, its top folder included, by
// its own blocks. With oneMount it enters no folder of another mount than mount, so a
// mount point or a bind mount below where it starts counts as a folder and
// nothing more. That goes by the mount, where `du -x` goes by the device
// number: every btrfs subvolume has a device number of its own, and a nested
// one is part of the folder that holds it.
type sizeWalk struct {
	ctx       context.Context
	total     int64
	files     int64
	folders   int64
	seen      int
	mount     uint64
	oneMount  bool
	diskUsage bool
	linked    map[[2]uint64]struct{} // device and inode of files with several links
}

// mountOf returns the ID of the mount the open file is on.
func mountOf(f *os.File) (uint64, error) {
	var stx unix.Statx_t
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &stx); err != nil {
		return 0, &fs.PathError{Op: "statx", Path: f.Name(), Err: err}
	}
	if stx.Mask&unix.STATX_MNT_ID == 0 {
		return 0, &fs.PathError{Op: "statx", Path: f.Name(), Err: unix.EOPNOTSUPP}
	}
	return stx.Mnt_id, nil
}

// add counts name in dir, and what it holds when it is a folder.
func (w *sizeWalk) add(dir *os.File, name string) error {
	w.seen++
	if w.seen%sizeWalkCancelInterval == 0 {
		if err := w.ctx.Err(); err != nil {
			return err
		}
	}
	var st unix.Stat_t
	if unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return nil
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		w.folders++
		return w.addChildren(dir, name)
	case unix.S_IFREG:
		if !w.diskUsage {
			w.total += st.Size
		}
	}
	w.files++
	if w.diskUsage {
		w.addOnDisk(&st)
	}
	return nil
}

// addOnDisk adds what an entry takes on disk, a file with several links once.
func (w *sizeWalk) addOnDisk(st *unix.Stat_t) {
	if st.Nlink > 1 {
		key := [2]uint64{st.Dev, st.Ino}
		if _, counted := w.linked[key]; counted {
			return
		}
		if w.linked == nil {
			w.linked = make(map[[2]uint64]struct{})
		}
		w.linked[key] = struct{}{}
	}
	w.total += st.Blocks * 512
}

// addChildren counts what the folder name in parent holds. A folder below
// where the walk starts that cannot be opened or listed counts nothing.
func (w *sizeWalk) addChildren(parent *os.File, name string) error {
	dir, err := linksafe.OpenDirAt(parent, name)
	if err != nil {
		return nil
	}
	defer dir.Close()
	if err = w.addFolder(dir); err != nil && w.ctx.Err() == nil {
		return nil
	}
	return err
}

// addFolder counts what the open folder dir holds, and returns why it cannot
// when dir cannot be listed: the walk's top folder has no size to give then.
func (w *sizeWalk) addFolder(dir *os.File) error {
	// The mount is read from the folder opened, which is what is read below.
	if w.oneMount {
		mount, err := mountOf(dir)
		if err != nil {
			return err
		}
		if mount != w.mount {
			return nil
		}
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}
	if w.diskUsage {
		// A folder the walk lists takes blocks of its own, which the
		// indexer counts in a folder's size, that folder's included.
		var st unix.Stat_t
		if err = unix.Fstat(int(dir.Fd()), &st); err == nil {
			w.total += st.Blocks * 512
		}
	}
	for _, child := range names {
		if err := w.add(dir, child); err != nil {
			return err
		}
	}
	return nil
}
