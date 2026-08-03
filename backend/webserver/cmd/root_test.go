package cmd

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

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
