package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

func TestReindexPathRejectsDirectorySymlink(t *testing.T) {
	d, db := newDaemonWithDB(t)
	if _, err := db.Exec(`INSERT INTO indexes (last_indexed) VALUES (1)`); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
	broadcaster := newWorkStreamBroadcaster("reindex", "op-1", link)
	defer broadcaster.close()
	_, events, err := broadcaster.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	d.reindexPathWithProgress(context.Background(), "op-1", link, broadcaster)
	evt := mustReceiveWorkEvent(t, events)
	if evt.event != "error" {
		t.Fatalf("event = %q, want error", evt.event)
	}
	got, ok := evt.data.(api.WorkErrorEvent)
	if !ok {
		t.Fatalf("event data type = %T, want api.WorkErrorEvent", evt.data)
	}
	if !strings.Contains(got.Message, "is not a directory") {
		t.Fatalf("error = %q, want non-directory rejection", got.Message)
	}
}

func TestPartialReindexSynchronizesMetadata(t *testing.T) {
	d, db := newDaemonWithDB(t)
	res, insertErr := db.Exec(`INSERT INTO indexes (num_dirs, last_indexed) VALUES (1, 1)`)
	if insertErr != nil {
		t.Fatalf("insert index: %v", insertErr)
	}
	indexID, idErr := res.LastInsertId()
	if idErr != nil {
		t.Fatalf("index id: %v", idErr)
	}
	if _, err := db.Exec(`
		INSERT INTO entries (index_id, relative_path, name, size, mod_time, type)
		VALUES (?, '/', '/', 0, 1, 'directory');
	`, indexID); err != nil {
		t.Fatalf("insert root entry: %v", err)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatalf("create child directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("file"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "child", "nested"), []byte("nested"), 0o600); err != nil {
		t.Fatalf("create nested file: %v", err)
	}

	broadcaster := newWorkStreamBroadcaster("reindex", "op-1", root)
	defer broadcaster.close()
	_, events, subscribeErr := broadcaster.subscribe()
	if subscribeErr != nil {
		t.Fatalf("subscribe: %v", subscribeErr)
	}
	d.reindexExistingPath(context.Background(), "op-1", root, broadcaster, time.Now(), configfile.Config{IncludeNetworkMounts: true}, indexID)
	if evt := mustReceiveWorkEvent(t, events); evt.event != "complete" {
		t.Fatalf("event = %q, want complete; data = %+v", evt.event, evt.data)
	}

	var dirs, files, totalSize, lastIndexed int64
	if err := db.QueryRow(`
		SELECT num_dirs, num_files, total_size, last_indexed
		FROM indexes WHERE id = ?;
	`, indexID).Scan(&dirs, &files, &totalSize, &lastIndexed); err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	var actualDirs, actualFiles, rootSize int64
	if err := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 0 ELSE 1 END), 0),
			COALESCE((SELECT size FROM entries WHERE index_id = ? AND relative_path = '/'), 0)
		FROM entries WHERE index_id = ?;
	`, indexID, indexID).Scan(&actualDirs, &actualFiles, &rootSize); err != nil {
		t.Fatalf("query entries: %v", err)
	}
	if dirs != actualDirs || files != actualFiles || totalSize != rootSize {
		t.Fatalf("metadata = dirs %d, files %d, size %d; entries = %d, %d, %d", dirs, files, totalSize, actualDirs, actualFiles, rootSize)
	}
	if lastIndexed <= 1 {
		t.Fatalf("last indexed = %d, want refreshed timestamp", lastIndexed)
	}
}

func TestPartialReindexPreservesHardlinkContributionOutsideSubtree(t *testing.T) {
	d, db := newDaemonWithDB(t)
	res, insertErr := db.Exec(`INSERT INTO indexes (num_dirs, num_files, last_indexed) VALUES (3, 2, 1)`)
	if insertErr != nil {
		t.Fatalf("insert index: %v", insertErr)
	}
	indexID, idErr := res.LastInsertId()
	if idErr != nil {
		t.Fatalf("index id: %v", idErr)
	}

	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	if err := os.Mkdir(left, 0o700); err != nil {
		t.Fatalf("mkdir left: %v", err)
	}
	if err := os.Mkdir(right, 0o700); err != nil {
		t.Fatalf("mkdir right: %v", err)
	}
	leftPath := filepath.Join(left, "link")
	rightPath := filepath.Join(right, "link")
	if err := os.WriteFile(leftPath, []byte("hardlinked"), 0o600); err != nil {
		t.Fatalf("write hardlink fixture: %v", err)
	}
	if err := os.Link(leftPath, rightPath); err != nil {
		t.Fatalf("create hardlink: %v", err)
	}

	entryForPath := func(path string) indexing.IndexEntry {
		t.Helper()
		info, statErr := os.Lstat(path)
		if statErr != nil {
			t.Fatalf("lstat %s: %v", path, statErr)
		}
		return indexing.EntryFromFileInfo(path, info)
	}
	leftEntry := entryForPath(leftPath)
	rightEntry := entryForPath(rightPath)
	leftEntry.SizeContribution = 0
	rightEntry.SizeContribution = rightEntry.Size
	leftDir := entryForPath(left)
	rightDir := entryForPath(right)
	rightDir.Size += rightEntry.SizeContribution
	rootSize := leftDir.Size + rightDir.Size
	rootEntry := indexing.IndexEntry{RelativePath: "/", Name: "/", Size: rootSize, ModTime: time.Now(), Type: "directory"}
	for _, entry := range []indexing.IndexEntry{rootEntry, leftDir, rightDir, leftEntry, rightEntry} {
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
			t.Fatalf("seed %s: %v", entry.RelativePath, err)
		}
	}
	if _, err := db.Exec(`UPDATE indexes SET total_size = ? WHERE id = ?`, rootSize, indexID); err != nil {
		t.Fatalf("seed total size: %v", err)
	}

	broadcaster := newWorkStreamBroadcaster("reindex", "op-hardlink", left)
	defer broadcaster.close()
	_, events, subscribeErr := broadcaster.subscribe()
	if subscribeErr != nil {
		t.Fatalf("subscribe: %v", subscribeErr)
	}
	d.reindexExistingPath(context.Background(), "op-hardlink", left, broadcaster, time.Now(), configfile.Config{IncludeNetworkMounts: true}, indexID)
	if evt := mustReceiveWorkEvent(t, events); evt.event != "complete" {
		t.Fatalf("event = %q, want complete; data = %+v", evt.event, evt.data)
	}

	var gotRoot, gotContribution int64
	if err := db.QueryRow(`SELECT size FROM entries WHERE index_id = ? AND relative_path = '/'`, indexID).Scan(&gotRoot); err != nil {
		t.Fatalf("query root size: %v", err)
	}
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(size_contribution), 0) FROM entries
		WHERE index_id = ? AND device = ? AND inode = ?;
	`, indexID, int64(leftEntry.Device), int64(leftEntry.Inode)).Scan(&gotContribution); err != nil {
		t.Fatalf("query hardlink contribution: %v", err)
	}
	if gotRoot != rootSize || gotContribution != leftEntry.Size {
		t.Fatalf("root size = %d, contribution = %d; want %d and %d", gotRoot, gotContribution, rootSize, leftEntry.Size)
	}
}
