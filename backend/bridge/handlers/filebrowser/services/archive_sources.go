package services

import (
	"archive/tar"
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

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// ArchiveSources is how a compress reads the files it archives. Without
// Owner set they are read as the user's own programs would read them: a link
// to a file adds the file, a link to a folder is left out.
//
// Owner is the session user a root bridge compresses for. Then every source
// is reached as openDirNoUserLinks reaches a folder and read through handles
// without following a symlink, so a folder swapped for a link mid-walk is
// never entered. A link inside a compressed folder is followed, as above,
// only where that user cannot have planted it; one in a folder they can
// write is left out and passed to Skipped, so root never copies what it
// points at into an archive the user can read.
type ArchiveSources struct {
	Owner   Owner
	Skipped func(path string, err error)
}

// archiveReader adds the sources of one archive to its writer.
type archiveReader struct {
	root    *fsroot.FSRoot
	tar     *tar.Writer
	zip     *zip.Writer
	opts    *OperationCallbacks
	sources ArchiveSources
	out     unix.Stat_t // the archive being written, never added to itself
}

func newArchiveReader(root *fsroot.FSRoot, out *os.File, tarWriter *tar.Writer, zipWriter *zip.Writer, opts *OperationCallbacks, sources ArchiveSources) (*archiveReader, error) {
	r := &archiveReader{root: root, tar: tarWriter, zip: zipWriter, opts: opts, sources: sources}
	if err := unix.Fstat(int(out.Fd()), &r.out); err != nil {
		return nil, &fs.PathError{Op: "fstat", Path: out.Name(), Err: err}
	}
	return r, nil
}

// add adds the source at path, a file or a folder and everything below it,
// under its own name. skipPath is the archive's own path.
func (r *archiveReader) add(path, skipPath string) error {
	if !r.sources.Owner.Set {
		return addFile(r.root, path, r.tar, r.zip, false, r.opts, skipPath)
	}
	path = fsroot.CleanAbsPath(path)
	at, err := entryFollowingNoUserLinks(r.root, path, r.sources.Owner)
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
	dir, err := openDirAt(at.dir, at.name)
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
		sub, err := openDirAt(dir, name)
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
// only when the session user cannot have planted the link or any link it
// leads through. Otherwise the link is left out and reported.
func (r *archiveReader) addLink(dir *os.File, name, archivePath string) error {
	path := filepath.Join(dir.Name(), name)
	target, isLink := linkTarget(dir, name)
	if !isLink {
		return nil // replaced since it was seen
	}
	plantable, err := userCouldPlant(dir, r.sources.Owner)
	if err != nil {
		return err
	}
	if plantable {
		r.skip(path, fmt.Errorf("left out: a link to %s in a folder you can write", linkDestination(dir, target)))
		return nil
	}
	at, err := entryFollowingNoUserLinks(r.root, path, r.sources.Owner)
	if errors.Is(err, bridgeipc.ErrForbidden) {
		r.skip(path, fmt.Errorf("left out: a link to %s that leads through a link in a folder you can write", linkDestination(dir, target)))
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
