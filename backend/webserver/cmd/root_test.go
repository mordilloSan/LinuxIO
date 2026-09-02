package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func newIdleWatcherTestManager() *session.Manager {
	cfg := session.DefaultConfig
	cfg.GCInterval = 0
	return session.NewManager(session.NewWithCleanupInterval(0), cfg)
}

func TestHTTPHandlerRefreshesIdleGraceAfterCompletion(t *testing.T) {
	sm := newIdleWatcherTestManager()
	defer sm.Close()
	srv, activity, err := newHTTPServer(ServerConfig{Port: 8080}, sm)
	if err != nil {
		t.Fatalf("newHTTPServer: %v", err)
	}

	activity.mu.Lock()
	activity.lastHit = time.Now().Add(-time.Second)
	activity.mu.Unlock()
	srv.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if activity.idleFor(500 * time.Millisecond) {
		t.Fatal("request completion did not start a fresh idle grace")
	}
}

func TestSocketIdleWatcherWaitsForReleasedSessionGrace(t *testing.T) {
	sm := newIdleWatcherTestManager()
	defer sm.Close()
	sessID, err := sm.NewSessionID()
	if err != nil {
		t.Fatalf("new session ID: %v", err)
	}
	if _, err := sm.CreateSession(sessID, session.User{Username: "test"}, false); err != nil {
		t.Fatalf("create session: %v", err)
	}

	const grace = 40 * time.Millisecond
	srv := &http.Server{}
	shutdown := make(chan time.Time, 1)
	srv.RegisterOnShutdown(func() { shutdown <- time.Now() })
	ctx := t.Context()
	activity := &serverActivity{}
	activity.touch()
	done := startSocketIdleExitWatcher(ctx, srv, sm, activity, grace, 5*time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	if err := sm.DeleteSession(sessID, session.ReasonLogout); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	released := time.Now()
	select {
	case <-shutdown:
		t.Fatal("watcher exited before the released session grace elapsed")
	case <-time.After(grace / 2):
	}
	select {
	case stopped := <-shutdown:
		if stopped.Sub(released) < grace {
			t.Fatalf("shutdown started after %v, want at least %v", stopped.Sub(released), grace)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not shut down an idle server")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not exit after shutdown")
	}
}

func TestSocketIdleWatcherStopsWithContext(t *testing.T) {
	sm := newIdleWatcherTestManager()
	defer sm.Close()
	ctx, cancel := context.WithCancel(context.Background())
	activity := &serverActivity{}
	activity.touch()
	done := startSocketIdleExitWatcher(ctx, &http.Server{}, sm, activity, time.Hour, time.Hour)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop with its server context")
	}
}

func TestNewHTTPServerConnectionTimeouts(t *testing.T) {
	store := session.New()
	sm := session.NewManager(store, session.DefaultConfig)
	defer sm.Close()
	srv, _, err := newHTTPServer(ServerConfig{Port: 8080}, sm)
	if err != nil {
		t.Fatalf("newHTTPServer: %v", err)
	}

	if srv.ReadHeaderTimeout != httpReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, httpReadHeaderTimeout)
	}
	if srv.IdleTimeout != httpIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, httpIdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want zero for streaming endpoints", srv.WriteTimeout)
	}
}
