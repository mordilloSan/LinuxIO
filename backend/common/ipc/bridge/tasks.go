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

type TaskState string

const (
	TaskStateQueued    TaskState = "queued"
	TaskStateRunning   TaskState = "running"
	TaskStateCompleted TaskState = "completed"
	TaskStateFailed    TaskState = "failed"
	TaskStateCanceled  TaskState = "canceled"
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

type TaskOwner struct {
	SessionID string `json:"session_id,omitempty"`
	Username  string `json:"username,omitempty"`
	UID       uint32 `json:"uid,omitempty"`
}

// Empty reports whether the TaskOwner is unset (all fields empty).
func (o TaskOwner) Empty() bool {
	return o.SessionID == "" && o.Username == "" && o.UID == 0
}

// Matches reports whether o and other refer to the same owner by username or UID.
func (o TaskOwner) Matches(other TaskOwner) bool {
	if o.Empty() || other.Empty() {
		return false
	}
	if o.Username != "" && other.Username != "" {
		return o.Username == other.Username
	}
	return o.UID != 0 && o.UID == other.UID
}

type TaskSnapshot struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Metadata   *TaskMetadata `json:"metadata,omitempty"`
	Owner      TaskOwner     `json:"owner"`
	State      TaskState     `json:"state"`
	Progress   any           `json:"progress,omitempty"`
	Result     any           `json:"result,omitempty"`
	Error      *Error        `json:"error,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	StartedAt  *time.Time    `json:"started_at,omitempty"`
	UpdatedAt  time.Time     `json:"updated_at"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}

// TaskMetadata is the deliberately small public projection of a task request.
// It is populated only by route-declared builders; the decoded request remains
// private execution state and must never be copied into a TaskSnapshot.
type TaskMetadata struct {
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

// TaskMetadataBuilder returns the safe public projection for one decoded request.
type TaskMetadataBuilder func(request any) TaskMetadata

type TaskEventType string

const (
	TaskEventSnapshot TaskEventType = "task.snapshot"
	TaskEventStarted  TaskEventType = "task.started"
	TaskEventProgress TaskEventType = "task.progress"
	TaskEventResult   TaskEventType = "task.result"
	TaskEventError    TaskEventType = "task.error"
	TaskEventCanceled TaskEventType = "task.canceled"
)

type TaskEvent struct {
	Type      TaskEventType `json:"type"`
	Task      TaskSnapshot  `json:"task"`
	Progress  any           `json:"progress,omitempty"`
	Result    any           `json:"result,omitempty"`
	Error     *Error        `json:"error,omitempty"`
	transient bool
}

type TaskRunner func(ctx context.Context, task *Task, request any) (any, error)
type TaskDataAttacher func(ctx context.Context, task *Task, stream net.Conn, request any) error

type TaskDataAttachRequest struct {
	Offset *string `json:"offset,omitempty"`
}

type TaskService struct {
	mu            sync.RWMutex
	dataAttachers map[string]TaskDataAttacher
	tasks         map[string]*Task
	subscribers   map[chan TaskEvent]*eventSubscriber
	nextID        uint64
	cleanupStop   chan struct{}
	cleanupOnce   sync.Once
}

type Task struct {
	registry *TaskService

	ctx         context.Context
	mu          sync.RWMutex
	id          string
	typ         string
	request     any
	metadata    *TaskMetadata
	owner       TaskOwner
	state       TaskState
	progress    any
	progressLog []TaskEvent
	// progressLogBytes tracks transient data payload bytes. Before the first
	// replay subscriber, the event-count window covers the start/watch race;
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
	subscribers      map[chan TaskEvent]*eventSubscriber
}

var DefaultTaskService = NewTaskService()

const (
	DefaultTerminalTaskTTL         = 30 * time.Minute
	DefaultTerminalTaskSweepPeriod = time.Minute
	DefaultTaskProgressReplayLimit = 1024
	// Once at least one direct subscriber has received the initial replay, only
	// retain a small reconnect window. This prevents 64 KiB follow frames from
	// pinning roughly 64 MiB per active task indefinitely.
	DefaultSubscribedTaskProgressReplayBytes = 4 * 1024 * 1024
	slowSubscriberLogInterval                = 30 * time.Second
)

type eventSubscriber struct {
	ch          chan TaskEvent
	lagged      chan struct{}
	dropped     atomic.Uint64
	lastDropLog atomic.Int64
}

func newEventSubscriber(ch chan TaskEvent) *eventSubscriber {
	return &eventSubscriber{
		ch:     ch,
		lagged: make(chan struct{}, 1),
	}
}

// NewTaskService creates a task service with automatic cleanup of terminal tasks.
func NewTaskService() *TaskService {
	r := &TaskService{
		dataAttachers: make(map[string]TaskDataAttacher),
		tasks:         make(map[string]*Task),
		subscribers:   make(map[chan TaskEvent]*eventSubscriber),
		cleanupStop:   make(chan struct{}),
	}
	r.startCleanupLoop(DefaultTerminalTaskTTL, DefaultTerminalTaskSweepPeriod)
	return r
}

// RegisterTaskDataAttacher registers a data attacher for the given task type on the default service.
func RegisterTaskDataAttacher(taskType string, attacher TaskDataAttacher) {
	DefaultTaskService.RegisterTaskDataAttacher(taskType, attacher)
}

// RegisterTaskDataAttacher registers a data attacher for the given task type.
func (r *TaskService) RegisterTaskDataAttacher(taskType string, attacher TaskDataAttacher) {
	if taskType == "" {
		panic("task type cannot be empty")
	}
	if attacher == nil {
		panic("task data attacher cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.dataAttachers[taskType] = attacher
}

// AttachData calls the registered data attacher for the task's type.
func (r *TaskService) AttachData(ctx context.Context, task *Task, stream net.Conn, request any) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}

	r.mu.RLock()
	attacher, ok := r.dataAttachers[task.Type()]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task data attacher not found: %s", task.Type())
	}
	return attacher(ctx, task, stream, request)
}

// Create creates a new unowned task in the registry.
func (r *TaskService) Create(taskType string, request any) (*Task, error) {
	return r.CreateForOwner(taskType, request, TaskOwner{})
}

// CreateForOwner creates a new task owned by the specified owner.
func (r *TaskService) CreateForOwner(taskType string, request any, owner TaskOwner, metadata ...*TaskMetadata) (*Task, error) {
	if taskType == "" {
		return nil, fmt.Errorf("task type cannot be empty")
	}

	r.mu.Lock()
	r.nextID++
	now := time.Now().UTC()
	id := fmt.Sprintf("task-%d", r.nextID)
	// Tasks are intentionally detached from the stream that created them; cancel
	// through tasks.cancel, watched stream abort, or policy timeout instead.
	ctx, cancel := context.WithCancel(context.Background())
	var publicMetadata *TaskMetadata
	if len(metadata) > 0 && metadata[0] != nil {
		publicMetadata = cloneTaskMetadata(metadata[0])
	}
	task := &Task{
		registry:    r,
		ctx:         ctx,
		id:          id,
		typ:         taskType,
		request:     request,
		metadata:    publicMetadata,
		owner:       owner,
		state:       TaskStateQueued,
		createdAt:   now,
		updatedAt:   now,
		cancel:      cancel,
		done:        make(chan struct{}),
		subscribers: make(map[chan TaskEvent]*eventSubscriber),
	}
	r.tasks[id] = task
	r.mu.Unlock()

	return task, nil
}

// Get retrieves a task by ID, returning false if not found.
func (r *TaskService) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[id]
	return task, ok
}

// GetForOwner retrieves a task by ID if it belongs to the owner, returning false otherwise.
func (r *TaskService) GetForOwner(id string, owner TaskOwner) (*Task, bool) {
	task, ok := r.Get(id)
	if !ok {
		return nil, false
	}
	if !task.Owner().Matches(owner) {
		return nil, false
	}
	return task, true
}

// List returns snapshots of all tasks in the registry.
func (r *TaskService) List() []TaskSnapshot {
	r.mu.RLock()
	tasks := make([]*Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}
	r.mu.RUnlock()

	snapshots := make([]TaskSnapshot, 0, len(tasks))
	for _, task := range tasks {
		snapshots = append(snapshots, task.Snapshot())
	}
	return snapshots
}

// ListForOwner returns snapshots of all tasks belonging to the owner.
func (r *TaskService) ListForOwner(owner TaskOwner) []TaskSnapshot {
	all := r.List()
	filtered := all[:0]
	for _, snapshot := range all {
		if snapshot.Owner.Matches(owner) {
			filtered = append(filtered, snapshot)
		}
	}
	return filtered
}

// ListActive returns snapshots of all queued and running tasks.
func (r *TaskService) ListActive() []TaskSnapshot {
	all := r.List()
	active := all[:0]
	for _, snapshot := range all {
		if snapshot.State == TaskStateQueued || snapshot.State == TaskStateRunning {
			active = append(active, snapshot)
		}
	}
	return active
}

// ListActiveForOwner returns snapshots of all queued and running tasks belonging to the owner.
func (r *TaskService) ListActiveForOwner(owner TaskOwner) []TaskSnapshot {
	all := r.ListForOwner(owner)
	active := all[:0]
	for _, snapshot := range all {
		if snapshot.State == TaskStateQueued || snapshot.State == TaskStateRunning {
			active = append(active, snapshot)
		}
	}
	return active
}

// Subscribe returns a channel that receives all task events from the service,
// and an unsubscribe function to stop receiving events.
func (r *TaskService) Subscribe(buffer int) (<-chan TaskEvent, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan TaskEvent, buffer)
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

// ID returns the task's unique identifier.
func (j *Task) ID() string {
	return j.id
}

// Type returns the task type.
func (j *Task) Type() string {
	return j.typ
}

// TaskOwner returns the task's owner.
func (j *Task) Owner() TaskOwner {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.owner
}

// TaskSnapshot returns a point-in-time snapshot of the task's state.
func (j *Task) Snapshot() TaskSnapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return TaskSnapshot{
		ID:         j.id,
		Type:       j.typ,
		Metadata:   cloneTaskMetadata(j.metadata),
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

// Cancel requests cancellation of the task. If the task is queued, it is marked
// canceled immediately; if running, the context is canceled and the task will
// transition to canceled when it detects the cancellation.
func (j *Task) Cancel() {
	j.mu.Lock()
	if j.state == TaskStateQueued {
		// Queue cancellation and Start contend on this same lock. Once this
		// transition wins, Start observes a terminal state and cannot run the
		// handler after a router has reserved the task for promotion.
		j.cancel()
		event := j.markCanceledLocked(time.Now().UTC())
		j.mu.Unlock()
		j.publishTerminal(event)
		return
	}
	j.mu.Unlock()
	// A running task owns its runner; cancellation is delivered through its
	// context and the runner's normal terminal path publishes the event.
	j.cancel()
}

// CancelTasksForSession cancels all non-terminal tasks belonging to the given session.
// The session ID must not be logged by callers.
func (r *TaskService) CancelTasksForSession(sessionID string) {
	if sessionID == "" {
		return
	}
	r.mu.RLock()
	tasks := make([]*Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if task.owner.SessionID == sessionID {
			tasks = append(tasks, task)
		}
	}
	r.mu.RUnlock()
	for _, task := range tasks {
		if task.IsTerminal() {
			continue
		}
		task.Cancel()
	}
}

// Done returns a channel that closes when the task reaches a terminal state.
func (j *Task) Done() <-chan struct{} {
	return j.done
}

// Start begins executing the task with the given runner. If runner is nil, the task fails immediately.
func (j *Task) Start(runner TaskRunner) bool {
	if runner == nil {
		j.markFailed(NewError("task runner cannot be nil", 500))
		return false
	}
	now := time.Now().UTC()
	j.mu.Lock()
	if j.state != TaskStateQueued {
		j.mu.Unlock()
		return false
	}
	j.state = TaskStateRunning
	j.startedAt = &now
	j.updatedAt = now
	request := j.request
	event := TaskEvent{Type: TaskEventStarted, Task: j.snapshotLocked()}
	j.mu.Unlock()
	j.broadcast(event)
	go j.run(j.ctx, runner, request)
	return true
}

// IsTerminal reports whether the task has reached a terminal state (completed, failed, or canceled).
func (j *Task) IsTerminal() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.isTerminalLocked()
}

// ReportProgress updates the task's durable progress. The progress is broadcast
// to direct task subscribers and the service, and recorded for replay to future
// direct subscribers.
func (j *Task) ReportProgress(progress any) {
	if isTaskDataProgress(progress) {
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
	event := TaskEvent{
		Type:     TaskEventProgress,
		Task:     j.snapshotLocked(),
		Progress: progress,
	}
	j.appendProgressLogLocked(event)
	j.mu.Unlock()
	j.broadcast(event)
}

// ReportData emits transient stream data to direct task subscribers only. Data
// events are replayed to future direct subscribers to cover the start-task/watch
// race, but they are not exposed through tasks.events or stored as snapshot
// progress.
func (j *Task) ReportData(data string) {
	j.ReportTransientProgress(map[string]any{"type": "data", "data": data})
}

// ReportTransientProgress emits a progress-shaped event to direct task
// subscribers only. Use it for stream output that should reach tasks.watch but
// should not become durable task state or a tasks.events notification.
func (j *Task) ReportTransientProgress(progress any) {
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	event := TaskEvent{
		Type:      TaskEventProgress,
		Task:      j.snapshotLocked(),
		Progress:  progress,
		transient: true,
	}
	j.appendProgressLogLocked(event)
	j.mu.Unlock()
	j.broadcastLocal(event)
}

func (j *Task) appendProgressLogLocked(event TaskEvent) {
	j.progressLog = append(j.progressLog, event)
	j.progressLogBytes += taskDataProgressBytes(event.Progress)
	j.trimProgressLogLocked()
}

func taskDataProgressBytes(progress any) int {
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

func (j *Task) trimProgressLogLocked() {
	start := 0
	for limit := DefaultTaskProgressReplayLimit; limit > 0 && len(j.progressLog)-start > limit; {
		j.progressLogBytes -= taskDataProgressBytes(j.progressLog[start].Progress)
		start++
	}
	if j.replaySubscribed {
		for limit := DefaultSubscribedTaskProgressReplayBytes; limit > 0 &&
			j.progressLogBytes > limit &&
			start < len(j.progressLog); {
			j.progressLogBytes -= taskDataProgressBytes(j.progressLog[start].Progress)
			start++
		}
	}
	if start > 0 {
		j.progressLog = append([]TaskEvent(nil), j.progressLog[start:]...)
	}
}

// Subscribe returns a channel that receives task events, and an unsubscribe function.
func (j *Task) Subscribe(buffer int) (<-chan TaskEvent, func()) {
	ch, _, unsubscribe := j.SubscribeWithReplay(buffer)
	return ch, unsubscribe
}

// SubscribeWithReplay returns a channel that receives task events, prior progress
// events for replay, and an unsubscribe function. The replay contains up to
// DefaultTaskProgressReplayLimit recent progress events.
func (j *Task) SubscribeWithReplay(buffer int) (<-chan TaskEvent, []TaskEvent, func()) {
	ch, replay, _, unsubscribe := j.subscribeWithReplayStatus(buffer)
	return ch, replay, unsubscribe
}

// subscribeWithReplayStatus additionally reports when the bounded live-event
// channel has overflowed. Watch streams use this to fail explicitly so a
// cursor-aware client can reconnect instead of silently accepting a gap.
func (j *Task) subscribeWithReplayStatus(buffer int) (<-chan TaskEvent, []TaskEvent, <-chan struct{}, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan TaskEvent, buffer)
	subscriber := newEventSubscriber(ch)
	j.mu.Lock()
	replay := append([]TaskEvent(nil), j.progressLog...)
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

func (j *Task) run(ctx context.Context, runner TaskRunner, request any) {
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
		if taskErr, ok := errors.AsType[*Error](err); ok {
			j.markFailed(taskErr)
			return
		}
		j.markFailed(NewError(err.Error(), 500))
		return
	}
	j.markCompleted(result)
}

// publishTerminal is the publication tail shared by every terminal transition:
// release Done() waiters, broadcast the terminal event, and close direct
// subscribers. Call exactly once per task, after j.mu has been released — the
// lock split is required by the Start/Cancel race, the tail is not.
func (j *Task) publishTerminal(event TaskEvent) {
	j.signalDone()
	j.broadcast(event)
	j.closeSubscribers()
}

func (j *Task) markCompleted(result any) {
	now := time.Now().UTC()
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = TaskStateCompleted
	j.request = nil
	j.result = result
	j.updatedAt = now
	j.finishedAt = &now
	event := TaskEvent{Type: TaskEventResult, Task: j.snapshotLocked(), Result: result}
	j.mu.Unlock()
	j.publishTerminal(event)
}

func (j *Task) markFailed(err *Error) {
	now := time.Now().UTC()
	j.mu.Lock()
	if j.isTerminalLocked() {
		j.mu.Unlock()
		return
	}
	j.state = TaskStateFailed
	j.request = nil
	j.err = err
	j.updatedAt = now
	j.finishedAt = &now
	event := TaskEvent{Type: TaskEventError, Task: j.snapshotLocked(), Error: err}
	j.mu.Unlock()
	j.publishTerminal(event)
}

func (j *Task) markCanceled() {
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
func (j *Task) markCanceledLocked(now time.Time) TaskEvent {
	taskErr := NewError("operation aborted", 499)
	j.state = TaskStateCanceled
	j.request = nil
	j.err = taskErr
	j.updatedAt = now
	j.finishedAt = &now
	return TaskEvent{Type: TaskEventCanceled, Task: j.snapshotLocked(), Error: taskErr}
}

func cloneTaskMetadata(metadata *TaskMetadata) *TaskMetadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	clone.Identity = append([]string(nil), metadata.Identity...)
	clone.PackageIDs = append([]string(nil), metadata.PackageIDs...)
	return &clone
}

func (j *Task) signalDone() {
	j.doneOnce.Do(func() {
		close(j.done)
	})
}

func (j *Task) broadcast(event TaskEvent) {
	j.broadcastLocal(event)

	if j.registry != nil {
		j.registry.broadcast(event)
	}
}

func (j *Task) broadcastLocal(event TaskEvent) {
	j.mu.RLock()
	for _, subscriber := range j.subscribers {
		subscriber.send(event, "task")
	}
	j.mu.RUnlock()
}

func (r *TaskService) broadcast(event TaskEvent) {
	r.mu.RLock()
	for _, subscriber := range r.subscribers {
		subscriber.send(event, "registry")
	}
	r.mu.RUnlock()
}

func (j *Task) closeSubscribers() {
	j.mu.Lock()
	subscribers := j.subscribers
	j.subscribers = make(map[chan TaskEvent]*eventSubscriber)
	for ch := range subscribers {
		close(ch)
	}
	j.mu.Unlock()
}

func (s *eventSubscriber) send(event TaskEvent, scope string) bool {
	select {
	case s.ch <- event:
		return true
	default:
	}

	if event.Type != TaskEventProgress && s.dropOldest() {
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

func (s *eventSubscriber) logDropped(event TaskEvent, scope string) {
	select {
	case s.lagged <- struct{}{}:
	default:
	}

	if event.transient || isTaskDataProgress(event.Progress) {
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
		"dropping task events for slow subscriber",
		"scope", scope,
		"dropped", dropped,
		"task_id", event.Task.ID,
		"task_type", event.Task.Type,
	)
}

func isTaskDataProgress(progress any) bool {
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

func (j *Task) isTerminalLocked() bool {
	return j.state == TaskStateCompleted || j.state == TaskStateFailed || j.state == TaskStateCanceled
}

func (j *Task) snapshotLocked() TaskSnapshot {
	return TaskSnapshot{
		ID:         j.id,
		Type:       j.typ,
		Metadata:   cloneTaskMetadata(j.metadata),
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

// SweepTerminalOlderThan removes all terminal tasks that finished before the cutoff time.
// It returns the number of tasks removed.
func (r *TaskService) SweepTerminalOlderThan(cutoff time.Time) int {
	r.mu.RLock()
	tasks := make(map[string]*Task, len(r.tasks))
	maps.Copy(tasks, r.tasks)
	r.mu.RUnlock()

	removeIDs := make([]string, 0)
	for id, task := range tasks {
		snapshot := task.Snapshot()
		if snapshot.FinishedAt == nil {
			continue
		}
		if snapshot.State != TaskStateCompleted && snapshot.State != TaskStateFailed && snapshot.State != TaskStateCanceled {
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
		if _, ok := r.tasks[id]; ok {
			delete(r.tasks, id)
			removed++
		}
	}
	return removed
}

func (r *TaskService) startCleanupLoop(ttl, interval time.Duration) {
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
						slog.Debug("swept terminal tasks", "count", removed, "ttl", ttl)
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
