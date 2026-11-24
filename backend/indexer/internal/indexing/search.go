package indexing

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// errSearchDeadline ends a search of a tree that has used up its time.
var errSearchDeadline = errors.New("search deadline reached")

// maxSearchDepth is how many folders deep below the folder searched in a
// search reads. Each level holds an open folder and a chunk of names, and a
// chain of nested folders is cheap for any local user to make. A variable
// for tests.
var maxSearchDepth = 256

// LeavesOut reports whether the index leaves path out: a folder it does not
// scan, or a path below one.
func (idx *Index) LeavesOut(path string) bool {
	return idx.shouldSkip(true, indexerapi.NormalizePath(path))
}

// TreeSearch is what a search of a folder's tree on disk found.
type TreeSearch struct {
	// Found are the newest matches by modification time, at most as many as
	// the search was asked to keep, in no particular order.
	Found []IndexEntry
	// Complete is set when the whole tree was read before the deadline, with
	// no folder or entry that could not be read.
	Complete bool
}

// treeSearch is one search of a tree in progress.
type treeSearch struct {
	idx      *Index
	ctx      context.Context
	deadline time.Time
	match    func(name string) bool
	keep     int
	found    []IndexEntry
	// skipped is set when an entry or folder that is there could not be
	// read: the index may hold what is in it, so the tree is not read whole.
	skipped bool
}

// SearchTree reads the tree below folder and returns the entries whose name
// match accepts; folder itself is not one of them. It reaches the folder and
// walks the tree as a scan does (every folder opened without following a
// symlink, a symlink an entry of its own), leaves out what a scan leaves
// out, and writes nothing. An entry that cannot be looked at, or a folder
// that cannot be read, is left out, as a scan leaves it out, and the search
// is then not Complete, unless the index would leave that entry out anyway.
// It reads no deeper than maxSearchDepth folders below folder, and a tree
// that goes deeper is not Complete either.
//
// It stops at deadline and returns what it found with Complete unset, and
// returns the context's error when ctx ends. A folder the index leaves out
// is not read: ErrNotIndexed. A folder that cannot be reached returns the
// error of the open (NotThere tells one that is not there).
func (idx *Index) SearchTree(ctx context.Context, folder string, match func(name string) bool, deadline time.Time, keep int) (TreeSearch, error) {
	folder = indexerapi.NormalizePath(folder)
	if idx.shouldSkip(true, folder) {
		return TreeSearch{}, ErrNotIndexed
	}
	dir, err := openScanRoot(strings.TrimRight(idx.Path, "/") + folder)
	if err != nil {
		return TreeSearch{}, err
	}
	s := &treeSearch{idx: idx, ctx: ctx, deadline: deadline, match: match, keep: max(keep, 1)}
	err = s.walk(dir, folder, 0)
	s.trim()
	switch {
	case errors.Is(err, errSearchDeadline):
		return TreeSearch{Found: s.found}, nil
	case err != nil:
		return TreeSearch{}, err
	}
	return TreeSearch{Found: s.found, Complete: !s.skipped}, nil
}

// stop is the reason to end the search now, if there is one.
func (s *treeSearch) stop() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if !time.Now().Before(s.deadline) {
		return errSearchDeadline
	}
	return nil
}

// walk reads an open folder, depth folders below the one searched in, and
// the folders below it, and closes it. Only the end of the search is an
// error: a folder that fails to read is left at what was read of it.
func (s *treeSearch) walk(dir *os.File, path string, depth int) error {
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			slog.Debug("failed to close directory", "path", dir.Name(), "error", closeErr)
		}
	}()
	if err := s.stop(); err != nil {
		return err
	}
	for {
		// Names only, as the scan reads a folder (indexOpenDirectory).
		names, readErr := dir.Readdirnames(readDirChunk)
		for _, name := range names {
			if err := s.stop(); err != nil {
				return err
			}
			if err := s.child(dir, path, name, depth); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			s.skipped = true
			slog.Debug("cannot read directory; rest missing from search", "path", path, "error", readErr)
			return nil
		}
	}
}

// child looks at one entry of an open folder, depth folders below the one
// searched in, and walks it when it is a folder.
func (s *treeSearch) child(dir *os.File, path, name string, depth int) error {
	childPath := makeChildRelativePath(path, name)
	info, err := statChild(dir, name)
	if err != nil {
		// One that is gone since the folder was read is gone, and one the
		// index leaves out was never in it.
		s.skipped = s.skipped || (!errors.Is(err, os.ErrNotExist) && !s.idx.shouldSkip(true, childPath))
		slog.Debug("cannot inspect filesystem entry; entry missing from search", "path", childPath, "error", err)
		return nil
	}
	if s.idx.shouldSkip(info.IsDir(), childPath) {
		return nil
	}
	if s.match(name) {
		s.add(EntryFromFileInfo(childPath, info))
	}
	if !info.IsDir() {
		return nil
	}
	if depth+1 > maxSearchDepth {
		s.skipped = true
		slog.Debug("directory below the search depth limit; subtree missing from search", "path", childPath)
		return nil
	}
	sub, err := openDirectory(dir, name, strings.TrimRight(s.idx.Path, "/")+childPath)
	if err != nil {
		// Gone, or no longer a folder, since it was looked at: nothing to read.
		s.skipped = s.skipped || !NotThere(err)
		slog.Debug("cannot open directory; subtree missing from search", "path", childPath, "error", err)
		return nil
	}
	return s.walk(sub, childPath, depth+1)
}

// add keeps a match, holding on to no more than twice what is kept.
func (s *treeSearch) add(entry IndexEntry) {
	s.found = append(s.found, entry)
	if len(s.found) >= 2*s.keep {
		s.trim()
	}
}

// trim keeps the newest matches.
func (s *treeSearch) trim() {
	if len(s.found) <= s.keep {
		return
	}
	slices.SortFunc(s.found, func(a, b IndexEntry) int {
		return cmp.Or(b.ModTime.Compare(a.ModTime), strings.Compare(a.RelativePath, b.RelativePath))
	})
	s.found = slices.Clip(s.found[:s.keep])
}
