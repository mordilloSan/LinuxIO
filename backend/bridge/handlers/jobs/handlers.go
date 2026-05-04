package jobs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	StreamTypeJobsAttach = "jobs-attach"
	StreamTypeJobsData   = "jobs-data"
	StreamTypeJobsEvents = "jobs-events"
)

func RegisterHandlers() {
	ipc.RegisterFunc("jobs", "start", handleStart)
	ipc.RegisterFunc("jobs", "recover", handleRecover)
	ipc.RegisterFunc("jobs", "list", handleList)
	ipc.RegisterFunc("jobs", "get", handleGet)
	ipc.RegisterFunc("jobs", "cancel", handleCancel)
}

func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeJobsAttach] = HandleAttachStream
	handlers[StreamTypeJobsData] = HandleDataStream
	handlers[StreamTypeJobsEvents] = HandleEventsStream
}

func handleStart(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job type")
	}
	job, err := bridgejobs.StartForOwner(args[0], args[1:], ownerFromContext(ctx))
	if err != nil {
		return err
	}
	return emit.Result(job.Snapshot())
}

func handleRecover(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job type")
	}
	job, err := bridgejobs.RecoverForOwner(args[0], ownerFromContext(ctx))
	if err != nil {
		var jobErr *bridgejobs.Error
		if errors.As(err, &jobErr) && jobErr.Code == 404 {
			return emit.Result((*bridgejobs.Snapshot)(nil))
		}
		return err
	}
	return emit.Result(job.Snapshot())
}

func handleList(ctx context.Context, args []string, emit ipc.Events) error {
	owner := ownerFromContext(ctx)
	if len(args) > 0 && args[0] == "active" {
		return emit.Result(bridgejobs.ListActiveForOwner(owner))
	}
	return emit.Result(bridgejobs.ListForOwner(owner))
}

func handleGet(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job id")
	}
	job, ok := bridgejobs.GetForOwner(args[0], ownerFromContext(ctx))
	if !ok {
		return fmt.Errorf("job not found: %s", args[0])
	}
	return emit.Result(job.Snapshot())
}

func handleCancel(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job id")
	}
	job, ok := bridgejobs.GetForOwner(args[0], ownerFromContext(ctx))
	if !ok {
		return fmt.Errorf("job not found: %s", args[0])
	}
	job.Cancel()
	return emit.Result(job.Snapshot())
}

func HandleAttachStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 1 {
		return ipc.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := bridgejobs.GetForOwner(args[0], ownerFromSession(sess))
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", args[0]), 404)
	}
	return AttachStream(stream, job)
}

func HandleDataStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 1 {
		return ipc.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := bridgejobs.GetForOwner(args[0], ownerFromSession(sess))
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", args[0]), 404)
	}
	if err := bridgejobs.AttachData(context.Background(), job, stream, args[1:]); err != nil {
		return err
	}
	return nil
}

func HandleEventsStream(sess *session.Session, stream net.Conn, args []string) error {
	owner := ownerFromSession(sess)
	events, unsubscribe := bridgejobs.Subscribe(128)
	defer unsubscribe()

	done := make(chan struct{})
	go monitorDetach(stream, done)

	if !writeInitialJobSnapshots(stream, owner) {
		return nil
	}

	interval := notificationInterval(sess)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	pending := make(map[string]bridgejobs.Event)
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
			if !writeSubscribedJobEvent(stream, event, owner, pending, lastSent, interval, time.Now()) {
				return nil
			}
		}
	}
}

func writeInitialJobSnapshots(stream net.Conn, owner bridgejobs.Owner) bool {
	for _, snapshot := range bridgejobs.ListActiveForOwner(owner) {
		if !writeJobEvent(stream, bridgejobs.Event{Type: bridgejobs.EventSnapshot, Job: snapshot}) {
			return false
		}
	}
	return true
}

func flushPendingJobEvents(stream net.Conn, pending map[string]bridgejobs.Event, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
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

func writeSubscribedJobEvent(stream net.Conn, event bridgejobs.Event, owner bridgejobs.Owner, pending map[string]bridgejobs.Event, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	if !event.Job.Owner.Matches(owner) {
		return true
	}
	switch event.Type {
	case bridgejobs.EventProgress:
		return writeThrottledProgressEvent(stream, event, pending, lastSent, interval, now)
	case bridgejobs.EventResult, bridgejobs.EventError, bridgejobs.EventCanceled:
		delete(pending, event.Job.ID)
		return writeTrackedJobEvent(stream, event, lastSent, now)
	default:
		return writeTrackedJobEvent(stream, event, lastSent, now)
	}
}

func writeThrottledProgressEvent(stream net.Conn, event bridgejobs.Event, pending map[string]bridgejobs.Event, lastSent map[string]time.Time, interval time.Duration, now time.Time) bool {
	if sentAt := lastSent[event.Job.ID]; !sentAt.IsZero() && now.Sub(sentAt) < interval {
		pending[event.Job.ID] = event
		return true
	}
	return writeTrackedJobEvent(stream, event, lastSent, now)
}

func writeTrackedJobEvent(stream net.Conn, event bridgejobs.Event, lastSent map[string]time.Time, now time.Time) bool {
	if !writeJobEvent(stream, event) {
		return false
	}
	lastSent[event.Job.ID] = now
	return true
}

func AttachStream(stream net.Conn, job *bridgejobs.Job) error {
	abortCh := make(chan struct{})
	detachCh := make(chan struct{})
	go monitorClient(stream, abortCh, detachCh)

	events, unsubscribe := job.Subscribe(16)
	defer unsubscribe()

	snapshot := job.Snapshot()
	if snapshot.Progress != nil {
		if err := ipc.WriteProgress(stream, 0, snapshot.Progress); err != nil {
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
			if !writeEvent(stream, event) {
				return nil
			}
			if event.Type == bridgejobs.EventResult || event.Type == bridgejobs.EventError || event.Type == bridgejobs.EventCanceled {
				return nil
			}
		}
	}
}

func monitorClient(stream net.Conn, abortCh, detachCh chan<- struct{}) {
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			close(detachCh)
			return
		}
		if frame.Opcode == ipc.OpStreamAbort {
			close(abortCh)
			return
		}
	}
}

func monitorDetach(stream net.Conn, done chan<- struct{}) {
	defer close(done)
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			return
		}
		if frame.Opcode == ipc.OpStreamClose || frame.Opcode == ipc.OpStreamAbort {
			return
		}
	}
}

func writeJobEvent(stream net.Conn, event bridgejobs.Event) bool {
	return ipc.WriteProgress(stream, 0, event) == nil
}

func ownerFromContext(ctx context.Context) bridgejobs.Owner {
	sess, ok := session.FromContext(ctx)
	if !ok {
		return bridgejobs.Owner{}
	}
	return ownerFromSession(sess)
}

func ownerFromSession(sess *session.Session) bridgejobs.Owner {
	if sess == nil {
		return bridgejobs.Owner{}
	}
	return bridgejobs.Owner{
		SessionID: sess.SessionID,
		Username:  sess.User.Username,
		UID:       sess.User.UID,
	}
}

func notificationInterval(sess *session.Session) time.Duration {
	if sess == nil {
		return time.Second
	}
	cfg, _, err := config.Load(sess.User.Username)
	if err != nil || cfg == nil {
		return time.Second
	}
	ms := cfg.Jobs.NotificationMinIntervalMs
	if ms <= 0 {
		return time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

func writeEvent(stream net.Conn, event bridgejobs.Event) bool {
	switch event.Type {
	case bridgejobs.EventProgress:
		return ipc.WriteProgress(stream, 0, event.Progress) == nil
	case bridgejobs.EventResult:
		return ipc.WriteResultOKAndClose(stream, 0, event.Result) == nil
	case bridgejobs.EventError, bridgejobs.EventCanceled:
		err := event.Error
		if err == nil {
			err = bridgejobs.NewError("job failed", 500)
		}
		return ipc.WriteResultErrorAndClose(stream, 0, err.Message, err.Code) == nil
	default:
		return true
	}
}

func writeTerminalSnapshot(stream net.Conn, snapshot bridgejobs.Snapshot) bool {
	switch snapshot.State {
	case bridgejobs.StateCompleted:
		_ = ipc.WriteResultOKAndClose(stream, 0, snapshot.Result)
		return true
	case bridgejobs.StateFailed, bridgejobs.StateCanceled:
		err := snapshot.Error
		if err == nil {
			err = bridgejobs.NewError("job failed", 500)
		}
		_ = ipc.WriteResultErrorAndClose(stream, 0, err.Message, err.Code)
		return true
	default:
		return false
	}
}
