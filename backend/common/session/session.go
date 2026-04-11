package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mordilloSan/go-logger/logger"
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
	// All returns all records still present in the store.
	// Manager expiry checks determine whether those records are still usable.
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

// NewManager constructs a manager from a fully resolved SessionConfig.
// Callers should start from DefaultConfig and override fields explicitly.
func NewManager(store Store, cfg SessionConfig) *Manager {
	m := &Manager{st: store, cfg: cfg}
	logger.Infof("Session manager ready")
	logger.Debugf("Session timings (idle=%v, absolute=%v, refresh=%v, singleUser=%v, gc=%v)",
		m.cfg.IdleTimeout, m.cfg.AbsoluteTimeout, m.cfg.RefreshThrottle, m.cfg.SingleSessionPerUser, m.cfg.GCInterval)

	// Stored records get a short TTL grace after absolute expiry so manager GC
	// can emit delete hooks before the store prunes any leftovers.
	// Manager GC handles idle expiry and transport teardown hooks.
	if m.cfg.GCInterval > 0 {
		m.gcStop = make(chan struct{})
		go m.gcLoop()
	}

	return m
}

func (m *Manager) Close() {
	if m.gcStop != nil {
		close(m.gcStop)
		m.gcStop = nil
	}
	if closer, ok := m.st.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			logger.Warnf("Failed to close session store: %v", err)
		}
	}
	logger.Infof("Session manager stopped")
}

// -----------------------------------------------------------------------------
// Read-only accessors
// -----------------------------------------------------------------------------

// CookieName returns the effective cookie name in use.
func (m *Manager) CookieName() string { return m.cfg.Cookie.Name }

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
			defer func() {
				if panicVal := recover(); panicVal != nil {
					logger.Warnf("panic in session onDelete callback: %v", panicVal)
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

func (m *Manager) newSessionTiming(now time.Time) Timing {
	abs := now.Add(m.cfg.AbsoluteTimeout)
	idle := now.Add(m.cfg.IdleTimeout)
	if idle.After(abs) {
		idle = abs
	}
	return Timing{
		CreatedAt:     now,
		LastAccess:    now,
		LastRefresh:   now,
		IdleUntil:     idle,
		AbsoluteUntil: abs,
	}
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

func (m *Manager) storeExpiry(absolute time.Time) time.Time {
	if m.cfg.GCInterval <= 0 {
		return absolute
	}
	// Keep the stored record briefly past absolute expiry so manager GC can emit
	// delete hooks and tear down websocket/yamux state consistently.
	return absolute.Add(m.cfg.GCInterval)
}

func (m *Manager) commitSession(s *Session) error {
	b, err := m.encode(s)
	if err != nil {
		return err
	}
	return m.st.Commit(s.SessionID, b, m.storeExpiry(s.Timing.AbsoluteUntil))
}

func (m *Manager) refreshLoadedSession(s *Session, now time.Time) error {
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
	return m.commitSession(s)
}

// -----------------------------------------------------------------------------
// Public API
// -----------------------------------------------------------------------------

func (m *Manager) NewSessionID() (string, error) {
	return randID(16)
}

func (m *Manager) CreateSession(user User, privileged bool) (*Session, error) {
	id, err := m.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("rand id: %w", err)
	}
	return m.CreateSessionWithID(id, user, privileged)
}

func (m *Manager) CreateSessionWithID(id string, user User, privileged bool) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session id required")
	}
	if _, ok, err := m.st.Find(id); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("session id already exists")
	}

	now := time.Now()

	sess := &Session{
		SessionID:  id,
		User:       user,
		Privileged: privileged,
		Timing:     m.newSessionTiming(now),
	}

	if m.cfg.SingleSessionPerUser {
		m.evictUserSessions(user.Username)
	}

	if err := m.commitSession(sess); err != nil {
		return nil, err
	}

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
	if err := m.st.Delete(id); err != nil {
		return err
	}
	if s, err := m.decode(b); err == nil {
		logger.Infof("Deleted session for user '%s' (reason=%s)", s.User.Username, r)
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
	return m.commitSession(s)
}

func (m *Manager) Refresh(id string) error {
	s, err := m.GetSession(id)
	if err != nil {
		return err
	}
	now := time.Now()
	if expiredAbsolute(s, now) {
		if delErr := m.DeleteSession(id, ReasonGCAbsolute); delErr != nil {
			logger.Warnf("failed to delete absolute-expired session '%s': %v", id, delErr)
		}
		return fmt.Errorf("session expired")
	}
	if expiredIdle(s, now) {
		if delErr := m.DeleteSession(id, ReasonGCIdle); delErr != nil {
			logger.Warnf("failed to delete idle-expired session '%s': %v", id, delErr)
		}
		return fmt.Errorf("session expired")
	}
	return m.refreshLoadedSession(s, now)
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
		if delErr := m.DeleteSession(s.SessionID, ReasonGCAbsolute); delErr != nil {
			logger.Warnf("failed to delete absolute-expired session '%s': %v", s.SessionID, delErr)
		}
		logger.Warnf("Expired session (absolute) by '%s'", s.User.Username)
		return nil, fmt.Errorf("session expired")
	}
	if expiredIdle(s, now) {
		if delErr := m.DeleteSession(s.SessionID, ReasonGCIdle); delErr != nil {
			logger.Warnf("failed to delete idle-expired session '%s': %v", s.SessionID, delErr)
		}
		logger.Warnf("Expired session (idle) by '%s'", s.User.Username)
		return nil, fmt.Errorf("session expired")
	}
	if refreshErr := m.refreshLoadedSession(s, now); refreshErr != nil {
		logger.Warnf("failed to refresh session '%s': %v", s.SessionID, refreshErr)
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
				logger.Debugf("failed to write unauthorized response: %v", writeErr)
			}
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

// WithSession returns a new context with the session stored in it.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxKey, s)
}

// -----------------------------------------------------------------------------
// Background session sweep.
// Stored records get a short TTL grace after absolute expiry so manager GC can
// emit delete hooks before the store eventually prunes any leftovers.
// Manager GC enforces both absolute and idle expiry.
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

func (m *Manager) evictUserSessions(username string) {
	mm, err := m.st.All()
	if err != nil {
		return
	}
	for tok, data := range mm {
		s, decodeErr := m.decode(data)
		if decodeErr != nil {
			continue
		}
		if s.User.Username == username {
			if deleteErr := m.st.Delete(tok); deleteErr != nil {
				logger.Warnf("failed deleting existing session for user '%s': %v", username, deleteErr)
				continue
			}
			m.broadcastOnDelete(s, ReasonManual)
		}
	}
}

func (m *Manager) gcCollect() {
	mm, err := m.st.All()
	if err != nil {
		return
	}
	now := time.Now()
	collected := 0
	for tok, data := range mm {
		s, err := m.decode(data)
		if err != nil {
			continue
		}
		reason := DeleteReason("")
		switch {
		case expiredAbsolute(s, now):
			reason = ReasonGCAbsolute
		case expiredIdle(s, now):
			reason = ReasonGCIdle
		}
		if reason != "" {
			if err := m.st.Delete(tok); err != nil {
				logger.Warnf("failed to delete expired session '%s': %v", tok, err)
				continue
			}
			m.broadcastOnDelete(s, reason)
			collected++
		}
	}
	if collected > 0 {
		logger.Infof("Session GC: collected %d idle-expired session(s)", collected)
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
