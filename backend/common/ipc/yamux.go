package ipc

import (
	"net"
	"sync"
	"time"

	"github.com/libp2p/go-yamux/v5"
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
	conn           net.Conn
	mu             sync.Mutex
	closed         bool
	onClose        func()
	onCloseInvoked bool
}

// NewYamuxServer creates a server-side yamux session
func NewYamuxServer(conn net.Conn) (*YamuxSession, error) {
	// Use default memory manager (nil for libp2p yamux)
	session, err := yamux.Server(conn, YamuxConfig(), nil)
	if err != nil {
		return nil, err
	}
	ys := &YamuxSession{
		Session: session,
		conn:    conn,
	}
	ys.watchClose()
	return ys, nil
}

// NewYamuxClient creates a client-side yamux session
func NewYamuxClient(conn net.Conn) (*YamuxSession, error) {
	// Use default memory manager (nil for libp2p yamux)
	session, err := yamux.Client(conn, YamuxConfig(), nil)
	if err != nil {
		return nil, err
	}
	ys := &YamuxSession{
		Session: session,
		conn:    conn,
	}
	ys.watchClose()
	return ys, nil
}

// SetOnClose sets a callback to be called when the session closes
func (s *YamuxSession) SetOnClose(fn func()) {
	var callNow func()
	s.mu.Lock()
	s.onClose = fn
	if s.closed && !s.onCloseInvoked && fn != nil {
		s.onCloseInvoked = true
		callNow = fn
	}
	s.mu.Unlock()

	if callNow != nil {
		callNow()
	}
}

// Close closes the yamux session and underlying connection
func (s *YamuxSession) Close() error {
	onClose := s.markClosed()
	if onClose == nil && s.Session.IsClosed() {
		return nil
	}

	err := s.Session.Close()
	if onClose == nil {
		onClose = s.closeCallback()
	}
	if onClose != nil {
		onClose()
	}
	return err
}

// IsClosed returns true if the session has been closed
func (s *YamuxSession) IsClosed() bool {
	if s.Session.IsClosed() {
		if onClose := s.markClosed(); onClose != nil {
			onClose()
		}
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *YamuxSession) watchClose() {
	go func() {
		<-s.CloseChan()
		if onClose := s.markClosed(); onClose != nil {
			onClose()
		}
	}()
}

func (s *YamuxSession) markClosed() func() {
	s.mu.Lock()
	s.closed = true
	onClose := s.closeCallbackLocked()
	s.mu.Unlock()
	return onClose
}

func (s *YamuxSession) closeCallback() func() {
	s.mu.Lock()
	onClose := s.closeCallbackLocked()
	s.mu.Unlock()
	return onClose
}

func (s *YamuxSession) closeCallbackLocked() func() {
	if s.onCloseInvoked || s.onClose == nil {
		return nil
	}
	s.onCloseInvoked = true
	return s.onClose
}
