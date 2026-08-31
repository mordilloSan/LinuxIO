package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing/iteminfo"
)

// FTS5 trigram substring index over entries.name.
//
// entries_fts is an external-content FTS5 table kept in sync with entries by
// the triggers below, so no Go write path needs to know about it. It is used
// when the binary includes FTS5; searches transparently fall back to LIKE
// when the SQLite module is unavailable.

const ftsCreateTable = `
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
	name,
	content='entries',
	content_rowid='id',
	tokenize='trigram'
);`

// fts_state holds a single row recording that the last full rebuild of
// entries_fts ran to completion. entries_fts is external-content, so its own
// rows cannot answer "is the trigram index populated?" — a SELECT without
// MATCH reads the backing entries table. The marker is written in the same
// transaction as the rebuild, so a rebuild interrupted by a crash, kill, or
// timeout leaves no marker and the next open rebuilds again.
const ftsStateCreateTable = `
CREATE TABLE IF NOT EXISTS fts_state (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	rebuilt_at INTEGER NOT NULL
);`

// ftsTriggers keeps entries_fts synchronized with entries. The update trigger
// is guarded so the hourly rescan, which upserts every row with an unchanged
// name, does no FTS work.
var ftsTriggers = []struct {
	name string
	ddl  string
}{
	{"entries_fts_ai", `
CREATE TRIGGER IF NOT EXISTS entries_fts_ai AFTER INSERT ON entries BEGIN
	INSERT INTO entries_fts(rowid, name) VALUES (new.id, new.name);
END;`},
	{"entries_fts_ad", `
CREATE TRIGGER IF NOT EXISTS entries_fts_ad AFTER DELETE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, name) VALUES ('delete', old.id, old.name);
END;`},
	{"entries_fts_au", `
CREATE TRIGGER IF NOT EXISTS entries_fts_au AFTER UPDATE OF name ON entries
WHEN old.name IS NOT new.name BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, name) VALUES ('delete', old.id, old.name);
	INSERT INTO entries_fts(rowid, name) VALUES (new.id, new.name);
END;`},
}

// ftsModuleAvailable reports whether this binary was compiled with FTS5
// support, probing with a throwaway table in the temp schema.
func ftsModuleAvailable(ctx context.Context, db *sql.DB) bool {
	if _, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE temp.fts5_probe USING fts5(x);`); err != nil {
		return false
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE temp.fts5_probe;`); err != nil {
		slog.Warn("failed to drop fts5 probe table", "err", err)
	}
	return true
}

func ftsTableExists(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'entries_fts');
	`).Scan(&exists)
	return exists, err
}

func dropFTSTriggers(ctx context.Context, db dbExecutor) error {
	for _, tr := range ftsTriggers {
		if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+tr.name+`;`); err != nil {
			return fmt.Errorf("drop trigger %s: %w", tr.name, err)
		}
	}
	return nil
}

func createFTSTriggers(ctx context.Context, db dbExecutor) error {
	for _, tr := range ftsTriggers {
		if _, err := db.ExecContext(ctx, tr.ddl); err != nil {
			return fmt.Errorf("create trigger %s: %w", tr.name, err)
		}
	}
	return nil
}

// ensureFTS is called from initSchema. FTS is enabled whenever the binary
// includes the SQLite module; otherwise searches use the LIKE fallback.
func ensureFTS(ctx context.Context, db *sql.DB) error {
	if !ftsModuleAvailable(ctx, db) {
		if err := dropFTSTriggers(ctx, db); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS entries_fts; DROP TABLE IF EXISTS fts_state;`); err != nil {
			return fmt.Errorf("remove unavailable FTS tables: %w", err)
		}
		return nil
	}
	return enableFTS(ctx, db)
}

func enableFTS(ctx context.Context, db *sql.DB) error {
	existed, err := ftsTableExists(ctx, db)
	if err != nil {
		return err
	}
	if _, execErr := db.ExecContext(ctx, ftsCreateTable); execErr != nil {
		return fmt.Errorf("create entries_fts: %w", execErr)
	}
	if _, execErr := db.ExecContext(ctx, ftsStateCreateTable); execErr != nil {
		return fmt.Errorf("create fts_state: %w", execErr)
	}
	if trigErr := createFTSTriggers(ctx, db); trigErr != nil {
		return trigErr
	}
	needRebuild := !existed
	if !needRebuild {
		needRebuild, err = ftsRebuildIncomplete(ctx, db)
		if err != nil {
			return err
		}
	}
	if needRebuild {
		if err := ftsRebuild(ctx, db); err != nil {
			return err
		}
	}
	slog.Info("FTS5 search index enabled")
	return nil
}

// ftsRebuildIncomplete reports whether entries_fts exists without a completed
// rebuild on record. The table's own rows cannot be probed for this: it is
// external-content, so a SELECT without MATCH reads the backing entries table
// and reports rows even when the trigram index itself is empty (e.g. after a
// rebuild interrupted by a crash).
func ftsRebuildIncomplete(ctx context.Context, db *sql.DB) (bool, error) {
	var done bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fts_state WHERE id = 1);`).Scan(&done); err != nil {
		return false, err
	}
	return !done, nil
}

// markFTSRebuilt records rebuild completion; callers run it in the same
// transaction as the rebuild itself.
func markFTSRebuilt(ctx context.Context, db dbExecutor) error {
	if _, err := db.ExecContext(ctx, ftsStateCreateTable); err != nil {
		return fmt.Errorf("create fts_state: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO fts_state (id, rebuilt_at) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET rebuilt_at = excluded.rebuilt_at;
	`, time.Now().Unix()); err != nil {
		return fmt.Errorf("record fts rebuild: %w", err)
	}
	return nil
}

// ftsRebuild repopulates entries_fts from the entries table and records
// completion in fts_state within the same transaction.
func ftsRebuild(ctx context.Context, db *sql.DB) error {
	var entryCount int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries;`).Scan(&entryCount); err != nil {
		return fmt.Errorf("count entries before fts rebuild: %w", err)
	}
	if entryCount > 0 {
		slog.Info("rebuilding FTS5 search index; large databases may take a while", "entries", entryCount)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("fts rebuild rollback failed", "err", rollbackErr)
		}
	}()
	if _, err := tx.ExecContext(ctx, `INSERT INTO entries_fts(entries_fts) VALUES ('rebuild');`); err != nil {
		return fmt.Errorf("rebuild entries_fts: %w", err)
	}
	if err := markFTSRebuilt(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// searchEntriesFTS answers a search from the trigram index: match rowids in
// entries_fts, then join back to entries for metadata and mod_time ordering.
func (s *Store) searchEntriesFTS(ctx context.Context, match, basePath string, limit int) ([]EntryResult, error) {
	where, args := appendSearchBaseFilter("1 = 1", nil, basePath)
	args = append([]any{match}, args...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
        SELECT e.relative_path, e.name, e.type, e.size, e.mod_time, e.inode
        FROM entries e
        JOIN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?) f ON e.id = f.rowid
        JOIN (SELECT id FROM indexes WHERE last_indexed > 0 ORDER BY last_indexed DESC, id DESC LIMIT 1) latest ON latest.id = e.index_id
        WHERE %s
        ORDER BY e.mod_time DESC
        LIMIT ?
    `, where), args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			slog.Warn("rows close failed", "query", "search", "err", cerr)
		}
	}()

	var results []EntryResult
	for rows.Next() {
		entry, scanErr := scanSearchEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, entry)
	}
	return results, rows.Err()
}

// ftsMatchQuery converts parsed search options into an FTS5 MATCH expression.
// ok is false when the query needs the LIKE path instead: case-sensitive
// searches (the trigram tokenizer folds case) and terms shorter than three
// runes (below the trigram size, they match nothing in FTS).
func ftsMatchQuery(opts iteminfo.SearchOptions) (match string, ok bool) {
	if opts.CaseSensitive {
		return "", false
	}
	var quoted []string
	for _, term := range opts.Terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if utf8.RuneCountInString(term) < 3 {
			return "", false
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	if len(quoted) == 0 {
		return "", false
	}
	return strings.Join(quoted, " OR "), true
}
