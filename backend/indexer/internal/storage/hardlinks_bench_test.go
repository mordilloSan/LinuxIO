package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

const (
	benchTopFolders = 20
	benchSubFolders = 15 // 300 folders that hold files
	benchReindexed  = "/srv"
)

// hardlinkBenchTree is a reindexed subtree as the writer leaves it before
// Commit: half the files are hardlinked in pairs whose two links sit in
// different folders, the first link of each pair contributes, and every folder
// row holds the sum of the contributions below it.
type hardlinkBenchTree struct {
	db         *sql.DB
	indexID    int64
	scanTime   int64
	hardlinked int
	// kept names the contributor the rescan picked, so nothing moves; moved
	// names the other link of every pair, so every pair moves its size to
	// another folder.
	kept, moved *hardlinkSnapshot
}

// BenchmarkReconcileHardlinksAfterReindex times reconcileHardlinksAfterReindex
// on 10,000 to 200,000 files in 300 folders, half of them hardlinked, in a
// cache SQLite has no statistics for. SameFile is stubbed, so the lstat it
// costs per hardlinked path on a host is not counted. Run it without the race
// detector, which slows SQLite calls several times:
//
//	make test-go GO_TEST_PKGS=./indexer/internal/storage GO_TEST_FLAGS='-run=NoTests -bench=BenchmarkReconcileHardlinksAfterReindex -benchtime=3x -race=false'
func BenchmarkReconcileHardlinksAfterReindex(b *testing.B) {
	for _, files := range []int{10_000, 100_000, 200_000} {
		b.Run(fmt.Sprintf("files=%d", files), func(b *testing.B) {
			tree := seedHardlinkBenchTree(b, files)
			b.Run("kept", func(b *testing.B) { benchmarkHardlinkReconcile(b, tree, tree.kept) })
			b.Run("moved", func(b *testing.B) { benchmarkHardlinkReconcile(b, tree, tree.moved) })
		})
	}
}

// benchmarkHardlinkReconcile times the reconcile alone, each run in a
// transaction rolled back afterwards so every run starts from the same rows.
func benchmarkHardlinkReconcile(b *testing.B, tree hardlinkBenchTree, snapshot *hardlinkSnapshot) {
	ctx := context.Background()
	sameFile := func(string, int64, int64) bool { return true }
	b.StopTimer()
	for range b.N {
		tx, err := tree.db.BeginTx(ctx, nil)
		if err != nil {
			b.Fatalf("begin: %v", err)
		}
		b.StartTimer()
		err = reconcileHardlinksAfterReindex(ctx, tx, tree.indexID, benchReindexed, tree.scanTime, snapshot, sameFile)
		b.StopTimer()
		if err != nil {
			b.Fatalf("reconcile: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			b.Fatalf("rollback: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(tree.hardlinked), "ns/hardlinked")
}

func seedHardlinkBenchTree(tb testing.TB, files int) hardlinkBenchTree {
	tb.Helper()
	ctx := context.Background()
	db, err := OpenContext(ctx, filepath.Join(tb.TempDir(), "bench.db"))
	if err != nil {
		tb.Fatalf("open db: %v", err)
	}
	tb.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			tb.Errorf("close db: %v", closeErr)
		}
	})
	tree := hardlinkBenchTree{
		db:       db,
		indexID:  insertTestIndex(tb, db),
		scanTime: time.Now().UnixNano(),
		kept:     &hardlinkSnapshot{groups: map[hardlinkIdentity]string{}},
		moved:    &hardlinkSnapshot{groups: map[hardlinkIdentity]string{}},
	}
	entries := tree.entries(files)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		tb.Fatalf("begin seed: %v", err)
	}
	defer rollbackTx(tx, "hardlink bench seed")
	for start := 0; start < len(entries); start += 1000 {
		end := min(start+1000, len(entries))
		if err := insertEntriesBatch(ctx, tx, tree.indexID, tree.scanTime, entries[start:end]); err != nil {
			tb.Fatalf("seed entries: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatalf("commit seed: %v", err)
	}
	return tree
}

// entries lists the rows of a tree of the given number of files, and records
// the pairs in the tree's snapshots.
func (tree *hardlinkBenchTree) entries(files int) []indexing.IndexEntry {
	folderSizes := map[string]int64{}
	modTime := time.Now()
	entries := make([]indexing.IndexEntry, 0, files)
	for i := range files {
		folder := fmt.Sprintf("%s/d%02d/s%02d", benchReindexed, (i/benchSubFolders)%benchTopFolders, i%benchSubFolders)
		path := fmt.Sprintf("%s/f%06d", folder, i)
		entry := indexing.IndexEntry{
			RelativePath: path,
			Name:         filepath.Base(path),
			Size:         int64(4096 * (1 + i%7)),
			ModTime:      modTime,
			Type:         "file",
			Device:       1,
			Inode:        uint64(i + 1),
		}
		entry.SizeContribution = entry.Size
		if i < files/2 {
			tree.addPairLink(&entry, i)
		}
		for dir := folder; ; dir = parentDirKey(dir) {
			folderSizes[dir] += entry.SizeContribution
			if dir == "/" {
				break
			}
		}
		entries = append(entries, entry)
	}
	for dir, size := range folderSizes {
		entries = append(entries, indexing.IndexEntry{
			RelativePath: dir, Name: filepath.Base(dir), Size: size, ModTime: modTime, Type: "directory",
		})
	}
	return entries
}

// addPairLink makes file i one link of pair i/2: files 2k and 2k+1 share an
// inode and its size, in two folders, and 2k contributes.
func (tree *hardlinkBenchTree) addPairLink(entry *indexing.IndexEntry, i int) {
	pair := i / 2
	entry.Inode = uint64(10_000_000 + pair)
	entry.Size = int64(4096 * (1 + pair%7))
	entry.SizeContribution = entry.Size
	identity := hardlinkIdentity{device: 1, inode: int64(entry.Inode)}
	if i%2 == 0 {
		tree.kept.groups[identity] = entry.RelativePath
	} else {
		entry.SizeContribution = 0
		tree.moved.groups[identity] = entry.RelativePath
	}
	tree.hardlinked++
}
