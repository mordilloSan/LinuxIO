package cmd

import (
	"net"
	"testing"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type shutdownTestConn struct {
	net.Conn
	sessionCanceled *bool
	job             *bridgeipc.Job
	done            chan struct{}
	canceledOnClose bool
	jobDoneOnClose  bool
}

func (c *shutdownTestConn) Close() error {
	c.canceledOnClose = *c.sessionCanceled
	c.jobDoneOnClose = c.job.IsTerminal()
	close(c.done)
	return c.Conn.Close()
}

func TestShutdownBridgeCancelsBeforeClosingTransport(t *testing.T) {
	registry := bridgeipc.NewRegistry()
	const sessionID = "shutdown-test"
	job, err := registry.CreateForOwner("test", nil, bridgeipc.Owner{SessionID: sessionID})
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
		job:             job,
		done:            done,
	}

	shutdownBridge(conn, registry, sessionID, func() { sessionCanceled = true }, done)

	if !conn.canceledOnClose {
		t.Fatal("transport closed before session context was canceled")
	}
	if !conn.jobDoneOnClose {
		t.Fatal("transport closed before session jobs were canceled")
	}
}
