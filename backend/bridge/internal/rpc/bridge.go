package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

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

	// Create context with session for handlers that need runtime session access.
	ctx := session.WithContext(context.Background(), sess)

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

type loggingEmitter struct {
	stream    net.Conn
	errorSent bool
}

func newLoggingEmitter(stream net.Conn) *loggingEmitter {
	return &loggingEmitter{stream: stream}
}

func (e *loggingEmitter) Data(chunk []byte) error {
	return ipc.WriteRelayFrame(e.stream, &ipc.StreamFrame{
		Opcode:  ipc.OpStreamData,
		Payload: chunk,
	})
}

func (e *loggingEmitter) Progress(progress any) error {
	return ipc.WriteProgress(e.stream, 0, progress)
}

func (e *loggingEmitter) Result(result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return ipc.WriteResultFrame(e.stream, 0, &ipc.ResultFrame{
		Status: "ok",
		Data:   data,
	})
}

func (e *loggingEmitter) Error(err error, code int) error {
	e.errorSent = true
	return ipc.WriteResultError(e.stream, 0, err.Error(), code)
}

func (e *loggingEmitter) Close(string) error {
	// Note: We could extend the protocol to include a reason in the close frame
	// For now, just send the close opcode
	return ipc.WriteStreamClose(e.stream, 0)
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
