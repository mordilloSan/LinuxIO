package cmd

import (
	"net"
	"testing"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type shutdownTestConn struct {
	net.Conn
	sessionCanceled *bool
	task            *bridgeipc.Task
	done            chan struct{}
	canceledOnClose bool
	taskDoneOnClose bool
}

func (c *shutdownTestConn) Close() error {
	c.canceledOnClose = *c.sessionCanceled
	c.taskDoneOnClose = c.task.IsTerminal()
	close(c.done)
	return c.Conn.Close()
}

func TestShutdownBridgeCancelsBeforeClosingTransport(t *testing.T) {
	registry := bridgeipc.NewTaskService()
	const sessionID = "shutdown-test"
	task, err := registry.CreateForOwner("test", nil, bridgeipc.TaskOwner{SessionID: sessionID})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}

	left, right := net.Pipe()
	t.Cleanup(func() { _ = right.Close() })
	done := make(chan struct{})
	sessionCanceled := false
	conn := &shutdownTestConn{
		Conn:            left,
		sessionCanceled: &sessionCanceled,
		task:            task,
		done:            done,
	}

	shutdownBridge(conn, registry, sessionID, func() { sessionCanceled = true }, done)

	if !conn.canceledOnClose {
		t.Fatal("transport closed before session context was canceled")
	}
	if !conn.taskDoneOnClose {
		t.Fatal("transport closed before session jobs were canceled")
	}
}
