package bridge

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func (r *Router) dispatchJobPrimitive(ctx context.Context, stream net.Conn, req Request) error {
	switch req.Route {
	case "jobs.get":
		return r.handleJobGet(stream, req)
	case "jobs.list":
		return r.handleJobList(stream, req)
	case "jobs.cancel":
		return r.handleJobCancel(stream, req)
	case "jobs.attach":
		return r.handleJobAttach(stream, req)
	case "jobs.data":
		return r.handleJobData(ctx, stream, req)
	case "jobs.events":
		return r.handleJobEvents(stream, req)
	default:
		err := fmt.Errorf("%w: %s", ErrRouteNotFound, req.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
		return err
	}
}

func (r *Router) handleJobGet(stream net.Conn, req Request) error {
	if len(req.Args) < 1 {
		return relay.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := r.registry.GetForOwner(req.Args[0], req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", req.Args[0]), 404)
	}
	return relay.WriteResultOKAndClose(stream, 0, job.Snapshot())
}

func (r *Router) handleJobList(stream net.Conn, req Request) error {
	if len(req.Args) > 0 && req.Args[0] == "active" {
		return relay.WriteResultOKAndClose(stream, 0, r.registry.ListActiveForOwner(req.Owner))
	}
	return relay.WriteResultOKAndClose(stream, 0, r.registry.ListForOwner(req.Owner))
}

func (r *Router) handleJobCancel(stream net.Conn, req Request) error {
	if len(req.Args) < 1 {
		return relay.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := r.registry.GetForOwner(req.Args[0], req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", req.Args[0]), 404)
	}
	job.Cancel()
	return relay.WriteResultOKAndClose(stream, 0, job.Snapshot())
}

func (r *Router) handleJobAttach(stream net.Conn, req Request) error {
	if len(req.Args) < 1 {
		return relay.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := r.registry.GetForOwner(req.Args[0], req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", req.Args[0]), 404)
	}
	return AttachJobStream(stream, job)
}

func (r *Router) handleJobData(ctx context.Context, stream net.Conn, req Request) error {
	if len(req.Args) < 1 {
		return relay.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := r.registry.GetForOwner(req.Args[0], req.Owner)
	if !ok {
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", req.Args[0]), 404)
	}
	return r.registry.AttachData(ctx, job, stream, req.Args[1:])
}

func (r *Router) handleJobEvents(stream net.Conn, req Request) error {
	events, unsubscribe := r.registry.Subscribe(128)
	defer unsubscribe()

	done := make(chan struct{})
	go monitorDetach(stream, done)

	if !r.writeInitialJobSnapshots(stream, req.Owner) {
		return nil
	}

	const interval = time.Second
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	pending := make(map[string]Event)
	lastSent := make(map[string]time.Time)

	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			if !flushPendingJobEvents(stream, pending, lastSent, interval, time.Now()) {
				return nil
			}
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if !writeSubscribedJobEvent(stream, event, req.Owner, pending, lastSent, interval, time.Now()) {
				return nil
			}
		}
	}
}

func (r *Router) writeInitialJobSnapshots(stream net.Conn, owner Owner) bool {
	for _, snapshot := range r.registry.ListActiveForOwner(owner) {
		if !writeJobEvent(stream, Event{Type: EventSnapshot, Job: snapshot}) {
			return false
		}
	}
	return true
}

func flushPendingJobEvents(stream net.Conn, pending map[string]Event, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	for id, event := range pending {
		if sentAt := lastSent[id]; !sentAt.IsZero() && now.Sub(sentAt) < interval {
			continue
		}
		if !writeJobEvent(stream, event) {
			return false
		}
		lastSent[id] = now
		delete(pending, id)
	}
	return true
}

func writeSubscribedJobEvent(stream net.Conn, event Event, owner Owner, pending map[string]Event, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	if !event.Job.Owner.Matches(owner) {
		return true
	}
	switch event.Type {
	case EventProgress:
		return writeThrottledProgressEvent(stream, event, pending, lastSent, interval, now)
	case EventResult, EventError, EventCanceled:
		delete(pending, event.Job.ID)
		return writeTrackedJobEvent(stream, event, lastSent, now)
	default:
		return writeTrackedJobEvent(stream, event, lastSent, now)
	}
}

func writeThrottledProgressEvent(stream net.Conn, event Event, pending map[string]Event, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	if sentAt := lastSent[event.Job.ID]; !sentAt.IsZero() && now.Sub(sentAt) < interval {
		pending[event.Job.ID] = event
		return true
	}
	return writeTrackedJobEvent(stream, event, lastSent, now)
}

func writeTrackedJobEvent(stream net.Conn, event Event, lastSent map[string]time.Time, now time.Time) bool {
	if !writeJobEvent(stream, event) {
		return false
	}
	lastSent[event.Job.ID] = now
	return true
}

func AttachJobStream(stream net.Conn, job *Job) error {
	abortCh := make(chan struct{})
	detachCh := make(chan struct{})
	go monitorClient(stream, abortCh, detachCh)

	events, replay, unsubscribe := job.SubscribeWithReplay(256)
	defer unsubscribe()

	snapshot := job.Snapshot()
	for _, event := range replay {
		if !writeAttachEvent(stream, event) {
			return nil
		}
	}
	if writeTerminalSnapshot(stream, snapshot) {
		return nil
	}

	for {
		select {
		case <-abortCh:
			job.Cancel()
			return nil
		case <-detachCh:
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if !writeAttachEvent(stream, event) {
				return nil
			}
			if event.Type == EventResult || event.Type == EventError || event.Type == EventCanceled {
				return nil
			}
		}
	}
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

func writeJobEvent(stream net.Conn, event Event) bool {
	return relay.WriteProgress(stream, 0, event) == nil
}

func writeAttachEvent(stream net.Conn, event Event) bool {
	switch event.Type {
	case EventProgress:
		return relay.WriteProgress(stream, 0, event.Progress) == nil
	case EventResult:
		return relay.WriteResultOKAndClose(stream, 0, event.Result) == nil
	case EventError, EventCanceled:
		err := event.Error
		if err == nil {
			err = NewError("job failed", 500)
		}
		return relay.WriteResultErrorAndClose(stream, 0, err.Message, err.Code) == nil
	default:
		return true
	}
}

func writeTerminalSnapshot(stream net.Conn, snapshot Snapshot) bool {
	switch snapshot.State {
	case StateCompleted:
		_ = relay.WriteResultOKAndClose(stream, 0, snapshot.Result)
		return true
	case StateFailed, StateCanceled:
		err := snapshot.Error
		if err == nil {
			err = NewError("job failed", 500)
		}
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Message, err.Code)
		return true
	default:
		return false
	}
}
