package generic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestHandleBridgeStreamLogsSuccessfulRPC(t *testing.T) {
	logs := captureGenericLogs(t)

	const handlerType = "test_bridge_logging"
	const command = "success"
	ipc.RegisterFunc(handlerType, command, func(ctx context.Context, args []string, emit ipc.Events) error {
		return emit.Result(map[string]bool{"ok": true})
	})
	t.Cleanup(func() {
		ipc.Unregister(handlerType, command)
	})

	conn := &memoryConn{}
	sess := &session.Session{User: session.User{Username: "alice", UID: 1000}}

	if err := HandleBridgeStream(sess, conn, []string{handlerType, command, "alpha"}); err != nil {
		t.Fatalf("HandleBridgeStream returned error: %v", err)
	}

	requireGenericContains(t, logs.String(),
		"bridge rpc succeeded: test_bridge_logging.success",
		"handler=test_bridge_logging",
		"command=success",
		"operation=test_bridge_logging.success",
		"arg_count=1",
		"outcome=success",
		"user=alice",
		"uid=1000",
		"duration=",
	)
}

func TestHandleBridgeStreamLogsFailedRPC(t *testing.T) {
	logs := captureGenericLogs(t)

	const handlerType = "test_bridge_logging"
	const command = "failure"
	handlerErr := errors.New("boom")
	ipc.RegisterFunc(handlerType, command, func(ctx context.Context, args []string, emit ipc.Events) error {
		return handlerErr
	})
	t.Cleanup(func() {
		ipc.Unregister(handlerType, command)
	})

	conn := &memoryConn{}
	sess := &session.Session{User: session.User{Username: "alice", UID: 1000}}

	err := HandleBridgeStream(sess, conn, []string{handlerType, command})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected %v, got %v", handlerErr, err)
	}

	requireGenericContains(t, logs.String(),
		"bridge rpc failed: test_bridge_logging.failure",
		"outcome=failure",
		"error=boom",
		"duration=",
	)
}

func TestHandleBridgeStreamTreatsEmitterErrorFrameAsFailure(t *testing.T) {
	logs := captureGenericLogs(t)

	const handlerType = "test_bridge_logging"
	const command = "error_frame"
	ipc.RegisterFunc(handlerType, command, func(ctx context.Context, args []string, emit ipc.Events) error {
		return emit.Error(errors.New("partial failure"), 409)
	})
	t.Cleanup(func() {
		ipc.Unregister(handlerType, command)
	})

	conn := &memoryConn{}
	sess := &session.Session{User: session.User{Username: "alice", UID: 1000}}

	if err := HandleBridgeStream(sess, conn, []string{handlerType, command}); err != nil {
		t.Fatalf("HandleBridgeStream returned error: %v", err)
	}

	requireGenericContains(t, logs.String(),
		"bridge rpc failed: test_bridge_logging.error_frame",
		"outcome=failure",
		"duration=",
	)
}

func captureGenericLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	return &buf
}

func requireGenericContains(t *testing.T, output string, needles ...string) {
	t.Helper()

	for _, needle := range needles {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected log output to contain %q, got %q", needle, output)
		}
	}
}

type memoryConn struct {
	bytes.Buffer
}

func (c *memoryConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *memoryConn) Close() error                     { return nil }
func (c *memoryConn) LocalAddr() net.Addr              { return memoryAddr("local") }
func (c *memoryConn) RemoteAddr() net.Addr             { return memoryAddr("remote") }
func (c *memoryConn) SetDeadline(time.Time) error      { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error { return nil }

type memoryAddr string

func (a memoryAddr) Network() string { return "memory" }
func (a memoryAddr) String() string  { return string(a) }
