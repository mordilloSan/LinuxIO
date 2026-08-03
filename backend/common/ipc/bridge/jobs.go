package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"sync"
	"sync/atomic"
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

// Error returns the error message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// NewError creates a new Error with the given message and code.
func NewError(message string, code int) *Error {
	return &Error{Message: message, Code: code}
}

type Owner struct {
	SessionID string `json:"session_id,omitempty"`
	Username  string `json:"username,omitempty"`
	UID       uint32 `json:"uid,omitempty"`
}

// Empty reports whether the Owner is unset (all fields empty).
func (o Owner) Empty() bool {
	return o.SessionID == "" && o.Username == "" && o.UID == 0
}

// Matches reports whether o and other refer to the same owner by username or UID.
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
	ID         string       `json:"id"`
	Type       string       `json:"type"`
	Metadata   *JobMetadata `json:"metadata,omitempty"`
	Owner      Owner        `json:"owner"`
	State      State        `json:"state"`
	Progress   any          `json:"progress,omitempty"`
	Result     any          `json:"result,omitempty"`
	Error      *Error       `json:"error,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	UpdatedAt  time.Time    `json:"updated_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
}

// JobMetadata is the deliberately small public projection of a job request.
// It is populated only by route-declared builders; the decoded request remains
// private execution state and must never be copied into a Snapshot.
type JobMetadata struct {
	Identity    []string `json:"identity,omitempty"`
	Label       string   `json:"label,omitempty"`
	Path        string   `json:"path,omitempty"`
	Action      string   `json:"action,omitempty"`
	ProjectName string   `json:"projectName,omitempty"`
	PackageIDs  []string `json:"packageIds,omitempty"`
	Device      string   `json:"device,omitempty"`
	TestType    string   `json:"testType,omitempty"`
	Capability  string   `json:"capability,omitempty"`
}

// JobMetadataBuilder returns the safe public projection for one decoded request.
type JobMetadataBuilder func(request any) JobMetadata

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
	Type      EventType `json:"type"`
	Job       Snapshot  `json:"job"`
	Progress  any       `json:"progress,omitempty"`
	Result    any       `json:"result,omitempty"`
	Error     *Error    `json:"error,omitempty"`
	transient bool
}

type Runner func(ctx context.Context, job *Job, request any) (any, error)
type DataAttacher func(ctx context.Context, job *Job, stream net.Conn, request any) error

type JobDataAttachRequest struct {
	Offset *string `json:"offset,omitempty"`
}

type Registry struct {
	mu            sync.RWMutex
	dataAttachers map[string]DataAttacher
	jobs          map[string]*Job
	subscribers   map[chan Event]*eventSubscriber
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
	request     any
	metadata    *JobMetadata
	owner       Owner
	state       State
	progress    any
	progressLog []Event
	// progressLogBytes tracks transient data payload bytes. Before the first
	// replay subscriber, the event-count window covers the start/attach race;
	// afterwards replaySubscribed enables a much smaller rolling byte window.
	progressLogBytes int
	replaySubscribed bool
	result           any
	err              *Error
	createdAt        time.Time
	startedAt        *time.Time
	updatedAt        time.Time
	finishedAt       *time.Time
	cancel           context.CancelFunc
	done             chan struct{}
	doneOnce         sync.Once
	subscribers      map[chan Event]*eventSubscriber
}

var DefaultRegistry = NewRegistry()

const (
	DefaultTerminalJobTTL         = 30 * time.Minute
	DefaultTerminalJobSweepPeriod = time.Minute
	DefaultJobProgressReplayLimit = 1024
	// Once at least one direct subscriber has received the initial replay, only
	// retain a small reconnect window. This prevents 64 KiB follow frames from
	// pinning roughly 64 MiB per active job indefinitely.
	DefaultSubscribedJobProgressReplayBytes = 4 * 1024 * 1024
	slowSubscriberLogInterval               = 30 * time.Second
)

type eventSubscriber struct {
	ch          chan Event
	lagged      chan struct{}
	dropped     atomic.Uint64
	lastDropLog atomic.Int64
}

func newEventSubscriber(ch chan Event) *eventSubscriber {
	return &eventSubscriber{
		ch:     ch,
		lagged: make(chan struct{}, 1),
	}
}

// NewRegistry creates a new job registry with automatic cleanup of terminal jobs.
func NewRegistry() *Registry {
	r := &Registry{
		dataAttachers: make(map[string]DataAttacher),
		jobs:          make(map[string]*Job),
		subscribers:   make(map[chan Event]*eventSubscriber),
		cleanupStop:   make(chan struct{}),
	}
	r.startCleanupLoop(DefaultTerminalJobTTL, DefaultTerminalJobSweepPeriod)
	return r
}

// RegisterDataAttacher registers a data attacher for the given job type on the default registry.
func RegisterDataAttacher(jobType string, attacher DataAttacher) {
	DefaultRegistry.RegisterDataAttacher(jobType, attacher)
}

// RegisterDataAttacher registers a data attacher for the given job type.
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

// AttachData calls the registered data attacher for the job's type.
func (r *Registry) AttachData(ctx context.Context, job *Job, stream net.Conn, request any) error {
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}

	r.mu.RLock()
	attacher, ok := r.dataAttachers[job.Type()]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("job data attacher not found: %s", job.Type())
	}
	return attacher(ctx, job, stream, request)
}

// Create creates a new unowned job in the registry.
func (r *Registry) Create(jobType string, request any) (*Job, error) {
	return r.CreateForOwner(jobType, request, Owner{})
}

// CreateForOwner creates a new job owned by the specified owner.
func (r *Registry) CreateForOwner(jobType string, request any, owner Owner, metadata ...*JobMetadata) (*Job, error) {
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
	var publicMetadata *JobMetadata
	if len(metadata) > 0 && metadata[0] != nil {
		publicMetadata = cloneJobMetadata(metadata[0])
	}
	job := &Job{
		registry:    r,
		ctx:         ctx,
		id:          id,
		typ:         jobType,
		request:     request,
		metadata:    publicMetadata,
		owner:       owner,
		state:       StateQueued,
		createdAt:   now,
		updatedAt:   now,
		cancel:      cancel,
		done:        make(chan struct{}),
		subscribers: make(map[chan Event]*eventSubscriber),
	}
	r.jobs[id] = job
	r.mu.Unlock()

	return job, nil
}

// Get retrieves a job by ID, returning false if not found.
func (r *Registry) Get(id string) (*Job, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	return job, ok
}

// GetForOwner retrieves a job by ID if it belongs to the owner, returning false otherwise.
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

// List returns snapshots of all jobs in the registry.
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

// ListForOwner returns snapshots of all jobs belonging to the owner.
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

// ListActive returns snapshots of all queued and running jobs.
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

// ListActiveForOwner returns snapshots of all queued and running jobs belonging to the owner.
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

// Subscribe returns a channel that receives all job events from the registry,
// and an unsubscribe function to stop receiving events.
func (r *Registry) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Event, buffer)
	r.mu.Lock()
	r.subscribers[ch] = newEventSubscriber(ch)
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

// ID returns the job's unique identifier.
func (j *Job) ID() string {
	return j.id
}

// Type returns the job type.
func (j *Job) Type() string {
	return j.typ
}

// Owner returns the job's owner.
func (j *Job) Owner() Owner {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.owner
}

// Snapshot returns a point-in-time snapshot of the job's state.
func (j *Job) Snapshot() Snapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return Snapshot{
		ID:         j.id,
		Type:       j.typ,
		Metadata:   cloneJobMetadata(j.metadata),
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

// Cancel requests cancellation of the job. If the job is queued, it is marked
// canceled immediately; if running, the context is canceled and the job will
// transition to canceled when it detects the cancellation.
func (j *Job) Cancel() {
	j.mu.Lock()
	if j.state == StateQueued {
		// Queue cancellation and Start contend on this same lock. Once this
		// transition wins, Start observes a terminal state and cannot run the
		// handler after a router has reserved the job for promotion.
		j.cancel()
		event := j.markCanceledLocked(time.Now().UTC())
		j.mu.Unlock()
		j.publishTerminal(event)
		return
	}
	j.mu.Unlock()
	// A running job owns its runner; cancellation is delivered through its
	// context and the runner's normal terminal path publishes the event.
	j.cancel()
}

// CancelForSession cancels all non-terminal jobs belonging to the given session.
// The session ID must not be logged by callers.
func (r *Registry) CancelForSession(sessionID string) {
	if sessionID == "" {
		return
	}
	r.mu.RLock()
	jobs := make([]*Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		if job.owner.SessionID == sessionID {
			jobs = append(jobs, job)
		}
	}
	r.mu.RUnlock()
	for _, job := range jobs {
		if job.IsTerminal() {
			continue
		}
		job.Cancel()
	}
}

// Done returns a channel that closes when the job reaches a terminal state.
func (j *Job) Done() <-chan struct{} {
	return j.done
}

// Start begins executing the job with the given runner. If runner is nil, the job fails immediately.
func (j *Job) Start(runner Runner) bool {
	if runner == nil {
		j.markFailed(NewError("job runner cannot be nil", 500))
		return false
	}
	now := time.Now().UTC()
	j.mu.Lock()
	if j.state != StateQueued {
		j.mu.Unlock()
		return false
	}
	j.state = StateRunning
	j.startedAt = &now
	j.updatedAt = now
	request := j.request
	event := Event{Type: EventStarted, Job: j.snapshotLocked()}
	j.mu.Unlock()
	j.broadcast(event)
	go j.run(j.ctx, runner, request)
	return true
}

// IsTerminal reports whether the job has reached a terminal state (completed, failed, or canceled).
func (j *Job) IsTerminal() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.isTerminalLocked()
}

// ReportProgress updates the job's durable progress. The progress is broadcast
// to direct job subscribers and the registry, and recorded for replay to future
// direct subscribers.
func (j *Job) ReportProgress(progress any) {
	if isJobDataProgress(progress) {
		j.ReportTransientProgress(progress)
		return
	}

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
	j.appendProgressLogLocked(event)
	j.mu.Unlock()
	j.broadcast(event)
}

// ReportData emits transient stream data to direct job subscribers only. Data
// events are replayed to future direct subscribers to cover the start-job/attach
// race, but they are not exposed through jobs.events or stored as snapshot
// progress.
func (j *Job) ReportData(data string) {
	j.ReportTransientProgress(map[string]any{"type": "data", "data": data})
}

// ReportTransientProgress emits a progress-shaped event to direct job
// subscribers only. Use it for stream output that should reach jobs.attach but
// should not become durable job state or a jobs.events notification.
func (j *Job) ReportTransientProgress(progress any) {
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	event := Event{
		Type:      EventProgress,
		Job:       j.snapshotLocked(),
		Progress:  progress,
		transient: true,
	}
	j.appendProgressLogLocked(event)
	j.mu.Unlock()
	j.broadcastLocal(event)
}

func (j *Job) appendProgressLogLocked(event Event) {
	j.progressLog = append(j.progressLog, event)
	j.progressLogBytes += jobDataProgressBytes(event.Progress)
	j.trimProgressLogLocked()
}

func jobDataProgressBytes(progress any) int {
	payload, ok := progress.(map[string]any)
	if !ok || payload["type"] != "data" {
		return 0
	}
	data, ok := payload["data"].(string)
	if !ok {
		return 0
	}
	return len(data)
}

func (j *Job) trimProgressLogLocked() {
	start := 0
	for limit := DefaultJobProgressReplayLimit; limit > 0 && len(j.progressLog)-start > limit; {
		j.progressLogBytes -= jobDataProgressBytes(j.progressLog[start].Progress)
		start++
	}
	if j.replaySubscribed {
		for limit := DefaultSubscribedJobProgressReplayBytes; limit > 0 &&
			j.progressLogBytes > limit &&
			start < len(j.progressLog); {
			j.progressLogBytes -= jobDataProgressBytes(j.progressLog[start].Progress)
			start++
		}
	}
	if start > 0 {
		j.progressLog = append([]Event(nil), j.progressLog[start:]...)
	}
}

// Subscribe returns a channel that receives job events, and an unsubscribe function.
func (j *Job) Subscribe(buffer int) (<-chan Event, func()) {
	ch, _, unsubscribe := j.SubscribeWithReplay(buffer)
	return ch, unsubscribe
}

// SubscribeWithReplay returns a channel that receives job events, prior progress
// events for replay, and an unsubscribe function. The replay contains up to
// DefaultJobProgressReplayLimit recent progress events.
func (j *Job) SubscribeWithReplay(buffer int) (<-chan Event, []Event, func()) {
	ch, replay, _, unsubscribe := j.subscribeWithReplayStatus(buffer)
	return ch, replay, unsubscribe
}

// subscribeWithReplayStatus additionally reports when the bounded live-event
// channel has overflowed. Attach streams use this to fail explicitly so a
// cursor-aware client can reconnect instead of silently accepting a gap.
func (j *Job) subscribeWithReplayStatus(buffer int) (<-chan Event, []Event, <-chan struct{}, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	subscriber := newEventSubscriber(ch)
	j.mu.Lock()
	replay := append([]Event(nil), j.progressLog...)
	j.replaySubscribed = true
	j.trimProgressLogLocked()
	j.subscribers[ch] = subscriber
	j.mu.Unlock()

	unsubscribe := func() {
		j.mu.Lock()
		if _, ok := j.subscribers[ch]; ok {
			delete(j.subscribers, ch)
			close(ch)
		}
		j.mu.Unlock()
	}
	return ch, replay, subscriber.lagged, unsubscribe
}

func (j *Job) run(ctx context.Context, runner Runner, request any) {
	result, err := runner(ctx, j, request)
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

// publishTerminal is the publication tail shared by every terminal transition:
// release Done() waiters, broadcast the terminal event, and close direct
// subscribers. Call exactly once per job, after j.mu has been released — the
// lock split is required by the Start/Cancel race, the tail is not.
func (j *Job) publishTerminal(event Event) {
	j.signalDone()
	j.broadcast(event)
	j.closeSubscribers()
}

func (j *Job) markCompleted(result any) {
	now := time.Now().UTC()
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = StateCompleted
	j.request = nil
	j.result = result
	j.updatedAt = now
	j.finishedAt = &now
	event := Event{Type: EventResult, Job: j.snapshotLocked(), Result: result}
	j.mu.Unlock()
	j.publishTerminal(event)
}

func (j *Job) markFailed(err *Error) {
	now := time.Now().UTC()
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = StateFailed
	j.request = nil
	j.err = err
	j.updatedAt = now
	j.finishedAt = &now
	event := Event{Type: EventError, Job: j.snapshotLocked(), Error: err}
	j.mu.Unlock()
	j.publishTerminal(event)
}

func (j *Job) markCanceled() {
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	event := j.markCanceledLocked(time.Now().UTC())
	j.mu.Unlock()
	j.publishTerminal(event)
}

// markCanceledLocked records cancellation while j.mu is held. The caller is
// responsible for signaling and broadcasting after releasing the lock.
func (j *Job) markCanceledLocked(now time.Time) Event {
	jobErr := NewError("operation aborted", 499)
	j.state = StateCanceled
	j.request = nil
	j.err = jobErr
	j.updatedAt = now
	j.finishedAt = &now
	return Event{Type: EventCanceled, Job: j.snapshotLocked(), Error: jobErr}
}

func cloneJobMetadata(metadata *JobMetadata) *JobMetadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	clone.Identity = append([]string(nil), metadata.Identity...)
	clone.PackageIDs = append([]string(nil), metadata.PackageIDs...)
	return &clone
}

func (j *Job) signalDone() {
	j.doneOnce.Do(func() {
		close(j.done)
	})
}

func (j *Job) broadcast(event Event) {
	j.broadcastLocal(event)

	if j.registry != nil {
		j.registry.broadcast(event)
	}
}

func (j *Job) broadcastLocal(event Event) {
	j.mu.RLock()
	for _, subscriber := range j.subscribers {
		subscriber.send(event, "job")
	}
	j.mu.RUnlock()
}

func (r *Registry) broadcast(event Event) {
	r.mu.RLock()
	for _, subscriber := range r.subscribers {
		subscriber.send(event, "registry")
	}
	r.mu.RUnlock()
}

func (j *Job) closeSubscribers() {
	j.mu.Lock()
	subscribers := j.subscribers
	j.subscribers = make(map[chan Event]*eventSubscriber)
	for ch := range subscribers {
		close(ch)
	}
	j.mu.Unlock()
}

func (s *eventSubscriber) send(event Event, scope string) bool {
	select {
	case s.ch <- event:
		return true
	default:
	}

	if event.Type != EventProgress && s.dropOldest() {
		select {
		case s.ch <- event:
			s.logDropped(event, scope)
			return true
		default:
		}
	}

	s.logDropped(event, scope)
	return false
}

func (s *eventSubscriber) dropOldest() bool {
	select {
	case _, ok := <-s.ch:
		return ok
	default:
		return false
	}
}

func (s *eventSubscriber) logDropped(event Event, scope string) {
	select {
	case s.lagged <- struct{}{}:
	default:
	}

	if event.transient || isJobDataProgress(event.Progress) {
		return
	}

	s.dropped.Add(1)
	now := time.Now()
	last := s.lastDropLog.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) < slowSubscriberLogInterval {
		return
	}
	if !s.lastDropLog.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	dropped := s.dropped.Swap(0)
	slog.Debug(
		"dropping job events for slow subscriber",
		"scope", scope,
		"dropped", dropped,
		"job_id", event.Job.ID,
		"job_type", event.Job.Type,
	)
}

func isJobDataProgress(progress any) bool {
	switch p := progress.(type) {
	case map[string]any:
		value, _ := p["type"].(string)
		return value == "data"
	case map[string]string:
		return p["type"] == "data"
	default:
		return false
	}
}

func (j *Job) isTerminalLocked() bool {
	return j.state == StateCompleted || j.state == StateFailed || j.state == StateCanceled
}

func (j *Job) snapshotLocked() Snapshot {
	return Snapshot{
		ID:         j.id,
		Type:       j.typ,
		Metadata:   cloneJobMetadata(j.metadata),
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

// SweepTerminalOlderThan removes all terminal jobs that finished before the cutoff time.
// It returns the number of jobs removed.
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
