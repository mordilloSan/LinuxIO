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

type TerminalKey struct {
	SessionID   string
	Target      string // "main" or "container"
	ContainerID string // If Target == "container"
}

var (
	sessions     = make(map[string]*TerminalSession)      // main shell only
	containerMap = make(map[TerminalKey]*TerminalSession) // multi-terminal support
	sessionsMu   sync.Mutex
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
		if err := ts.PTY.Close(); err != nil {
			logger.Warnf("failed to close PTY for session %s: %v", sessionID, err)
		}
	}
	if ts.Cmd != nil && ts.Open {
		_ = ts.Cmd.Process.Kill()
		if err := ts.Cmd.Wait(); err != nil {
			logger.Warnf("failed to wait for Cmd in session %s: %v", sessionID, err)
		}
	}
	delete(sessions, sessionID)
	logger.Infof("Closed terminal for session %s", sessionID)
}

// New: StartContainerTerminal
func StartContainerTerminal(sess *session.Session, containerID, shell string) error {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	key := TerminalKey{SessionID: sess.SessionID, Target: "container", ContainerID: containerID}
	if _, exists := containerMap[key]; exists {
		return errors.New("container terminal already exists")
	}
	if shell == "" {
		return errors.New("no shell available in this container")
	}
	var shellArgs []string
	switch shell {
	case "bash":
		shellArgs = []string{"exec", "-it", containerID, "bash", "-il"}
	default:
		shellArgs = []string{"exec", "-it", containerID, shell, "-i"}
	}
	cmd := exec.Command("docker", shellArgs...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	containerMap[key] = &TerminalSession{
		PTY:  ptmx,
		Cmd:  cmd,
		Open: true,
	}
	return nil
}

// New: GetContainerTerminal
func GetContainerTerminal(sessionID, containerID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	key := TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}
	return containerMap[key]
}

// New: CloseContainerTerminal
func CloseContainerTerminal(sessionID, containerID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	key := TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}
	ts := containerMap[key]
	if ts != nil {
		ts.Mu.Lock()
		if ts.PTY != nil {
			_ = ts.PTY.Close()
		}
		if ts.Cmd != nil && ts.Open {
			_ = ts.Cmd.Process.Kill()
			_ = ts.Cmd.Wait()
		}
		ts.Mu.Unlock()
		delete(containerMap, key)
	}
}

func ListContainerShells(containerID string) ([]string, error) {
	// Common shells to check
	shells := []string{"bash", "sh", "zsh", "ash", "dash"}
	available := []string{}
	for _, shell := range shells {
		cmd := exec.Command("docker", "exec", containerID, "which", shell)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			available = append(available, shell)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no known shell found")
	}
	return available, nil
}
