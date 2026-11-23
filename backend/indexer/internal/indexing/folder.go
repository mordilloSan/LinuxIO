package indexing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// ErrFolderTooLarge is returned by ReadFolder for a folder with more entries
// than its caller is willing to look at.
var ErrFolderTooLarge = errors.New("folder has too many entries")

// FolderContents is what one folder holds, one level deep.
type FolderContents struct {
	// Files are the entries that are not folders. SizeContribution is not
	// set: which link of a hard-linked file counts is the store's to say.
	Files []IndexEntry
	// Folders are the paths of the subfolders the index covers.
	Folders []string
}

// ReadFolder reads the entries of one folder without going into its
// subfolders, leaving out what a scan leaves out. Like a scan it reaches the
// folder without following a symlink, and looks at each entry without
// following one. It reads at most limit entries and returns ErrFolderTooLarge
// when the folder has more, and ErrNotIndexed for a folder the index leaves
// out.
func (idx *Index) ReadFolder(ctx context.Context, path string, limit int) (FolderContents, error) {
	folder := indexerapi.NormalizePath(path)
	if idx.shouldSkip(true, folder) {
		return FolderContents{}, ErrNotIndexed
	}
	dir, err := openScanRoot(strings.TrimRight(idx.Path, "/") + folder)
	if err != nil {
		return FolderContents{}, err
	}
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			slog.Warn("failed to close directory", "path", dir.Name(), "error", closeErr)
		}
	}()

	var contents FolderContents
	read := 0
	for {
		// Names only, as the scan reads a folder (indexOpenDirectory).
		names, readErr := dir.Readdirnames(readDirChunk)
		if read += len(names); read > limit {
			return FolderContents{}, ErrFolderTooLarge
		}
		for _, name := range names {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return FolderContents{}, ctxErr
			}
			idx.readFolderEntry(dir, folder, name, &contents)
		}
		if errors.Is(readErr, io.EOF) {
			return contents, nil
		}
		if readErr != nil {
			return FolderContents{}, readErr
		}
	}
}

// readFolderEntry adds one entry of an open folder to contents. An entry that
// cannot be looked at is left out, as a scan leaves it out.
func (idx *Index) readFolderEntry(dir *os.File, folder, name string, contents *FolderContents) {
	childPath := makeChildRelativePath(folder, name)
	info, err := statChild(dir, name)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("cannot inspect filesystem entry; entry missing from folder refresh", "path", childPath, "error", err)
		}
		return
	}
	if idx.shouldSkip(info.IsDir(), childPath) {
		return
	}
	if info.IsDir() {
		contents.Folders = append(contents.Folders, childPath)
		return
	}
	contents.Files = append(contents.Files, EntryFromFileInfo(childPath, info))
}

// Lstat looks at path the way a scan reaches it: every folder on the way is
// opened without following a symlink, and the entry itself is not followed
// either. A path that leads through a link fails with ELOOP or ENOTDIR; the
// index knows every entry by its real path and holds nothing under that
// name. os.Lstat follows the links in the folders above the entry, which in
// a folder another user may write lets that user choose what root looks at.
func Lstat(path string) (os.FileInfo, error) {
	clean := indexerapi.NormalizePath(path)
	parent, name := filepath.Split(clean)
	dir, err := openScanRoot(parent)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			slog.Warn("failed to close directory", "path", dir.Name(), "error", closeErr)
		}
	}()
	if name == "" {
		return dir.Stat()
	}
	return statChild(dir, name)
}

// NotThere reports whether err says that a path names nothing the index can
// hold: it does not exist, or it leads through something that is not a
// folder, a symlink included.
func NotThere(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP)
}
