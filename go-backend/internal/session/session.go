package session

import (
	"fmt"
	"go-backend/internal/logger"
	"go-backend/internal/utils"
	"net/http"
	"time"
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

// Starts a goroutine that periodically checks for expired sessions
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

// Creates a new session, returns error if already exists
func CreateSession(id string, user utils.User, duration time.Duration, privileged bool) error {
	done := make(chan error)
	secret := utils.GenerateSecretKey(32) // Returns 64 hex chars (32 bytes)
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

// Deletes a session, returns error if not found
func DeleteSession(id string) error {
	done := make(chan error)
	SessionMux <- func() {
		sess, exists := Sessions[id]
		if exists {
			delete(Sessions, id)
			logger.Infof("Deleted session for user '%s'", sess.User.ID)
			done <- nil
		} else {
			done <- fmt.Errorf("session ID '%s' not found", id)
		}
	}
	return <-done
}

// Checks if a session is privileged
func IsSessionPrivileged(sessionID string) (bool, error) {
	done := make(chan struct {
		privileged bool
		err        error
	})
	SessionMux <- func() {
		sess, exists := Sessions[sessionID]
		if exists {
			done <- struct {
				privileged bool
				err        error
			}{sess.Privileged, nil}
		} else {
			done <- struct {
				privileged bool
				err        error
			}{false, fmt.Errorf("session ID '%s' not found", sessionID)}
		}
	}
	result := <-done
	return result.privileged, result.err
}

// ValidateSessionFromRequest validates the session cookie and returns the session pointer and error
func ValidateSessionFromRequest(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		return nil, fmt.Errorf("missing or invalid session_id cookie")
	}

	var sess *Session
	var exists bool
	done := make(chan bool)

	SessionMux <- func() {
		s, ok := Sessions[cookie.Value]
		exists = ok
		if ok {
			copy := s // avoid race
			sess = &copy
		}
		done <- true
	}
	<-done

	if !exists {
		logger.Warnf("Access attempt with unknown session_id: %s", cookie.Value)
		return nil, fmt.Errorf("unknown session ID")
	}

	if sess.ExpiresAt.Before(time.Now()) {
		logger.Warnf("Expired session access attempt by user '%s'", sess.User.ID)
		return nil, fmt.Errorf("session expired")
	}

	return sess, nil
}

// Checks if a session is valid
func IsSessionValid(id string) (bool, error) {
	done := make(chan struct {
		valid bool
		err   error
	})
	SessionMux <- func() {
		session, exists := Sessions[id]
		if exists && session.ExpiresAt.After(time.Now()) {
			done <- struct {
				valid bool
				err   error
			}{true, nil}
		} else if exists {
			done <- struct {
				valid bool
				err   error
			}{false, fmt.Errorf("session expired")}
		} else {
			done <- struct {
				valid bool
				err   error
			}{false, fmt.Errorf("session not found")}
		}
	}
	result := <-done
	return result.valid, result.err
}

// Changes the privileged status of a session, returns error if not found
func SetSessionPrivileged(sessionID string, privileged bool) error {
	done := make(chan error)
	SessionMux <- func() {
		sess, exists := Sessions[sessionID]
		if exists {
			sess.Privileged = privileged
			Sessions[sessionID] = sess
			done <- nil
		} else {
			done <- fmt.Errorf("session ID '%s' not found", sessionID)
		}
	}
	return <-done
}

// Returns a list of all currently valid (non-expired) session IDs
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

// GetSession returns a pointer to the Session struct for the given sessionID, or error if not found.
// WARNING: The returned pointer is to a *copy*; do not modify fields directly!
func GetSession(id string) (*Session, error) {
	done := make(chan struct {
		sess *Session
		err  error
	})
	SessionMux <- func() {
		sess, exists := Sessions[id]
		if exists {
			s := sess // copy to new var to avoid data race
			done <- struct {
				sess *Session
				err  error
			}{&s, nil}
		} else {
			done <- struct {
				sess *Session
				err  error
			}{nil, fmt.Errorf("session not found")}
		}
	}
	result := <-done
	return result.sess, result.err
}
