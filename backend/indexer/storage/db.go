package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
)

const (
	defaultDBPath = "indexer.db"
	schemaTimeout = 30 * time.Second
	batchSize     = 500
	batchTimeout  = 1 * time.Second
)

type OpenOptions struct {
	BusyTimeout     time.Duration
	JournalMode     string
	Synchronous     string
	AutoVacuum      string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
	// StmtCacheSize is the number of prepared statements the driver caches
	// per connection (go-sqlite3 _stmt_cache_size); 0 disables the cache.
	StmtCacheSize int
	// DisableFTS drops and stops maintaining the FTS5 search index even when
	// the binary was built with sqlite_fts5. Searches fall back to LIKE
	// scans; full fresh scans run substantially faster without trigram
	// maintenance. Re-enabling rebuilds the index from entries on next open.
	DisableFTS bool
}

func DefaultOpenOptions() OpenOptions {
	return OpenOptions{
		BusyTimeout:     5 * time.Second,
		JournalMode:     "WAL",
		Synchronous:     "OFF",
		AutoVacuum:      "INCREMENTAL",
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxIdleTime: 5 * time.Minute,
		StmtCacheSize:   16,
	}
}

func NormalizeOpenOptions(opts OpenOptions) (OpenOptions, error) {
	defaults := DefaultOpenOptions()
	if opts.JournalMode == "" {
		opts.JournalMode = defaults.JournalMode
	}
	if opts.Synchronous == "" {
		opts.Synchronous = defaults.Synchronous
	}
	if opts.AutoVacuum == "" {
		opts.AutoVacuum = defaults.AutoVacuum
	}

	if opts.BusyTimeout < 0 {
		return OpenOptions{}, fmt.Errorf("db busy timeout must be non-negative")
	}
	if opts.MaxOpenConns < 0 {
		return OpenOptions{}, fmt.Errorf("db max open conns must be non-negative")
	}
	if opts.MaxIdleConns < 0 {
		return OpenOptions{}, fmt.Errorf("db max idle conns must be non-negative")
	}
	if opts.ConnMaxIdleTime < 0 {
		return OpenOptions{}, fmt.Errorf("db conn max idle time must be non-negative")
	}
	if opts.StmtCacheSize < 0 {
		return OpenOptions{}, fmt.Errorf("db stmt cache size must be non-negative")
	}
	opts.JournalMode = strings.ToUpper(strings.TrimSpace(opts.JournalMode))
	opts.Synchronous = strings.ToUpper(strings.TrimSpace(opts.Synchronous))
	opts.AutoVacuum = strings.ToUpper(strings.TrimSpace(opts.AutoVacuum))
	if !validSQLiteSetting(opts.JournalMode, "DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF") {
		return OpenOptions{}, fmt.Errorf("invalid db journal mode %q", opts.JournalMode)
	}
	if !validSQLiteSetting(opts.Synchronous, "OFF", "NORMAL", "FULL", "EXTRA") {
		return OpenOptions{}, fmt.Errorf("invalid db synchronous %q", opts.Synchronous)
	}
	if !validSQLiteSetting(opts.AutoVacuum, "NONE", "FULL", "INCREMENTAL") {
		return OpenOptions{}, fmt.Errorf("invalid db auto vacuum %q", opts.AutoVacuum)
	}
	return opts, nil
}

func validSQLiteSetting(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}

// dbExecutor is an interface that both sql.DB and sql.Tx implement
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ProgressCallback is called after each batch write with cumulative counts and last path processed
type ProgressCallback func(filesWritten, dirsWritten int64, lastPath string)

// StreamingWriter accepts entries via a channel and writes them to the database in batches.
type StreamingWriter struct {
	db           *sql.DB
	indexID      int64
	scanTime     int64
	entryCh      chan indexing.IndexEntry
	doneCh       chan error
	ctx          context.Context
	cancel       context.CancelFunc
	errVal       atomic.Value
	progressCb   ProgressCallback
	filesWritten int64
	dirsWritten  int64
	lastPath     string
}

// NewStreamingWriter creates a writer with an optional progress callback.
// The callback is invoked after each entry is processed with cumulative file/dir counts.
func NewStreamingWriter(ctx context.Context, db *sql.DB, indexID int64, bufferSize int, progressCb ProgressCallback) *StreamingWriter {
	if ctx == nil {
		ctx = context.Background()
	}
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	ctx, cancel := context.WithCancel(ctx)
	sw := &StreamingWriter{
		db:      db,
		indexID: indexID,
		// Nanoseconds, not seconds: last_seen cleanup deletes rows with
		// last_seen < scanTime, so two scans starting within the same second
		// would otherwise retain each other's deleted rows.
		scanTime:   time.Now().UTC().UnixNano(),
		entryCh:    make(chan indexing.IndexEntry, bufferSize),
		doneCh:     make(chan error, 1),
		ctx:        ctx,
		cancel:     cancel,
		progressCb: progressCb,
	}
	go sw.run()
	return sw
}

// Write sends an entry to be written to the database.
func (sw *StreamingWriter) Write(entry indexing.IndexEntry) error {
	select {
	case sw.entryCh <- entry:
		return nil
	case <-sw.ctx.Done():
		if v, ok := sw.errVal.Load().(error); ok && v != nil {
			return v
		}
		return sw.ctx.Err()
	}
}

// Close signals completion and waits for all pending writes to finish.
func (sw *StreamingWriter) Close() error {
	close(sw.entryCh)
	return <-sw.doneCh
}

// DB returns the underlying database connection for direct operations.
func (sw *StreamingWriter) DB() *sql.DB {
	return sw.db
}

// IndexID returns the index ID this writer is associated with.
func (sw *StreamingWriter) IndexID() int64 {
	return sw.indexID
}

// ScanTime returns the timestamp when this scan started.
func (sw *StreamingWriter) ScanTime() int64 {
	return sw.scanTime
}

// run is the background goroutine that batches and writes entries.
func (sw *StreamingWriter) run() {
	err := sw.processEntries()
	if err != nil {
		sw.errVal.Store(err)
	}
	sw.doneCh <- err
	sw.cancel()
}

func (sw *StreamingWriter) processEntries() error {
	batch := make([]indexing.IndexEntry, 0, batchSize)
	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-sw.entryCh:
			if !ok {
				return sw.flushBatch(&batch)
			}
			sw.recordProgress(entry)
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				if err := sw.flushBatch(&batch); err != nil {
					return err
				}
			}
		case <-ticker.C:
			if err := sw.flushBatch(&batch); err != nil {
				return err
			}
		case <-sw.ctx.Done():
			return sw.ctx.Err()
		}
	}
}

func (sw *StreamingWriter) flushBatch(batch *[]indexing.IndexEntry) error {
	if len(*batch) == 0 {
		return nil
	}
	if err := sw.writeBatch(*batch); err != nil {
		return err
	}
	// Clear the backing array to release string references.
	for i := range *batch {
		(*batch)[i] = indexing.IndexEntry{}
	}
	*batch = (*batch)[:0]
	return nil
}

func (sw *StreamingWriter) recordProgress(entry indexing.IndexEntry) {
	if entry.Type == "directory" {
		sw.dirsWritten++
	} else {
		sw.filesWritten++
	}
	sw.lastPath = entry.RelativePath
	if sw.progressCb != nil {
		sw.progressCb(sw.filesWritten, sw.dirsWritten, sw.lastPath)
	}
}

// writeBatch writes a batch of entries within a single transaction.
func (sw *StreamingWriter) writeBatch(batch []indexing.IndexEntry) error {
	if len(batch) == 0 {
		return nil
	}

	tx, err := sw.db.BeginTx(sw.ctx, nil)
	if err != nil {
		return err
	}
	// Rollback unconditionally: after a successful Commit it returns
	// ErrTxDone, which is ignored. Guarding on an error variable instead is
	// fragile — a shadowed err on any failure path leaks the transaction and
	// pins both a pool connection and SQLite's write lock.
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("writeBatch rollback failed", "err", rollbackErr)
		}
	}()

	err = insertEntriesBatch(sw.ctx, tx, sw.indexID, sw.scanTime, batch)
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

// Open creates (or reuses) a SQLite database with the supplied options and
// ensures the schema exists.
func Open(path string, opts OpenOptions) (*sql.DB, error) {
	if path == "" {
		path = defaultDBPath
	}
	if opts == (OpenOptions{}) {
		opts = DefaultOpenOptions()
	}
	opts, err := NormalizeOpenOptions(opts)
	if err != nil {
		return nil, err
	}

	// Use WAL for concurrent readers while streaming writes happen.
	// auto_vacuum=INCREMENTAL to automatically reclaim space when deleting records.
	// The path is URI-escaped (keeping '/' literal) inside a file: URI so that
	// reserved characters like '?' or '#' in filenames cannot be misparsed as
	// DSN parameters.
	escapedPath := strings.ReplaceAll(url.PathEscape(path), "%2F", "/")
	dsn := fmt.Sprintf("file:%s?_busy_timeout=%d&_foreign_keys=on&_journal_mode=%s&_synchronous=%s&_auto_vacuum=%s&_stmt_cache_size=%d",
		escapedPath,
		int(opts.BusyTimeout/time.Millisecond),
		opts.JournalMode,
		opts.Synchronous,
		opts.AutoVacuum,
		opts.StmtCacheSize,
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), schemaTimeout)
	defer cancel()

	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = `+opts.JournalMode+`;`).Scan(&journalMode); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close DB after journal mode setup error", "err", closeErr)
		}
		return nil, err
	}

	// Configure connection pool for WAL mode concurrent access
	// WAL mode allows multiple readers + 1 writer simultaneously
	db.SetMaxOpenConns(opts.MaxOpenConns)
	db.SetMaxIdleConns(opts.MaxIdleConns)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(opts.ConnMaxIdleTime)

	if err := initSchema(ctx, db, opts.DisableFTS); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close DB after schema init error", "err", closeErr)
		}
		return nil, err
	}

	return db, nil
}

// GetJournalMode returns the SQLite journal mode for the provided database.
func GetJournalMode(ctx context.Context, db *sql.DB) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return "", fmt.Errorf("db is nil")
	}

	var mode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

func initSchema(ctx context.Context, db *sql.DB, disableFTS bool) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		return err
	}

	// name is deliberately NOT unique: every fresh scan inserts a new
	// generation row for its name with last_indexed=0 and publishes it by
	// setting last_indexed on success, so readers atomically switch from the
	// previous generation and a failed scan leaves the old one untouched.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS indexes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			root_path TEXT NOT NULL,
			source TEXT,
			include_hidden INTEGER NOT NULL DEFAULT 0,
			num_dirs INTEGER NOT NULL DEFAULT 0,
			num_files INTEGER NOT NULL DEFAULT 0,
			total_size INTEGER NOT NULL DEFAULT 0,
			disk_used INTEGER NOT NULL DEFAULT 0,
			disk_total INTEGER NOT NULL DEFAULT 0,
			last_indexed INTEGER NOT NULL,
			index_duration_ms INTEGER NOT NULL DEFAULT 0,
			export_duration_ms INTEGER NOT NULL DEFAULT 0,
			vacuum_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			index_id INTEGER NOT NULL,
			relative_path TEXT NOT NULL,
			path_depth INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			size INTEGER NOT NULL,
			mod_time INTEGER NOT NULL,
			type TEXT NOT NULL,
			hidden INTEGER NOT NULL DEFAULT 0,
			inode INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (index_id) REFERENCES indexes(id) ON DELETE CASCADE
		);
	`); err != nil {
		return err
	}

	// Breaking schema change: absolute_path is no longer stored.
	// If an existing database still has entries.absolute_path, require a rebuild.
	hasAbsolutePath, err := tableHasColumn(ctx, db, "entries", "absolute_path")
	if err != nil {
		return err
	}
	if hasAbsolutePath {
		return fmt.Errorf("unsupported database schema: entries.absolute_path exists; delete the DB file and reindex to rebuild")
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_index_id ON entries(index_id);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_path ON entries(index_id, relative_path);
	`); err != nil {
		return err
	}

	if err := ensureColumn(ctx, db, "entries", "path_depth", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "entries", "inode", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "entries", "last_seen", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "indexes", "index_duration_ms", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "indexes", "export_duration_ms", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "indexes", "vacuum_duration_ms", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	if err := migrateIndexesDropUniqueName(ctx, db); err != nil {
		return fmt.Errorf("migrate indexes table: %w", err)
	}

	// Backfill path_depth for existing rows (0 for root "/", otherwise number of segments)
	// Safe to run on every startup; it only updates rows still at the default value.
	if _, err := db.ExecContext(ctx, `
		UPDATE entries
		SET path_depth = (LENGTH(relative_path) - LENGTH(REPLACE(relative_path, '/', '')))
		WHERE relative_path != '/' AND path_depth = 0;
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_subfolders ON entries(index_id, type, path_depth, relative_path);
	`); err != nil {
		return err
	}

	return ensureFTS(ctx, db, disableFTS)
}

// migrateIndexesDropUniqueName rebuilds a legacy indexes table to remove the
// UNIQUE constraint on name (SQLite cannot drop constraints in place).
// Generation-based scans insert multiple rows per name, which the old
// constraint forbids. Row ids are preserved so entries' foreign keys stay
// valid; the rebuild runs on one connection with foreign_keys off (a no-op
// inside a transaction, so it is toggled outside one).
func migrateIndexesDropUniqueName(ctx context.Context, db *sql.DB) error {
	var tableSQL sql.NullString
	err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'indexes';`).Scan(&tableSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToUpper(tableSQL.String), "UNIQUE") {
		return nil
	}
	slog.Info("migrating indexes table: dropping UNIQUE constraint on name for generation-based scans")

	// Copy only columns the legacy table actually has; the new table's
	// defaults cover anything it lacks (e.g. created_at on very old DBs).
	canonical := []string{
		"id", "name", "root_path", "source", "include_hidden",
		"num_dirs", "num_files", "total_size", "disk_used", "disk_total",
		"last_indexed", "index_duration_ms", "export_duration_ms",
		"vacuum_duration_ms", "created_at",
	}
	var copyCols []string
	for _, col := range canonical {
		found, colErr := tableHasColumn(ctx, db, "indexes", col)
		if colErr != nil {
			return colErr
		}
		if found {
			copyCols = append(copyCols, col)
		}
	}
	colsCSV := strings.Join(copyCols, ", ")

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.Warn("failed to release migration connection", "err", closeErr)
		}
	}()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF;`); err != nil {
		return err
	}
	defer func() {
		if _, fkErr := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON;`); fkErr != nil {
			slog.Warn("failed to re-enable foreign keys after migration", "err", fkErr)
		}
	}()

	stmts := []string{
		`BEGIN IMMEDIATE;`,
		`CREATE TABLE indexes_migrated (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			root_path TEXT NOT NULL,
			source TEXT,
			include_hidden INTEGER NOT NULL DEFAULT 0,
			num_dirs INTEGER NOT NULL DEFAULT 0,
			num_files INTEGER NOT NULL DEFAULT 0,
			total_size INTEGER NOT NULL DEFAULT 0,
			disk_used INTEGER NOT NULL DEFAULT 0,
			disk_total INTEGER NOT NULL DEFAULT 0,
			last_indexed INTEGER NOT NULL,
			index_duration_ms INTEGER NOT NULL DEFAULT 0,
			export_duration_ms INTEGER NOT NULL DEFAULT 0,
			vacuum_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);`,
		fmt.Sprintf(`INSERT INTO indexes_migrated (%s) SELECT %s FROM indexes;`, colsCSV, colsCSV),
		`DROP TABLE indexes;`,
		`ALTER TABLE indexes_migrated RENAME TO indexes;`,
		`COMMIT;`,
	}
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if _, rbErr := conn.ExecContext(ctx, `ROLLBACK;`); rbErr != nil {
				slog.Warn("migration rollback failed", "err", rbErr)
			}
			return fmt.Errorf("indexes table migration failed: %w", err)
		}
	}
	return nil
}

// tableHasColumn reports whether table has a column with the given name.
func tableHasColumn(ctx context.Context, db *sql.DB, table, column string) (found bool, err error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s);`, table))
	if err != nil {
		return false, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Warn("failed to close table_info rows", "table", table, "err", closeErr)
		}
	}()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	found, err := tableHasColumn(ctx, db, table, column)
	if err != nil || found {
		return err
	}
	stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s;`, table, column, definition)
	_, err = db.ExecContext(ctx, stmt)
	return err
}

func insertEntriesBatch(ctx context.Context, tx *sql.Tx, indexID int64, scanTime int64, batch []indexing.IndexEntry) error {
	if len(batch) == 0 {
		return nil
	}

	const insertPrefix = `
INSERT INTO entries (
	index_id,
	relative_path,
	path_depth,
	name,
	size,
	mod_time,
	type,
	hidden,
	inode,
	last_seen
) VALUES `
	const singlePlaceholder = "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	const upsertSuffix = `
ON CONFLICT(index_id, relative_path) DO UPDATE SET
	name = excluded.name,
	size = excluded.size,
	mod_time = excluded.mod_time,
	type = excluded.type,
	hidden = excluded.hidden,
	inode = excluded.inode,
	last_seen = excluded.last_seen;
`

	var builder strings.Builder
	builder.Grow(len(insertPrefix) + len(singlePlaceholder)*len(batch) + len(batch) + len(upsertSuffix))
	builder.WriteString(insertPrefix)

	args := make([]any, 0, len(batch)*10)
	for i, entry := range batch {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(singlePlaceholder)
		pathDepth := 0
		if entry.RelativePath != "/" {
			pathDepth = strings.Count(entry.RelativePath, "/")
		}
		args = append(args,
			indexID,
			entry.RelativePath,
			pathDepth,
			entry.Name,
			entry.Size,
			entry.ModTime.Unix(),
			entry.Type,
			indexing.BoolToInt(entry.Hidden),
			int64(entry.Inode),
			scanTime,
		)
	}

	builder.WriteString(upsertSuffix)

	_, err := tx.ExecContext(ctx, builder.String(), args...)
	return err
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

// subtreeBounds returns half-open relative_path bounds [lo, hi) spanning the
// entry at path plus its whole subtree, so the (index_id, relative_path)
// index can be range-seeked instead of scanned with a non-sargable LIKE
// (LIKE cannot use a BINARY-collated index under case-insensitive matching).
// The range also admits sibling paths whose byte after the prefix sorts below
// '/' (e.g. "/a.bak" for "/a"), so subtree queries must add the residual
// filter (relative_path = lo OR relative_path >= childLo). For the root path
// the bounds cover the whole partition and the residual passes everything.
func subtreeBounds(path string) (lo, childLo, hi string) {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/", "/", string('/' + 1)
	}
	return trimmed, trimmed + "/", trimmed + string('/'+1)
}

// SubtreeLikePattern returns an escaped LIKE pattern for descendants of path.
func SubtreeLikePattern(path string) string {
	if path == "" || path == "/" {
		return "/%"
	}
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/%"
	}
	return escapeLikePattern(trimmed) + "/%"
}

func parentDirKey(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return "/"
	}
	parent := filepath.Dir(trimmed)
	if parent == "." || parent == "" {
		return "/"
	}
	if parent == "/" {
		return "/"
	}
	return "/" + strings.Trim(parent, "/")
}

// UpdateEntry updates or inserts a single entry in the database.
// Returns the old size if the entry existed, or 0 if it's new.
func UpdateEntry(ctx context.Context, db dbExecutor, indexID int64, entry indexing.IndexEntry) (oldSize int64, err error) {
	ctx = ensureContext(ctx)

	// Check if entry already exists and get old size
	var existingSize sql.NullInt64
	err = db.QueryRowContext(ctx, `
		SELECT size FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, entry.RelativePath).Scan(&existingSize)

	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	if existingSize.Valid {
		oldSize = existingSize.Int64
	}

	// Upsert the entry
	pathDepth := 0
	if entry.RelativePath != "/" {
		pathDepth = strings.Count(entry.RelativePath, "/")
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO entries (
			index_id, relative_path, path_depth, name, size, mod_time,
			type, hidden, inode
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(index_id, relative_path) DO UPDATE SET
			name = excluded.name,
			size = excluded.size,
			mod_time = excluded.mod_time,
			type = excluded.type,
			hidden = excluded.hidden,
			inode = excluded.inode;
	`,
		indexID,
		entry.RelativePath,
		pathDepth,
		entry.Name,
		entry.Size,
		entry.ModTime.Unix(),
		entry.Type,
		indexing.BoolToInt(entry.Hidden),
		int64(entry.Inode),
	)

	return oldSize, err
}

// UpdateParentDirectorySizes propagates size changes up the directory tree.
// sizeDelta is the change in size (positive for additions, negative for deletions).
func UpdateParentDirectorySizes(ctx context.Context, db dbExecutor, indexID int64, childPath string, sizeDelta int64) error {
	ctx = ensureContext(ctx)
	if sizeDelta == 0 {
		return nil
	}

	currentPath := parentDirKey(childPath)

	// Walk up the directory tree
	for currentPath != "" {
		_, err := db.ExecContext(ctx, `
			UPDATE entries
			SET size = size + ?
			WHERE index_id = ? AND relative_path = ? AND type = 'directory';
		`, sizeDelta, indexID, currentPath)

		if err != nil {
			return err
		}

		// Move to parent
		if currentPath == "/" {
			break
		}
		currentPath = parentDirKey(currentPath)
	}

	return nil
}

// UpsertEntryWithSizeUpdate updates an entry and propagates size changes to parents.
// This is a convenience function that combines UpdateEntry and UpdateParentDirectorySizes.
func UpsertEntryWithSizeUpdate(ctx context.Context, db *sql.DB, indexID int64, entry indexing.IndexEntry) error {
	ctx = ensureContext(ctx)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("UpsertEntryWithSizeUpdate rollback failed", "err", rollbackErr)
		}
	}()

	oldSize, err := UpdateEntry(ctx, tx, indexID, entry)
	if err != nil {
		return err
	}

	sizeDelta := entry.Size - oldSize
	if sizeDelta != 0 {
		if err := UpdateParentDirectorySizes(ctx, tx, indexID, entry.RelativePath, sizeDelta); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeletePathRecursive deletes all entries under a path (including the path itself) and propagates size changes.
func DeletePathRecursive(ctx context.Context, db *sql.DB, indexID int64, relativePath string) error {
	ctx = ensureContext(ctx)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("DeletePathRecursive rollback failed", "err", rollbackErr)
		}
	}()

	// Read the size of the target row. Directory rows already hold the rolled-up
	// subtree size, and file rows hold their own size — either way, this is the
	// correct delta to apply to ancestors. Summing across the LIKE pattern would
	// double-count, since descendants are already accounted for in the parent
	// directory's size column.
	var targetSize sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT size FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, relativePath).Scan(&targetSize)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Delete all entries under this path (including the path itself)
	lo, childLo, hi := subtreeBounds(relativePath)
	_, err = tx.ExecContext(ctx, `
		DELETE FROM entries
		WHERE index_id = ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, lo, hi, lo, childLo)
	if err != nil {
		return err
	}

	// Propagate size changes to parent directories
	if targetSize.Valid && targetSize.Int64 != 0 {
		if err := UpdateParentDirectorySizes(ctx, tx, indexID, relativePath, -targetSize.Int64); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CleanupDeletedEntries removes entries that were not seen during the latest scan.
// Returns the number of entries deleted.
func CleanupDeletedEntries(ctx context.Context, db *sql.DB, indexID int64, scanTime int64) (int64, error) {
	ctx = ensureContext(ctx)

	result, err := db.ExecContext(ctx, `
		DELETE FROM entries
		WHERE index_id = ? AND last_seen < ?;
	`, indexID, scanTime)
	if err != nil {
		return 0, err
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return deleted, nil
}

// CleanupDeletedEntriesUnderPath removes entries under a specific path that were not seen during the latest scan.
// This is used for partial reindexing to avoid deleting entries outside the reindexed path.
// Returns the number of entries deleted.
func CleanupDeletedEntriesUnderPath(ctx context.Context, db *sql.DB, indexID int64, relativePath string, scanTime int64) (int64, error) {
	ctx = ensureContext(ctx)

	lo, childLo, hi := subtreeBounds(relativePath)
	result, err := db.ExecContext(ctx, `
		DELETE FROM entries
		WHERE index_id = ?
		  AND last_seen < ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, scanTime, lo, hi, lo, childLo)
	if err != nil {
		return 0, err
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return deleted, nil
}

// ReleaseSQLiteMemory forces SQLite to release cached memory.
// Call this after heavy write operations to return memory to the OS.
func ReleaseSQLiteMemory(ctx context.Context, db *sql.DB) error {
	ctx = ensureContext(ctx)
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	var releaseErr error
	// Shrink SQLite's page cache
	if _, err := db.ExecContext(ctx, `PRAGMA shrink_memory;`); err != nil {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("shrink SQLite memory: %w", err))
	}

	// Optimize database (lightweight, doesn't rebuild like VACUUM)
	if _, err := db.ExecContext(ctx, `PRAGMA optimize;`); err != nil {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("optimize SQLite database: %w", err))
	}

	return releaseErr
}
