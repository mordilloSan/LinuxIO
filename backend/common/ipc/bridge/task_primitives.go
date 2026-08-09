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

func (r *Router) dispatchTaskPrimitive(ctx context.Context, stream net.Conn, req Request) error {
	switch req.Route {
	case "tasks.get":
		return r.dispatchTaskCallPrimitive(ctx, stream, req, r.handleTaskGet)
	case "tasks.list":
		return r.dispatchTaskCallPrimitive(ctx, stream, req, r.handleTaskList)
	case "tasks.cancel":
		return r.dispatchTaskCallPrimitive(ctx, stream, req, r.handleTaskCancel)
	case "tasks.watch":
		return r.handleTaskWatch(stream, req)
	case "tasks.data":
		return r.handleTaskData(ctx, stream, req)
	case "tasks.events":
		return r.handleTaskEvents(stream, req)
	default:
		err := fmt.Errorf("%w: %s", ErrRouteNotFound, req.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
		return err
	}
}

type taskCallPrimitiveHandler func(context.Context, net.Conn, Request) error

func (r *Router) dispatchTaskCallPrimitive(
	ctx context.Context,
	stream net.Conn,
	req Request,
	handler taskCallPrimitiveHandler,
) error {
	ctx, cleanup := requestAbortContext(ctx, stream)
	defer cleanup()
	return handler(ctx, stream, req)
}

func (r *Router) handleTaskGet(ctx context.Context, stream net.Conn, req Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := decodeTaskPrimitiveRequest[taskIDRequest](req.RawRequest)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.registry.GetForOwner(payload.TaskID, req.Owner)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	return relay.WriteResultOKAndClose(stream, 0, task.Snapshot())
}

func (r *Router) handleTaskList(ctx context.Context, stream net.Conn, req Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := decodeTaskPrimitiveRequest[taskListRequest](req.RawRequest)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return relay.WriteResultErrorAndClose(stream, 0, "invalid tasks list request", 400)
	}
	var snapshots []TaskSnapshot
	if payload.Status == "active" {
		snapshots = r.registry.ListActiveForOwner(req.Owner)
	} else {
		snapshots = r.registry.ListForOwner(req.Owner)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return relay.WriteResultOKAndClose(stream, 0, snapshots)
}

func (r *Router) handleTaskCancel(ctx context.Context, stream net.Conn, req Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := decodeTaskPrimitiveRequest[taskIDRequest](req.RawRequest)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.registry.GetForOwner(payload.TaskID, req.Owner)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	task.Cancel()
	return relay.WriteResultOKAndClose(stream, 0, task.Snapshot())
}

func (r *Router) handleTaskWatch(stream net.Conn, req Request) error {
	payload, err := decodeTaskPrimitiveRequest[taskIDRequest](req.RawRequest)
	if err != nil || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.registry.GetForOwner(payload.TaskID, req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	return WatchTaskStream(stream, task)
}

func (r *Router) handleTaskData(ctx context.Context, stream net.Conn, req Request) error {
	payload, err := decodeTaskPrimitiveRequest[taskDataRequest](req.RawRequest)
	if err != nil || payload.TaskID == "" {
		return relay.WriteResultErrorAndClose(stream, 0, "missing task id", 400)
	}
	task, ok := r.registry.GetForOwner(payload.TaskID, req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", payload.TaskID), 404)
	}
	var offset *string
	if payload.Offset != "" {
		offset = &payload.Offset
	}
	return r.registry.AttachData(ctx, task, stream, TaskDataAttachRequest{Offset: offset})
}

func (r *Router) handleTaskEvents(stream net.Conn, req Request) error {
	events, unsubscribe := r.registry.Subscribe(128)
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

func (r *Router) writeInitialTaskSnapshots(stream net.Conn, owner TaskOwner) bool {
	for _, snapshot := range r.registry.ListActiveForOwner(owner) {
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
	abortCh := make(chan struct{})
	detachCh := make(chan struct{})
	go monitorClient(stream, abortCh, detachCh)

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
	return streamWatchedTaskEvents(stream, task, events, abortCh, detachCh, lagged)
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
	task *Task,
	events <-chan TaskEvent,
	abortCh <-chan struct{},
	detachCh <-chan struct{},
	lagged <-chan struct{},
) error {
	for {
		if stopWatchForLag(stream, lagged) {
			return nil
		}
		select {
		case <-abortCh:
			task.Cancel()
			return nil
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

func monitorClient(stream net.Conn, abortCh, detachCh chan<- struct{}) {
	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			close(detachCh)
			return
		}
		if frame.Opcode == relay.OpStreamAbort {
			close(abortCh)
			return
		}
	}
}

func monitorDetach(stream net.Conn, done chan<- struct{}) {
	defer close(done)
	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			return
		}
		if frame.Opcode == relay.OpStreamClose || frame.Opcode == relay.OpStreamAbort {
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
