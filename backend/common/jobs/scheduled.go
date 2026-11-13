package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	// MaxScheduledRunsPerUnit is how many ended runs of one systemd unit the
	// scheduled store keeps.
	MaxScheduledRunsPerUnit = 50
	// MaxOutputBytes is the size of the output kept for one run.
	MaxOutputBytes = 10 << 20
	// DefaultOutputChunk is how much of an output ReadOutput returns when
	// asked for no particular amount, and MaxOutputChunk the most it returns.
	DefaultOutputChunk = 256 << 10
	MaxOutputChunk     = 1 << 20
)

// NewScheduledStore opens the store of runs that systemd timers start.
func NewScheduledStore() *Store {
	return NewStore(
		filepath.Join(version.DataDir, "jobs", "scheduled"),
		filepath.Join(version.RunDir, "jobs", "scheduled"),
	)
}

// UnitRunID turns the 32 lowercase hex characters of a systemd invocation ID
// into the job ID of that run.
func UnitRunID(invocationID string) (string, error) {
	if len(invocationID) != 32 {
		return "", errors.New("invocation ID must be 32 hex characters")
	}
	for _, c := range invocationID {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", errors.New("invocation ID must be lowercase hex")
		}
	}
	id := invocationID[:8] + "-" + invocationID[8:12] + "-" + invocationID[12:16] + "-" + invocationID[16:20] + "-" + invocationID[20:]
	if err := ValidateID(id); err != nil {
		return "", fmt.Errorf("invocation ID: %w", err)
	}
	return id, nil
}

// OutputPath is where the output of a job is kept, next to its record.
func (s *Store) OutputPath(id string) string { return filepath.Join(s.stateDir, id+".log") }

// ReadOutput returns up to limit bytes of the saved output of id starting at
// offset, and the output's size. A negative offset means the last limit bytes.
// A limit of 0 means DefaultOutputChunk, and one above MaxOutputChunk is
// capped. An offset at or past the end returns no data and start is the size.
// The bytes are the file's as they are; a missing output is ErrNotFound.
func (s *Store) ReadOutput(id string, offset int64, limit int) (data []byte, start, size int64, err error) {
	if err = ValidateID(id); err != nil {
		return nil, 0, 0, err
	}
	switch {
	case limit <= 0:
		limit = DefaultOutputChunk
	case limit > MaxOutputChunk:
		limit = MaxOutputChunk
	}
	file, err := openRegularFile(s.stateDir, id+".log")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open output of job %s: %w", id, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("stat output of job %s: %w", id, err)
	}
	size = info.Size()
	start = min(offset, size)
	if offset < 0 {
		start = max(size-int64(limit), 0)
	}
	data = make([]byte, min(int64(limit), size-start))
	n, err := file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, 0, fmt.Errorf("read output of job %s: %w", id, err)
	}
	return data[:n], start, size, nil
}

// PrunePerTarget removes unreadable records, terminal records older than
// TerminalRetention, and all but the newest keep terminal records of each
// unit (the first target; records without one form a group). Running records
// are never removed.
func (s *Store) PrunePerTarget(ctx context.Context, now time.Time, keep int) error {
	return s.withLock(ctx, func() error {
		records, err := s.terminalRecords()
		if err != nil {
			return err
		}
		cutoff := now.Add(-TerminalRetention)
		slices.SortFunc(records, func(a, b Record) int { return finishedOrCreated(b).Compare(finishedOrCreated(a)) })
		kept := map[string]int{}
		for _, record := range records {
			var unit string
			if len(record.Target) > 0 {
				unit = record.Target[0]
			}
			kept[unit]++
			if kept[unit] <= keep && !finishedOrCreated(record).Before(cutoff) {
				continue
			}
			if err := s.removeRecord(record.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// ModTime is the modification time of the state directory, which changes when
// a record is created, replaced or removed. It is the zero time when the
// directory does not exist.
func (s *Store) ModTime() (time.Time, error) {
	info, err := os.Stat(s.stateDir)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("stat job directory: %w", err)
	}
	return info.ModTime(), nil
}
