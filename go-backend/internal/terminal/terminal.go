package terminal

import (
	"errors"
	"fmt"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"os"
	"os/exec"
	"os/user"
	"strings"
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
func StartTerminal(sess *session.Session) error {
	sessionsMu.Lock()
	if _, exists := sessions[sess.SessionID]; exists {
		sessionsMu.Unlock()
		logger.Warnf("Terminal already exists for session: %s", sess.SessionID)
		return errors.New("terminal already exists for session")
	}
	sessionsMu.Unlock()

	u, err := user.Lookup(sess.User.Name)
	if err != nil {
		logger.Errorf("Could not lookup user %s: %v", sess.User.Name, err)
		return fmt.Errorf("could not lookup user %s: %w", sess.User.Name, err)
	}
	userHome := u.HomeDir

	env := append(os.Environ(),
		"TERM=xterm-256color",
		"PROMPT_COMMAND=history -a; history -n",
		"HISTCONTROL=ignoredups:erasedups",
		"HISTFILE="+userHome+"/.bash_history",
	)

	cmd := exec.Command("bash", "--noprofile", "--norc", "-i")
	cmd.Dir = userHome
	cmd.Env = env
	// Note: If you need privilege separation, use SysProcAttr as in earlier responses.

	ptmx, err := pty.Start(cmd)
	if err != nil {
		logger.Errorf("Failed to start PTY for session %s: %v", sess.SessionID, err)
		return err
	}

	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 32}); err != nil {
		logger.Warnf("Failed to set initial PTY size: %v", err)
	}

	sessionsMu.Lock()
	sessions[sess.SessionID] = &TerminalSession{
		PTY:  ptmx,
		Cmd:  cmd,
		Open: true,
	}
	sessionsMu.Unlock()

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

// Close cleans up and removes the terminal.
func Close(sessionID string) error {
	sessionsMu.Lock()
	ts := sessions[sessionID]
	if ts == nil {
		sessionsMu.Unlock()
		logger.Infof("Close: No terminal found for session %s", sessionID)
		return fmt.Errorf("no terminal found for session %s", sessionID)
	}
	delete(sessions, sessionID)
	sessionsMu.Unlock()

	ts.Mu.Lock()
	defer ts.Mu.Unlock()

	var errs []error

	if ts.PTY != nil {
		if err := ts.PTY.Close(); err != nil {
			logger.Warnf("Close: failed to close PTY for session %s: %v", sessionID, err)
			errs = append(errs, fmt.Errorf("failed to close PTY: %w", err))
		} else {
			logger.Debugf("Close: PTY closed for session %s", sessionID)
		}
	}

	if ts.Cmd != nil && ts.Open {
		if err := ts.Cmd.Process.Kill(); err != nil {
			logger.Warnf("Close: failed to kill process for session %s: %v", sessionID, err)
			errs = append(errs, fmt.Errorf("failed to kill process: %w", err))
		} else {
			logger.Debugf("Close: process killed for session %s", sessionID)
		}
		if err := ts.Cmd.Wait(); err != nil {
			logger.Warnf("Close: failed to wait for Cmd in session %s: %v", sessionID, err)
			errs = append(errs, fmt.Errorf("failed to wait for Cmd: %w", err))
		} else {
			logger.Debugf("Close: Cmd wait finished for session %s", sessionID)
		}
	}

	ts.Open = false

	if len(errs) > 0 {
		logger.Warnf("Close: terminal cleanup for session %s had errors: %v", sessionID, errs)
		return fmt.Errorf("terminal cleanup for session %s had errors: %v", sessionID, errs)
	}
	logger.Infof("Closed terminal for session %s", sessionID)
	return nil
}

// --- Container terminal logic ---

// StartContainerTerminal launches a shell inside a container.
func StartContainerTerminal(sess *session.Session, containerID, shell string) error {
	sessionsMu.Lock()
	key := TerminalKey{SessionID: sess.SessionID, Target: "container", ContainerID: containerID}
	if _, exists := containerMap[key]; exists {
		sessionsMu.Unlock()
		logger.Warnf("Container terminal already exists for session=%s, container=%s", sess.SessionID, containerID)
		return errors.New("container terminal already exists")
	}
	sessionsMu.Unlock()

	if shell == "" {
		logger.Warnf("StartContainerTerminal: No shell available in container %s", containerID)
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
		logger.Errorf("Failed to start PTY for container terminal: %v", err)
		return err
	}

	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 32}); err != nil {
		logger.Warnf("Failed to set initial PTY size for container terminal: %v", err)
	}
	sessionsMu.Lock()
	containerMap[key] = &TerminalSession{
		PTY:  ptmx,
		Cmd:  cmd,
		Open: true,
	}
	sessionsMu.Unlock()
	logger.Infof("Started container terminal for session=%s, container=%s", sess.SessionID, containerID)
	return nil
}

// GetContainerTerminal returns the TerminalSession for the container terminal.
func GetContainerTerminal(sessionID, containerID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	key := TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}
	return containerMap[key]
}

// CloseContainerTerminal cleans up and removes the container terminal.
func CloseContainerTerminal(sessionID, containerID string) error {
	sessionsMu.Lock()
	key := TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}
	ts := containerMap[key]
	if ts == nil {
		sessionsMu.Unlock()
		logger.Infof("CloseContainerTerminal: No container terminal found for session=%s, container=%s", sessionID, containerID)
		return fmt.Errorf("no container terminal found for session %s and container %s", sessionID, containerID)
	}
	delete(containerMap, key)
	sessionsMu.Unlock()

	ts.Mu.Lock()
	defer ts.Mu.Unlock()

	var errs []error

	if ts.PTY != nil {
		if err := ts.PTY.Close(); err != nil {
			logger.Warnf("CloseContainerTerminal: failed to close PTY for (session=%s, container=%s): %v", sessionID, containerID, err)
			errs = append(errs, fmt.Errorf("failed to close PTY: %w", err))
		} else {
			logger.Debugf("CloseContainerTerminal: PTY closed for (session=%s, container=%s)", sessionID, containerID)
		}
	}

	if ts.Cmd != nil && ts.Open {
		if err := ts.Cmd.Process.Kill(); err != nil {
			logger.Warnf("CloseContainerTerminal: failed to kill process for (session=%s, container=%s): %v", sessionID, containerID, err)
			errs = append(errs, fmt.Errorf("failed to kill process: %w", err))
		} else {
			logger.Debugf("CloseContainerTerminal: process killed for (session=%s, container=%s)", sessionID, containerID)
		}
		err := ts.Cmd.Wait()
		if err != nil {
			if strings.Contains(err.Error(), "signal: killed") {
				logger.Debugf("Process for session %s killed by signal", sessionID)
			} else {
				logger.Warnf("Failed to wait for Cmd in session %s: %v", sessionID, err)
				errs = append(errs, fmt.Errorf("failed to wait for Cmd: %w", err))
			}
		}
	}

	ts.Open = false

	if len(errs) > 0 {
		logger.Warnf("CloseContainerTerminal: cleanup for (session=%s, container=%s) had errors: %v", sessionID, containerID, errs)
		return fmt.Errorf("container terminal cleanup (session=%s, container=%s) had errors: %v", sessionID, containerID, errs)
	}
	logger.Infof("Closed container terminal for session=%s, container=%s", sessionID, containerID)
	return nil
}

func ListContainerShells(containerID string) ([]string, error) {
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
