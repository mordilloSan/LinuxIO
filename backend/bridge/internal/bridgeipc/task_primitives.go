package bridgeipc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

type taskIDRequest struct {
	TaskID string `json:"taskId"`
}

type taskListRequest struct {
	Status string `json:"status,omitempty"`
}

type taskDataRequest struct {
	TaskID string `json:"taskId"`
	Offset string `json:"offset,omitempty"`
}

// ResumeRoute asks the root job host to resume an interrupted resumable job.
const ResumeRoute = "jobs.resume"

type jobResumeRequest struct {
	ID string `json:"id"`
}

// RemoteTasks serves the tasks.* routes for the jobs of a bridge's job host,
// whose IDs are UUIDs, as the bridge's own registry serves its tasks.
type RemoteTasks interface {
	Get(ctx context.Context, id string) (TaskSnapshot, error)
	Cancel(ctx context.Context, id string) (TaskSnapshot, error)
	Watch(ctx context.Context, stream net.Conn, id string) error
	List(ctx context.Context) ([]TaskSnapshot, error)
	Events(ctx context.Context, stream net.Conn) error
}

// SetRemote sends tasks.* requests for job IDs this service does not know to
// rt, and merges its jobs into tasks.list and tasks.events.
func (r *TaskService) SetRemote(rt RemoteTasks) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remote = rt
}

func (r *TaskService) getRemote() RemoteTasks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.remote
}

// remoteFor returns the remote that owns id, a job ID this service has no
// task for, or nil.
func (r *TaskService) remoteFor(id string) RemoteTasks {
	if jobs.ValidateID(id) != nil {
		return nil
	}
	return r.getRemote()
}

// RegisterRoutes installs the Task control Calls and Channels on the router
// that owns this service. The general Router does not dispatch the reserved
// namespace itself.
func (r *TaskService) RegisterRoutes(router *Router) {
	routes := []Route{
		{Name: "tasks.get", Mode: ModeCall, Call: r.handleTaskGet, Decode: JSONRequestDecoder[taskIDRequest]()},
		{Name: "tasks.list", Mode: ModeCall, Call: r.handleTaskList, Decode: JSONRequestDecoder[taskListRequest]()},
		{Name: "tasks.cancel", Mode: ModeCall, Call: r.handleTaskCancel, Decode: JSONRequestDecoder[taskIDRequest]()},
		{Name: "tasks.watch", Mode: ModeDuplex, Duplex: r.handleTaskWatch, Decode: JSONRequestDecoder[taskIDRequest]()},
		{Name: "tasks.data", Mode: ModeDuplex, Duplex: r.handleTaskData, Decode: JSONRequestDecoder[taskDataRequest]()},
		{Name: "tasks.events", Mode: ModeDuplex, Duplex: r.handleTaskEvents, Decode: JSONRequestDecoder[struct{}]()},
		{Name: ResumeRoute, Mode: ModeCall, Call: r.handleJobResume, Decode: JSONRequestDecoder[jobResumeRequest]()},
	}
	for _, route := range routes {
		router.registerTaskServiceRoute(r, route)
	}
}

func (r *TaskService) handleTaskGet(ctx context.Context, req Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, ok := req.DecodedValue.(taskIDRequest)
	if !ok || payload.TaskID == "" {
		return nil, NewError("missing task id", 400)
	}
	task, ok := r.GetForOwner(payload.TaskID, req.Owner)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ok {
		if remote := r.remoteFor(payload.TaskID); remote != nil {
			return remote.Get(ctx, payload.TaskID)
		}
		return nil, NewError(fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	return task.Snapshot(), nil
}

// handleJobResume serves jobs.resume to root only: resumable jobs are
// privileged, so only the root job host has them.
func (r *TaskService) handleJobResume(ctx context.Context, req Request) (any, error) {
	if req.Session == nil || req.Session.User.UID != 0 {
		return nil, fmt.Errorf("%w: %s is served only to root", ErrForbidden, ResumeRoute)
	}
	payload, _ := req.DecodedValue.(jobResumeRequest)
	if err := jobs.ValidateID(payload.ID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgs, err)
	}
	r.mu.RLock()
	resume := r.resumer
	r.mu.RUnlock()
	if resume == nil {
		return nil, NewError("no resumer", 503)
	}
	return resume(ctx, payload.ID)
}

func (r *TaskService) handleTaskList(ctx context.Context, req Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, ok := req.DecodedValue.(taskListRequest)
	if !ok {
		return nil, NewError("invalid tasks list request", 400)
	}
	var snapshots []TaskSnapshot
	if payload.Status == "active" {
		snapshots = r.ListActiveForOwner(req.Owner)
	} else {
		snapshots = r.ListForOwner(req.Owner)
	}
	if remote := r.getRemote(); remote != nil {
		remoteSnapshots, err := remote.List(ctx)
		if err != nil {
			slog.Warn("job host tasks unavailable for tasks list", "error", err)
		}
		for _, snapshot := range remoteSnapshots {
			if payload.Status != "active" || !snapshot.State.Terminal() {
				snapshots = append(snapshots, snapshot)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (r *TaskService) handleTaskCancel(ctx context.Context, req Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, ok := req.DecodedValue.(taskIDRequest)
	if !ok || payload.TaskID == "" {
		return nil, NewError("missing task id", 400)
	}
	task, ok := r.GetForOwner(payload.TaskID, req.Owner)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ok {
		if remote := r.remoteFor(payload.TaskID); remote != nil {
			return remote.Cancel(ctx, payload.TaskID)
		}
		return nil, NewError(fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	task.Cancel()
	return task.Snapshot(), nil
}

func (r *TaskService) handleTaskWatch(ctx context.Context, stream net.Conn, req Request) error {
	payload, ok := req.DecodedValue.(taskIDRequest)
	if !ok || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.GetForOwner(payload.TaskID, req.Owner)
	if !ok {
		if remote := r.remoteFor(payload.TaskID); remote != nil {
			return remote.Watch(ctx, stream, payload.TaskID)
		}
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	return WatchTaskStream(stream, task)
}

func (r *TaskService) handleTaskData(ctx context.Context, stream net.Conn, req Request) error {
	payload, ok := req.DecodedValue.(taskDataRequest)
	if !ok || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.GetForOwner(payload.TaskID, req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	var offset *string
	if payload.Offset != "" {
		offset = &payload.Offset
	}
	return r.AttachData(ctx, task, stream, TaskDataAttachRequest{Offset: offset})
}

// lockedConn serialises the frames two writers put on one stream: the relay
// helpers write each frame with a single Write.
type lockedConn struct {
	net.Conn
	mu sync.Mutex
}

func (c *lockedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.Write(p)
}

func (r *TaskService) handleTaskEvents(ctx context.Context, conn net.Conn, req Request) error {
	events, unsubscribe := r.Subscribe(128)
	defer unsubscribe()

	stream := &lockedConn{Conn: conn}
	done := make(chan struct{})
	go monitorDetach(conn, done)

	if !r.writeInitialTaskSnapshots(stream, req.Owner) {
		return nil
	}
	if remote := r.getRemote(); remote != nil {
		remoteCtx, cancel := context.WithCancel(ctx)
		var merged sync.WaitGroup
		defer func() {
			cancel()
			interruptTaskStreamWrite(stream)
			merged.Wait()
		}()
		merged.Go(func() {
			// The remote follows host reconnects itself; it ends with the
			// client stream, and a host that goes away never ends the local share.
			if err := remote.Events(remoteCtx, stream); err != nil {
				slog.Debug("job host task events ended", "error", err)
			}
		})
	}

	const interval = time.Second
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	pending := make(map[string]TaskEvent)
	lastSent := make(map[string]time.Time)

	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			if !flushPendingTaskEvents(stream, pending, lastSent, interval, time.Now()) {
				return nil
			}
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if !r.writeSubscribedTaskEvent(stream, event, req.Owner, pending, lastSent, interval, time.Now()) {
				return nil
			}
		}
	}
}

func (r *TaskService) writeInitialTaskSnapshots(stream net.Conn, owner TaskOwner) bool {
	for _, snapshot := range r.ListActiveForOwner(owner) {
		if !writeTaskEvent(stream, TaskEvent{Type: TaskEventSnapshot, Task: snapshot}) {
			return false
		}
	}
	return true
}

func flushPendingTaskEvents(stream net.Conn, pending map[string]TaskEvent, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	for id, event := range pending {
		if sentAt := lastSent[id]; !sentAt.IsZero() && now.Sub(sentAt) < interval {
			continue
		}
		if !writeTaskEvent(stream, event) {
			return false
		}
		lastSent[id] = now
		delete(pending, id)
	}
	return true
}

func (r *TaskService) writeSubscribedTaskEvent(stream net.Conn, event TaskEvent, owner TaskOwner, pending map[string]TaskEvent, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	task, ok := r.Get(event.Task.ID)
	if !ok || !task.matchesOwner(owner) {
		return true
	}
	switch event.Type {
	case TaskEventProgress:
		return writeThrottledTaskProgress(stream, event, pending, lastSent, interval, now)
	case TaskEventResult, TaskEventError, TaskEventCanceled:
		delete(pending, event.Task.ID)
		return writeTrackedTaskEvent(stream, event, lastSent, now)
	default:
		return writeTrackedTaskEvent(stream, event, lastSent, now)
	}
}

func writeThrottledTaskProgress(stream net.Conn, event TaskEvent, pending map[string]TaskEvent, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	if sentAt := lastSent[event.Task.ID]; !sentAt.IsZero() && now.Sub(sentAt) < interval {
		pending[event.Task.ID] = event
		return true
	}
	return writeTrackedTaskEvent(stream, event, lastSent, now)
}

func writeTrackedTaskEvent(stream net.Conn, event TaskEvent, lastSent map[string]time.Time, now time.Time) bool {
	if !writeTaskEvent(stream, event) {
		return false
	}
	lastSent[event.Task.ID] = now
	return true
}

// WatchTaskStream streams task's progress and then its result or error until
// the task ends or the client detaches (close) or cancels it (abort).
func WatchTaskStream(stream net.Conn, task *Task) error {
	return watchTaskStream(stream, task, false)
}

// WatchTaskStreamWithSnapshot writes one task.snapshot event first, then
// behaves as WatchTaskStream.
func WatchTaskStreamWithSnapshot(stream net.Conn, task *Task) error {
	return watchTaskStream(stream, task, true)
}

func watchTaskStream(stream net.Conn, task *Task, withSnapshot bool) error {
	detachCh := make(chan struct{})
	go monitorTaskClient(stream, task, detachCh)

	// The general-log backlog is deliberately sized to fit this full replay
	// window. Matching the live channel prevents a subscriber that watches
	// before backlog emission from dropping a burst that would otherwise fit
	// when it watches afterwards.
	events, replay, lagged, unsubscribe := task.subscribeWithReplayStatus(DefaultTaskProgressReplayLimit)
	defer unsubscribe()

	snapshot := task.Snapshot()
	if withSnapshot && !writeTaskEvent(stream, TaskEvent{Type: TaskEventSnapshot, Task: snapshot}) {
		return nil
	}
	if !writeWatchReplay(stream, replay, lagged) {
		return nil
	}
	if writeTerminalTaskSnapshot(stream, snapshot) {
		return nil
	}
	return streamWatchedTaskEvents(stream, events, detachCh, lagged)
}

func writeWatchReplay(stream net.Conn, replay []TaskEvent, lagged <-chan struct{}) bool {
	for _, event := range replay {
		if !writeWatchTaskEvent(stream, event) || stopWatchForLag(stream, lagged) {
			return false
		}
	}
	return true
}

func streamWatchedTaskEvents(
	stream net.Conn,
	events <-chan TaskEvent,
	detachCh <-chan struct{},
	lagged <-chan struct{},
) error {
	for {
		if stopWatchForLag(stream, lagged) {
			return nil
		}
		select {
		case <-detachCh:
			return nil
		case <-lagged:
			writeWatchLagError(stream)
			return nil
		case event, ok := <-events:
			if !ok || !forwardWatchedTaskEvent(stream, event, lagged) {
				return nil
			}
		}
	}
}

func forwardWatchedTaskEvent(stream net.Conn, event TaskEvent, lagged <-chan struct{}) bool {
	if stopWatchForLag(stream, lagged) || !writeWatchTaskEvent(stream, event) {
		return false
	}
	return event.Type != TaskEventResult && event.Type != TaskEventError && event.Type != TaskEventCanceled
}

func stopWatchForLag(stream net.Conn, lagged <-chan struct{}) bool {
	if !watchStreamLagged(lagged) {
		return false
	}
	writeWatchLagError(stream)
	return true
}

func watchStreamLagged(lagged <-chan struct{}) bool {
	select {
	case <-lagged:
		return true
	default:
		return false
	}
}

func writeWatchLagError(stream net.Conn) {
	_ = relay.WriteResultErrorAndClose(
		stream,
		0,
		"task stream fell behind; reconnect to resume",
		503,
	)
}

func interruptTaskStreamWrite(stream net.Conn) {
	_ = stream.SetWriteDeadline(time.Now())
}

func monitorTaskClient(stream net.Conn, task *Task, detachCh chan<- struct{}) {
	defer close(detachCh)
	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			interruptTaskStreamWrite(stream)
			return
		}
		switch frame.Opcode {
		case relay.OpStreamAbort:
			task.Cancel()
			interruptTaskStreamWrite(stream)
			return
		case relay.OpStreamClose:
			interruptTaskStreamWrite(stream)
			return
		}
	}
}

func monitorDetach(stream net.Conn, done chan<- struct{}) {
	defer close(done)
	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			interruptTaskStreamWrite(stream)
			return
		}
		if frame.Opcode == relay.OpStreamClose || frame.Opcode == relay.OpStreamAbort {
			interruptTaskStreamWrite(stream)
			return
		}
	}
}

func writeTaskEvent(stream net.Conn, event TaskEvent) bool {
	return relay.WriteProgress(stream, 0, event) == nil
}

func writeWatchTaskEvent(stream net.Conn, event TaskEvent) bool {
	switch event.Type {
	case TaskEventProgress:
		return relay.WriteProgress(stream, 0, event.Progress) == nil
	case TaskEventResult:
		return relay.WriteResultOKAndClose(stream, 0, event.Result) == nil
	case TaskEventError, TaskEventCanceled:
		err := event.Error
		if err == nil {
			err = NewError("task failed", 500)
		}
		return relay.WriteResultErrorAndClose(stream, 0, err.Message, err.Code) == nil
	default:
		return true
	}
}

func writeTerminalTaskSnapshot(stream net.Conn, snapshot TaskSnapshot) bool {
	switch snapshot.State {
	case TaskStateCompleted:
		_ = relay.WriteResultOKAndClose(stream, 0, snapshot.Result)
		return true
	case TaskStateFailed, TaskStateCanceled, TaskStateInterrupted:
		err := snapshot.Error
		if err == nil {
			err = NewError("task failed", 500)
		}
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Message, err.Code)
		return true
	default:
		return false
	}
}
