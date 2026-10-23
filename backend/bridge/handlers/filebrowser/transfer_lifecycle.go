package filebrowser

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// transferIdleTimeout bounds how long a transfer task may sit with no client
// progress before it is abandoned. Uploads and archives park in
// waiting_for_client on a stream error (so the client can resume) instead of
// failing; without this backstop a client that never reconnects — tab closed,
// crash, network loss — would hold a limited transfer task slot indefinitely.
const transferIdleTimeout = 5 * time.Minute

// transferAttachWaitTimeout bounds how long an incoming data stream waits for
// its transfer to appear in fileTransferTasks. It only has to cover the gap
// between the start reply and the task goroutine registering itself, so every
// task runner must register before doing any slow work — see runArchiveTask.
const transferAttachWaitTimeout = 2 * time.Second

// fileTransferTasks maps task IDs to the transferAttacher a client data
// stream attaches to.
var fileTransferTasks sync.Map

type transferOutcome struct {
	result any
	err    error
}

// transferAttacher is a registered transfer that can take over a client data stream.
type transferAttacher interface {
	attach(stream net.Conn, req bridgeipc.TaskDataAttachRequest) error
}

// registerTransfer publishes a transfer for data-stream attachment until the
// returned func is called.
func registerTransfer(taskID string, transfer transferAttacher) (unregister func()) {
	fileTransferTasks.Store(taskID, transfer)
	return func() { fileTransferTasks.Delete(taskID) }
}

// transferLifecycle is the attach/wait/fail/cancel/finish state machine shared
// by every stream-backed transfer task (upload, batch upload, archive
// download). Owners embed it and keep their own progress and payload logic.
// Its mu also guards the owner's fields.
type transferLifecycle struct {
	task       *bridgeipc.Task
	done       chan transferOutcome
	activity   chan struct{}
	finishOnce sync.Once

	// cleanup removes the owner's partial output; report sends the owner's
	// progress under a phase name.
	cleanup func()
	report  func(phase string)

	mu       sync.Mutex
	bytes    int64 // stream position; a re-attach must resume exactly here
	attached bool
	active   net.Conn
}

func newTransferLifecycle(task *bridgeipc.Task, cleanup func(), report func(phase string)) *transferLifecycle {
	return &transferLifecycle{
		task:     task,
		done:     make(chan transferOutcome, 1),
		activity: make(chan struct{}, 1),
		cleanup:  cleanup,
		report:   report,
	}
}

// await blocks until the transfer finishes, ctx is cancelled, or the client
// goes idle.
func (l *transferLifecycle) await(ctx context.Context) (any, error) {
	return awaitTransferOutcome(ctx, l.done, l.activity, l.cancel)
}

// attachStream parses the resume offset and claims the single data stream. On
// rejection it has already answered the client and ok is false; err is then
// only the reply's write error.
func (l *transferLifecycle) attachStream(stream net.Conn, req bridgeipc.TaskDataAttachRequest, limit int64) (offset int64, ok bool, err error) {
	offset, err = parseTransferOffset(req)
	if err != nil {
		return 0, false, ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 400)
	}
	if err = l.beginAttach(stream, offset, limit); err != nil {
		return 0, false, ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 409)
	}
	return offset, true, nil
}

// beginAttach claims the data stream at offset. A negative limit means the
// transfer size is unknown.
func (l *transferLifecycle) beginAttach(stream net.Conn, offset, limit int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.attached {
		return fmt.Errorf("transfer already has an attached data stream")
	}
	if offset != l.bytes {
		return fmt.Errorf("offset mismatch: expected %d, got %d", l.bytes, offset)
	}
	if limit >= 0 && offset > limit {
		return fmt.Errorf("offset exceeds transfer size")
	}
	l.attached = true
	l.active = stream
	signalActivity(l.activity)
	return nil
}

func (l *transferLifecycle) endAttach(stream net.Conn) {
	l.mu.Lock()
	if l.active == stream {
		l.attached = false
		l.active = nil
	}
	l.mu.Unlock()
}

// receiveFrames runs the client-to-bridge frame loop. A dropped stream parks the
// transfer for resume; onData handles payload frames and onClose a client
// close; both may end the loop by returning errors or by finishing the task.
func (l *transferLifecycle) receiveFrames(stream net.Conn, onData func(payload []byte) error, onClose func() error) error {
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			l.markWaiting()
			return nil
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) == 0 {
				continue
			}
			if err := onData(frame.Payload); err != nil {
				return err
			}
		case ipc.OpStreamClose:
			return onClose()
		case ipc.OpStreamAbort:
			l.cancel()
			return services.ErrAborted
		default:
			slog.Debug("ignoring file transfer stream opcode", "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		}
	}
}

// sendProgress reports progress on the task and mirrors wire to the client stream.
func (l *transferLifecycle) sendProgress(stream net.Conn, progress bridgeipc.ProgressDetail, wire any) {
	l.task.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, wire))
}

func (l *transferLifecycle) markWaiting() {
	l.report("waiting_for_client")
}

// succeed answers the client with result and finishes the task successfully.
func (l *transferLifecycle) succeed(stream net.Conn, result any) {
	l.report("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	l.finish(result, nil)
}

// fail drops the partial output, answers the client with the error and
// finishes the task as failed. It returns err (or the task error) for the
// attach caller.
func (l *transferLifecycle) fail(stream net.Conn, message string, code int, err error) error {
	l.cleanup()
	taskErr := bridgeipc.NewError(message, code)
	if stream != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, code))
	}
	l.finish(nil, taskErr)
	if err != nil {
		return err
	}
	return taskErr
}

// cancel closes any attached stream, drops the partial output and finishes
// the task as canceled.
func (l *transferLifecycle) cancel() {
	l.mu.Lock()
	active := l.active
	l.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	l.cleanup()
	l.finish(nil, context.Canceled)
}

func (l *transferLifecycle) finish(result any, err error) {
	l.finishOnce.Do(func() {
		l.done <- transferOutcome{result: result, err: err}
	})
}

// signalActivity records forward progress on a transfer without blocking. The
// activity channel is buffered (size 1) and coalesces: a full buffer already
// means "progress happened since the last reset", so a dropped signal is fine.
func signalActivity(activity chan struct{}) {
	if activity == nil {
		return
	}
	select {
	case activity <- struct{}{}:
	default:
	}
}

// awaitTransferOutcome blocks until the transfer finishes, its context is
// cancelled, or the client is idle past transferIdleTimeout. Every activity
// signal resets the idle deadline, so transfers making progress are never
// interrupted; only genuinely abandoned ones are cancelled (freeing the slot).
func awaitTransferOutcome(ctx context.Context, done <-chan transferOutcome, activity <-chan struct{}, cancel func()) (any, error) {
	timer := time.NewTimer(transferIdleTimeout)
	defer timer.Stop()
	for {
		select {
		case outcome := <-done:
			return outcome.result, outcome.err
		case <-ctx.Done():
			cancel()
			return nil, context.Canceled
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(transferIdleTimeout)
		case <-timer.C:
			slog.Warn("transfer abandoned by client; cancelling task", "idle_timeout", transferIdleTimeout)
			cancel()
			return nil, bridgeipc.NewError("transfer abandoned: client idle", 408)
		}
	}
}

func attachFileTransferData(ctx context.Context, task *bridgeipc.Task, stream net.Conn, request any) error {
	req, ok := request.(bridgeipc.TaskDataAttachRequest)
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, "invalid transfer request", 400)
	}
	transfer, ok := waitForFileTransferTask(ctx, task.ID())
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("transfer task not ready: %s", task.ID()), 404)
	}
	return transfer.attach(stream, req)
}

func waitForFileTransferTask(ctx context.Context, taskID string) (transferAttacher, bool) {
	deadline := time.NewTimer(transferAttachWaitTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if transfer, ok := fileTransferTasks.Load(taskID); ok {
			if attacher, isAttacher := transfer.(transferAttacher); isAttacher {
				return attacher, true
			}
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-deadline.C:
			return nil, false
		case <-ticker.C:
		}
	}
}

func parseTransferOffset(req bridgeipc.TaskDataAttachRequest) (int64, error) {
	if req.Offset == nil || *req.Offset == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(*req.Offset, 10, 64)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid transfer offset")
	}
	return offset, nil
}

func logWriteErr(action string, err error) {
	if err != nil {
		slog.Debug("failed to write filebrowser transfer frame", "action", action, "error", err)
	}
}
