package jobs

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/iolimit"
)

const (
	maxRecordBytes   = 1 << 20
	maxProgressBytes = 64 << 10
)

var (
	ErrExists   = errors.New("job already exists")
	ErrNotFound = errors.New("job not found")
	ErrActive   = errors.New("another job on this route is running")
)

// Store keeps one record per job under stateDir and ephemeral progress under
// runDir. Writers serialize on <stateDir>/.lock; readers rely on records being
// replaced by rename and take no lock.
type Store struct {
	stateDir string
	runDir   string
	now      func() time.Time
}

func NewStore(stateDir, runDir string) *Store {
	return &Store{
		stateDir: filepath.Clean(stateDir),
		runDir:   filepath.Clean(runDir),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// DetachedContext gives cleanup work an explicit bounded lifetime after the
// request or host context that initiated it has ended.
func DetachedContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		panic("jobs detached context requires a positive timeout")
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (s *Store) Create(ctx context.Context, r Record) error {
	return s.create(ctx, r, false)
}

func (s *Store) CreateUnlessActive(ctx context.Context, r Record) error {
	return s.create(ctx, r, true)
}

func (s *Store) create(ctx context.Context, r Record, exclusiveRoute bool) error {
	if err := validateRecord(r); err != nil {
		return err
	}
	for _, dir := range []string{s.stateDir, s.runDir} {
		if err := mkdirStore(dir, 0o700); err != nil {
			return fmt.Errorf("create job directory: %w", err)
		}
	}
	return s.withLock(ctx, func() error {
		if _, err := os.Lstat(s.recordPath(r.ID)); err == nil {
			return ErrExists
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat job %s: %w", r.ID, err)
		}
		if exclusiveRoute {
			records, err := s.readAll()
			if err != nil {
				return err
			}
			if slices.ContainsFunc(records, func(o Record) bool { return o.State == StateRunning && o.Route == r.Route }) {
				return ErrActive
			}
		}
		return s.writeRecord(r, false)
	})
}

func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if err := ValidateID(id); err != nil {
		return Record{}, err
	}
	return s.readRecord(id)
}

// Update applies fn to the record under the lock. The write is fsynced when
// the job reached a terminal state after running longer than
// TransientMaxDuration, or when the checkpoint changed.
func (s *Store) Update(ctx context.Context, id string, fn func(*Record) error) (Record, error) {
	if fn == nil {
		return Record{}, errors.New("job update is nil")
	}
	return s.mutate(ctx, id, func(r *Record) (bool, error) { return false, fn(r) })
}

// Finish moves a running job to a terminal state. A job that completed in under
// TransientMaxDuration is deleted instead of kept. Finishing an already
// terminal job returns it unchanged.
func (s *Store) Finish(ctx context.Context, id string, state State, result jsontext.Value, jobErr *Error) (Record, error) {
	if !state.terminal() {
		return Record{}, fmt.Errorf("job cannot finish in state %q", state)
	}
	return s.mutate(ctx, id, func(r *Record) (bool, error) {
		if r.State.terminal() {
			return false, nil
		}
		r.State = state
		r.Result = result
		r.Error = jobErr
		r.FinishedAt = s.now()
		return state == StateCompleted && r.FinishedAt.Sub(r.StartedAt) < TransientMaxDuration, nil
	})
}

// mutate runs fn on the record under the lock. fn reports whether the record
// should be deleted rather than written back.
func (s *Store) mutate(ctx context.Context, id string, fn func(*Record) (remove bool, err error)) (Record, error) {
	if err := ValidateID(id); err != nil {
		return Record{}, err
	}
	var record Record
	err := s.withLock(ctx, func() error {
		current, err := s.readRecord(id)
		if err != nil {
			return err
		}
		before := current
		before.Checkpoint = slices.Clone(current.Checkpoint)
		remove, err := fn(&current)
		if err != nil {
			return err
		}
		if current.ID != id {
			return fmt.Errorf("job %s changed its ID", id)
		}
		record = current
		if remove {
			return s.removeRecord(id)
		}
		return s.writeRecord(current, needsSync(before, current, s.now()))
	})
	return record, err
}

func (s *Store) ListActive(ctx context.Context) ([]Record, error) {
	records, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	records = slices.DeleteFunc(records, func(r Record) bool { return r.State != StateRunning })
	slices.Reverse(records)
	return records, nil
}

// List returns every record, newest first.
func (s *Store) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(records, func(a, b Record) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return records, nil
}

func (s *Store) WriteProgress(id string, progress jsontext.Value) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if len(progress) > maxProgressBytes {
		return fmt.Errorf("job progress exceeds %d bytes", maxProgressBytes)
	}
	return writeFileNoSync(s.progressPath(id), progress, 0o600)
}

func (s *Store) ReadProgress(id string) (jsontext.Value, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	data, err := readRegularFile(s.runDir, id+".progress", maxProgressBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

// Prune removes unreadable records and terminal records older than TerminalRetention and, per owner,
// all but the newest MaxTerminalRecordsPerUID. Running records are never removed.
func (s *Store) Prune(ctx context.Context, now time.Time) error {
	return s.withLock(ctx, func() error {
		byUID, err := s.terminalByUID()
		if err != nil {
			return err
		}
		cutoff := now.Add(-TerminalRetention)
		for _, records := range byUID {
			slices.SortFunc(records, func(a, b Record) int { return finishedOrCreated(b).Compare(finishedOrCreated(a)) })
			for i, record := range records {
				if i < MaxTerminalRecordsPerUID && !finishedOrCreated(record).Before(cutoff) {
					continue
				}
				if err := s.removeRecord(record.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Store) terminalByUID() (map[uint32][]Record, error) {
	ids, err := s.recordIDs()
	if err != nil {
		return nil, err
	}
	byUID := make(map[uint32][]Record)
	for _, id := range ids {
		record, err := s.readRecord(id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			// A file that does not decode, normally a torn write after a power
			// loss (live writes are a temp file plus rename): remove it.
			slog.Warn("removing unreadable job record", "path", s.recordPath(id), "error", err)
			if err := s.removeRecord(id); err != nil {
				return nil, err
			}
			continue
		}
		if record.State.terminal() {
			byUID[record.Owner.UID] = append(byUID[record.Owner.UID], record)
		}
	}
	return byUID, nil
}

// ReconcileCrashed marks running host-executed records whose executor is gone
// as interrupted and returns the records it changed.
func (s *Store) ReconcileCrashed(ctx context.Context, alive func(Executor) bool, reason Reason) ([]Record, error) {
	var changed []Record
	err := s.withLock(ctx, func() error {
		records, err := s.readAll()
		if err != nil {
			return err
		}
		now := s.now()
		for _, r := range records {
			if r.State != StateRunning || r.Executor.Kind != "host" || alive(r.Executor) {
				continue
			}
			r.State = StateInterrupted
			r.Reason = reason
			r.FinishedAt = now
			if err := s.writeRecord(r, true); err != nil {
				return err
			}
			changed = append(changed, r)
		}
		return nil
	})
	return changed, err
}

func (s *Store) withLock(ctx context.Context, fn func() error) error {
	return filelock.RunExclusive(
		ctx,
		filepath.Join(s.stateDir, ".lock"),
		fn,
		filelock.WithPermissions(0o600),
		filelock.WithDirPermissions(0o700),
		filelock.WithTimeout(10*time.Second),
	)
}

func needsSync(before, after Record, now time.Time) bool {
	if !slices.Equal(before.Checkpoint, after.Checkpoint) {
		return true
	}
	started := after.StartedAt
	if started.IsZero() {
		started = after.CreatedAt
	}
	return after.State.terminal() && now.Sub(started) > TransientMaxDuration
}

// mkdirStore creates dir with perm and each missing parent with 0755, whatever
// the umask: the jobs roots are shared by every uid's job host and must stay
// traversable. Directories that already exist are left as they are.
func mkdirStore(dir string, perm fs.FileMode) error {
	err := os.Mkdir(dir, perm)
	if errors.Is(err, fs.ErrNotExist) {
		if err = mkdirStore(filepath.Dir(dir), 0o755); err == nil {
			err = os.Mkdir(dir, perm)
		}
	}
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Chmod(dir, perm)
}

func finishedOrCreated(r Record) time.Time {
	if r.FinishedAt.IsZero() {
		return r.CreatedAt
	}
	return r.FinishedAt
}

func (s *Store) recordPath(id string) string   { return filepath.Join(s.stateDir, id+".json") }
func (s *Store) progressPath(id string) string { return filepath.Join(s.runDir, id+".progress") }

func (s *Store) readRecord(id string) (Record, error) {
	data, err := readRegularFile(s.stateDir, id+".json", maxRecordBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, err
	}
	// Unknown members are ignored: a newer binary may add fields.
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("decode job %s: %w", id, err)
	}
	if record.ID != id {
		return Record{}, fmt.Errorf("job file %s contains ID %q", id, record.ID)
	}
	if err := validateRecord(record); err != nil {
		return Record{}, fmt.Errorf("validate job %s: %w", id, err)
	}
	return record, nil
}

func (s *Store) writeRecord(r Record, sync bool) error {
	if err := validateRecord(r); err != nil {
		return err
	}
	data, err := json.Marshal(r, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encode job %s: %w", r.ID, err)
	}
	data = append(data, '\n')
	if len(data) > maxRecordBytes {
		return fmt.Errorf("job %s exceeds %d bytes", r.ID, maxRecordBytes)
	}
	if sync {
		err = atomicfile.Write(s.recordPath(r.ID), data, 0o600)
	} else {
		err = writeFileNoSync(s.recordPath(r.ID), data, 0o600)
	}
	if err != nil {
		return fmt.Errorf("persist job %s: %w", r.ID, err)
	}
	return nil
}

func (s *Store) removeRecord(id string) error {
	for _, path := range []string{s.recordPath(id), s.progressPath(id)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove job %s: %w", id, err)
		}
	}
	return nil
}

// recordIDs lists the job IDs that have a record file; a missing directory is empty.
func (s *Store) recordIDs() ([]string, error) {
	entries, err := os.ReadDir(s.stateDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read job directory: %w", err)
	}
	var out []string
	for _, entry := range entries {
		if id, ok := strings.CutSuffix(entry.Name(), ".json"); ok && ValidateID(id) == nil {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Store) readAll() ([]Record, error) {
	ids, err := s.recordIDs()
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(ids))
	for _, id := range ids {
		record, err := s.readRecord(id)
		if errors.Is(err, ErrNotFound) {
			continue // removed since the directory was listed
		}
		if err != nil {
			// A file that does not decode, normally a torn write after a power
			// loss, must not break listing or recovery; Prune removes it.
			slog.Warn("skipping unreadable job record", "path", s.recordPath(id), "error", err)
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// writeFileNoSync replaces path by renaming a temp file. No half-written file
// is ever visible under the final name, but nothing is durable until an fsync
// write: after a power loss the file may be empty or partial.
func writeFileNoSync(path string, data []byte, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".linuxio-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readRegularFile(root, name string, limit int64) ([]byte, error) {
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootDir.Close()

	info, err := rootDir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, fmt.Errorf("refusing to read non-regular job file %q", name)
	}
	file, err := rootDir.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return iolimit.ReadAll(file, limit)
}
