package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
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

// RegisterRoutes installs the Task control Calls and Channels on the router
// that owns this service. The general Router does not dispatch the reserved
// namespace itself.
func (r *TaskService) RegisterRoutes(router *Router) {
	routes := []Route{
		{Name: "tasks.get", Mode: ModeCall, Call: r.handleTaskGet, Decode: taskPrimitiveDecoder[taskIDRequest]()},
		{Name: "tasks.list", Mode: ModeCall, Call: r.handleTaskList, Decode: taskPrimitiveDecoder[taskListRequest]()},
		{Name: "tasks.cancel", Mode: ModeCall, Call: r.handleTaskCancel, Decode: taskPrimitiveDecoder[taskIDRequest]()},
		{Name: "tasks.watch", Mode: ModeDuplex, Duplex: r.handleTaskWatch, Decode: taskPrimitiveDecoder[taskIDRequest]()},
		{Name: "tasks.data", Mode: ModeDuplex, Duplex: r.handleTaskData, Decode: taskPrimitiveDecoder[taskDataRequest]()},
		{Name: "tasks.events", Mode: ModeDuplex, Duplex: r.handleTaskEvents, Decode: taskPrimitiveDecoder[struct{}]()},
	}
	for _, route := range routes {
		router.registerTaskServiceRoute(r, route)
	}
}

func taskPrimitiveDecoder[T any]() RequestDecoder {
	return func(raw json.RawMessage) (any, error) {
		return decodeTaskPrimitiveRequest[T](raw)
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
		return nil, NewError(fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	return task.Snapshot(), nil
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
		return nil, NewError(fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	task.Cancel()
	return task.Snapshot(), nil
}

func (r *TaskService) handleTaskWatch(_ context.Context, stream net.Conn, req Request) error {
	payload, ok := req.DecodedValue.(taskIDRequest)
	if !ok || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.GetForOwner(payload.TaskID, req.Owner)
	if !ok {
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

func (r *TaskService) handleTaskEvents(_ context.Context, stream net.Conn, req Request) error {
	events, unsubscribe := r.Subscribe(128)
	defer unsubscribe()

	done := make(chan struct{})
	go monitorDetach(stream, done)

	if !r.writeInitialTaskSnapshots(stream, req.Owner) {
		return nil
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
			if !writeSubscribedTaskEvent(stream, event, req.Owner, pending, lastSent, interval, time.Now()) {
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

func writeSubscribedTaskEvent(stream net.Conn, event TaskEvent, owner TaskOwner, pending map[string]TaskEvent, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	if !event.Task.Owner.Matches(owner) {
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

func WatchTaskStream(stream net.Conn, task *Task) error {
	detachCh := make(chan struct{})
	go monitorTaskClient(stream, task, detachCh)

	// The general-log backlog is deliberately sized to fit this full replay
	// window. Matching the live channel prevents a subscriber that watches
	// before backlog emission from dropping a burst that would otherwise fit
	// when it watches afterwards.
	events, replay, lagged, unsubscribe := task.subscribeWithReplayStatus(DefaultTaskProgressReplayLimit)
	defer unsubscribe()

	snapshot := task.Snapshot()
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

func decodeTaskPrimitiveRequest[T any](raw json.RawMessage) (T, error) {
	var payload T
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, err
	}
	return payload, nil
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
	case TaskStateFailed, TaskStateCanceled:
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
