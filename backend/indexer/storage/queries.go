package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing/iteminfo"
)

// Store wraps the database connection
type Store struct {
	db     *sql.DB
	dbPath string

	ftsOnce    sync.Once
	ftsEnabled bool
}

var ErrDirectoryNotFound = errors.New("directory not found")

// NewStoreWithDB reuses an existing database handle (e.g., long-lived server).
// dbPath should be the actual SQLite file path (for stats / size reporting).
func NewStoreWithDB(db *sql.DB, dbPath string) *Store {
	return &Store{db: db, dbPath: dbPath}
}

// LatestIndexID returns the ID of the most recently completed index
// generation. Rows with last_indexed=0 are scans still in progress (or
// crashed) and stay invisible until their final metadata update publishes
// them.
// - sql.ErrNoRows means "no completed indexes yet"
// - Any other error is a real DB problem and should be surfaced.
func (s *Store) LatestIndexID(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
        SELECT id
        FROM indexes
        WHERE last_indexed > 0
        ORDER BY last_indexed DESC, id DESC
        LIMIT 1
    `).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// EntryResult represents a query result entry
type EntryResult = api.EntryResult

// SearchEntriesUnder performs a name search within one indexed subtree.
func (s *Store) SearchEntriesUnder(ctx context.Context, pattern, basePath string, limit int) ([]EntryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 100
	}

	indexID, err := s.LatestIndexID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No index yet → no results, not an error
			return []EntryResult{}, nil
		}
		return nil, fmt.Errorf("failed to get latest index: %w", err)
	}

	opts := iteminfo.ParseSearch(pattern)

	// Prefer the FTS5 trigram index when available and the query is eligible:
	// it answers substring searches from the index instead of scanning every
	// row. Any FTS failure falls through to the LIKE scan.
	if match, ok := ftsMatchQuery(opts); ok && s.searchIndexAvailable(ctx) {
		results, ftsErr := s.searchEntriesFTS(ctx, indexID, match, basePath, limit)
		if ftsErr == nil {
			return results, nil
		}
		slog.Warn("FTS search failed; falling back to LIKE scan", "err", ftsErr)
	}

	where, args := buildSearchFilter(indexID, opts)
	where, args = appendSearchBaseFilter(where, args, basePath)
	args = append(args, limit)

	query := fmt.Sprintf(`
        SELECT relative_path, name, type, size, mod_time, inode
        FROM entries
        WHERE %s
        ORDER BY mod_time DESC
        LIMIT ?
    `, where)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
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

func appendSearchBaseFilter(where string, args []any, basePath string) (string, []any) {
	if basePath == "" || basePath == "/" {
		return where, args
	}
	return where + ` AND (relative_path = ? OR relative_path LIKE ? ESCAPE '\')`,
		append(args, basePath, SubtreeLikePattern(basePath))
}

func buildSearchFilter(indexID int64, opts iteminfo.SearchOptions) (string, []any) {
	args := []any{indexID}
	termClauses := make([]string, 0, len(opts.Terms))
	for _, term := range opts.Terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if opts.CaseSensitive {
			termClauses = append(termClauses, "instr(name, ?) > 0")
			args = append(args, term)
		} else {
			termClauses = append(termClauses, "LOWER(name) LIKE LOWER(?) ESCAPE '\\'")
			args = append(args, "%"+escapeLikePattern(term)+"%")
		}
	}

	where := "index_id = ?"
	if len(termClauses) > 0 {
		where += " AND (" + strings.Join(termClauses, " OR ") + ")"
	}
	return where, args
}

func scanSearchEntry(rows *sql.Rows) (EntryResult, error) {
	var entry EntryResult
	var modUnix int64
	var dbType string
	if err := rows.Scan(&entry.Path, &entry.Name, &dbType, &entry.Size, &modUnix, &entry.Inode); err != nil {
		return EntryResult{}, fmt.Errorf("scan failed: %w", err)
	}
	if dbType == "directory" {
		entry.Type = "folder"
	} else {
		entry.Type = "file"
	}
	entry.ModTime = time.Unix(modUnix, 0)
	return entry, nil
}

// DirSize returns the pre-calculated size for a directory.
// The size is calculated during indexing and stored in the directory entry.
func (s *Store) DirSize(ctx context.Context, path string) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	indexID, err := s.LatestIndexID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil // no index yet
		}
		return 0, fmt.Errorf("failed to get latest index: %w", err)
	}

	var size sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
        SELECT size
        FROM entries
        WHERE index_id = ? AND relative_path = ? AND type = 'directory'
    `, indexID, path).Scan(&size); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: %s", ErrDirectoryNotFound, path)
		}
		return 0, fmt.Errorf("dir size query failed: %w", err)
	}
	if size.Valid {
		return size.Int64, nil
	}
	return 0, nil
}

// EntryCount returns the number of file and directory entries at and under path.
// The path itself is included in the counts when present in the index.
func (s *Store) EntryCount(ctx context.Context, path string) (files int64, dirs int64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	indexID, err := s.LatestIndexID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("failed to get latest index: %w", err)
	}

	// Deliberately kept on LIKE: the covering scan of idx_entries_subfolders
	// beats a subtreeBounds range seek here, because the range plan pays a
	// table lookup per row for `type` (measured 6x slower on large subtrees).
	rows, err := s.db.QueryContext(ctx, `
        SELECT type, COUNT(*)
        FROM entries
        WHERE index_id = ?
          AND (relative_path = ? OR relative_path LIKE ? ESCAPE '\')
        GROUP BY type
    `, indexID, path, SubtreeLikePattern(path))
	if err != nil {
		return 0, 0, fmt.Errorf("entry count query failed: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			slog.Warn("rows close failed", "query", "entrycount", "err", cerr)
		}
	}()

	for rows.Next() {
		var typ string
		var count int64
		if err := rows.Scan(&typ, &count); err != nil {
			return 0, 0, fmt.Errorf("scan failed: %w", err)
		}
		switch typ {
		case "file":
			files = count
		case "directory":
			dirs = count
		}
	}
	return files, dirs, rows.Err()
}

// QueryPath queries entries at or under a given path.
// For recursive queries, a non-empty after acts as a keyset cursor: only
// entries with relative_path strictly greater than it are returned, in path
// order, and offset is ignored. This stays fast at any depth, unlike offset
// pagination which walks and discards all skipped rows.
func (s *Store) QueryPath(ctx context.Context, path string, recursive bool, limit, offset int, after string) ([]EntryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	indexID, err := s.LatestIndexID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []EntryResult{}, nil
		}
		return nil, fmt.Errorf("failed to get latest index: %w", err)
	}

	var query string
	var args []any

	if recursive {
		var cursored bool
		query, args, cursored = recursiveQueryArgs(indexID, path, after)
		if cursored {
			offset = 0
		}
	} else {
		query = `
            SELECT relative_path, name, type, size, mod_time, inode
            FROM entries
            WHERE index_id = ? AND relative_path = ?
        `
		args = []any{indexID, path}
	}

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			slog.Warn("rows close failed", "query", "entries", "err", cerr)
		}
	}()

	var results []EntryResult
	for rows.Next() {
		var entry EntryResult
		var modUnix int64
		var dbType string

		err := rows.Scan(&entry.Path, &entry.Name, &dbType, &entry.Size, &modUnix, &entry.Inode)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		// Convert database type to API type
		if dbType == "directory" {
			entry.Type = "folder"
		} else {
			entry.Type = "file"
		}
		entry.ModTime = time.Unix(modUnix, 0)
		results = append(results, entry)
	}

	return results, rows.Err()
}

// recursiveQueryArgs builds the seekable subtree-listing query. cursored
// reports that the keyset cursor replaced the lower bound — the cursor must
// replace it rather than add a second one, because with two lower bounds on
// the same column SQLite seeks on only one and filters the other, walking
// every skipped row. Callers ignore offset when cursored.
func recursiveQueryArgs(indexID int64, path, after string) (query string, args []any, cursored bool) {
	lo, childLo, hi := subtreeBounds(path)
	lowerBound, lowerArg := "relative_path >= ?", lo
	if after != "" && after >= lo {
		lowerBound, lowerArg = "relative_path > ?", after
		cursored = true
	}
	query = `
            SELECT relative_path, name, type, size, mod_time, inode
            FROM entries
            WHERE index_id = ?
              AND ` + lowerBound + ` AND relative_path < ?
              AND (relative_path = ? OR relative_path >= ?)
            ORDER BY relative_path
        `
	return query, []any{indexID, lowerArg, hi, lo, childLo}, cursored
}

// Stats represents database statistics
type Stats = api.Stats

// GetStats returns database statistics
func (s *Store) GetStats(ctx context.Context) (*Stats, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var stats Stats

	// Count index names with a completed generation; in-progress or
	// superseded generations of the same name must not inflate the stats.
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT name) FROM indexes WHERE last_indexed > 0`).Scan(&stats.TotalIndexes)
	if err != nil {
		return nil, err
	}

	// Sum entries and size over the latest completed generation of each name.
	var lastIndexed sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
        SELECT
            COALESCE(SUM(num_files + num_dirs), 0),
            COALESCE(SUM(total_size), 0),
            MAX(last_indexed)
        FROM indexes i
        WHERE i.last_indexed > 0
          AND i.id = (
            SELECT j.id FROM indexes j
            WHERE j.name = i.name AND j.last_indexed > 0
            ORDER BY j.last_indexed DESC, j.id DESC
            LIMIT 1
          )
    `).Scan(&stats.TotalEntries, &stats.TotalSize, &lastIndexed)
	if err != nil {
		return nil, err
	}

	if lastIndexed.Valid {
		stats.LastScanTime = time.Unix(lastIndexed.Int64, 0)
	}

	// Get database file size using the actual dbPath
	if s.dbPath != "" {
		if fi, err := os.Stat(s.dbPath); err == nil {
			stats.DatabaseSize = fi.Size()
		}
		if fi, err := os.Stat(s.dbPath + "-wal"); err == nil {
			stats.WALSize = fi.Size()
		}
		if fi, err := os.Stat(s.dbPath + "-shm"); err == nil {
			stats.SHMSize = fi.Size()
		}
		stats.TotalOnDisk = stats.DatabaseSize + stats.WALSize + stats.SHMSize
	}

	return &stats, nil
}

// SubfolderResult represents a direct subfolder with its size
type SubfolderResult = api.SubfolderResult

// GetDirectSubfolders returns all direct child folders of a given path with their sizes.
// This only returns immediate children (not recursive).
func (s *Store) GetDirectSubfolders(ctx context.Context, parentPath string) ([]SubfolderResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	indexID, err := s.LatestIndexID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []SubfolderResult{}, nil
		}
		return nil, fmt.Errorf("failed to get latest index: %w", err)
	}

	// Normalize parent path - ensure it starts with / and doesn't end with / (unless it's root)
	if parentPath != "/" {
		parentPath = "/" + strings.Trim(parentPath, "/")
	}

	parentDepth := 0
	if parentPath != "/" {
		parentDepth = strings.Count(parentPath, "/")
	}
	childDepth := parentDepth + 1

	// Query for direct children that are directories using path_depth (precomputed at write time).
	var (
		query string
		args  []any
	)

	if parentPath == "/" {
		query = `
            SELECT relative_path, name, size, mod_time
            FROM entries
            WHERE index_id = ?
              AND type = 'directory'
              AND path_depth = 1
            ORDER BY name
        `
		args = []any{indexID}
	} else {
		// The child-path range constrains the 4th column of
		// idx_entries_subfolders after three equality columns, so this is a
		// pure index range scan; no residual filter is needed because direct
		// children always start with childLo.
		_, childLo, hi := subtreeBounds(parentPath)
		query = `
            SELECT relative_path, name, size, mod_time
            FROM entries
            WHERE index_id = ?
              AND type = 'directory'
              AND path_depth = ?
              AND relative_path >= ? AND relative_path < ?
            ORDER BY name
        `
		args = []any{indexID, childDepth, childLo, hi}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query subfolders failed: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			slog.Warn("rows close failed", "query", "subfolders", "err", cerr)
		}
	}()

	var results []SubfolderResult
	for rows.Next() {
		var subfolder SubfolderResult
		var modUnix int64
		if err := rows.Scan(&subfolder.Path, &subfolder.Name, &subfolder.Size, &modUnix); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		subfolder.ModTime = time.Unix(modUnix, 0)
		results = append(results, subfolder)
	}

	return results, rows.Err()
}
