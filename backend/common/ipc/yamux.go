package ipc

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/libp2p/go-yamux/v4"
)

const (
	// DefaultYamuxSessionMemoryLimit caps yamux-managed memory per session.
	// 64 MiB allows several active streams without leaving the session unbounded.
	DefaultYamuxSessionMemoryLimit = 64 * 1024 * 1024
)

// MemoryManagerFactory creates a new yamux.MemoryManager per session.
// Set it to nil to disable the budget and keep libp2p yamux unlimited.
var MemoryManagerFactory func() yamux.MemoryManager = func() yamux.MemoryManager {
	return NewFixedBudgetMemoryManager(DefaultYamuxSessionMemoryLimit)
}

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
	once    sync.Once
}

// NewYamuxServer creates a server-side yamux session
func NewYamuxServer(conn net.Conn) (*YamuxSession, error) {
	session, err := yamux.Server(conn, YamuxConfig(), yamuxMemoryManagerFactory())
	if err != nil {
		return nil, err
	}
	return newYamuxSession(session, conn), nil
}

// NewYamuxClient creates a client-side yamux session
func NewYamuxClient(conn net.Conn) (*YamuxSession, error) {
	session, err := yamux.Client(conn, YamuxConfig(), yamuxMemoryManagerFactory())
	if err != nil {
		return nil, err
	}
	return newYamuxSession(session, conn), nil
}

func newYamuxSession(session *yamux.Session, conn net.Conn) *YamuxSession {
	ys := &YamuxSession{
		Session: session,
		conn:    conn,
	}
	go ys.watchClose()
	return ys
}

func yamuxMemoryManagerFactory() func() (yamux.MemoryManager, error) {
	if MemoryManagerFactory == nil {
		return nil
	}
	return func() (yamux.MemoryManager, error) {
		return MemoryManagerFactory(), nil
	}
}

// NewFixedBudgetMemoryManager enforces a hard upper bound on yamux-managed
// memory for a single session. A non-positive limit disables the budget.
func NewFixedBudgetMemoryManager(limit int) yamux.MemoryManager {
	return &fixedBudgetMemoryManager{limit: limit}
}

type fixedBudgetMemoryManager struct {
	mu    sync.Mutex
	used  int
	limit int
	done  bool
}

func (m *fixedBudgetMemoryManager) ReserveMemory(size int, _ uint8) error {
	if size <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.done {
		return fmt.Errorf("yamux memory manager closed")
	}
	if m.limit > 0 && m.used+size > m.limit {
		return fmt.Errorf("yamux session memory budget exceeded: requested=%d used=%d limit=%d", size, m.used, m.limit)
	}
	m.used += size
	return nil
}

func (m *fixedBudgetMemoryManager) ReleaseMemory(size int) {
	if size <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.used -= size
	if m.used < 0 {
		m.used = 0
	}
}

func (m *fixedBudgetMemoryManager) Done() {
	m.mu.Lock()
	m.used = 0
	m.done = true
	m.mu.Unlock()
}

func (s *YamuxSession) watchClose() {
	<-s.CloseChan()
	s.notifyClose()
}

func (s *YamuxSession) notifyClose() {
	s.mu.Lock()
	s.closed = true
	onClose := s.onClose
	s.mu.Unlock()

	if onClose != nil {
		s.once.Do(onClose)
	}
}

// SetOnClose sets a callback to be called when the session closes
func (s *YamuxSession) SetOnClose(fn func()) {
	s.mu.Lock()
	s.onClose = fn
	closed := s.closed || s.Session.IsClosed()
	s.mu.Unlock()

	if closed {
		s.notifyClose()
	}
}

// Close closes the yamux session and underlying connection
func (s *YamuxSession) Close() error {
	err := s.Session.Close()
	s.notifyClose()
	return err
}

// IsClosed returns true if the session has been closed
func (s *YamuxSession) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.Session.IsClosed()
}
