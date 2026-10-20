package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
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

type CapabilitiesAvailable struct {
	DockerAvailable           bool `json:"docker_available"`
	DockerUpdatesAvailable    bool `json:"docker_updates_available"`
	MonitoringAvailable       bool `json:"monitoring_available"`
	LMSensorsAvailable        bool `json:"lm_sensors_available"`
	MemoryInventoryAvailable  bool `json:"memory_inventory_available"`
	SmartmontoolsAvailable    bool `json:"smartmontools_available"`
	PackageKitAvailable       bool `json:"packagekit_available"`
	NFSClientAvailable        bool `json:"nfs_client_available"`
	NFSServerAvailable        bool `json:"nfs_server_available"`
	SambaServerAvailable      bool `json:"samba_server_available"`
	SambaClientAvailable      bool `json:"samba_client_available"`
	RsyncAvailable            bool `json:"rsync_available"`
	TunedAvailable            bool `json:"tuned_available"`
	AvahiAvailable            bool `json:"avahi_available"`
	WireGuardAvailable        bool `json:"wireguard_available"`
	LibvirtAvailable          bool `json:"libvirt_available"`
	NodeAvailable             bool `json:"node_available"`
	BtrfsMaintenanceAvailable bool `json:"btrfs_maintenance_available"`
	BtrfsAvailable            bool `json:"btrfs_available"`
}

type CapabilitiesError struct {
	DockerError           *string `json:"docker_error,omitempty"`
	DockerUpdatesError    *string `json:"docker_updates_error,omitempty"`
	MonitoringError       *string `json:"monitoring_error,omitempty"`
	LMSensorsError        *string `json:"lm_sensors_error,omitempty"`
	MemoryInventoryError  *string `json:"memory_inventory_error,omitempty"`
	SmartmontoolsError    *string `json:"smartmontools_error,omitempty"`
	PackageKitError       *string `json:"packagekit_error,omitempty"`
	NFSClientError        *string `json:"nfs_client_error,omitempty"`
	NFSServerError        *string `json:"nfs_server_error,omitempty"`
	SambaServerError      *string `json:"samba_server_error,omitempty"`
	SambaClientError      *string `json:"samba_client_error,omitempty"`
	RsyncError            *string `json:"rsync_error,omitempty"`
	TunedError            *string `json:"tuned_error,omitempty"`
	AvahiError            *string `json:"avahi_error,omitempty"`
	WireGuardError        *string `json:"wireguard_error,omitempty"`
	LibvirtError          *string `json:"libvirt_error,omitempty"`
	NodeError             *string `json:"node_error,omitempty"`
	BtrfsMaintenanceError *string `json:"btrfs_maintenance_error,omitempty"`
	BtrfsError            *string `json:"btrfs_error,omitempty"`
}

type Session struct {
	SessionID    string                `json:"session_id"`
	User         User                  `json:"user"`
	Privileged   bool                  `json:"privileged"`
	Capabilities CapabilitiesAvailable `json:"capabilities"`
	Timing       Timing                `json:"timing"`

	// Termination handler (not serialized)
	terminateFunc func(DeleteReason)
	terminateMu   sync.Mutex
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
	sessions map[string]*Session

	onDeleteMu sync.RWMutex
	onDelete   []func(*Session, DeleteReason)

	gcStop    chan struct{}
	closeOnce sync.Once
}

// NewManager constructs a manager from a fully resolved SessionConfig.
// Callers should start from DefaultConfig and override fields explicitly.
func NewManager(cfg SessionConfig) *Manager {
	m := &Manager{cfg: cfg, sessions: make(map[string]*Session)}
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

// Terminate invokes the session's termination handler if set.
// This allows the session to request its own deletion (e.g., on bridge failure).
func (s *Session) Terminate(reason DeleteReason) {
	s.terminateMu.Lock()
	fn := s.terminateFunc
	s.terminateMu.Unlock()
	if fn != nil {
		fn(reason)
	}
}

// setTerminateFunc sets the termination handler for this session.
func (s *Session) setTerminateFunc(fn func(DeleteReason)) {
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

// snapshot returns a caller-owned copy of a stored session so callers never
// share mutable state with the table. Callers must hold m.mu.
func (m *Manager) snapshot(s *Session) *Session {
	out := &Session{
		SessionID:    s.SessionID,
		User:         s.User,
		Privileged:   s.Privileged,
		Capabilities: s.Capabilities,
		Timing:       s.Timing,
	}
	out.setTerminateFunc(func(reason DeleteReason) {
		m.DeleteSession(out.SessionID, reason)
	})
	return out
}

func (m *Manager) refreshTiming(s *Session, now time.Time) {
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
func expiryReason(s *Session, now time.Time) DeleteReason {
	switch {
	case expiredAbsolute(s, now):
		return ReasonGCAbsolute
	case expiredIdle(s, now):
		return ReasonGCIdle
	}
	return ""
}

// deleted logs and broadcasts sessions already removed from the table.
func (m *Manager) deleted(sessions []*Session, r DeleteReason) {
	for _, s := range sessions {
		slog.Info("session deleted",
			"session_ref", DiagnosticRef(s.SessionID),
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

// DiagnosticRef returns a stable, non-secret correlation value for a session
// credential. Production session IDs contain 128 bits of randomness, so a
// one-way digest preserves diagnostic correlation without disclosing the
// bearer credential itself. The result is diagnostic-only and must never be
// accepted for authentication or authorization.
func DiagnosticRef(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("linuxio-session-diagnostic-ref\x00" + sessionID))
	return "sr-" + hex.EncodeToString(sum[:16])
}

func (m *Manager) CreateSession(id string, user User, privileged bool) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session id required")
	}

	m.mu.Lock()
	if _, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session id already exists")
	}
	var evicted []*Session
	if m.cfg.SingleSessionPerUser {
		for tok, s := range m.sessions {
			if s.User.Username == user.Username {
				delete(m.sessions, tok)
				evicted = append(evicted, s)
			}
		}
	}
	stored := &Session{
		SessionID:  id,
		User:       user,
		Privileged: privileged,
		Timing:     m.newSessionTiming(time.Now()),
	}
	m.sessions[id] = stored
	sess := m.snapshot(stored)
	m.mu.Unlock()

	m.deleted(evicted, ReasonManual)
	slog.Info("session created",
		"session_ref", DiagnosticRef(sess.SessionID),
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
		m.deleted([]*Session{s}, r)
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
func (m *Manager) ValidateSession(id string) (*Session, error) {
	return m.validate(id, false)
}

func (m *Manager) validate(id string, refresh bool) (*Session, error) {
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
		m.deleted([]*Session{s}, reason)
		return nil, fmt.Errorf("session expired")
	}
	if refresh {
		m.refreshTiming(s, now)
	}
	out := m.snapshot(s)
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

func (m *Manager) ValidateFromRequest(r *http.Request) (*Session, error) {
	ck, err := r.Cookie(m.cfg.Cookie.Name)
	if err != nil || ck.Value == "" {
		return nil, fmt.Errorf("missing or invalid %s", m.cfg.Cookie.Name)
	}
	s, err := m.ValidateSession(ck.Value)
	if err != nil {
		slog.Debug("access attempt with unknown session cookie",
			"cookie_name", m.cfg.Cookie.Name,
			"session_ref", DiagnosticRef(ck.Value))
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
	expired := make(map[DeleteReason][]*Session)
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
func (m *Manager) ActiveSessions() []*Session {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if expiryReason(s, now) == "" {
			out = append(out, m.snapshot(s))
		}
	}
	return out
}
