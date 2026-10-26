package storage

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

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

// NewStreamingWriter creates a writer that commits each batch in its own
// transaction. progressCb may be nil; otherwise it is invoked after each entry
// is processed with cumulative file/dir counts.
func (s *Store) NewStreamingWriter(ctx context.Context, indexID int64, bufferSize int, progressCb ProgressCallback) *StreamingWriter {
	return newStreamingWriter(ctx, s.db, nil, indexID, bufferSize, progressCb)
}

func newStreamingWriter(ctx context.Context, db *sql.DB, tx *sql.Tx, indexID int64, bufferSize int, progressCb ProgressCallback) *StreamingWriter {
	if bufferSize <= 0 {
		bufferSize = defaultWriterBuffer
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
	defer rollbackTx(tx, "writeBatch")

	err = insertEntriesBatch(sw.ctx, tx, sw.indexID, sw.scanTime, batch)
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
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
			boolToInt(entry.Hidden),
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

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
