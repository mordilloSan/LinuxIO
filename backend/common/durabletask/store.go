// Package durabletask persists the small authoritative record for operations
// whose executor outlives the bridge process that started them.
package durabletask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	DefaultRoot              = version.DataDir + "/durable-operations"
	TerminalRetention        = 30 * 24 * time.Hour
	MaxTerminalRecordsPerUID = 200
	MaxProgressEntries       = 32
	maxRecordBytes           = 64 << 10
	maxResultBytes           = 8 << 10
)

var (
	ErrNotFound = errors.New("durable operation not found")
	ErrConflict = errors.New("durable operation identity conflict")
	ErrActive   = errors.New("another durable operation is active")

	artifactNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type State string

const (
	StateQueued    State = "queued"
	StateLaunching State = "launching"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
	StateUnknown   State = "unknown"
)

type Executor struct {
	Kind     string `json:"kind"`
	Handle   string `json:"handle"`
	Identity string `json:"identity"`
}

type Progress struct {
	At      time.Time `json:"at"`
	Phase   string    `json:"phase"`
	Message string    `json:"message,omitempty"`
}

type StructuredError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

type Record struct {
	ID                 string           `json:"id"`
	Route              string           `json:"route"`
	UID                uint32           `json:"uid"`
	RequestFingerprint string           `json:"request_fingerprint"`
	Target             string           `json:"target,omitempty"`
	Executor           Executor         `json:"executor"`
	State              State            `json:"state"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	StartedAt          *time.Time       `json:"started_at,omitempty"`
	FinishedAt         *time.Time       `json:"finished_at,omitempty"`
	CancelRequestedAt  *time.Time       `json:"cancel_requested_at,omitempty"`
	Progress           []Progress       `json:"progress,omitempty"`
	Result             json.RawMessage  `json:"result,omitempty"`
	Error              *StructuredError `json:"error,omitempty"`
	LogCursor          string           `json:"log_cursor,omitempty"`
}

type Claim struct {
	ID                 string
	Route              string
	UID                uint32
	RequestFingerprint string
	Target             string
	ExclusiveRoute     bool
}

type ExecutorResult struct {
	ID         string    `json:"id"`
	State      State     `json:"state"`
	ExitCode   int       `json:"exit_code"`
	FinishedAt time.Time `json:"finished_at"`
	// Result is the route-owned typed terminal payload. It is deliberately
	// opaque to the durable store; route code validates and decodes it after
	// ApplyExecutorResult persists it.
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type Store struct {
	root string
	now  func() time.Time
}

func NewStore(root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = DefaultRoot
	}
	return &Store{root: filepath.Clean(root), now: func() time.Time { return time.Now().UTC() }}
}

// DetachedContext gives durable cleanup and recovery work an explicit bounded
// lifetime after the request or bridge context that initiated it has ended.
func DetachedContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		panic("durable task detached context requires a positive timeout")
	}
	return context.WithTimeout(context.Background(), timeout)
}

func ValidateID(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil() || id.String() != value {
		return fmt.Errorf("operation ID must be a canonical non-zero UUID")
	}
	return nil
}

func Fingerprint(route, safeRequest string) string {
	sum := sha256.Sum256([]byte(route + "\x00" + safeRequest))
	return hex.EncodeToString(sum[:])
}

func (r Record) Terminal() bool {
	switch r.State {
	case StateCompleted, StateFailed, StateCanceled, StateUnknown:
		return true
	default:
		return false
	}
}

func (r *Record) AppendProgress(now time.Time, phase, message string) {
	if len(message) > 512 {
		message = message[:512]
	}
	r.Progress = append(r.Progress, Progress{At: now.UTC(), Phase: phase, Message: message})
	if len(r.Progress) > MaxProgressEntries {
		r.Progress = slices.Clone(r.Progress[len(r.Progress)-MaxProgressEntries:])
	}
}

func (s *Store) Claim(ctx context.Context, claim Claim) (Record, bool, error) {
	if err := validateClaim(claim); err != nil {
		return Record{}, false, err
	}

	var record Record
	created := false
	err := s.withLock(ctx, func() error {
		current, err := s.readRecord(claim.ID)
		if err == nil {
			if current.UID != claim.UID || current.Route != claim.Route || current.RequestFingerprint != claim.RequestFingerprint {
				return ErrConflict
			}
			record = current
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if claim.ExclusiveRoute {
			active, activeErr := s.activeRouteExists(claim.Route)
			if activeErr != nil {
				return activeErr
			}
			if active {
				return ErrActive
			}
		}

		now := s.now().UTC()
		record = Record{
			ID:                 claim.ID,
			Route:              claim.Route,
			UID:                claim.UID,
			RequestFingerprint: claim.RequestFingerprint,
			Target:             claim.Target,
			State:              StateQueued,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := s.writeRecord(record); err != nil {
			return err
		}
		created = true
		return s.pruneLocked(now)
	})
	return record, created, err
}

func (s *Store) Get(ctx context.Context, id string, uid uint32) (Record, error) {
	if err := ValidateID(id); err != nil {
		return Record{}, err
	}
	var record Record
	err := s.withLock(ctx, func() error {
		current, err := s.readRecord(id)
		if err != nil {
			return err
		}
		if current.UID != uid {
			return ErrNotFound
		}
		record = current
		return nil
	})
	return record, err
}

func (s *Store) Update(ctx context.Context, id string, uid uint32, update func(*Record) error) (Record, error) {
	if update == nil {
		return Record{}, errors.New("durable operation update is nil")
	}
	if err := ValidateID(id); err != nil {
		return Record{}, err
	}

	var record Record
	err := s.withLock(ctx, func() error {
		current, err := s.readRecord(id)
		if err != nil {
			return err
		}
		if current.UID != uid {
			return ErrNotFound
		}
		if err := update(&current); err != nil {
			return err
		}
		current.UpdatedAt = s.now().UTC()
		if err := s.writeRecord(current); err != nil {
			return err
		}
		if current.Terminal() {
			if err := s.pruneLocked(current.UpdatedAt); err != nil {
				return err
			}
		}
		record = current
		return nil
	})
	return record, err
}

func (s *Store) ListActiveForUID(ctx context.Context, uid uint32) ([]Record, error) {
	var records []Record
	err := s.withLock(ctx, func() error {
		entries, err := os.ReadDir(s.root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read durable operation directory: %w", err)
		}
		for _, entry := range entries {
			id, ok := recordIDFromName(entry.Name())
			if !ok {
				continue
			}
			record, readErr := s.readRecord(id)
			if readErr != nil {
				return readErr
			}
			if record.UID == uid && !record.Terminal() {
				records = append(records, record)
			}
		}
		slices.SortFunc(records, func(a, b Record) int { return a.CreatedAt.Compare(b.CreatedAt) })
		return nil
	})
	return records, err
}

func (s *Store) ArtifactPath(id, name string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	if !artifactNameRE.MatchString(name) {
		return "", fmt.Errorf("invalid durable operation artifact name %q", name)
	}
	return filepath.Join(s.root, "artifacts", id, name), nil
}

func (s *Store) WriteArtifact(id, name string, data []byte, mode fs.FileMode) (string, error) {
	path, err := s.ArtifactPath(id, name)
	if err != nil {
		return "", err
	}
	if err := utils.WriteFileAtomic(path, data, mode); err != nil {
		return "", fmt.Errorf("write durable operation artifact: %w", err)
	}
	return path, nil
}

func (s *Store) ExecutorResultPath(id string) (string, error) {
	return s.ArtifactPath(id, "executor-result.json")
}

func (s *Store) ReadExecutorResult(id string) (ExecutorResult, error) {
	if err := ValidateID(id); err != nil {
		return ExecutorResult{}, err
	}
	name := filepath.Join("artifacts", id, "executor-result.json")
	data, err := readRegularFile(s.root, name, maxResultBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ExecutorResult{}, ErrNotFound
		}
		return ExecutorResult{}, err
	}
	var result ExecutorResult
	if err := json.Unmarshal(data, &result); err != nil {
		return ExecutorResult{}, fmt.Errorf("decode executor result for %s: %w", id, err)
	}
	if result.ID != id || (result.State != StateCompleted && result.State != StateFailed && result.State != StateCanceled) || result.FinishedAt.IsZero() {
		return ExecutorResult{}, fmt.Errorf("invalid executor result for %s", id)
	}
	if len(result.Result) > maxResultBytes {
		return ExecutorResult{}, fmt.Errorf("executor result for %s exceeds %d bytes", id, maxResultBytes)
	}
	return result, nil
}

// GetForExecutor reads an operation for a system-owned worker. The worker is
// intentionally not authorized by the initiating UID: it runs as root under a
// deterministic systemd unit, so route and executor identity are the security
// boundary instead. The operation ID is still validated as a canonical UUID
// and the record is read under the store lock.
func (s *Store) GetForExecutor(ctx context.Context, id, route string, executor Executor) (Record, error) {
	if err := ValidateID(id); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(route) == "" || executor.Kind == "" || executor.Identity == "" || executor.Handle == "" {
		return Record{}, errors.New("durable executor identity is incomplete")
	}
	var record Record
	err := s.withLock(ctx, func() error {
		current, err := s.readRecord(id)
		if err != nil {
			return err
		}
		if current.Route != route || current.Executor != executor {
			return ErrNotFound
		}
		record = current
		return nil
	})
	return record, err
}

func (s *Store) ApplyExecutorResult(ctx context.Context, uid uint32, result ExecutorResult) (Record, error) {
	return s.Update(ctx, result.ID, uid, func(record *Record) error {
		if record.Terminal() && record.State != StateUnknown {
			return nil
		}
		payload := result.Result
		if len(payload) == 0 {
			var err error
			payload, err = json.Marshal(struct {
				ExitCode int `json:"exit_code"`
			}{ExitCode: result.ExitCode})
			if err != nil {
				return err
			}
		}
		record.State = result.State
		record.Result = payload
		record.FinishedAt = new(result.FinishedAt.UTC())
		switch result.State {
		case StateFailed:
			message := strings.TrimSpace(result.Error)
			if message == "" {
				message = fmt.Sprintf("updater exited with status %d", result.ExitCode)
			}
			record.Error = &StructuredError{Code: result.ExitCode, Message: message}
		case StateCanceled:
			message := strings.TrimSpace(result.Error)
			if message == "" {
				message = "operation canceled"
			}
			record.Error = &StructuredError{Code: 499, Message: message}
		default:
			record.Error = nil
		}
		return nil
	})
}

func (s *Store) withLock(ctx context.Context, fn func() error) error {
	return filelock.RunExclusive(
		ctx,
		filepath.Join(s.root, ".lock"),
		fn,
		filelock.WithPermissions(0o600),
		filelock.WithDirPermissions(0o750),
		filelock.WithTimeout(10*time.Second),
	)
}

func (s *Store) readRecord(id string) (Record, error) {
	data, err := readRegularFile(s.root, id+".json", maxRecordBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("decode durable operation %s: %w", id, err)
	}
	if record.ID != id {
		return Record{}, fmt.Errorf("durable operation filename %s contains ID %q", id, record.ID)
	}
	if err := validateRecord(record); err != nil {
		return Record{}, fmt.Errorf("validate durable operation %s: %w", id, err)
	}
	return record, nil
}

func (s *Store) writeRecord(record Record) error {
	if err := validateRecord(record); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode durable operation %s: %w", record.ID, err)
	}
	if len(data) > maxRecordBytes {
		return fmt.Errorf("durable operation %s exceeds %d bytes", record.ID, maxRecordBytes)
	}
	data = append(data, '\n')
	if err := utils.WriteFileAtomic(s.recordPath(record.ID), data, 0o600); err != nil {
		return fmt.Errorf("persist durable operation %s: %w", record.ID, err)
	}
	return nil
}

func (s *Store) pruneLocked(now time.Time) error {
	byUID, err := s.terminalRecordsByUID()
	if err != nil {
		return err
	}
	cutoff := now.Add(-TerminalRetention)
	for _, records := range byUID {
		if err := s.pruneTerminalRecords(records, cutoff); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) terminalRecordsByUID() (map[uint32][]Record, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[uint32][]Record{}, nil
		}
		return nil, err
	}
	byUID := make(map[uint32][]Record)
	for _, entry := range entries {
		id, ok := recordIDFromName(entry.Name())
		if !ok {
			continue
		}
		record, readErr := s.readRecord(id)
		if readErr != nil {
			// Never delete a record whose state cannot be proven terminal.
			continue
		}
		if record.Terminal() {
			byUID[record.UID] = append(byUID[record.UID], record)
		}
	}
	return byUID, nil
}

func (s *Store) activeRouteExists(route string) (bool, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, entry := range entries {
		id, ok := recordIDFromName(entry.Name())
		if !ok {
			continue
		}
		record, readErr := s.readRecord(id)
		if readErr != nil {
			return false, readErr
		}
		if record.Route == route && !record.Terminal() {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) pruneTerminalRecords(records []Record, cutoff time.Time) error {
	slices.SortFunc(records, func(a, b Record) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	for index, record := range records {
		if index < MaxTerminalRecordsPerUID && !record.UpdatedAt.Before(cutoff) {
			continue
		}
		if err := s.removeRecord(record.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) removeRecord(id string) error {
	if err := os.Remove(s.recordPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("prune durable operation %s: %w", id, err)
	}
	if err := os.RemoveAll(filepath.Join(s.root, "artifacts", id)); err != nil {
		return fmt.Errorf("prune durable operation artifacts %s: %w", id, err)
	}
	return nil
}

func (s *Store) recordPath(id string) string {
	return filepath.Join(s.root, id+".json")
}

func validateClaim(claim Claim) error {
	if err := ValidateID(claim.ID); err != nil {
		return err
	}
	if strings.TrimSpace(claim.Route) == "" || len(claim.Route) > 128 {
		return errors.New("durable operation route is invalid")
	}
	if len(claim.RequestFingerprint) != sha256.Size*2 {
		return errors.New("durable operation request fingerprint is invalid")
	}
	if _, err := hex.DecodeString(claim.RequestFingerprint); err != nil {
		return errors.New("durable operation request fingerprint is invalid")
	}
	if len(claim.Target) > 256 {
		return errors.New("durable operation target is too long")
	}
	return nil
}

func validateRecord(record Record) error {
	if err := validateClaim(Claim{
		ID:                 record.ID,
		Route:              record.Route,
		UID:                record.UID,
		RequestFingerprint: record.RequestFingerprint,
		Target:             record.Target,
	}); err != nil {
		return err
	}
	switch record.State {
	case StateQueued, StateLaunching, StateRunning, StateCompleted, StateFailed, StateCanceled, StateUnknown:
	default:
		return fmt.Errorf("invalid durable operation state %q", record.State)
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return errors.New("durable operation timestamps are required")
	}
	if err := validateExecutor(record); err != nil {
		return err
	}
	if err := validateProgress(record.Progress); err != nil {
		return err
	}
	if err := validateRecordPayload(record); err != nil {
		return err
	}
	if record.Terminal() && record.FinishedAt == nil {
		return errors.New("terminal durable operation is missing finished_at")
	}
	return nil
}

func validateExecutor(record Record) error {
	if len(record.Executor.Kind) > 64 || len(record.Executor.Handle) > 255 || len(record.Executor.Identity) > 64 {
		return errors.New("durable operation executor metadata is too long")
	}
	if (record.State == StateLaunching || record.State == StateRunning) && (record.Executor.Kind == "" || record.Executor.Handle == "" || record.Executor.Identity == "") {
		return errors.New("active durable operation is missing executor metadata")
	}
	return nil
}

func validateProgress(entries []Progress) error {
	if len(entries) > MaxProgressEntries {
		return errors.New("durable operation progress exceeds retention limit")
	}
	for _, progress := range entries {
		if progress.At.IsZero() || len(progress.Phase) > 64 || len(progress.Message) > 512 {
			return errors.New("durable operation progress entry is invalid")
		}
	}
	return nil
}

func validateRecordPayload(record Record) error {
	if len(record.Result) > maxResultBytes || len(record.LogCursor) > 1024 {
		return errors.New("durable operation result or log cursor is too large")
	}
	if record.Error != nil && (strings.TrimSpace(record.Error.Message) == "" || len(record.Error.Message) > 1024) {
		return errors.New("durable operation error is invalid")
	}
	return nil
}

func readRegularFile(root, name string, limit int64) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("invalid durable operation relative path %q", name)
	}
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootDir.Close()

	info, err := rootDir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, fmt.Errorf("refusing to read non-regular durable operation file %q", name)
	}
	file, err := rootDir.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return utils.ReadAllLimited(file, limit)
}

func recordIDFromName(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, ".json")
	if !ok || ValidateID(id) != nil {
		return "", false
	}
	return id, true
}
