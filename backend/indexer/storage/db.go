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
	schemaVersion = 2
	schemaTimeout = 30 * time.Second
	batchSize     = 500
	batchTimeout  = 1 * time.Second
)

// ErrIncompatibleSchema means the cache belongs to a different schema version.
var ErrIncompatibleSchema = errors.New("incompatible database schema")

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
	if !slices.Contains([]string{"DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF"}, opts.JournalMode) {
		return OpenOptions{}, fmt.Errorf("invalid db journal mode %q", opts.JournalMode)
	}
	if !slices.Contains([]string{"OFF", "NORMAL", "FULL", "EXTRA"}, opts.Synchronous) {
		return OpenOptions{}, fmt.Errorf("invalid db synchronous %q", opts.Synchronous)
	}
	if !slices.Contains([]string{"NONE", "FULL", "INCREMENTAL"}, opts.AutoVacuum) {
		return OpenOptions{}, fmt.Errorf("invalid db auto vacuum %q", opts.AutoVacuum)
	}
	return opts, nil
}

// dbExecutor is an interface that both sql.DB and sql.Tx implement
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ProgressCallback is called after each batch write with cumulative counts and last path processed
type ProgressCallback func(filesWritten, dirsWritten int64, lastPath string)

// StreamingWriter accepts entries via a channel and writes them to the database in batches.
type StreamingWriter struct {
	db           *sql.DB
	tx           *sql.Tx
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
	return newStreamingWriter(ctx, db, nil, indexID, bufferSize, progressCb)
}

// NewTransactionalStreamingWriter writes every batch through tx. The caller
// owns commit or rollback after traversal and cleanup complete.
func NewTransactionalStreamingWriter(ctx context.Context, tx *sql.Tx, indexID int64, bufferSize int, progressCb ProgressCallback) *StreamingWriter {
	return newStreamingWriter(ctx, nil, tx, indexID, bufferSize, progressCb)
}

func newStreamingWriter(ctx context.Context, db *sql.DB, tx *sql.Tx, indexID int64, bufferSize int, progressCb ProgressCallback) *StreamingWriter {
	if ctx == nil {
		ctx = context.TODO()
	}
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	ctx, cancel := context.WithCancel(ctx)
	sw := &StreamingWriter{
		db:      db,
		tx:      tx,
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
	if sw.tx != nil {
		return insertEntriesBatch(sw.ctx, sw.tx, sw.indexID, sw.scanTime, batch)
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
	return OpenContext(context.Background(), path, opts)
}

// OpenContext opens and initializes the cache while honoring ctx cancellation.
func OpenContext(parent context.Context, path string, opts OpenOptions) (*sql.DB, error) {
	if parent == nil {
		parent = context.TODO()
	}
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

	ctx, cancel := context.WithTimeout(parent, schemaTimeout)
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

	if err := initSchema(ctx, db); err != nil {
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
		ctx = context.TODO()
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

func initSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		return err
	}
	if err := validateSchemaVersion(ctx, db); err != nil {
		return err
	}

	// Every full scan inserts a new generation with last_indexed=0 and publishes it by
	// setting last_indexed on success, so readers atomically switch from the
	// previous generation and a failed scan leaves the old one untouched.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS indexes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			num_dirs INTEGER NOT NULL DEFAULT 0,
			num_files INTEGER NOT NULL DEFAULT 0,
			total_size INTEGER NOT NULL DEFAULT 0,
			last_indexed INTEGER NOT NULL,
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
			device INTEGER NOT NULL DEFAULT 0,
			inode INTEGER NOT NULL DEFAULT 0,
			size_contribution INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (index_id) REFERENCES indexes(id) ON DELETE CASCADE
		);
	`); err != nil {
		return err
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

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_subfolders ON entries(index_id, type, path_depth, relative_path);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_hardlinks ON entries(index_id, device, inode);
	`); err != nil {
		return err
	}

	if err := ensureFTS(ctx, db); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion))
	return err
}

func validateSchemaVersion(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version;`).Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if version == schemaVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("%w: got %d, want %d", ErrIncompatibleSchema, version, schemaVersion)
	}

	var tables int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%';
	`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect unversioned database schema: %w", err)
	}
	if tables != 0 {
		return fmt.Errorf("%w: existing database is unversioned", ErrIncompatibleSchema)
	}
	return nil
}

func insertEntriesBatch(ctx context.Context, tx dbExecutor, indexID int64, scanTime int64, batch []indexing.IndexEntry) error {
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
	device,
	inode,
	size_contribution,
	last_seen
) VALUES `
	const singlePlaceholder = "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	const upsertSuffix = `
ON CONFLICT(index_id, relative_path) DO UPDATE SET
	name = excluded.name,
	size = excluded.size,
	mod_time = excluded.mod_time,
	type = excluded.type,
	hidden = excluded.hidden,
	device = excluded.device,
	inode = excluded.inode,
	size_contribution = excluded.size_contribution,
	last_seen = excluded.last_seen;
`

	var builder strings.Builder
	builder.Grow(len(insertPrefix) + len(singlePlaceholder)*len(batch) + len(batch) + len(upsertSuffix))
	builder.WriteString(insertPrefix)

	args := make([]any, 0, len(batch)*12)
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
			int64(entry.Device),
			int64(entry.Inode),
			entry.SizeContribution,
			scanTime,
		)
	}

	builder.WriteString(upsertSuffix)

	_, err := tx.ExecContext(ctx, builder.String(), args...)
	return err
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
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

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("query existing entry %q: %w", entry.RelativePath, err)
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
			type, hidden, device, inode, size_contribution
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(index_id, relative_path) DO UPDATE SET
			name = excluded.name,
			size = excluded.size,
			mod_time = excluded.mod_time,
			type = excluded.type,
			hidden = excluded.hidden,
			device = excluded.device,
			inode = excluded.inode,
			size_contribution = excluded.size_contribution;
	`,
		indexID,
		entry.RelativePath,
		pathDepth,
		entry.Name,
		entry.Size,
		entry.ModTime.Unix(),
		entry.Type,
		indexing.BoolToInt(entry.Hidden),
		int64(entry.Device),
		int64(entry.Inode),
		entry.SizeContribution,
	)

	if err != nil {
		return oldSize, fmt.Errorf("upsert entry %q: %w", entry.RelativePath, err)
	}
	return oldSize, nil
}

// UpdateParentDirectorySizes propagates size changes up the directory tree.
// sizeDelta is the change in size (positive for additions, negative for deletions).
func UpdateParentDirectorySizes(ctx context.Context, db dbExecutor, indexID int64, childPath string, sizeDelta int64) error {
	return updateParentDirectorySizesThrough(ctx, db, indexID, childPath, sizeDelta, "")
}

func updateParentDirectorySizesThrough(ctx context.Context, db dbExecutor, indexID int64, childPath string, sizeDelta int64, stopPath string) error {
	ctx = ensureContext(ctx)
	if sizeDelta == 0 {
		return nil
	}
	if stopPath != "" {
		stopPath = indexing.NormalizeIndexPath(stopPath)
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
			return fmt.Errorf("update parent directory %q: %w", currentPath, err)
		}

		// Move to parent
		if currentPath == "/" || currentPath == stopPath {
			break
		}
		currentPath = parentDirKey(currentPath)
	}

	return nil
}

// UpdateIndexMetadata applies entry-count deltas and synchronizes the stored
// total and timestamp with the queryable generation.
func UpdateIndexMetadata(ctx context.Context, db dbExecutor, indexID, dirDelta, fileDelta int64) error {
	ctx = ensureContext(ctx)
	_, err := db.ExecContext(ctx, `
		UPDATE indexes
		SET num_dirs = num_dirs + ?,
			num_files = num_files + ?,
			total_size = COALESCE((
				SELECT size FROM entries
				WHERE index_id = ? AND relative_path = '/'
			), 0),
			last_indexed = CAST(strftime('%s', 'now') AS INTEGER)
		WHERE id = ?;
	`, dirDelta, fileDelta, indexID, indexID)
	return err
}

// CountEntriesUnderPath counts queryable directory and non-directory entries
// in one subtree.
func CountEntriesUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string) (dirs, files int64, err error) {
	ctx = ensureContext(ctx)
	lo, childLo, hi := subtreeBounds(relativePath)
	err = db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 0 ELSE 1 END), 0)
		FROM entries
		WHERE index_id = ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, lo, hi, lo, childLo).Scan(&dirs, &files)
	return dirs, files, err
}

func entryTypeDelta(oldType string, existed bool, newType string) (dirs, files int64) {
	if existed && oldType == newType {
		return 0, 0
	}
	if existed {
		if oldType == "directory" {
			dirs--
		} else {
			files--
		}
	}
	if newType == "directory" {
		dirs++
	} else {
		files++
	}
	return dirs, files
}

// UpsertEntryWithSizeUpdate updates an entry and propagates size changes to parents.
// This is a convenience function that combines UpdateEntry and UpdateParentDirectorySizes.
func UpsertEntryWithSizeUpdate(ctx context.Context, db *sql.DB, indexID int64, entry indexing.IndexEntry) error {
	ctx = ensureContext(ctx)
	if entry.Type == "directory" {
		return fmt.Errorf("cannot add a directory entry; reindex its subtree")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entry upsert: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("UpsertEntryWithSizeUpdate rollback failed", "err", rollbackErr)
		}
	}()

	old, err := loadStoredEntryAccounting(ctx, tx, indexID, entry.RelativePath)
	if err != nil {
		return fmt.Errorf("read existing entry type: %w", err)
	}
	if old.exists && old.typ == "directory" {
		return fmt.Errorf("cannot replace a directory entry; reindex its parent")
	}
	if err := replaceEntryAccounting(ctx, tx, indexID, entry, old); err != nil {
		return fmt.Errorf("replace entry accounting: %w", err)
	}

	dirDelta, fileDelta := entryTypeDelta(old.typ, old.exists, entry.Type)
	if err := UpdateIndexMetadata(ctx, tx, indexID, dirDelta, fileDelta); err != nil {
		return fmt.Errorf("update index metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entry upsert: %w", err)
	}
	return nil
}

// DeletePathRecursive deletes all entries under a path (including the path itself) and propagates size changes.
func DeletePathRecursive(ctx context.Context, db *sql.DB, indexID int64, relativePath string) error {
	ctx = ensureContext(ctx)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recursive delete: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("DeletePathRecursive rollback failed", "err", rollbackErr)
		}
	}()

	deletedDirs, deletedFiles, err := CountEntriesUnderPath(ctx, tx, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("count deleted entries: %w", err)
	}

	removedContribution, err := removedContributionForPath(ctx, tx, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("read removed contribution: %w", err)
	}
	promotedGroups, err := promotedHardlinksForDelete(ctx, tx, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("find hardlinks requiring promotion: %w", err)
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
		return fmt.Errorf("delete entries under %q: %w", relativePath, err)
	}

	if removedContribution != 0 {
		if err := UpdateParentDirectorySizes(ctx, tx, indexID, relativePath, -removedContribution); err != nil {
			return fmt.Errorf("subtract removed contribution: %w", err)
		}
	}
	if err := promoteHardlinks(ctx, tx, indexID, promotedGroups); err != nil {
		return fmt.Errorf("promote surviving hardlinks: %w", err)
	}
	if err := UpdateIndexMetadata(ctx, tx, indexID, -deletedDirs, -deletedFiles); err != nil {
		return fmt.Errorf("update index metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit recursive delete: %w", err)
	}
	return nil
}

// CleanupDeletedEntriesUnderPath removes entries under a specific path that were not seen during the latest scan.
// This is used for partial reindexing to avoid deleting entries outside the reindexed path.
// Returns the number of entries deleted.
func CleanupDeletedEntriesUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string, scanTime int64) (int64, error) {
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
