package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/utils"
)

// ===== Config & model =====

type SessionConfig struct {
	// Sliding idle window (extended by activity, capped by absolute)
	IdleTimeout time.Duration

	// Absolute maximum lifetime since creation (hard cap)
	AbsoluteTimeout time.Duration

	// Avoids refreshing on every request; only extend if at least this period passed
	RefreshInterval time.Duration

	// If true, a new login for a user revokes any previous sessions
	SingleSessionPerUser bool

	// GC cadence
	GCInterval time.Duration
}

var defaultConfig = SessionConfig{
	IdleTimeout:          30 * time.Minute,
	AbsoluteTimeout:      12 * time.Hour,
	RefreshInterval:      60 * time.Second,
	SingleSessionPerUser: false,
	GCInterval:           10 * time.Minute,
}

type User struct {
	ID   string // Username (unique key)
	Name string // Display name (can be same as ID)
}

type Session struct {
	SessionID    string
	User         User
	Privileged   bool
	BridgeSecret string

	// Timing
	CreatedAt     time.Time
	LastAccess    time.Time
	LastRefresh   time.Time
	IdleUntil     time.Time // sliding
	AbsoluteUntil time.Time // cap
}

// ===== Actor state =====

type store struct {
	cfg       SessionConfig
	sessions  map[string]Session             // id -> session
	userIndex map[string]map[string]struct{} // userID -> set(sessionID)
	gcTicker  *time.Ticker
	closing   chan struct{}
}

// Public state
var (
	mem        *store
	SessionMux = make(chan func(*store)) // actor channel
)

// ===== Init / Shutdown =====

func Init(cfg *SessionConfig) (shutdown func(), err error) {
	if mem != nil {
		return nil, errors.New("session: already initialized")
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

	// Actor
	go func() {
		for f := range SessionMux {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("Panic in session actor: %v", r)
					}
				}()
				f(mem)
			}()
		}
	}()

	// GC
	mem.gcTicker = time.NewTicker(mem.cfg.GCInterval)
	go func() {
		for {
			select {
			case <-mem.gcTicker.C:
				now := time.Now()
				SessionMux <- func(s *store) {
					collected := 0
					for id, sess := range s.sessions {
						if expired(&sess, now) {
							deleteSessionLocked(s, id)
							collected++
						}
					}
					if collected > 0 {
						logger.Infof("Garbage collected %d expired sessions", collected)
					}
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
	}, nil
}

// ===== internals (actor-only) =====

func expired(sess *Session, now time.Time) bool {
	return now.After(sess.AbsoluteUntil) || now.After(sess.IdleUntil)
}

func deleteSessionLocked(s *store, id string) {
	old, ok := s.sessions[id]
	if !ok {
		return
	}
	delete(s.sessions, id)
	if set, ok := s.userIndex[old.User.ID]; ok {
		delete(set, id)
		if len(set) == 0 {
			delete(s.userIndex, old.User.ID)
		}
	}
	logger.Infof("Deleted session for user '%s'", old.User.ID)
}

func indexSessionLocked(s *store, sess Session) {
	set := s.userIndex[sess.User.ID]
	if set == nil {
		set = make(map[string]struct{})
		s.userIndex[sess.User.ID] = set
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

// ===== Public API (compatible with your existing callers) =====

// CreateSession creates a new session, returns error if already exists.
// `duration` is treated as absolute cap if provided; idle timeout comes from config.
func CreateSession(id string, user User, duration time.Duration, privileged bool) error {
	if mem == nil {
		_, _ = Init(nil)
	}
	done := make(chan error, 1)

	SessionMux <- func(s *store) {
		if id == "" {
			var err error
			id, err = randID(16)
			if err != nil {
				done <- fmt.Errorf("failed generating session id: %w", err)
				return
			}
		}
		if _, exists := s.sessions[id]; exists {
			done <- fmt.Errorf("session already exists for ID %s", id)
			return
		}

		now := time.Now()
		abs := s.cfg.AbsoluteTimeout
		if duration > 0 && duration < abs {
			abs = duration
		}
		bridgeSecret := utils.GenerateSecretKey(32)
		sess := Session{
			SessionID:     id,
			User:          user,
			Privileged:    privileged,
			BridgeSecret:  bridgeSecret,
			CreatedAt:     now,
			LastAccess:    now,
			LastRefresh:   now,
			AbsoluteUntil: now.Add(abs),
			IdleUntil:     now.Add(s.cfg.IdleTimeout),
		}

		// Optional: one session per user
		if s.cfg.SingleSessionPerUser {
			if set, ok := s.userIndex[user.ID]; ok {
				for sid := range set {
					deleteSessionLocked(s, sid)
				}
			}
		}

		s.sessions[id] = sess
		indexSessionLocked(s, sess)
		done <- nil
	}

	err := <-done
	if err == nil {
		logger.Infof("Created session for user '%s'", user.ID)
	}
	return err
}

func DeleteSession(id string) error {
	done := make(chan error, 1)
	SessionMux <- func(s *store) {
		if _, exists := s.sessions[id]; !exists {
			done <- fmt.Errorf("session ID '%s' not found", id)
			return
		}
		deleteSessionLocked(s, id)
		done <- nil
	}
	return <-done
}

// Generic access helpers remain (but use actor store)
func withSession(id string, fn func(Session) error) error {
	done := make(chan error, 1)
	SessionMux <- func(s *store) {
		sess, exists := s.sessions[id]
		if !exists {
			done <- fmt.Errorf("session ID '%s' not found", id)
			return
		}
		done <- fn(sess)
	}
	return <-done
}

func updateSession(id string, fn func(*Session) error) error {
	done := make(chan error, 1)
	SessionMux <- func(s *store) {
		sess, exists := s.sessions[id]
		if !exists {
			done <- fmt.Errorf("session ID '%s' not found", id)
			return
		}
		if err := fn(&sess); err != nil {
			done <- err
			return
		}

		s.sessions[id] = sess
		done <- nil
	}
	return <-done
}

func getSessionCopy(id string) (*Session, error) {
	done := make(chan struct {
		sess *Session
		err  error
	}, 1)
	SessionMux <- func(s *store) {
		sess, exists := s.sessions[id]
		if !exists {
			done <- struct {
				sess *Session
				err  error
			}{nil, fmt.Errorf("session not found")}
			return
		}
		copy := sess
		done <- struct {
			sess *Session
			err  error
		}{&copy, nil}
	}
	result := <-done
	return result.sess, result.err
}

// Refactored public functions (kept)

func GetSession(id string) (*Session, error) { return getSessionCopy(id) }

func GetActiveSessionIDs() ([]string, error) {
	done := make(chan struct {
		ids []string
		err error
	}, 1)
	SessionMux <- func(s *store) {
		now := time.Now()
		var active []string
		for id, sess := range s.sessions {
			if !expired(&sess, now) {
				active = append(active, id)
			}
		}
		done <- struct {
			ids []string
			err error
		}{active, nil}
	}
	result := <-done
	return result.ids, result.err
}

func SetSessionPrivileged(id string, privileged bool) error {
	return updateSession(id, func(s *Session) error {
		s.Privileged = privileged
		return nil
	})
}

func IsSessionPrivileged(id string) (bool, error) {
	var result bool
	err := withSession(id, func(s Session) error {
		result = s.Privileged
		return nil
	})
	return result, err
}

func IsSessionValid(id string) (bool, error) {
	done := make(chan struct {
		ok  bool
		err error
	}, 1)
	SessionMux <- func(s *store) {
		sess, ok := s.sessions[id]
		if !ok {
			done <- struct {
				ok  bool
				err error
			}{false, fmt.Errorf("session not found")}
			return
		}
		if expired(&sess, time.Now()) {
			done <- struct {
				ok  bool
				err error
			}{false, fmt.Errorf("session expired")}
			return
		}
		done <- struct {
			ok  bool
			err error
		}{true, nil}
	}
	res := <-done
	return res.ok, res.err
}

// Touch updates LastAccess & IdleUntil (cap at AbsoluteUntil)
func Touch(id string) error {
	return updateSession(id, func(s *Session) error {
		now := time.Now()
		s.LastAccess = now
		newIdle := now.Add(mem.cfg.IdleTimeout)
		if newIdle.After(s.AbsoluteUntil) {
			newIdle = s.AbsoluteUntil
		}
		s.IdleUntil = newIdle
		return nil
	})
}

// Refresh extends idle window if RefreshInterval elapsed; otherwise only touch.
func Refresh(id string) error {
	return updateSession(id, func(s *Session) error {
		now := time.Now()
		if now.Sub(s.LastRefresh) < mem.cfg.RefreshInterval {
			s.LastAccess = now
			return nil
		}
		s.LastAccess = now
		s.LastRefresh = now
		newIdle := now.Add(mem.cfg.IdleTimeout)
		if newIdle.After(s.AbsoluteUntil) {
			newIdle = s.AbsoluteUntil
		}
		s.IdleUntil = newIdle
		return nil
	})
}

// ===== HTTP Helpers =====

// ValidateSessionFromRequest validates the cookie and returns a COPY of the session.
// **Also implements sliding expiration** via throttled Refresh() to keep your middleware unchanged.
func ValidateSessionFromRequest(r *http.Request) (*Session, error) {
	if mem == nil {
		_, _ = Init(nil)
	}
	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		return nil, fmt.Errorf("missing or invalid session_id cookie")
	}

	sess, err := getSessionCopy(cookie.Value)
	if err != nil {
		logger.Debugf("Access attempt with unknown session_id: %s", cookie.Value)
		return nil, fmt.Errorf("unknown session ID")
	}

	now := time.Now()
	if now.After(sess.AbsoluteUntil) || now.After(sess.IdleUntil) {
		logger.Warnf("Expired session access attempt by user '%s'", sess.User.ID)
		return nil, fmt.Errorf("session expired")
	}

	// Sliding refresh (throttled) – keeps middleware behavior identical
	_ = Refresh(sess.SessionID)

	return sess, nil
}

// Convenience: GetSessionOrAbort still available for direct route usage
func GetSessionOrAbort(c *gin.Context) *Session {
	sess, err := ValidateSessionFromRequest(c.Request)
	if err != nil || sess == nil {
		logger.Warnf("Unauthorized access: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session"})
		c.Abort()
		return nil
	}
	return sess
}
