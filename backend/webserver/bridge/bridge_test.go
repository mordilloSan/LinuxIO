package bridge

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestGetYamuxSessionErrorsDoNotExposeSessionID(t *testing.T) {
	const sessionID = "0123456789abcdef0123456789abcdef"

	if _, err := GetYamuxSession(sessionID); err == nil {
		t.Fatal("GetYamuxSession returned nil error for missing session")
	} else if strings.Contains(err.Error(), sessionID) {
		t.Fatalf("missing session error contains session credential: %q", err)
	}

	yamuxSession, _ := newYamuxPair(t)
	if err := yamuxSession.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	yamuxSessions.Lock()
	yamuxSessions.sessions[sessionID] = yamuxSession
	yamuxSessions.Unlock()
	t.Cleanup(func() {
		yamuxSessions.Lock()
		delete(yamuxSessions.sessions, sessionID)
		yamuxSessions.Unlock()
	})

	if _, err := GetYamuxSession(sessionID); err == nil {
		t.Fatal("GetYamuxSession returned nil error for closed session")
	} else if strings.Contains(err.Error(), sessionID) {
		t.Fatalf("closed session error contains session credential: %q", err)
	}
}

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
