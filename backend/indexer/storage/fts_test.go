package storage

import (
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing/iteminfo"
)

// skipWithoutFTS skips tests that need the sqlite_fts5 build tag.
func skipWithoutFTS(t *testing.T) {
	t.Helper()
	ctx, db, _ := setupTestDB(t)
	if !ftsModuleAvailable(ctx, db) {
		t.Skip("binary built without -tags sqlite_fts5")
	}
}

func TestFTSMatchQuery(t *testing.T) {
	tests := []struct {
		name  string
		opts  iteminfo.SearchOptions
		match string
		ok    bool
	}{
		{"single term", iteminfo.SearchOptions{Terms: []string{"report"}}, `"report"`, true},
		{"multiple terms", iteminfo.SearchOptions{Terms: []string{"foo", "barbaz"}}, `"foo" OR "barbaz"`, true},
		{"embedded quote escaped", iteminfo.SearchOptions{Terms: []string{`my"file`}}, `"my""file"`, true},
		{"case sensitive falls back", iteminfo.SearchOptions{CaseSensitive: true, Terms: []string{"Report"}}, "", false},
		{"short term falls back", iteminfo.SearchOptions{Terms: []string{"ab"}}, "", false},
		{"short term among long falls back", iteminfo.SearchOptions{Terms: []string{"report", "ab"}}, "", false},
		{"empty falls back", iteminfo.SearchOptions{Terms: []string{}}, "", false},
		{"whitespace-only falls back", iteminfo.SearchOptions{Terms: []string{"  "}}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, ok := ftsMatchQuery(tt.opts)
			if match != tt.match || ok != tt.ok {
				t.Fatalf("ftsMatchQuery(%+v) = (%q, %v), want (%q, %v)", tt.opts, match, ok, tt.match, tt.ok)
			}
		})
	}
}

func TestFTSSearchStaysInSyncAcrossMutations(t *testing.T) {
	skipWithoutFTS(t)
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	store := NewStoreWithDB(db, dbPath)

	if !store.searchIndexAvailable(ctx) {
		t.Fatal("entries_fts missing after setup with FTS5 compiled in")
	}

	now := time.Now()
	seed := []indexing.IndexEntry{
		{RelativePath: "/", Name: "/", Size: 0, ModTime: now, Type: "directory"},
		{RelativePath: "/holiday-photos", Name: "holiday-photos", Size: 0, ModTime: now, Type: "directory"},
		{RelativePath: "/holiday-photos/beach.jpg", Name: "beach.jpg", Size: 10, ModTime: now, Type: "file"},
		{RelativePath: "/report_final.pdf", Name: "report_final.pdf", Size: 20, ModTime: now, Type: "file"},
	}
	for _, e := range seed {
		if _, err := UpdateEntry(ctx, db, indexID, e); err != nil {
			t.Fatalf("seed %s: %v", e.RelativePath, err)
		}
	}

	assertSearch := func(q string, want map[string]bool) {
		t.Helper()
		results, err := store.SearchEntriesUnder(ctx, q, "/", 100)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		got := map[string]bool{}
		for _, r := range results {
			got[r.Name] = true
		}
		if len(got) != len(want) {
			t.Fatalf("search %q = %v, want %v", q, got, want)
		}
		for name := range want {
			if !got[name] {
				t.Fatalf("search %q missing %s; got %v", q, name, got)
			}
		}
	}

	// insert trigger
	assertSearch("holiday", map[string]bool{"holiday-photos": true})
	assertSearch("beach", map[string]bool{"beach.jpg": true})
	// substring + case-insensitivity through the trigram index
	assertSearch("EPORT_FIN", map[string]bool{"report_final.pdf": true})
	// multi-term OR
	assertSearch("beach|report", map[string]bool{"beach.jpg": true, "report_final.pdf": true})
	// miss
	assertSearch("nosuchname", map[string]bool{})

	// update trigger: rename via upsert on the same relative_path
	renamed := seed[3]
	renamed.Name = "summary_final.pdf"
	if _, err := UpdateEntry(ctx, db, indexID, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}
	assertSearch("report", map[string]bool{})
	assertSearch("summary", map[string]bool{"summary_final.pdf": true})

	// delete trigger via recursive subtree delete
	if err := DeletePathRecursive(ctx, db, indexID, "/holiday-photos"); err != nil {
		t.Fatalf("delete subtree: %v", err)
	}
	assertSearch("beach", map[string]bool{})
	assertSearch("holiday", map[string]bool{})

}

// TestFTSDisableByOption verifies the fts_search=false path: opening with
// DisableFTS drops the index and triggers so writes stop paying for trigram
// maintenance and search falls back to LIKE; re-enabling backfills.
func TestFTSDisableByOption(t *testing.T) {
	skipWithoutFTS(t)
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)

	e := indexing.IndexEntry{RelativePath: "/toggle.txt", Name: "toggle.txt", Size: 1, ModTime: time.Now(), Type: "file"}
	if _, err := UpdateEntry(ctx, db, indexID, e); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Disable: table and triggers must be gone, writes and search still work.
	if err := ensureFTS(ctx, db, true); err != nil {
		t.Fatalf("ensureFTS disabled: %v", err)
	}
	if exists, err := ftsTableExists(ctx, db); err != nil || exists {
		t.Fatalf("entries_fts should be dropped when disabled: exists=%v err=%v", exists, err)
	}
	// SearchIndexActive probes fresh and must report the actual state
	// immediately, unlike the per-Store cached probe.
	if NewStoreWithDB(db, dbPath).SearchIndexActive(ctx) {
		t.Fatal("SearchIndexActive should be false after disable")
	}
	e2 := indexing.IndexEntry{RelativePath: "/afterward.txt", Name: "afterward.txt", Size: 1, ModTime: time.Now(), Type: "file"}
	if _, err := UpdateEntry(ctx, db, indexID, e2); err != nil {
		t.Fatalf("insert with FTS disabled: %v", err)
	}
	store := NewStoreWithDB(db, dbPath)
	results, err := store.SearchEntriesUnder(ctx, "afterward", "/", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("LIKE fallback search = %v, %v; want 1 row", results, err)
	}

	// Re-enable: index is rebuilt including rows written while disabled.
	if err := ensureFTS(ctx, db, false); err != nil {
		t.Fatalf("ensureFTS re-enabled: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries_fts WHERE entries_fts MATCH '"afterward"'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("backfill after re-enable: n=%d err=%v", n, err)
	}
}

func TestFTSBackfillOnOpenWithExistingEntries(t *testing.T) {
	skipWithoutFTS(t)
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)

	// Simulate a DB whose FTS index is missing content (e.g. written by a
	// binary without the tag): drop triggers+table, insert, then re-init.
	if err := dropFTSTriggers(ctx, db); err != nil {
		t.Fatalf("drop triggers: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE entries_fts;`); err != nil {
		t.Fatalf("drop fts table: %v", err)
	}
	e := indexing.IndexEntry{RelativePath: "/orphaned.txt", Name: "orphaned.txt", Size: 1, ModTime: time.Now(), Type: "file"}
	if _, err := UpdateEntry(ctx, db, indexID, e); err != nil {
		t.Fatalf("insert without fts: %v", err)
	}

	if err := ensureFTS(ctx, db, false); err != nil {
		t.Fatalf("ensureFTS: %v", err)
	}

	store := NewStoreWithDB(db, dbPath)
	results, err := store.SearchEntriesUnder(ctx, "orphaned", "/", 10)
	if err != nil {
		t.Fatalf("search after backfill: %v", err)
	}
	if len(results) != 1 || results[0].Name != "orphaned.txt" {
		t.Fatalf("backfill search = %+v, want orphaned.txt", results)
	}
}

// TestFTSInterruptedRebuildRecoversOnReopen covers the silent-empty-index
// failure: entries_fts is external-content, so a plain row probe cannot see
// that the trigram index is empty after a rebuild was interrupted (crash,
// kill, timeout). The fts_state completion marker — written in the same
// transaction as the rebuild — must trigger a rebuild on the next open.
func TestFTSInterruptedRebuildRecoversOnReopen(t *testing.T) {
	skipWithoutFTS(t)
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	store := NewStoreWithDB(db, dbPath)

	e := indexing.IndexEntry{RelativePath: "/needle.txt", Name: "needle.txt", Size: 1, ModTime: time.Now(), Type: "file"}
	if _, err := UpdateEntry(ctx, db, indexID, e); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Simulate an interrupted rebuild: empty the real trigram index and
	// remove the completion marker, exactly the state a crash mid-rebuild
	// leaves behind (the rebuild transaction rolled back, table present).
	if _, err := db.ExecContext(ctx, `INSERT INTO entries_fts(entries_fts) VALUES ('delete-all');`); err != nil {
		t.Fatalf("empty fts index: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM fts_state;`); err != nil {
		t.Fatalf("remove rebuild marker: %v", err)
	}

	// Sanity: the index is now silently empty — MATCH misses while the
	// entry row exists. This is the state that previously went undetected.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries_fts WHERE entries_fts MATCH '"needle"'`).Scan(&n); err != nil {
		t.Fatalf("match probe: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected empty fts index after delete-all, got %d matches", n)
	}

	// Reopen path: ensureFTS must detect the missing marker and rebuild.
	if err := ensureFTS(ctx, db, false); err != nil {
		t.Fatalf("ensureFTS on reopen: %v", err)
	}
	results, err := store.SearchEntriesUnder(ctx, "needle", "/", 10)
	if err != nil {
		t.Fatalf("search after recovery: %v", err)
	}
	if len(results) != 1 || results[0].Name != "needle.txt" {
		t.Fatalf("search after recovery = %+v, want needle.txt", results)
	}

	// And with the marker intact, reopening must NOT rebuild again: verify
	// the marker survived and a second ensureFTS is a no-op that keeps the
	// index serving.
	if incomplete, markerErr := ftsRebuildIncomplete(ctx, db); markerErr != nil || incomplete {
		t.Fatalf("rebuild marker missing after successful rebuild: incomplete=%v err=%v", incomplete, markerErr)
	}
	if ensureErr := ensureFTS(ctx, db, false); ensureErr != nil {
		t.Fatalf("second ensureFTS: %v", ensureErr)
	}
	if results, err = store.SearchEntriesUnder(ctx, "needle", "/", 10); err != nil || len(results) != 1 {
		t.Fatalf("search after second open = %+v, %v; want 1 row", results, err)
	}
}
