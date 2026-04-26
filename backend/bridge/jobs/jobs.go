package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

type Snapshot struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Args       []string   `json:"args,omitempty"`
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
type Recoverer func(registry *Registry) (*Job, error)
type DataAttacher func(ctx context.Context, job *Job, stream net.Conn, args []string) error

type Registry struct {
	mu            sync.RWMutex
	runners       map[string]Runner
	recoverers    map[string]Recoverer
	dataAttachers map[string]DataAttacher
	jobs          map[string]*Job
	nextID        uint64
}

type Job struct {
	registry *Registry

	mu          sync.RWMutex
	id          string
	typ         string
	args        []string
	state       State
	progress    any
	result      any
	err         *Error
	createdAt   time.Time
	startedAt   *time.Time
	updatedAt   time.Time
	finishedAt  *time.Time
	cancel      context.CancelFunc
	subscribers map[chan Event]struct{}
}

var DefaultRegistry = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{
		runners:       make(map[string]Runner),
		recoverers:    make(map[string]Recoverer),
		dataAttachers: make(map[string]DataAttacher),
		jobs:          make(map[string]*Job),
	}
}

func RegisterRunner(jobType string, runner Runner) {
	DefaultRegistry.RegisterRunner(jobType, runner)
}

func RegisterRecoverer(jobType string, recoverer Recoverer) {
	DefaultRegistry.RegisterRecoverer(jobType, recoverer)
}

func RegisterDataAttacher(jobType string, attacher DataAttacher) {
	DefaultRegistry.RegisterDataAttacher(jobType, attacher)
}

func Start(jobType string, args []string) (*Job, error) {
	return DefaultRegistry.Start(jobType, args)
}

func StartWithRunner(jobType string, args []string, runner Runner) (*Job, error) {
	return DefaultRegistry.StartWithRunner(jobType, args, runner)
}

func Get(id string) (*Job, bool) {
	return DefaultRegistry.Get(id)
}

func List() []Snapshot {
	return DefaultRegistry.List()
}

func ListActive() []Snapshot {
	return DefaultRegistry.ListActive()
}

func FindActiveByType(jobType string) (*Job, bool) {
	return DefaultRegistry.FindActiveByType(jobType)
}

func Recover(jobType string) (*Job, error) {
	return DefaultRegistry.Recover(jobType)
}

func AttachData(ctx context.Context, job *Job, stream net.Conn, args []string) error {
	return DefaultRegistry.AttachData(ctx, job, stream, args)
}

func (r *Registry) RegisterRunner(jobType string, runner Runner) {
	if jobType == "" {
		panic("job type cannot be empty")
	}
	if runner == nil {
		panic("job runner cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.runners[jobType] = runner
}

func (r *Registry) RegisterRecoverer(jobType string, recoverer Recoverer) {
	if jobType == "" {
		panic("job type cannot be empty")
	}
	if recoverer == nil {
		panic("job recoverer cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoverers[jobType] = recoverer
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

func (r *Registry) Start(jobType string, args []string) (*Job, error) {
	r.mu.Lock()
	runner, ok := r.runners[jobType]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("job runner not found: %s", jobType)
	}
	r.mu.Unlock()
	return r.StartWithRunner(jobType, args, runner)
}

func (r *Registry) Recover(jobType string) (*Job, error) {
	if job, ok := r.FindActiveByType(jobType); ok {
		return job, nil
	}

	r.mu.RLock()
	recoverer, ok := r.recoverers[jobType]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("job recoverer not found: %s", jobType)
	}
	return recoverer(r)
}

func (r *Registry) StartWithRunner(jobType string, args []string, runner Runner) (*Job, error) {
	if jobType == "" {
		return nil, fmt.Errorf("job type cannot be empty")
	}
	if runner == nil {
		return nil, fmt.Errorf("job runner cannot be nil")
	}

	r.mu.Lock()
	r.nextID++
	now := time.Now().UTC()
	id := fmt.Sprintf("job-%d", r.nextID)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		registry:    r,
		id:          id,
		typ:         jobType,
		args:        append([]string(nil), args...),
		state:       StateQueued,
		createdAt:   now,
		updatedAt:   now,
		cancel:      cancel,
		subscribers: make(map[chan Event]struct{}),
	}
	r.jobs[id] = job
	r.mu.Unlock()

	go job.run(ctx, runner)
	return job, nil
}

func (r *Registry) Get(id string) (*Job, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	return job, ok
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

func (r *Registry) FindActiveByType(jobType string) (*Job, bool) {
	r.mu.RLock()
	jobs := make([]*Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}
	r.mu.RUnlock()

	for _, job := range jobs {
		snapshot := job.Snapshot()
		if snapshot.Type == jobType && (snapshot.State == StateQueued || snapshot.State == StateRunning) {
			return job, true
		}
	}
	return nil, false
}

func (j *Job) ID() string {
	return j.id
}

func (j *Job) Type() string {
	return j.typ
}

func (j *Job) Snapshot() Snapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return Snapshot{
		ID:         j.id,
		Type:       j.typ,
		Args:       append([]string(nil), j.args...),
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
	j.mu.Unlock()
	j.broadcast(event)
}

func (j *Job) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	j.mu.Lock()
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
	return ch, unsubscribe
}

func (j *Job) run(ctx context.Context, runner Runner) {
	j.markStarted()
	result, err := runner(ctx, j, append([]string(nil), j.args...))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			j.markCanceled()
			return
		}
		var jobErr *Error
		if errors.As(err, &jobErr) {
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
	j.broadcast(event)
	j.closeSubscribers()
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

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cloned := *t
	return &cloned
}
