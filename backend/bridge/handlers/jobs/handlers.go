package jobs

import (
	"context"
	"errors"
	"fmt"
	"net"

	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	StreamTypeJobsAttach = "jobs-attach"
	StreamTypeJobsData   = "jobs-data"
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
}

func handleStart(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job type")
	}
	job, err := bridgejobs.Start(args[0], args[1:])
	if err != nil {
		return err
	}
	return emit.Result(job.Snapshot())
}

func handleRecover(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job type")
	}
	job, err := bridgejobs.Recover(args[0])
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
	if len(args) > 0 && args[0] == "active" {
		return emit.Result(bridgejobs.ListActive())
	}
	return emit.Result(bridgejobs.List())
}

func handleGet(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job id")
	}
	job, ok := bridgejobs.Get(args[0])
	if !ok {
		return fmt.Errorf("job not found: %s", args[0])
	}
	return emit.Result(job.Snapshot())
}

func handleCancel(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return fmt.Errorf("missing job id")
	}
	job, ok := bridgejobs.Get(args[0])
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
	job, ok := bridgejobs.Get(args[0])
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", args[0]), 404)
	}
	return AttachStream(stream, job)
}

func HandleDataStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 1 {
		return ipc.WriteResultErrorAndClose(stream, 0, "missing job id", 400)
	}
	job, ok := bridgejobs.Get(args[0])
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("job not found: %s", args[0]), 404)
	}
	if err := bridgejobs.AttachData(context.Background(), job, stream, args[1:]); err != nil {
		return err
	}
	return nil
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
