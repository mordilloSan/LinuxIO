package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing/iteminfo"
)

// Store wraps the database connection
type Store struct {
	db     *sql.DB
	dbPath string
}

var ErrDirectoryNotFound = errors.New("directory not found")
var ErrNotInitialized = errors.New("indexer is not initialized")

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
		ctx = context.TODO()
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
		ctx = context.TODO()
	}
	if limit <= 0 {
		limit = 100
	}

	opts := iteminfo.ParseSearch(pattern)

	// Prefer the FTS5 trigram index when available and the query is eligible:
	// it answers substring searches from the index instead of scanning every
	// row. Any FTS failure falls through to the LIKE scan.
	if match, ok := ftsMatchQuery(opts); ok {
		results, ftsErr := s.searchEntriesFTS(ctx, match, basePath, limit)
		if ftsErr == nil {
			if results == nil && !s.hasCompletedGeneration(ctx) {
				return nil, ErrNotInitialized
			}
			return results, nil
		}
		slog.Warn("FTS search failed; falling back to LIKE scan", "err", ftsErr)
	}

	where, args := buildSearchFilter(opts)
	where, args = appendSearchBaseFilter(where, args, basePath)
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT e.relative_path, e.name, e.type, e.size, e.mod_time, e.inode
		FROM entries e
		JOIN (SELECT id FROM indexes WHERE last_indexed > 0 ORDER BY last_indexed DESC, id DESC LIMIT 1) latest
		  ON latest.id = e.index_id
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil && !s.hasCompletedGeneration(ctx) {
		return nil, ErrNotInitialized
	}
	return results, nil
}

func appendSearchBaseFilter(where string, args []any, basePath string) (string, []any) {
	if basePath == "" || basePath == "/" {
		return where, args
	}
	return where + ` AND (relative_path = ? OR relative_path LIKE ? ESCAPE '\')`,
		append(args, basePath, SubtreeLikePattern(basePath))
}

func buildSearchFilter(opts iteminfo.SearchOptions) (string, []any) {
	args := []any{}
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

	where := "1 = 1"
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
func (s *Store) DirDetails(ctx context.Context, path string) (totalSize, files, dirs int64, err error) {
	if ctx == nil {
		ctx = context.TODO()
	}

	var size sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT e.size,
		       COALESCE((SELECT COUNT(*) FROM entries c
		                 WHERE c.index_id = e.index_id
		                   AND (c.relative_path = ? OR c.relative_path LIKE ? ESCAPE '\')
		                   AND c.type = 'file'), 0),
		       COALESCE((SELECT COUNT(*) FROM entries c
		                 WHERE c.index_id = e.index_id
		                   AND (c.relative_path = ? OR c.relative_path LIKE ? ESCAPE '\')
		                   AND c.type = 'directory'), 0)
		FROM entries e
		JOIN (SELECT id FROM indexes WHERE last_indexed > 0 ORDER BY last_indexed DESC, id DESC LIMIT 1) latest
		  ON latest.id = e.index_id
		WHERE e.relative_path = ? AND e.type = 'directory'
	`, path, SubtreeLikePattern(path), path, SubtreeLikePattern(path), path).Scan(&size, &files, &dirs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var initialized bool
			if checkErr := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM indexes WHERE last_indexed > 0)`).Scan(&initialized); checkErr == nil && !initialized {
				return 0, 0, 0, ErrNotInitialized
			}
			return 0, 0, 0, fmt.Errorf("%w: %s", ErrDirectoryNotFound, path)
		}
		return 0, 0, 0, fmt.Errorf("dir size query failed: %w", err)
	}
	if !size.Valid {
		size.Int64 = 0
	}
	return size.Int64, files, dirs, nil
}

// Stats represents database statistics
type Stats = api.Stats

// GetStats returns database statistics
func (s *Store) GetStats(ctx context.Context) (*Stats, error) {
	var stats Stats

	// Get database file size using the actual dbPath
	if s.dbPath != "" {
		if fi, err := os.Stat(s.dbPath); err == nil {
			stats.DatabaseSize = fi.Size()
		}
	}

	return &stats, nil
}

// SubfolderResult represents a direct subfolder with its size
type SubfolderResult = api.SubfolderResult

// GetDirectSubfolders returns all direct child folders of a given path with their sizes.
// This only returns immediate children (not recursive).
func (s *Store) GetDirectSubfolders(ctx context.Context, parentPath string) ([]SubfolderResult, error) {
	if ctx == nil {
		ctx = context.TODO()
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
            SELECT e.relative_path, e.name, e.size, e.mod_time
            FROM entries e
			JOIN (SELECT id FROM indexes WHERE last_indexed > 0 ORDER BY last_indexed DESC, id DESC LIMIT 1) latest
			  ON latest.id = e.index_id
			WHERE e.type = 'directory' AND e.path_depth = 1
            ORDER BY name
        `
		args = nil
	} else {
		// The child-path range constrains the 4th column of
		// idx_entries_subfolders after three equality columns, so this is a
		// pure index range scan; no residual filter is needed because direct
		// children always start with childLo.
		_, childLo, hi := subtreeBounds(parentPath)
		query = `
            SELECT e.relative_path, e.name, e.size, e.mod_time
            FROM entries e
			JOIN (SELECT id FROM indexes WHERE last_indexed > 0 ORDER BY last_indexed DESC, id DESC LIMIT 1) latest
			  ON latest.id = e.index_id
			WHERE e.type = 'directory' AND e.path_depth = ?
              AND e.relative_path >= ? AND e.relative_path < ?
            ORDER BY name
        `
		args = []any{childDepth, childLo, hi}
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

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil && !s.hasCompletedGeneration(ctx) {
		return nil, ErrNotInitialized
	}
	return results, nil
}

func (s *Store) hasCompletedGeneration(ctx context.Context) bool {
	var exists bool
	return s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM indexes WHERE last_indexed > 0)`).Scan(&exists) == nil && exists
}
