package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// movedTo is what is at path on disk, as the daemon reads it for a move.
func movedTo(t *testing.T, path string) indexing.IndexEntry {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return indexing.EntryFromFileInfo(path, info)
}

// assertDepthsAndNames fails when a row's depth or name does not follow from
// its path, which a rescan would not notice.
func assertDepthsAndNames(t *testing.T, db *sql.DB, indexID int64) {
	t.Helper()
	rows, err := db.Query(`SELECT relative_path, path_depth, name FROM entries WHERE index_id = ?`, indexID)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var path, name string
		var depth int
		if err := rows.Scan(&path, &depth, &name); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		if want := strings.Count(path, "/"); depth != want {
			t.Errorf("%s has depth %d, want %d", path, depth, want)
		}
		if want := filepath.Base(path); name != want {
			t.Errorf("%s has name %q, want %q", path, name, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rows: %v", err)
	}
}

func movePath(t *testing.T, ctx context.Context, store *Store, indexID int64, from, to string) bool {
	t.Helper()
	moved, err := store.MovePath(ctx, indexID, from, movedTo(t, to))
	if err != nil {
		t.Fatalf("move %s to %s: %v", from, to, err)
	}
	return moved
}

func TestMoveRenamesTheRowsOfAFolderWithoutReadingTheDisk(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	from, to := filepath.Join(root, "docs"), filepath.Join(root, "empty", "papers")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// Not in the index: a walk of the moved folder would find it.
	writeTestFile(t, filepath.Join(to, "unseen.txt"), 4096)

	if !movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("the move was not applied")
	}

	rows := indexRows(t, db, root)
	want := []string{
		" 1 16384 0",
		"/a.txt 0 4096 4096",
		"/empty 1 12288 0",
		"/empty/.keep 0 0 0",
		"/empty/papers 1 12288 0",
		"/empty/papers/b.txt 0 8192 8192",
		"/empty/papers/deep 1 4096 0",
		"/empty/papers/deep/c.txt 0 4096 4096",
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows after the move:\n got %v\nwant %v", rows, want)
	}
	assertDepthsAndNames(t, db, indexID)
}

func TestAMovedFolderIsFoundByItsNewNameAndPath(t *testing.T) {
	ctx, store, _, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	from, to := filepath.Join(root, "docs"), filepath.Join(root, "papers")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	movePath(t, ctx, store, indexID, from, to)

	found, err := store.SearchEntriesUnder(ctx, "papers", root, 10)
	if err != nil || len(found) != 1 || found[0].Path != to {
		t.Fatalf("search for the new name = %v, %v; want the moved folder", found, err)
	}
	if old, searchErr := store.SearchEntriesUnder(ctx, "docs", root, 10); searchErr != nil || len(old) != 0 {
		t.Fatalf("search for the old name = %v, %v; want nothing", old, searchErr)
	}
	below, err := store.SearchEntriesUnder(ctx, "b.txt", to, 10)
	if err != nil || len(below) != 1 || below[0].Path != filepath.Join(to, "b.txt") {
		t.Fatalf("search below the new path = %v, %v; want b.txt", below, err)
	}
}

func TestMoveOfAFileMovesItsSizeBetweenFolders(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	from, to := filepath.Join(root, "docs", "b.txt"), filepath.Join(root, "empty", "b-moved.txt")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if !movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("the move was not applied")
	}

	assertDepthsAndNames(t, db, indexID)
	assertAgreesWithDisk(t, ctx, store, db, indexID, root)
}

func TestMoveIsNotAppliedWhenTheDestinationIsNotWhatTheSourceWas(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	before := indexRows(t, db, root)
	from, to := filepath.Join(root, "docs"), filepath.Join(root, "copied")
	// A copy and a delete, as a move to another filesystem is: new folders.
	writeTestFile(t, filepath.Join(to, "b.txt"), 8192)
	if err := os.RemoveAll(from); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("rows were renamed to a folder that is not the one that was indexed")
	}
	if after := indexRows(t, db, root); !slices.Equal(after, before) {
		t.Fatalf("a move that was not applied changed rows:\nbefore %v\nafter  %v", before, after)
	}
}

func TestMoveIsNotAppliedWithoutARowForTheSourceOrForTheNewParent(t *testing.T) {
	ctx, store, _, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, filepath.Join(root, "docs"))
	// The source was never indexed.
	from, to := filepath.Join(root, "a.txt"), filepath.Join(root, "docs", "a.txt")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("a move was applied for a source the index does not hold")
	}
	// The new parent was never indexed: the rows would hang below nothing.
	from, to = filepath.Join(root, "docs", "b.txt"), filepath.Join(root, "empty", "b.txt")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("a move was applied into a folder the index does not hold")
	}
}

// Rows at the new path mean the index was told of that path some other way,
// and it cannot say which rows are right: the caller lets it follow the disk.
func TestMoveIsNotAppliedWhenTheIndexHoldsRowsAtTheDestination(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	// empty is replaced by docs: the index still holds the old empty.
	from, to := filepath.Join(root, "docs"), filepath.Join(root, "empty")
	if err := os.RemoveAll(to); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	before := indexRows(t, db, root)

	if movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("rows were renamed onto a path the index holds rows for")
	}
	if after := indexRows(t, db, root); !slices.Equal(after, before) {
		t.Fatalf("a move that was not applied changed rows:\nbefore %v\nafter  %v", before, after)
	}
}

// The folder a file moved into was refreshed before the index heard of the
// move: the refresh added the file there with its size and took the size
// from the row at the old path. Renaming that row over the new one would
// leave the file counting nothing.
func TestMoveAfterTheDestinationWasRefreshedKeepsTheSizeCounted(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	from, to := filepath.Join(root, "docs", "b.txt"), filepath.Join(root, "empty", "b.txt")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	refreshFolder(t, ctx, store, indexID, filepath.Join(root, "empty"))

	if movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("the stale row at the old path was renamed over the one the refresh wrote")
	}
	var counted int64
	if err := db.QueryRow(`SELECT size_contribution FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, to).Scan(&counted); err != nil {
		t.Fatalf("read the moved file's row: %v", err)
	}
	if counted != 8192 {
		t.Fatalf("the moved file counts %d bytes, want 8192", counted)
	}
}

func TestMoveKeepsAHardLinkCountedOnce(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	// b.txt has a second name outside the folder that moves.
	if err := os.Link(filepath.Join(root, "docs", "b.txt"), filepath.Join(root, "b-link.txt")); err != nil {
		t.Fatalf("link: %v", err)
	}
	rescan(t, ctx, store, indexID, root)
	from, to := filepath.Join(root, "docs"), filepath.Join(root, "empty", "docs")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if !movePath(t, ctx, store, indexID, from, to) {
		t.Fatal("the move was not applied")
	}

	assertAgreesWithDisk(t, ctx, store, db, indexID, root)
}

func TestMoveIsNotAppliedIntoItself(t *testing.T) {
	ctx, store, _, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	docs := filepath.Join(root, "docs")
	moved, err := store.MovePath(ctx, indexID, docs, movedTo(t, filepath.Join(docs, "deep")))
	if err != nil || moved {
		t.Fatalf("move into itself = %v, %v; want not applied", moved, err)
	}
}
