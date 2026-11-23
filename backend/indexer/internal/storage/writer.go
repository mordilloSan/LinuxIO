package storage

import (
	"context"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// ProgressCallback is called after each batch write with cumulative counts and last path processed
type ProgressCallback func(filesWritten, dirsWritten int64, lastPath string)

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
	device,
	inode,
	nlink,
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
	device = excluded.device,
	inode = excluded.inode,
	nlink = excluded.nlink,
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
			typeCode(entry.Type),
			int64(entry.Device),
			int64(entry.Inode),
			storedNlink(entry),
			entry.SizeContribution,
			scanTime,
		)
	}

	builder.WriteString(upsertSuffix)

	_, err := tx.ExecContext(ctx, builder.String(), args...)
	return err
}

// storedNlink is the link count kept with an entry: 1 for everything that is
// not a file with several links, which is all the hard link accounting asks.
func storedNlink(entry indexing.IndexEntry) int64 {
	if entry.Type == "directory" || entry.Nlink < 2 {
		return 1
	}
	return int64(entry.Nlink)
}
