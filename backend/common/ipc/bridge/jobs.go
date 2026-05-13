package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"sync"
	"time"
)

type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

type Error struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewError(message string, code int) *Error {
	return &Error{Message: message, Code: code}
}

type Owner struct {
	SessionID string `json:"session_id,omitempty"`
	Username  string `json:"username,omitempty"`
	UID       uint32 `json:"uid,omitempty"`
}

func (o Owner) Empty() bool {
	return o.SessionID == "" && o.Username == "" && o.UID == 0
}

func (o Owner) Matches(other Owner) bool {
	if o.Empty() || other.Empty() {
		return false
	}
	if o.Username != "" && other.Username != "" {
		return o.Username == other.Username
	}
	return o.UID != 0 && o.UID == other.UID
}

type Snapshot struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Args       []string   `json:"args,omitempty"`
	Owner      Owner      `json:"owner"`
	State      State      `json:"state"`
	Progress   any        `json:"progress,omitempty"`
	Result     any        `json:"result,omitempty"`
	Error      *Error     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type EventType string

const (
	EventSnapshot EventType = "job.snapshot"
	EventStarted  EventType = "job.started"
	EventProgress EventType = "job.progress"
	EventResult   EventType = "job.result"
	EventError    EventType = "job.error"
	EventCanceled EventType = "job.canceled"
)

type Event struct {
	Type     EventType `json:"type"`
	Job      Snapshot  `json:"job"`
	Progress any       `json:"progress,omitempty"`
	Result   any       `json:"result,omitempty"`
	Error    *Error    `json:"error,omitempty"`
}

type Runner func(ctx context.Context, job *Job, args []string) (any, error)
type DataAttacher func(ctx context.Context, job *Job, stream net.Conn, args []string) error

type Registry struct {
	mu            sync.RWMutex
	dataAttachers map[string]DataAttacher
	jobs          map[string]*Job
	subscribers   map[chan Event]struct{}
	nextID        uint64
	cleanupStop   chan struct{}
	cleanupOnce   sync.Once
}

type Job struct {
	registry *Registry

	ctx         context.Context
	mu          sync.RWMutex
	id          string
	typ         string
	args        []string
	owner       Owner
	state       State
	progress    any
	progressLog []Event
	result      any
	err         *Error
	createdAt   time.Time
	startedAt   *time.Time
	updatedAt   time.Time
	finishedAt  *time.Time
	cancel      context.CancelFunc
	done        chan struct{}
	doneOnce    sync.Once
	subscribers map[chan Event]struct{}
}

var DefaultRegistry = NewRegistry()

const (
	DefaultTerminalJobTTL         = 30 * time.Minute
	DefaultTerminalJobSweepPeriod = time.Minute
	DefaultJobProgressReplayLimit = 1024
)

func NewRegistry() *Registry {
	r := &Registry{
		dataAttachers: make(map[string]DataAttacher),
		jobs:          make(map[string]*Job),
		subscribers:   make(map[chan Event]struct{}),
		cleanupStop:   make(chan struct{}),
	}
	r.startCleanupLoop(DefaultTerminalJobTTL, DefaultTerminalJobSweepPeriod)
	return r
}

func RegisterDataAttacher(jobType string, attacher DataAttacher) {
	DefaultRegistry.RegisterDataAttacher(jobType, attacher)
}

func Get(id string) (*Job, bool) {
	return DefaultRegistry.Get(id)
}

func GetForOwner(id string, owner Owner) (*Job, bool) {
	return DefaultRegistry.GetForOwner(id, owner)
}

func List() []Snapshot {
	return DefaultRegistry.List()
}

func ListForOwner(owner Owner) []Snapshot {
	return DefaultRegistry.ListForOwner(owner)
}

func ListActive() []Snapshot {
	return DefaultRegistry.ListActive()
}

func ListActiveForOwner(owner Owner) []Snapshot {
	return DefaultRegistry.ListActiveForOwner(owner)
}

func AttachData(ctx context.Context, job *Job, stream net.Conn, args []string) error {
	return DefaultRegistry.AttachData(ctx, job, stream, args)
}

func Subscribe(buffer int) (<-chan Event, func()) {
	return DefaultRegistry.Subscribe(buffer)
}

func (r *Registry) RegisterDataAttacher(jobType string, attacher DataAttacher) {
	if jobType == "" {
		panic("job type cannot be empty")
	}
	if attacher == nil {
		panic("job data attacher cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.dataAttachers[jobType] = attacher
}

func (r *Registry) AttachData(ctx context.Context, job *Job, stream net.Conn, args []string) error {
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}

	r.mu.RLock()
	attacher, ok := r.dataAttachers[job.Type()]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("job data attacher not found: %s", job.Type())
	}
	return attacher(ctx, job, stream, args)
}

func (r *Registry) Create(jobType string, args []string) (*Job, error) {
	return r.CreateForOwner(jobType, args, Owner{})
}

func (r *Registry) CreateForOwner(jobType string, args []string, owner Owner) (*Job, error) {
	if jobType == "" {
		return nil, fmt.Errorf("job type cannot be empty")
	}

	r.mu.Lock()
	r.nextID++
	now := time.Now().UTC()
	id := fmt.Sprintf("job-%d", r.nextID)
	// Jobs are intentionally detached from the stream that created them; cancel
	// through jobs.cancel, attached stream abort, or policy timeout instead.
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		registry:    r,
		ctx:         ctx,
		id:          id,
		typ:         jobType,
		args:        append([]string(nil), args...),
		owner:       owner,
		state:       StateQueued,
		createdAt:   now,
		updatedAt:   now,
		cancel:      cancel,
		done:        make(chan struct{}),
		subscribers: make(map[chan Event]struct{}),
	}
	r.jobs[id] = job
	r.mu.Unlock()

	return job, nil
}

func (r *Registry) Get(id string) (*Job, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	return job, ok
}

func (r *Registry) GetForOwner(id string, owner Owner) (*Job, bool) {
	job, ok := r.Get(id)
	if !ok {
		return nil, false
	}
	if !job.Owner().Matches(owner) {
		return nil, false
	}
	return job, true
}

func (r *Registry) List() []Snapshot {
	r.mu.RLock()
	jobs := make([]*Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}
	r.mu.RUnlock()

	snapshots := make([]Snapshot, 0, len(jobs))
	for _, job := range jobs {
		snapshots = append(snapshots, job.Snapshot())
	}
	return snapshots
}

func (r *Registry) ListForOwner(owner Owner) []Snapshot {
	all := r.List()
	filtered := all[:0]
	for _, snapshot := range all {
		if snapshot.Owner.Matches(owner) {
			filtered = append(filtered, snapshot)
		}
	}
	return filtered
}

func (r *Registry) ListActive() []Snapshot {
	all := r.List()
	active := all[:0]
	for _, snapshot := range all {
		if snapshot.State == StateQueued || snapshot.State == StateRunning {
			active = append(active, snapshot)
		}
	}
	return active
}

func (r *Registry) ListActiveForOwner(owner Owner) []Snapshot {
	all := r.ListForOwner(owner)
	active := all[:0]
	for _, snapshot := range all {
		if snapshot.State == StateQueued || snapshot.State == StateRunning {
			active = append(active, snapshot)
		}
	}
	return active
}

func (r *Registry) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Event, buffer)
	r.mu.Lock()
	r.subscribers[ch] = struct{}{}
	r.mu.Unlock()

	unsubscribe := func() {
		r.mu.Lock()
		if _, ok := r.subscribers[ch]; ok {
			delete(r.subscribers, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
	return ch, unsubscribe
}

func (j *Job) ID() string {
	return j.id
}

func (j *Job) Type() string {
	return j.typ
}

func (j *Job) Owner() Owner {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.owner
}

func (j *Job) Snapshot() Snapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return Snapshot{
		ID:         j.id,
		Type:       j.typ,
		Args:       append([]string(nil), j.args...),
		Owner:      j.owner,
		State:      j.state,
		Progress:   j.progress,
		Result:     j.result,
		Error:      j.err,
		CreatedAt:  j.createdAt,
		StartedAt:  cloneTimePtr(j.startedAt),
		UpdatedAt:  j.updatedAt,
		FinishedAt: cloneTimePtr(j.finishedAt),
	}
}

func (j *Job) Cancel() {
	j.cancel()
}

func (j *Job) Done() <-chan struct{} {
	return j.done
}

func (j *Job) Start(runner Runner) {
	if runner == nil {
		j.markFailed(NewError("job runner cannot be nil", 500))
		return
	}
	go j.run(j.ctx, runner)
}

func (j *Job) IsTerminal() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.isTerminalLocked()
}

func (j *Job) ReportProgress(progress any) {
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.progress = progress
	j.updatedAt = time.Now().UTC()
	event := Event{
		Type:     EventProgress,
		Job:      j.snapshotLocked(),
		Progress: progress,
	}
	j.progressLog = append(j.progressLog, event)
	if limit := DefaultJobProgressReplayLimit; limit > 0 && len(j.progressLog) > limit {
		j.progressLog = append([]Event(nil), j.progressLog[len(j.progressLog)-limit:]...)
	}
	j.mu.Unlock()
	j.broadcast(event)
}

func (j *Job) Subscribe(buffer int) (<-chan Event, func()) {
	ch, _, unsubscribe := j.SubscribeWithReplay(buffer)
	return ch, unsubscribe
}

func (j *Job) SubscribeWithReplay(buffer int) (<-chan Event, []Event, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	j.mu.Lock()
	replay := append([]Event(nil), j.progressLog...)
	j.subscribers[ch] = struct{}{}
	j.mu.Unlock()

	unsubscribe := func() {
		j.mu.Lock()
		if _, ok := j.subscribers[ch]; ok {
			delete(j.subscribers, ch)
			close(ch)
		}
		j.mu.Unlock()
	}
	return ch, replay, unsubscribe
}

func (j *Job) run(ctx context.Context, runner Runner) {
	j.markStarted()
	result, err := runner(ctx, j, append([]string(nil), j.args...))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			j.markFailed(NewError("operation timed out", 504))
			return
		}
		if errors.Is(err, context.Canceled) {
			j.markCanceled()
			return
		}
		if jobErr, ok := errors.AsType[*Error](err); ok {
			j.markFailed(jobErr)
			return
		}
		j.markFailed(NewError(err.Error(), 500))
		return
	}
	j.markCompleted(result)
}

func (j *Job) markStarted() {
	now := time.Now().UTC()
	j.mu.Lock()
	j.state = StateRunning
	j.startedAt = &now
	j.updatedAt = now
	event := Event{Type: EventStarted, Job: j.snapshotLocked()}
	j.mu.Unlock()
	j.broadcast(event)
}

func (j *Job) markCompleted(result any) {
	now := time.Now().UTC()
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = StateCompleted
	j.result = result
	j.updatedAt = now
	j.finishedAt = &now
	event := Event{Type: EventResult, Job: j.snapshotLocked(), Result: result}
	j.mu.Unlock()
	j.signalDone()
	j.broadcast(event)
	j.closeSubscribers()
}

func (j *Job) markFailed(err *Error) {
	now := time.Now().UTC()
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = StateFailed
	j.err = err
	j.updatedAt = now
	j.finishedAt = &now
	event := Event{Type: EventError, Job: j.snapshotLocked(), Error: err}
	j.mu.Unlock()
	j.signalDone()
	j.broadcast(event)
	j.closeSubscribers()
}

func (j *Job) markCanceled() {
	now := time.Now().UTC()
	jobErr := NewError("operation aborted", 499)
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = StateCanceled
	j.err = jobErr
	j.updatedAt = now
	j.finishedAt = &now
	event := Event{Type: EventCanceled, Job: j.snapshotLocked(), Error: jobErr}
	j.mu.Unlock()
	j.signalDone()
	j.broadcast(event)
	j.closeSubscribers()
}

func (j *Job) signalDone() {
	j.doneOnce.Do(func() {
		close(j.done)
	})
}

func (j *Job) broadcast(event Event) {
	j.mu.RLock()
	subscribers := make([]chan Event, 0, len(j.subscribers))
	for ch := range j.subscribers {
		subscribers = append(subscribers, ch)
	}
	j.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			slog.Debug("dropping job event for slow subscriber", "job_id", j.id, "job_type", j.typ)
		}
	}

	if j.registry != nil {
		j.registry.broadcast(event)
	}
}

func (r *Registry) broadcast(event Event) {
	r.mu.RLock()
	subscribers := make([]chan Event, 0, len(r.subscribers))
	for ch := range r.subscribers {
		subscribers = append(subscribers, ch)
	}
	r.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			slog.Debug("dropping registry job event for slow subscriber", "job_id", event.Job.ID, "job_type", event.Job.Type)
		}
	}
}

func (j *Job) closeSubscribers() {
	j.mu.Lock()
	subscribers := j.subscribers
	j.subscribers = make(map[chan Event]struct{})
	for ch := range subscribers {
		close(ch)
	}
	j.mu.Unlock()
}

func (j *Job) isTerminalLocked() bool {
	return j.state == StateCompleted || j.state == StateFailed || j.state == StateCanceled
}

func (j *Job) snapshotLocked() Snapshot {
	return Snapshot{
		ID:         j.id,
		Type:       j.typ,
		Args:       append([]string(nil), j.args...),
		Owner:      j.owner,
		State:      j.state,
		Progress:   j.progress,
		Result:     j.result,
		Error:      j.err,
		CreatedAt:  j.createdAt,
		StartedAt:  cloneTimePtr(j.startedAt),
		UpdatedAt:  j.updatedAt,
		FinishedAt: cloneTimePtr(j.finishedAt),
	}
}

func (r *Registry) SweepTerminalOlderThan(cutoff time.Time) int {
	r.mu.RLock()
	jobs := make(map[string]*Job, len(r.jobs))
	maps.Copy(jobs, r.jobs)
	r.mu.RUnlock()

	removeIDs := make([]string, 0)
	for id, job := range jobs {
		snapshot := job.Snapshot()
		if snapshot.FinishedAt == nil {
			continue
		}
		if snapshot.State != StateCompleted && snapshot.State != StateFailed && snapshot.State != StateCanceled {
			continue
		}
		if snapshot.FinishedAt.Before(cutoff) {
			removeIDs = append(removeIDs, id)
		}
	}

	if len(removeIDs) == 0 {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for _, id := range removeIDs {
		if _, ok := r.jobs[id]; ok {
			delete(r.jobs, id)
			removed++
		}
	}
	return removed
}

func (r *Registry) startCleanupLoop(ttl, interval time.Duration) {
	if ttl <= 0 || interval <= 0 {
		return
	}
	r.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					cutoff := time.Now().UTC().Add(-ttl)
					removed := r.SweepTerminalOlderThan(cutoff)
					if removed > 0 {
						slog.Debug("swept terminal jobs", "count", removed, "ttl", ttl)
					}
				case <-r.cleanupStop:
					return
				}
			}
		}()
	})
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cloned := *t
	return &cloned
}
