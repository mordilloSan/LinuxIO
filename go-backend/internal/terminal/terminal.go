package terminal

import (
	"errors"
	"fmt"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"os"
	"os/exec"
	"os/user"
	"sync"

	"github.com/creack/pty"
)

// TerminalSession holds the PTY and process for a user's shell session.
type TerminalSession struct {
	PTY    *os.File
	Cmd    *exec.Cmd
	Mu     sync.Mutex
	Open   bool
	Buffer []byte
}

var (
	sessions   = make(map[string]*TerminalSession)
	sessionsMu sync.Mutex
)

// StartTerminal creates a PTY and shell for the sessionID.
// Returns error if already exists.
func StartTerminal(sess *session.Session) error {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if _, exists := sessions[sess.SessionID]; exists {
		logger.Warnf("Terminal already exists for session: %s", sess.SessionID)
		return errors.New("terminal already exists for session")
	}
	u, err := user.Lookup(sess.User.Name)
	if err != nil {
		logger.Errorf("Could not lookup user %s: %v", sess.User.Name, err)
		return fmt.Errorf("could not lookup user %s: %w", sess.User.Name, err)
	}
	userHome := u.HomeDir

	env := append(os.Environ(),
		"PROMPT_COMMAND=history -a; history -n",
		"HISTCONTROL=ignoredups:erasedups",
		"HISTFILE="+userHome+"/.bash_history",
	)

	cmd := exec.Command("bash", "-i")
	cmd.Dir = userHome
	cmd.Env = env

	ptmx, err := pty.Start(cmd)
	if err != nil {
		logger.Errorf("Failed to start PTY for session %s: %v", sess.SessionID, err)
		return err
	}

	sessions[sess.SessionID] = &TerminalSession{
		PTY:  ptmx,
		Cmd:  cmd,
		Open: true,
	}

	logger.Infof("Started terminal for session %s (user: %s)", sess.SessionID, sess.User.Name)
	return nil
}

// Get returns the TerminalSession for the sessionID, or nil.
func Get(sessionID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	ts := sessions[sessionID]
	if ts == nil {
		logger.Debugf("Get: No terminal found for session %s", sessionID)
	}
	return ts
}

// Close cleans up and removes the session.
func Close(sessionID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	ts := sessions[sessionID]
	if ts == nil {
		logger.Debugf("Close: No terminal found for session %s", sessionID)
		return
	}
	ts.Mu.Lock()
	defer ts.Mu.Unlock()

	if ts.PTY != nil {
		ts.PTY.Close()
	}
	if ts.Cmd != nil && ts.Open {
		_ = ts.Cmd.Process.Kill()
		ts.Cmd.Wait()
	}
	delete(sessions, sessionID)
	logger.Infof("Closed terminal for session %s", sessionID)
}
