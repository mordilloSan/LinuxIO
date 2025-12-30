package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mordilloSan/go_logger/logger"
)

// -----------------------------------------------------------------------------
// Types and defaults
// -----------------------------------------------------------------------------

type DeleteReason string

const (
	ReasonLogout        DeleteReason = "logout"
	ReasonGCIdle        DeleteReason = "gc_idle"
	ReasonGCAbsolute    DeleteReason = "gc_absolute"
	ReasonManual        DeleteReason = "manual"
	ReasonServerQuit    DeleteReason = "server_quit"
	ReasonBridgeFailure DeleteReason = "bridge_failure"
)

type SessionConfig struct {
	IdleTimeout          time.Duration
	AbsoluteTimeout      time.Duration
	RefreshThrottle      time.Duration
	SingleSessionPerUser bool
	GCInterval           time.Duration
	Cookie               CookieConfig
}

type CookieConfig struct {
	Name        string
	Domain      string
	Path        string
	SameSite    http.SameSite
	Secure      bool
	HTTPOnly    bool
	Partitioned bool
}

var DefaultConfig = SessionConfig{
	IdleTimeout:          15 * time.Minute,
	AbsoluteTimeout:      12 * time.Hour,
	RefreshThrottle:      60 * time.Second,
	SingleSessionPerUser: false,
	GCInterval:           15 * time.Second,
	Cookie: CookieConfig{
		Name:        "session_id",
		Path:        "/",
		SameSite:    http.SameSiteStrictMode,
		Secure:      true,
		HTTPOnly:    true,
		Partitioned: false,
	},
}

type User struct {
	Username string `json:"username"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
}

type Timing struct {
	CreatedAt     time.Time `json:"created_at"`
	LastAccess    time.Time `json:"last_access"`
	LastRefresh   time.Time `json:"last_refresh"`
	IdleUntil     time.Time `json:"idle_until"`
	AbsoluteUntil time.Time `json:"absolute_until"`
}

type Session struct {
	SessionID  string `json:"session_id"`
	User       User   `json:"user"`
	Privileged bool   `json:"privileged"`
	Timing     Timing `json:"timing"`

	// Persistent bridge connection (not serialized)
	bridgeConn   net.Conn
	bridgeConnMu sync.Mutex

	// Termination handler (not serialized)
	terminateFunc func(DeleteReason) error
	terminateMu   sync.Mutex
}

// -----------------------------------------------------------------------------
// Store interface
// -----------------------------------------------------------------------------

type Store interface {
	Find(string) ([]byte, bool, error)
	Commit(string, []byte, time.Time) error
	Delete(string) error
	All() (map[string][]byte, error)
}

// -----------------------------------------------------------------------------
// Manager
// -----------------------------------------------------------------------------

type Manager struct {
	cfg SessionConfig
	st  Store

	onDeleteMu sync.RWMutex
	onDelete   []func(*Session, DeleteReason)

	gcStop chan struct{}
}

func NewManager(store Store, cfg SessionConfig) *Manager {
	m := &Manager{st: store, cfg: cfg}
	// Fill defaults
	if m.cfg.IdleTimeout == 0 {
		m.cfg.IdleTimeout = DefaultConfig.IdleTimeout
	}
	if m.cfg.AbsoluteTimeout == 0 {
		m.cfg.AbsoluteTimeout = DefaultConfig.AbsoluteTimeout
	}
	if m.cfg.RefreshThrottle == 0 {
		m.cfg.RefreshThrottle = DefaultConfig.RefreshThrottle
	}
	if m.cfg.GCInterval == 0 {
		m.cfg.GCInterval = DefaultConfig.GCInterval
	}
	if m.cfg.Cookie.Name == "" {
		m.cfg.Cookie = DefaultConfig.Cookie
	}

	logger.Infof("Session manager ready")
	logger.Debugf("Session timings (idle=%v, absolute=%v, refresh=%v, singleUser=%v, gc=%v)",
		m.cfg.IdleTimeout, m.cfg.AbsoluteTimeout, m.cfg.RefreshThrottle, m.cfg.SingleSessionPerUser, m.cfg.GCInterval)

	// Background idle sweeper (absolute expiry handled by store TTL)
	if m.cfg.GCInterval > 0 {
		m.gcStop = make(chan struct{})
		go m.gcLoop()
	}

	return m
}

func (m *Manager) Close() {
	if m.gcStop != nil {
		close(m.gcStop)
	}
	logger.Infof("Session manager stopped")
}

// -----------------------------------------------------------------------------
// Read-only accessors (single source of truth for auth package)
// -----------------------------------------------------------------------------

// CookieName returns the effective cookie name in use.
func (m *Manager) CookieName() string { return m.cfg.Cookie.Name }

// CookieConfig returns a copy of the effective cookie config.
func (m *Manager) CookieConfig() CookieConfig { return m.cfg.Cookie }

// Config returns a copy of the effective session config.
func (m *Manager) Config() SessionConfig { return m.cfg }

// SetBridgeConn stores the persistent bridge connection in the session.
func (s *Session) SetBridgeConn(conn net.Conn) {
	s.bridgeConnMu.Lock()
	defer s.bridgeConnMu.Unlock()
	s.bridgeConn = conn
}

// GetBridgeConn retrieves the persistent bridge connection, if any.
func (s *Session) GetBridgeConn() net.Conn {
	s.bridgeConnMu.Lock()
	defer s.bridgeConnMu.Unlock()
	return s.bridgeConn
}

// CloseBridgeConn closes and clears the persistent bridge connection.
func (s *Session) CloseBridgeConn() error {
	s.bridgeConnMu.Lock()
	defer s.bridgeConnMu.Unlock()
	if s.bridgeConn != nil {
		err := s.bridgeConn.Close()
		s.bridgeConn = nil
		return err
	}
	return nil
}

// Terminate invokes the session's termination handler if set.
// This allows the session to request its own deletion (e.g., on bridge failure).
func (s *Session) Terminate(reason DeleteReason) error {
	s.terminateMu.Lock()
	fn := s.terminateFunc
	s.terminateMu.Unlock()
	if fn != nil {
		return fn(reason)
	}
	return nil
}

// setTerminateFunc sets the termination handler for this session.
func (s *Session) setTerminateFunc(fn func(DeleteReason) error) {
	s.terminateMu.Lock()
	s.terminateFunc = fn
	s.terminateMu.Unlock()
}

// -----------------------------------------------------------------------------
// Hooks
// -----------------------------------------------------------------------------

func (m *Manager) RegisterOnDelete(fn func(*Session, DeleteReason)) {
	m.onDeleteMu.Lock()
	m.onDelete = append(m.onDelete, fn)
	m.onDeleteMu.Unlock()
}

func (m *Manager) broadcastOnDelete(s *Session, r DeleteReason) {
	m.onDeleteMu.RLock()
	subs := append([]func(*Session, DeleteReason){}, m.onDelete...)
	m.onDeleteMu.RUnlock()
	for _, f := range subs {
		go func(ff func(*Session, DeleteReason)) {
			defer func() { _ = recover() }()
			ff(s, r)
		}(f)
	}
}

// -----------------------------------------------------------------------------
// Core helpers
// -----------------------------------------------------------------------------

func randID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func expiredIdle(s *Session, now time.Time) bool     { return now.After(s.Timing.IdleUntil) }
func expiredAbsolute(s *Session, now time.Time) bool { return now.After(s.Timing.AbsoluteUntil) }

func (m *Manager) decode(b []byte) (*Session, error) {
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (m *Manager) encode(s *Session) ([]byte, error) {
	return json.Marshal(s)
}

// -----------------------------------------------------------------------------
// Public API
// -----------------------------------------------------------------------------

func (m *Manager) CreateSession(user User, privileged bool) (*Session, error) {
	id, err := randID(16)
	if err != nil {
		return nil, fmt.Errorf("rand id: %w", err)
	}
	now := time.Now()
	abs := now.Add(m.cfg.AbsoluteTimeout)
	idle := now.Add(m.cfg.IdleTimeout)
	if idle.After(abs) {
		idle = abs
	}

	sess := &Session{
		SessionID:  id,
		User:       user,
		Privileged: privileged,
		Timing: Timing{
			CreatedAt:     now,
			LastAccess:    now,
			LastRefresh:   now,
			IdleUntil:     idle,
			AbsoluteUntil: abs,
		},
	}

	// Enforce single-session-per-user
	if m.cfg.SingleSessionPerUser {
		if mm, singleErr := m.st.All(); singleErr == nil {
			for tok, data := range mm {
				os, decodeErr := m.decode(data)
				if decodeErr != nil {
					continue
				}
				if os.User.Username == user.Username {
					_ = m.st.Delete(tok)
					m.broadcastOnDelete(os, ReasonManual)
				}
			}
		}
	}

	b, err := m.encode(sess)
	if err != nil {
		return nil, err
	}
	if err := m.st.Commit(id, b, abs); err != nil {
		return nil, err
	}

	// Set up termination handler so the session can delete itself
	sess.setTerminateFunc(func(reason DeleteReason) error {
		return m.DeleteSession(sess.SessionID, reason)
	})

	logger.Infof("Created session for user '%s'", user.Username)
	return sess, nil
}

func (m *Manager) GetSession(id string) (*Session, error) {
	b, ok, err := m.st.Find(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("session not found")
	}
	sess, err := m.decode(b)
	if err != nil {
		return nil, err
	}
	// Restore termination handler (not serialized)
	sess.setTerminateFunc(func(reason DeleteReason) error {
		return m.DeleteSession(sess.SessionID, reason)
	})
	return sess, nil
}

func (m *Manager) DeleteSession(id string, r DeleteReason) error {
	b, ok, err := m.st.Find(id)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_ = m.st.Delete(id)
	if s, err := m.decode(b); err == nil {
		logger.Infof("Deleted session for user '%s' (reason=%s)", s.User.Username, r)
		// Close persistent bridge connection
		if closeErr := s.CloseBridgeConn(); closeErr != nil {
			logger.WarnKV("failed to close bridge connection on session delete",
				"user", s.User.Username,
				"error", closeErr)
		}
		m.broadcastOnDelete(s, r)
	}
	return nil
}

func (m *Manager) SetPrivileged(id string, v bool) error {
	s, err := m.GetSession(id)
	if err != nil {
		return err
	}
	s.Privileged = v
	b, err := m.encode(s)
	if err != nil {
		return err
	}
	return m.st.Commit(id, b, s.Timing.AbsoluteUntil)
}

func (m *Manager) Refresh(id string) error {
	s, err := m.GetSession(id)
	if err != nil {
		return err
	}
	now := time.Now()
	if now.Sub(s.Timing.LastRefresh) < m.cfg.RefreshThrottle {
		s.Timing.LastAccess = now
	} else {
		s.Timing.LastAccess = now
		s.Timing.LastRefresh = now
		newIdle := now.Add(m.cfg.IdleTimeout)
		if newIdle.After(s.Timing.AbsoluteUntil) {
			newIdle = s.Timing.AbsoluteUntil
		}
		s.Timing.IdleUntil = newIdle
	}
	b, err := m.encode(s)
	if err != nil {
		return err
	}
	return m.st.Commit(id, b, s.Timing.AbsoluteUntil)
}

// -----------------------------------------------------------------------------
// HTTP helpers (cookies + validation)
// -----------------------------------------------------------------------------

func (m *Manager) CookieMaxAgeSeconds() int {
	return int(m.cfg.AbsoluteTimeout.Seconds())
}

func (m *Manager) WriteCookie(w http.ResponseWriter, sessionID string) {
	c := &http.Cookie{
		Name:        m.cfg.Cookie.Name,
		Value:       sessionID,
		Domain:      m.cfg.Cookie.Domain,
		Path:        m.cfg.Cookie.Path,
		SameSite:    m.cfg.Cookie.SameSite,
		Secure:      m.cfg.Cookie.Secure,
		HttpOnly:    m.cfg.Cookie.HTTPOnly,
		Partitioned: m.cfg.Cookie.Partitioned,
	}
	if sessionID == "" {
		c.Expires = time.Unix(1, 0)
		c.MaxAge = -1
	} else {
		c.MaxAge = m.CookieMaxAgeSeconds()
	}
	w.Header().Add("Set-Cookie", c.String())
	w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
}

func (m *Manager) DeleteCookie(w http.ResponseWriter) { m.WriteCookie(w, "") }

func (m *Manager) ValidateFromRequest(r *http.Request) (*Session, error) {
	ck, err := r.Cookie(m.cfg.Cookie.Name)
	if err != nil || ck.Value == "" {
		return nil, fmt.Errorf("missing or invalid %s", m.cfg.Cookie.Name)
	}
	s, err := m.GetSession(ck.Value)
	if err != nil {
		logger.Debugf("Access attempt with unknown %s: %s", m.cfg.Cookie.Name, ck.Value)
		return nil, fmt.Errorf("unknown session ID")
	}
	now := time.Now()
	if expiredAbsolute(s, now) {
		_ = m.DeleteSession(s.SessionID, ReasonGCAbsolute)
		logger.Warnf("Expired session (absolute) by '%s'", s.User.Username)
		return nil, fmt.Errorf("session expired")
	}
	if expiredIdle(s, now) {
		_ = m.DeleteSession(s.SessionID, ReasonGCIdle)
		logger.Warnf("Expired session (idle) by '%s'", s.User.Username)
		return nil, fmt.Errorf("session expired")
	}
	_ = m.Refresh(s.SessionID)
	return s, nil
}

// -----------------------------------------------------------------------------
// HTTP middleware helpers
// -----------------------------------------------------------------------------

type ctxKeyType string

const ctxKey ctxKeyType = "session"

// RequireSession returns middleware that validates the session cookie
// and stores the session in the request context.
func (m *Manager) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := m.ValidateFromRequest(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey, s)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SessionFromContext extracts the session from the request context.
func SessionFromContext(ctx context.Context) *Session {
	if s, ok := ctx.Value(ctxKey).(*Session); ok {
		return s
	}
	return nil
}

// -----------------------------------------------------------------------------
// Background idle sweep (absolute expiry is handled by TTL in Store)
// -----------------------------------------------------------------------------

func (m *Manager) gcLoop() {
	t := time.NewTicker(m.cfg.GCInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			mm, err := m.st.All()
			if err != nil {
				continue
			}
			now := time.Now()
			collected := 0
			for tok, data := range mm {
				s, err := m.decode(data)
				if err != nil {
					continue
				}
				// absolute expiry likely already gone due to Commit(expiry),
				// here we enforce idle expiry.
				if expiredIdle(s, now) {
					_ = m.st.Delete(tok)
					m.broadcastOnDelete(s, ReasonGCIdle)
					collected++
				}
			}
			if collected > 0 {
				logger.Infof("Session GC: collected %d idle-expired session(s)", collected)
			}
		case <-m.gcStop:
			return
		}
	}
}

// ActiveSessions returns decoded, non-expired sessions.
// It filters out sessions that are idle- or absolute-expired.
func (m *Manager) ActiveSessions() ([]*Session, error) {
	all, err := m.st.All()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]*Session, 0, len(all))
	for _, b := range all {
		s, err := m.decode(b)
		if err != nil {
			continue
		}
		if expiredAbsolute(s, now) || expiredIdle(s, now) {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
