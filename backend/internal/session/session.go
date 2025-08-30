package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/utils"
)

////////////////////////////////////////////////////////////////////////////////
// CONFIG & MODELS
////////////////////////////////////////////////////////////////////////////////

type SessionConfig struct {
	IdleTimeout          time.Duration
	AbsoluteTimeout      time.Duration
	RefreshInterval      time.Duration
	SingleSessionPerUser bool
	GCInterval           time.Duration
}

var defaultConfig = SessionConfig{
	IdleTimeout:          30 * time.Minute,
	AbsoluteTimeout:      12 * time.Hour,
	RefreshInterval:      60 * time.Second,
	SingleSessionPerUser: false,
	GCInterval:           10 * time.Minute,
}

type User struct {
	Username string
	UID      string
	GID      string
}

type Timing struct {
	CreatedAt     time.Time
	LastAccess    time.Time
	LastRefresh   time.Time
	IdleUntil     time.Time
	AbsoluteUntil time.Time
}

type Session struct {
	SessionID    string
	User         User
	Privileged   bool
	BridgeSecret string
	Timing       Timing
}

////////////////////////////////////////////////////////////////////////////////
// STORE (mutex-based)
////////////////////////////////////////////////////////////////////////////////

type store struct {
	mu        sync.RWMutex
	cfg       SessionConfig
	sessions  map[string]Session             // sessionID -> Session
	userIndex map[string]map[string]struct{} // username -> set(sessionID)
	gcTicker  *time.Ticker
	closing   chan struct{}
}

var (
	mem               *store
	ErrNotInitialized = errors.New("session: not initialized")
)

////////////////////////////////////////////////////////////////////////////////
// INIT & SHUTDOWN
////////////////////////////////////////////////////////////////////////////////

func Init(cfgs ...*SessionConfig) (shutdown func()) {
	var cfg *SessionConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if mem != nil {
		logger.Warnf("session init: already initialized")
		return func() {}
	}

	final := defaultConfig
	if cfg != nil {
		if cfg.IdleTimeout > 0 {
			final.IdleTimeout = cfg.IdleTimeout
		}
		if cfg.AbsoluteTimeout > 0 {
			final.AbsoluteTimeout = cfg.AbsoluteTimeout
		}
		if cfg.RefreshInterval > 0 {
			final.RefreshInterval = cfg.RefreshInterval
		}
		final.SingleSessionPerUser = cfg.SingleSessionPerUser
		if cfg.GCInterval > 0 {
			final.GCInterval = cfg.GCInterval
		}
	}

	mem = &store{
		cfg:       final,
		sessions:  make(map[string]Session),
		userIndex: make(map[string]map[string]struct{}),
		closing:   make(chan struct{}),
	}

	logger.Infof("🔑 Session system initialized (idle=%v, absolute=%v, refresh=%v, singleUser=%v, gc=%v)",
		final.IdleTimeout, final.AbsoluteTimeout, final.RefreshInterval, final.SingleSessionPerUser, final.GCInterval)

	// Periodic GC for expired sessions (GC-only)
	mem.gcTicker = time.NewTicker(mem.cfg.GCInterval)
	go func() {
		for {
			select {
			case <-mem.gcTicker.C:
				now := time.Now()
				collected := 0
				mem.mu.Lock()
				for id, sess := range mem.sessions {
					if expired(&sess, now) {
						deleteSessionLocked(mem, id) // expects mem.mu held
						collected++
					}
				}
				mem.mu.Unlock()
				if collected > 0 {
					logger.Infof("🧽 Session GC: collected %d expired session(s)", collected)
				}
			case <-mem.closing:
				return
			}
		}
	}()

	return func() {
		close(mem.closing)
		if mem.gcTicker != nil {
			mem.gcTicker.Stop()
		}
		logger.Infof("🧹 Session system shut down")
	}
}

////////////////////////////////////////////////////////////////////////////////
// INTERNAL HELPERS
////////////////////////////////////////////////////////////////////////////////

func expired(sess *Session, now time.Time) bool {
	return now.After(sess.Timing.AbsoluteUntil) || now.After(sess.Timing.IdleUntil)
}

// deleteSessionLocked expects s.mu to be held (write lock).
func deleteSessionLocked(s *store, id string) {
	old, ok := s.sessions[id]
	if !ok {
		return
	}
	delete(s.sessions, id)
	if set, ok := s.userIndex[old.User.Username]; ok {
		delete(set, id)
		if len(set) == 0 {
			delete(s.userIndex, old.User.Username)
		}
	}
	logger.Infof("Deleted session for user '%s'", old.User.Username)
}

func indexSessionLocked(s *store, sess Session) {
	set := s.userIndex[sess.User.Username]
	if set == nil {
		set = make(map[string]struct{})
		s.userIndex[sess.User.Username] = set
	}
	set[sess.SessionID] = struct{}{}
}

func randID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// updateSession expects mem != nil; it locks internally.
func updateSession(id string, fn func(*Session) error) error {
	if mem == nil {
		return ErrNotInitialized
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()

	sess, exists := mem.sessions[id]
	if !exists {
		return fmt.Errorf("session ID '%s' not found", id)
	}
	if err := fn(&sess); err != nil {
		return err
	}
	mem.sessions[id] = sess
	return nil
}

func getSessionCopy(id string) (*Session, error) {
	if mem == nil {
		return nil, ErrNotInitialized
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()

	sess, exists := mem.sessions[id]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}
	copy := sess
	return &copy, nil
}

////////////////////////////////////////////////////////////////////////////////
// PUBLIC API: SESSION LIFECYCLE
////////////////////////////////////////////////////////////////////////////////

func CreateSession(id string, user User, privileged bool) (*Session, error) {
	if mem == nil {
		return nil, ErrNotInitialized
	}

	mem.mu.Lock()
	defer mem.mu.Unlock()

	// Generate ID if needed
	if id == "" {
		var err error
		id, err = randID(16)
		if err != nil {
			return nil, fmt.Errorf("failed generating session id: %w", err)
		}
	}
	if _, exists := mem.sessions[id]; exists {
		return nil, fmt.Errorf("session already exists for ID %s", id)
	}

	now := time.Now()
	abs := mem.cfg.AbsoluteTimeout

	bridgeSecret := utils.GenerateSecretKey(32)
	sess := Session{
		SessionID:    id,
		User:         user,
		Privileged:   privileged,
		BridgeSecret: bridgeSecret,
		Timing: Timing{
			CreatedAt:     now,
			LastAccess:    now,
			LastRefresh:   now,
			AbsoluteUntil: now.Add(abs),
			IdleUntil: func() time.Time {
				idle := now.Add(mem.cfg.IdleTimeout)
				absUntil := now.Add(abs)
				if idle.After(absUntil) {
					idle = absUntil
				}
				return idle
			}(),
		},
	}

	if mem.cfg.SingleSessionPerUser {
		if set, ok := mem.userIndex[user.Username]; ok {
			for sid := range set {
				deleteSessionLocked(mem, sid)
			}
		}
	}

	mem.sessions[id] = sess
	indexSessionLocked(mem, sess)

	copy := sess
	logger.Infof("Created session for user '%s'", user.Username)
	return &copy, nil
}

func DeleteSession(id string) error {
	if mem == nil {
		return ErrNotInitialized
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()

	if _, exists := mem.sessions[id]; !exists {
		return fmt.Errorf("session ID '%s' not found", id)
	}
	deleteSessionLocked(mem, id)
	return nil
}

func GetSession(id string) (*Session, error) { return getSessionCopy(id) }

func GetActiveSessionIDs() ([]string, error) {
	if mem == nil {
		return nil, ErrNotInitialized
	}
	now := time.Now()

	mem.mu.RLock()
	defer mem.mu.RUnlock()

	var active []string
	for id, sess := range mem.sessions {
		if !expired(&sess, now) {
			active = append(active, id)
		}
	}
	return active, nil
}

func SetPrivileged(id string, v bool) error {
	return updateSession(id, func(s *Session) error {
		s.Privileged = v
		return nil
	})
}

////////////////////////////////////////////////////////////////////////////////
// REFRESH & VALIDATION
////////////////////////////////////////////////////////////////////////////////

func refresh(id string) error {
	if mem == nil {
		return ErrNotInitialized
	}
	cfg := mem.cfg
	return updateSession(id, func(s *Session) error {
		now := time.Now()
		if now.Sub(s.Timing.LastRefresh) < cfg.RefreshInterval {
			s.Timing.LastAccess = now
			return nil
		}
		s.Timing.LastAccess = now
		s.Timing.LastRefresh = now

		newIdle := now.Add(cfg.IdleTimeout)
		if newIdle.After(s.Timing.AbsoluteUntil) {
			newIdle = s.Timing.AbsoluteUntil
		}
		s.Timing.IdleUntil = newIdle
		return nil
	})
}

// ValidateSessionFromRequest reads the CookieName cookie,
// verifies deadlines, and performs a throttled sliding refresh.
func ValidateSessionFromRequest(r *http.Request) (*Session, error) {
	if mem == nil {
		return nil, ErrNotInitialized
	}

	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, fmt.Errorf("missing or invalid %s cookie", CookieName)
	}

	sess, err := getSessionCopy(cookie.Value)
	if err != nil {
		logger.Debugf("Access attempt with unknown %s: %s", CookieName, cookie.Value)
		return nil, fmt.Errorf("unknown session ID")
	}

	now := time.Now()
	if now.After(sess.Timing.AbsoluteUntil) || now.After(sess.Timing.IdleUntil) {
		logger.Warnf("Expired session access attempt by user '%s'", sess.User.Username)
		return nil, fmt.Errorf("session expired")
	}

	_ = refresh(sess.SessionID) // slide idle window (throttled)
	return sess, nil
}

////////////////////////////////////////////////////////////////////////////////
// COOKIE HELPERS
////////////////////////////////////////////////////////////////////////////////

const CookieName = "session_id"

func CookieMaxAgeSeconds() int {
	if mem == nil {
		return int(defaultConfig.AbsoluteTimeout.Seconds())
	}
	sec := int(mem.cfg.AbsoluteTimeout.Seconds())
	if sec <= 0 {
		return int(defaultConfig.AbsoluteTimeout.Seconds())
	}
	return sec
}

func SetCookie(c *gin.Context, sessionID string, secure bool) {
	c.SetSameSite(http.SameSiteStrictMode)
	maxAge := CookieMaxAgeSeconds()
	c.SetCookie(CookieName, sessionID, maxAge, "/", "", secure, true)
}

func DeleteCookie(c *gin.Context, secure bool) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(CookieName, "", -1, "/", "", secure, true)
}

////////////////////////////////////////////////////////////////////////////////
// GIN HELPERS
////////////////////////////////////////////////////////////////////////////////

func SessionFromContext(c *gin.Context) *Session {
	v, _ := c.Get("session")
	s, _ := v.(*Session)
	return s
}
