package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func newYamuxPair(t *testing.T) (*relay.YamuxSession, *relay.YamuxSession) {
	t.Helper()
	left, right := net.Pipe()
	client, err := relay.NewYamuxClient(left)
	if err != nil {
		t.Fatalf("NewYamuxClient: %v", err)
	}
	server, err := relay.NewYamuxServer(right)
	if err != nil {
		_ = client.Close()
		t.Fatalf("NewYamuxServer: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client, server
}

func TestRegisterYamuxSessionAlreadyClosedDoesNotDeadlock(t *testing.T) {
	yamuxSession, _ := newYamuxPair(t)
	if err := yamuxSession.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sess := &session.Session{SessionID: "already-closed"}
	done := make(chan struct{})
	go func() {
		registerYamuxSession(sess, yamuxSession)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registerYamuxSession blocked while registering close callback")
	}

	yamuxSessions.Lock()
	_, present := yamuxSessions.sessions[sess.SessionID]
	delete(yamuxSessions.sessions, sess.SessionID)
	yamuxSessions.Unlock()
	if present {
		t.Fatal("already-closed yamux session was retained")
	}
}

func TestOldYamuxCloseDoesNotRemoveReplacement(t *testing.T) {
	store := session.NewWithCleanupInterval(0)
	cfg := session.DefaultConfig
	cfg.GCInterval = 0
	manager := session.NewManager(store, cfg)
	t.Cleanup(manager.Close)
	oldSession, err := manager.CreateSession("replacement", session.User{Username: "test"}, false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	oldYamux, _ := newYamuxPair(t)
	newYamux, _ := newYamuxPair(t)
	registerYamuxSession(oldSession, oldYamux)
	newSession := &session.Session{SessionID: oldSession.SessionID}
	registerYamuxSession(newSession, newYamux)

	yamuxSessions.RLock()
	current := yamuxSessions.sessions[newSession.SessionID]
	yamuxSessions.RUnlock()
	if current != newYamux {
		t.Fatal("old session close removed replacement mapping")
	}
	if _, err := manager.GetSession(oldSession.SessionID); err != nil {
		t.Fatalf("old session close terminated owning session: %v", err)
	}

	CloseYamuxSession(newSession.SessionID)
}

func TestFetchSessionCapabilitiesHonorsParentDeadlineAfterOpen(t *testing.T) {
	client, server := newYamuxPair(t)
	const sessionID = "capabilities-timeout"
	done := make(chan struct{})
	yamuxSessions.Lock()
	yamuxSessions.sessions[sessionID] = client
	yamuxSessions.Unlock()
	t.Cleanup(func() {
		close(done)
		yamuxSessions.Lock()
		if current := yamuxSessions.sessions[sessionID]; current == client {
			delete(yamuxSessions.sessions, sessionID)
		}
		yamuxSessions.Unlock()
	})

	accepted := make(chan struct{})
	go func() {
		stream, err := server.Accept()
		if err == nil {
			defer stream.Close()
			close(accepted)
			// Keep the stream open without responding.
			<-done
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := make(chan error, 1)
	go func() {
		_, err := fetchSessionCapabilities(ctx, sessionID)
		result <- err
	}()

	select {
	case <-accepted:
	case err := <-result:
		t.Fatalf("fetchSessionCapabilities returned before peer accepted stream: %v", err)
	case <-time.After(time.Second):
		t.Fatal("peer did not accept capabilities stream")
	}

	var err error
	select {
	case err = <-result:
	case <-time.After(time.Second):
		t.Fatal("fetchSessionCapabilities did not honor stream deadline")
	}
	if err == nil {
		t.Fatal("fetchSessionCapabilities unexpectedly succeeded")
	}
	var timeoutErr net.Error
	if !errors.As(err, &timeoutErr) || !timeoutErr.Timeout() {
		t.Fatalf("fetchSessionCapabilities error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("fetchSessionCapabilities took too long: %v", elapsed)
	}
}
