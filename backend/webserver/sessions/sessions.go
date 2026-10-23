// Package sessions holds the webserver-only session table: the in-memory
// Manager, cookie handling, request middleware and expiry GC. The
// cross-process session record lives in backend/common/session.
package sessions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
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
	ReasonBridgeFailure DeleteReason = "bridge_failure"
)

type SessionConfig struct {
	IdleTimeout          time.Duration
	AbsoluteTimeout      time.Duration
	RefreshThrottle      time.Duration // 0 means refresh on every activity
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
		Name:        "__Host-session_id",
		Path:        "/",
		SameSite:    http.SameSiteStrictMode,
		Secure:      true,
		HTTPOnly:    true,
		Partitioned: false,
	},
}

// -----------------------------------------------------------------------------
// Manager
// -----------------------------------------------------------------------------

// Manager owns the in-memory session table. Every read-modify-write and
// delete happens under mu, so a refresh cannot resurrect a session that a
// concurrent logout or GC already removed, and delete hooks fire once.
type Manager struct {
	cfg SessionConfig

	mu       sync.Mutex
	sessions map[string]*session.Session

	onDeleteMu sync.RWMutex
	onDelete   []func(*session.Session, DeleteReason)

	gcStop    chan struct{}
	closeOnce sync.Once
}

// NewManager constructs a manager from a fully resolved SessionConfig.
// Callers should start from DefaultConfig and override fields explicitly.
func NewManager(cfg SessionConfig) *Manager {
	m := &Manager{cfg: cfg, sessions: make(map[string]*session.Session)}
	slog.Info("Session manager ready")
	slog.Debug("session timings configured",
		"idle_timeout", m.cfg.IdleTimeout,
		"absolute_timeout", m.cfg.AbsoluteTimeout,
		"refresh_throttle", m.cfg.RefreshThrottle,
		"single_user", m.cfg.SingleSessionPerUser,
		"gc_interval", m.cfg.GCInterval)

	if m.cfg.GCInterval > 0 {
		m.gcStop = make(chan struct{})
		go m.gcLoop()
	}

	return m
}

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		if m.gcStop != nil {
			close(m.gcStop)
		}
		slog.Info("Session manager stopped")
	})
}

// -----------------------------------------------------------------------------
// Read-only accessors
// -----------------------------------------------------------------------------

// CookieName returns the effective cookie name in use.
func (m *Manager) CookieName() string { return m.cfg.Cookie.Name }

// -----------------------------------------------------------------------------
// Hooks
// -----------------------------------------------------------------------------

func (m *Manager) RegisterOnDelete(fn func(*session.Session, DeleteReason)) {
	m.onDeleteMu.Lock()
	m.onDelete = append(m.onDelete, fn)
	m.onDeleteMu.Unlock()
}

func (m *Manager) broadcastOnDelete(s *session.Session, r DeleteReason) {
	m.onDeleteMu.RLock()
	subs := append([]func(*session.Session, DeleteReason){}, m.onDelete...)
	m.onDeleteMu.RUnlock()
	for _, f := range subs {
		go func(ff func(*session.Session, DeleteReason)) {
			defer func() {
				if panicVal := recover(); panicVal != nil {
					slog.Warn("panic in session delete callback", "panic", panicVal)
				}
			}()
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

func (m *Manager) newSessionTiming(now time.Time) session.Timing {
	abs := now.Add(m.cfg.AbsoluteTimeout)
	idle := now.Add(m.cfg.IdleTimeout)
	if idle.After(abs) {
		idle = abs
	}
	return session.Timing{
		CreatedAt:     now,
		LastAccess:    now,
		LastRefresh:   now,
		IdleUntil:     idle,
		AbsoluteUntil: abs,
	}
}

func expiredIdle(s *session.Session, now time.Time) bool { return now.After(s.Timing.IdleUntil) }
func expiredAbsolute(s *session.Session, now time.Time) bool {
	return now.After(s.Timing.AbsoluteUntil)
}

// snapshot returns a caller-owned copy of a stored session so callers never
// share mutable state with the table. Callers must hold m.mu.
func snapshot(s *session.Session) *session.Session {
	out := *s
	return &out
}

func (m *Manager) refreshTiming(s *session.Session, now time.Time) {
	s.Timing.LastAccess = now
	if now.Sub(s.Timing.LastRefresh) < m.cfg.RefreshThrottle {
		return
	}
	s.Timing.LastRefresh = now
	newIdle := now.Add(m.cfg.IdleTimeout)
	if newIdle.After(s.Timing.AbsoluteUntil) {
		newIdle = s.Timing.AbsoluteUntil
	}
	s.Timing.IdleUntil = newIdle
}

// expiryReason reports why s is no longer usable, or "" when it is live.
func expiryReason(s *session.Session, now time.Time) DeleteReason {
	switch {
	case expiredAbsolute(s, now):
		return ReasonGCAbsolute
	case expiredIdle(s, now):
		return ReasonGCIdle
	}
	return ""
}

// deleted logs and broadcasts sessions already removed from the table.
func (m *Manager) deleted(sessions []*session.Session, r DeleteReason) {
	for _, s := range sessions {
		slog.Info("session deleted",
			"session_ref", session.DiagnosticRef(s.SessionID),
			"reason", string(r))
		m.broadcastOnDelete(s, r)
	}
}

// -----------------------------------------------------------------------------
// Public API
// -----------------------------------------------------------------------------

func (m *Manager) NewSessionID() (string, error) {
	return randID(16)
}

func (m *Manager) CreateSession(id string, user session.User, privileged bool) (*session.Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session id required")
	}

	m.mu.Lock()
	if _, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session id already exists")
	}
	var evicted []*session.Session
	if m.cfg.SingleSessionPerUser {
		for tok, s := range m.sessions {
			if s.User.Username == user.Username {
				delete(m.sessions, tok)
				evicted = append(evicted, s)
			}
		}
	}
	stored := &session.Session{
		SessionID:  id,
		User:       user,
		Privileged: privileged,
		Timing:     m.newSessionTiming(time.Now()),
	}
	m.sessions[id] = stored
	sess := snapshot(stored)
	m.mu.Unlock()

	m.deleted(evicted, ReasonManual)
	slog.Info("session created",
		"session_ref", session.DiagnosticRef(sess.SessionID),
		"privileged", privileged)
	return sess, nil
}

// DeleteSession removes a session and fires delete hooks. Only the call that
// actually removes the session broadcasts, so concurrent logout, GC and bridge
// failure tear down transports once.
func (m *Manager) DeleteSession(id string, r DeleteReason) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		m.deleted([]*session.Session{s}, r)
	}
}

// Refresh validates a session and extends its idle deadline as explicit user
// activity. Validation and the timing update happen under one lock, so a
// concurrent delete either wins outright or sees the refreshed session.
func (m *Manager) Refresh(id string) error {
	_, err := m.validate(id, true)
	return err
}

// ValidateSession loads a session and verifies its absolute and idle expiry
// without extending its idle deadline. Callers that represent explicit user
// activity must use Refresh after validation.
func (m *Manager) ValidateSession(id string) (*session.Session, error) {
	return m.validate(id, false)
}

func (m *Manager) validate(id string, refresh bool) (*session.Session, error) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("session not found")
	}
	now := time.Now()
	if reason := expiryReason(s, now); reason != "" {
		delete(m.sessions, id)
		m.mu.Unlock()
		m.deleted([]*session.Session{s}, reason)
		return nil, fmt.Errorf("session expired")
	}
	if refresh {
		m.refreshTiming(s, now)
	}
	out := snapshot(s)
	m.mu.Unlock()
	return out, nil
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

func (m *Manager) ValidateFromRequest(r *http.Request) (*session.Session, error) {
	ck, err := r.Cookie(m.cfg.Cookie.Name)
	if err != nil || ck.Value == "" {
		return nil, fmt.Errorf("missing or invalid %s", m.cfg.Cookie.Name)
	}
	s, err := m.ValidateSession(ck.Value)
	if err != nil {
		slog.Debug("access attempt with unknown session cookie",
			"cookie_name", m.cfg.Cookie.Name,
			"session_ref", session.DiagnosticRef(ck.Value))
		return nil, fmt.Errorf("unknown session ID")
	}
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
			if _, writeErr := w.Write([]byte(`{"error":"unauthorized"}`)); writeErr != nil {
				slog.Debug("failed to write unauthorized response", "error", writeErr)
			}
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey, s)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SessionFromContext extracts the session from the request context.
func SessionFromContext(ctx context.Context) *session.Session {
	if s, ok := ctx.Value(ctxKey).(*session.Session); ok {
		return s
	}
	return nil
}

// WithSession returns a new context with the session stored in it.
func WithSession(ctx context.Context, s *session.Session) context.Context {
	return context.WithValue(ctx, ctxKey, s)
}

// -----------------------------------------------------------------------------
// Background session sweep.
// Manager GC enforces both absolute and idle expiry so delete hooks tear down
// websocket/yamux state for sessions nobody touches again.
// -----------------------------------------------------------------------------

func (m *Manager) gcLoop() {
	t := time.NewTicker(m.cfg.GCInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.gcCollect()
		case <-m.gcStop:
			return
		}
	}
}

func (m *Manager) gcCollect() {
	now := time.Now()
	expired := make(map[DeleteReason][]*session.Session)
	m.mu.Lock()
	for tok, s := range m.sessions {
		if reason := expiryReason(s, now); reason != "" {
			delete(m.sessions, tok)
			expired[reason] = append(expired[reason], s)
		}
	}
	m.mu.Unlock()

	collected := 0
	for reason, sessions := range expired {
		m.deleted(sessions, reason)
		collected += len(sessions)
	}
	if collected > 0 {
		slog.Info("session garbage collection completed", "count", collected)
	}
}

// ActiveSessions returns copies of the sessions that are neither idle- nor
// absolute-expired.
func (m *Manager) ActiveSessions() []*session.Session {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*session.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if expiryReason(s, now) == "" {
			out = append(out, snapshot(s))
		}
	}
	return out
}
