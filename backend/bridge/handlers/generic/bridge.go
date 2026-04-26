package generic

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const sessionContextKey contextKey = "session"

// HandleBridgeStream is the universal entry point for all bridge streams.
//
// Protocol: "bridge\0handlerType\0command\0arg1\0arg2..."
// Example: "bridge\0storage\0get_drive_info"
func HandleBridgeStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 2 {
		err := fmt.Errorf("invalid bridge args: expected [type, command, ...args], got %d arg(s)", len(args))
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 400); writeErr != nil {
			slog.Debug("failed to write bridge error response",
				"component", "bridge",
				"stream_type", "bridge",
				"error", writeErr)
		}
		slog.LogAttrs(context.Background(), slog.LevelDebug, "bridge rpc failed: invalid request",
			slog.String("component", "bridge"),
			slog.String("stream_type", "bridge"),
			slog.Int("arg_count", len(args)),
			slog.String("outcome", "failure"),
			slog.Any("error", err))
		return err
	}

	handlerType := args[0]
	command := args[1]
	handlerArgs := args[2:]
	logMeta := newBridgeRPCLogMeta(sess, handlerType, command, handlerArgs)

	// Look up handler
	h, ok := ipc.Get(handlerType, command)
	if !ok {
		err := fmt.Errorf("handler not found: %s.%s", handlerType, command)
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 404); writeErr != nil {
			slog.Debug("failed to write missing bridge handler response",
				"component", "bridge",
				"stream_type", "bridge",
				"subsystem", handlerType,
				"command", command,
				"error", writeErr)
		}
		logBridgeRPCCompletion(context.Background(), logMeta, nil, err)
		return err
	}

	// Create context with session for handlers that need runtime session access
	ctx := context.WithValue(context.Background(), sessionContextKey, sess)

	// Check if bidirectional
	if bidirHandler, ok := h.(ipc.BidirectionalHandler); ok {
		return handleBidirectional(ctx, stream, bidirHandler, handlerArgs, logMeta)
	}

	// Standard unidirectional handler
	return handleUnidirectional(ctx, stream, h, handlerArgs, logMeta)
}

func handleUnidirectional(ctx context.Context, stream net.Conn, h ipc.Handler, args []string, logMeta bridgeRPCLogMeta) (err error) {
	emit := newLoggingEmitter(stream)
	defer func() {
		logBridgeRPCCompletion(ctx, logMeta, emit, err)
	}()

	// Execute handler
	if err = h.Execute(ctx, args, emit); err != nil {
		// Handler returned error - send error result
		if emitErr := emit.Error(err, 500); emitErr != nil {
			slog.Debug("failed to write bridge handler error frame",
				"component", "bridge",
				"stream_type", "bridge",
				"error", emitErr)
		}
		if closeErr := emit.Close("handler error"); closeErr != nil {
			slog.Debug("failed to write bridge stream close frame",
				"component", "bridge",
				"stream_type", "bridge",
				"error", closeErr)
		}
		return err
	}

	// Success - close stream
	if closeErr := emit.Close(""); closeErr != nil {
		slog.Debug("failed to write bridge stream close frame",
			"component", "bridge",
			"stream_type", "bridge",
			"error", closeErr)
	}
	return nil
}

func handleBidirectional(ctx context.Context, stream net.Conn, h ipc.BidirectionalHandler, args []string, logMeta bridgeRPCLogMeta) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resizeChan := make(chan ipc.ResizeEvent, 1)
	ctx = ipc.WithResizeChannel(ctx, resizeChan)

	emit := newLoggingEmitter(stream)
	defer func() {
		logBridgeRPCCompletion(ctx, logMeta, emit, err)
	}()

	inputChan := make(chan []byte, 16)

	go readClientFrames(ctx, cancel, stream, inputChan, resizeChan)

	// Execute handler with input channel
	if err = h.ExecuteWithInput(ctx, args, emit, inputChan); err != nil {
		if emitErr := emit.Error(err, 500); emitErr != nil {
			slog.Debug("failed to write bridge handler error frame",
				"component", "bridge",
				"stream_type", "bridge",
				"error", emitErr)
		}
		if closeErr := emit.Close("handler error"); closeErr != nil {
			slog.Debug("failed to write bridge stream close frame",
				"component", "bridge",
				"stream_type", "bridge",
				"error", closeErr)
		}
		return err
	}

	if closeErr := emit.Close(""); closeErr != nil {
		slog.Debug("failed to write bridge stream close frame",
			"component", "bridge",
			"stream_type", "bridge",
			"error", closeErr)
	}
	return nil
}

func readClientFrames(ctx context.Context, cancel context.CancelFunc, stream net.Conn, inputChan chan<- []byte, resizeChan chan<- ipc.ResizeEvent) {
	defer close(resizeChan)
	defer close(inputChan)
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			cancel()
			return
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			select {
			case inputChan <- frame.Payload:
			case <-ctx.Done():
				return
			}
		case ipc.OpStreamClose, ipc.OpStreamAbort:
			cancel()
			return
		case ipc.OpStreamResize:
			if len(frame.Payload) < 4 {
				continue
			}
			cols := binary.BigEndian.Uint16(frame.Payload[0:2])
			rows := binary.BigEndian.Uint16(frame.Payload[2:4])
			select {
			case resizeChan <- ipc.ResizeEvent{Cols: cols, Rows: rows}:
			default:
			}
		}
	}
}

// eventEmitter implements ipc.Events
type eventEmitter struct {
	stream net.Conn
}

func newEventEmitter(stream net.Conn) *eventEmitter {
	return &eventEmitter{stream: stream}
}

type loggingEmitter struct {
	inner        *eventEmitter
	dataSent     bool
	progressSent bool
	resultSent   bool
	errorSent    bool
}

func newLoggingEmitter(stream net.Conn) *loggingEmitter {
	return &loggingEmitter{inner: newEventEmitter(stream)}
}

func (e *eventEmitter) Data(chunk []byte) error {
	return ipc.WriteRelayFrame(e.stream, &ipc.StreamFrame{
		Opcode:  ipc.OpStreamData,
		Payload: chunk,
	})
}

func (e *eventEmitter) Progress(progress any) error {
	return ipc.WriteProgress(e.stream, 0, progress)
}

func (e *eventEmitter) Result(result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return ipc.WriteResultFrame(e.stream, 0, &ipc.ResultFrame{
		Status: "ok",
		Data:   data,
	})
}

func (e *eventEmitter) Error(err error, code int) error {
	return ipc.WriteResultError(e.stream, 0, err.Error(), code)
}

func (e *eventEmitter) Close(reason string) error {
	// Note: We could extend the protocol to include a reason in the close frame
	// For now, just send the close opcode
	return ipc.WriteStreamClose(e.stream, 0)
}

func (e *loggingEmitter) Data(chunk []byte) error {
	e.dataSent = true
	return e.inner.Data(chunk)
}

func (e *loggingEmitter) Progress(progress any) error {
	e.progressSent = true
	return e.inner.Progress(progress)
}

func (e *loggingEmitter) Result(result any) error {
	e.resultSent = true
	return e.inner.Result(result)
}

func (e *loggingEmitter) Error(err error, code int) error {
	e.errorSent = true
	return e.inner.Error(err, code)
}

func (e *loggingEmitter) Close(reason string) error {
	return e.inner.Close(reason)
}

type bridgeRPCLogMeta struct {
	startedAt   time.Time
	handlerType string
	command     string
	operation   string
	user        string
	uid         uint32
	argCount    int
}

func newBridgeRPCLogMeta(sess *session.Session, handlerType, command string, args []string) bridgeRPCLogMeta {
	return bridgeRPCLogMeta{
		startedAt:   time.Now(),
		handlerType: handlerType,
		command:     command,
		operation:   handlerType + "." + command,
		user:        sess.User.Username,
		uid:         sess.User.UID,
		argCount:    len(args),
	}
}

func logBridgeRPCCompletion(ctx context.Context, meta bridgeRPCLogMeta, emit *loggingEmitter, err error) {
	outcome := "success"
	verb := "succeeded"
	if err != nil || (emit != nil && emit.errorSent) {
		outcome = "failure"
		verb = "failed"
	}

	attrs := []slog.Attr{
		slog.String("component", "bridge"),
		slog.String("stream_type", "bridge"),
		slog.String("handler", meta.handlerType),
		slog.String("command", meta.command),
		slog.String("operation", meta.operation),
		slog.String("user", meta.user),
		slog.Uint64("uid", uint64(meta.uid)),
		slog.Int("arg_count", meta.argCount),
		slog.String("outcome", outcome),
		slog.Duration("duration", time.Since(meta.startedAt)),
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}

	slog.LogAttrs(ctx, slog.LevelDebug, fmt.Sprintf("bridge rpc %s: %s", verb, meta.operation), attrs...)
}
