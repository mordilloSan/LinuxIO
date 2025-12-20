package ipc

import (
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// YamuxConfig returns the default yamux configuration for LinuxIO
func YamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.AcceptBacklog = 256
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 30 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.MaxStreamWindowSize = 256 * 1024 // 256KB per stream
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
	session, err := yamux.Server(conn, YamuxConfig())
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
	session, err := yamux.Client(conn, YamuxConfig())
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

// IsYamuxConnection checks if the first byte indicates a yamux connection.
// Yamux protocol starts with version byte 0x00.
func IsYamuxConnection(firstByte byte) bool {
	return firstByte == 0x00
}
