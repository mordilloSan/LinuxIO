package ipc

import (
	"net"
	"sync"
	"time"

	"github.com/libp2p/go-yamux/v4"
)

// MemoryManagerFactory creates a new yamux.MemoryManager per session.
// Return nil to use unlimited memory (default behavior).
var MemoryManagerFactory func() yamux.MemoryManager = nil

// YamuxConfig returns the default yamux configuration for LinuxIO
func YamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.AcceptBacklog = 256
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 35 * time.Second
	cfg.ConnectionWriteTimeout = 20 * time.Second
	cfg.MaxStreamWindowSize = 16 * 1024 * 1024 // 16MB per stream (supports 10MB chunks)
	return cfg
}

// YamuxSession wraps a yamux session with connection tracking
type YamuxSession struct {
	*yamux.Session
	conn    net.Conn
	mu      sync.Mutex
	closed  bool
	onClose func()
}

// NewYamuxServer creates a server-side yamux session
func NewYamuxServer(conn net.Conn) (*YamuxSession, error) {
	// Use default memory manager (nil for libp2p yamux)
	session, err := yamux.Server(conn, YamuxConfig(), nil)
	if err != nil {
		return nil, err
	}
	return &YamuxSession{
		Session: session,
		conn:    conn,
	}, nil
}

// NewYamuxClient creates a client-side yamux session
func NewYamuxClient(conn net.Conn) (*YamuxSession, error) {
	// Use default memory manager (nil for libp2p yamux)
	session, err := yamux.Client(conn, YamuxConfig(), nil)
	if err != nil {
		return nil, err
	}
	return &YamuxSession{
		Session: session,
		conn:    conn,
	}, nil
}

// SetOnClose sets a callback to be called when the session closes
func (s *YamuxSession) SetOnClose(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = fn
}

// Close closes the yamux session and underlying connection
func (s *YamuxSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	onClose := s.onClose
	s.mu.Unlock()

	err := s.Session.Close()

	if onClose != nil {
		onClose()
	}
	return err
}

// IsClosed returns true if the session has been closed
func (s *YamuxSession) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.Session.IsClosed()
}
