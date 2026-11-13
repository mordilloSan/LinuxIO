package cmd

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobsclient"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
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
	task, _, err := registry.ClaimForOwnerWithIdentity("test", nil, bridgeipc.TaskOwner{SessionID: sessionID}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
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

	oldConnect := jobsConnect
	jobsConnect = func(context.Context, uint32, bridgeipc.HelloSession, string) (*jobsclient.Client, error) {
		return nil, errors.New("no job host in this test")
	}
	t.Cleanup(func() { jobsConnect = oldConnect })
	relayCtx, relayCancel := context.WithCancel(context.Background())
	hostRelay := newJobsRelay(relayCtx, &session.Session{SessionID: sessionID})

	shutdownBridge(conn, registry, hostRelay, sessionID, func() { sessionCanceled = true; relayCancel() }, done)

	if !conn.canceledOnClose {
		t.Fatal("transport closed before session context was canceled")
	}
	if !conn.taskDoneOnClose {
		t.Fatal("transport closed before session jobs were canceled")
	}
}
