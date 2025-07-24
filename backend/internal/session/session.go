package session

import (
	"fmt"
	"github.com/mordilloSan/LinuxIO/backend/internal/logger"
	"github.com/mordilloSan/LinuxIO/backend/internal/utils"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Session struct {
	SessionID    string
	User         utils.User
	ExpiresAt    time.Time
	Privileged   bool
	BridgeSecret string
}

var (
	Sessions   = make(map[string]Session)
	SessionMux = make(chan func())
)

func init() {
	go func() {
		for f := range SessionMux {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("Panic in session actor: %v", r)
					}
				}()
				f()
			}()
		}
	}()
}

// Start a goroutine that periodically removes expired sessions
func StartSessionGC() {
	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for range ticker.C {
			now := time.Now()
			SessionMux <- func() {
				count := 0
				for id, s := range Sessions {
					if s.ExpiresAt.Before(now) {
						delete(Sessions, id)
						count++
					}
				}
				if count > 0 {
					logger.Infof("Garbage collected %d expired sessions", count)
				}
			}
		}
	}()
}

// CreateSession creates a new session, returns error if already exists
func CreateSession(id string, user utils.User, duration time.Duration, privileged bool) error {
	done := make(chan error)
	secret := utils.GenerateSecretKey(32)
	sess := Session{
		SessionID:    id,
		User:         user,
		ExpiresAt:    time.Now().Add(duration),
		Privileged:   privileged,
		BridgeSecret: secret,
	}
	SessionMux <- func() {
		if _, exists := Sessions[id]; exists {
			done <- fmt.Errorf("session already exists for ID %s", id)
			return
		}
		Sessions[id] = sess
		done <- nil
	}
	err := <-done
	if err == nil {
		logger.Infof("Created session for user '%s'", user.ID)
	}
	return err
}

// DeleteSession removes a session by ID
func DeleteSession(id string) error {
	done := make(chan error)
	SessionMux <- func() {
		sess, exists := Sessions[id]
		if !exists {
			done <- fmt.Errorf("session ID '%s' not found", id)
			return
		}
		delete(Sessions, id)
		logger.Infof("Deleted session for user '%s'", sess.User.ID)
		done <- nil
	}
	return <-done
}

// ---- Generic Access Helpers ----

func withSession(id string, fn func(Session) error) error {
	done := make(chan error)
	SessionMux <- func() {
		sess, exists := Sessions[id]
		if !exists {
			done <- fmt.Errorf("session ID '%s' not found", id)
			return
		}
		done <- fn(sess)
	}
	return <-done
}

func updateSession(id string, fn func(*Session) error) error {
	done := make(chan error)
	SessionMux <- func() {
		sess, exists := Sessions[id]
		if !exists {
			done <- fmt.Errorf("session ID '%s' not found", id)
			return
		}
		if err := fn(&sess); err != nil {
			done <- err
			return
		}
		Sessions[id] = sess
		done <- nil
	}
	return <-done
}

func getSessionCopy(id string) (*Session, error) {
	done := make(chan struct {
		sess *Session
		err  error
	})
	SessionMux <- func() {
		sess, exists := Sessions[id]
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

// ---- Refactored Public Functions ----

// GetSession returns a copy of the session if it exists
func GetSession(id string) (*Session, error) {
	return getSessionCopy(id)
}

// GetActiveSessionIDs returns all non-expired session IDs
func GetActiveSessionIDs() ([]string, error) {
	done := make(chan struct {
		ids []string
		err error
	})
	SessionMux <- func() {
		now := time.Now()
		var active []string
		for id, s := range Sessions {
			if s.ExpiresAt.After(now) {
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

// SetSessionPrivileged sets or unsets privilege on a session
func SetSessionPrivileged(id string, privileged bool) error {
	return updateSession(id, func(s *Session) error {
		s.Privileged = privileged
		return nil
	})
}

// IsSessionPrivileged returns whether the session is privileged
func IsSessionPrivileged(id string) (bool, error) {
	var result bool
	err := withSession(id, func(s Session) error {
		result = s.Privileged
		return nil
	})
	return result, err
}

// IsSessionValid returns true if session exists and hasn't expired
func IsSessionValid(id string) (bool, error) {
	var valid bool
	err := withSession(id, func(s Session) error {
		if s.ExpiresAt.Before(time.Now()) {
			return fmt.Errorf("session expired")
		}
		valid = true
		return nil
	})
	return valid, err
}

// ValidateSessionFromRequest validates a session cookie and returns the session
func ValidateSessionFromRequest(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		return nil, fmt.Errorf("missing or invalid session_id cookie")
	}

	sess, err := getSessionCopy(cookie.Value)
	if err != nil {
		logger.Warnf("Access attempt with unknown session_id: %s", cookie.Value)
		return nil, fmt.Errorf("unknown session ID")
	}

	if sess.ExpiresAt.Before(time.Now()) {
		logger.Warnf("Expired session access attempt by user '%s'", sess.User.ID)
		return nil, fmt.Errorf("session expired")
	}

	return sess, nil
}

// GetSessionOrAbort aborts the request if the session is not valid
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
