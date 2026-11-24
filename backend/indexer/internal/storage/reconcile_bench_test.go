package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// BenchmarkRescanOfTree indexes a real folder into a new database and then
// rescans it, and reports what each took and wrote. It needs a folder to
// read, named by LINUXIO_INDEX_TREE, and is skipped without one:
//
//	LINUXIO_INDEX_TREE=/usr make bench-go GO_TEST_PKGS=./indexer/internal/storage/ \
//	    GO_BENCH=RescanOfTree
func BenchmarkRescanOfTree(b *testing.B) {
	tree := os.Getenv("LINUXIO_INDEX_TREE")
	if tree == "" {
		b.Skip("LINUXIO_INDEX_TREE names no folder to index")
	}
	ctx := context.Background()
	dbPath := filepath.Join(b.TempDir(), "indexer.db")
	db, err := OpenContext(ctx, dbPath)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer db.Close()
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	indexID, err := store.EnsureGeneration(ctx)
	if err != nil {
		b.Fatalf("ensure generation: %v", err)
	}

	scan := func() (time.Duration, int64, uint64) {
		start := time.Now()
		reindex, err := store.BeginReindex(ctx, indexID, tree)
		if err != nil {
			b.Fatalf("begin reindex: %v", err)
		}
		defer reindex.Rollback()
		index := indexing.Initialize("/", indexing.WithHardlinkContributors(reindex.HardlinkContributors()))
		reconciler := reindex.NewReconciler(ctx, nil)
		index.EnableStreaming(reconciler)
		if err := index.StartIndexingFromPath(ctx, tree); err != nil {
			b.Fatalf("scan: %v", err)
		}
		if err := reconciler.Close(); err != nil {
			b.Fatalf("close: %v", err)
		}
		if _, err := reindex.Commit(ctx, reconciler.ScanTime()); err != nil {
			b.Fatalf("commit: %v", err)
		}
		store.CheckpointAndRelease(ctx, "benchmark")
		return time.Since(start), reconciler.Changed(), index.NumFiles + index.NumDirs
	}

	first, firstChanged, entries := scan()
	size := store.DatabaseSize()
	b.Logf("first scan of %s: %d entries in %s, %d rows written, database %d MB", tree, entries, first.Round(time.Millisecond), firstChanged, size>>20)
	for b.Loop() {
		took, changed, _ := scan()
		b.Logf("rescan: %s, %d rows written or deleted", took.Round(time.Millisecond), changed)
	}
}
