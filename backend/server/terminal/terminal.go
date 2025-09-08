package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
)

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
	ContainerID string
}

var (
	sessions     = make(map[string]*TerminalSession)      // main shell only
	containerMap = make(map[TerminalKey]*TerminalSession) // multi-terminal support
	sessionsMu   sync.Mutex
)

func StartTerminal(sess *session.Session) error {
	sessionsMu.Lock()
	if _, exists := sessions[sess.SessionID]; exists {
		sessionsMu.Unlock()
		logger.Warnf("Terminal already exists for session: %s", sess.SessionID)
		return errors.New("terminal already exists for session")
	}
	sessionsMu.Unlock()

	u, err := user.LookupId(sess.User.UID)
	if err != nil {
		logger.Errorf("Could not lookup user %s: %v", sess.User.Username, err)
		return fmt.Errorf("could not lookup user %s: %w", sess.User.Username, err)
	}
	userHome := u.HomeDir

	env := append(os.Environ(),
		"HISTFILE="+userHome+"/.bash_history",
	)

	cmd := exec.Command("bash", "-i", "-l")
	cmd.Dir = userHome
	cmd.Env = env
	// Put the shell in its own session so we can signal its process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}

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

	logger.Infof("Started terminal for session %s (user: %s)", sess.SessionID, sess.User.Username)
	return nil
}

func Get(sessionID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	return sessions[sessionID]
}

// CloseAllForSession closes main + all container terminals for a session.
func CloseAllForSession(sessionID string) {
	_ = Close(sessionID)
	// close all container terminals for this session
	sessionsMu.Lock()
	var keys []TerminalKey
	for k := range containerMap {
		if k.SessionID == sessionID {
			keys = append(keys, k)
		}
	}
	sessionsMu.Unlock()
	for _, k := range keys {
		_ = CloseContainerTerminal(k.SessionID, k.ContainerID)
	}
}

// Close cleans up main terminal deterministically.
func Close(sessionID string) error {
	sessionsMu.Lock()
	ts := sessions[sessionID]
	if ts == nil {
		sessionsMu.Unlock()
		logger.Debugf("Close: No terminal found for session %s", sessionID)
		return fmt.Errorf("no terminal found for session %s", sessionID)
	}
	delete(sessions, sessionID)
	sessionsMu.Unlock()

	ts.Mu.Lock()
	defer ts.Mu.Unlock()

	var errs []error
	// Mark closed early to prevent concurrent ops
	ts.Open = false

	// 1) Ask the shell nicely: SIGHUP to process group
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		// Negative pid => signal process group
		_ = syscall.Kill(-ts.Cmd.Process.Pid, syscall.SIGHUP)
	}

	// 2) Close PTY write side (closing file is fine; readers will see EIO/EBADF)
	if ts.PTY != nil {
		if err := ts.PTY.Close(); err != nil {
			// EBADF after double-close is expected during races; keep at Debug
			if !isExpectedFileClosed(err) {
				logger.Warnf("Close: failed to close PTY for session %s: %v", sessionID, err)
				errs = append(errs, fmt.Errorf("failed to close PTY: %w", err))
			} else {
				logger.Debugf("Close: PTY already closed for session %s", sessionID)
			}
		} else {
			logger.Debugf("Close: PTY closed for session %s", sessionID)
		}
	}

	// 3) Wait briefly for graceful exit; if still alive, Kill
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- ts.Cmd.Wait() }()
		select {
		case err := <-done:
			// If err is from signal, treat as normal
			if err != nil && !isExpectedWaitError(err) {
				logger.Warnf("Close: wait error for session %s: %v", sessionID, err)
				errs = append(errs, fmt.Errorf("failed to wait for Cmd: %w", err))
			} else {
				logger.Debugf("Close: Cmd exited for session %s", sessionID)
			}
		case <-time.After(750 * time.Millisecond):
			// Still running, force kill
			_ = ts.Cmd.Process.Kill()
			if err := <-done; err != nil && !isExpectedWaitError(err) {
				logger.Warnf("Close: forced kill wait error for session %s: %v", sessionID, err)
				errs = append(errs, fmt.Errorf("forced kill wait: %w", err))
			} else {
				logger.Debugf("Close: process killed for session %s", sessionID)
			}
		}
	}

	if len(errs) > 0 {
		logger.Warnf("Close: terminal cleanup for session %s had errors: %v", sessionID, errs)
		return fmt.Errorf("terminal cleanup for session %s had errors: %v", sessionID, errs)
	}
	logger.Infof("Closed terminal for session %s", sessionID)
	return nil
}

func isExpectedWaitError(err error) bool {
	// Expected when we sent SIGHUP or Kill
	msg := err.Error()
	return strings.Contains(msg, "signal: hangup") ||
		strings.Contains(msg, "signal: terminated") ||
		strings.Contains(msg, "signal: killed") ||
		strings.Contains(strings.ToLower(msg), "status 129") || // HUP on some systems
		strings.Contains(strings.ToLower(msg), "status 137") // KILL on some systems
}

func isExpectedFileClosed(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "bad file descriptor") ||
		strings.Contains(s, "file already closed") ||
		strings.Contains(s, "input/output error")
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
	// Make it its own session so signals reach the group (docker exec + shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}
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
	// Mark closed early
	ts.Open = false

	// 1) Ask nicely: HUP the process group (if still around)
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		_ = syscall.Kill(-ts.Cmd.Process.Pid, syscall.SIGHUP)
	}

	// 2) Close PTY (tolerate "already closed"/EIO/EBADF)
	if ts.PTY != nil {
		if err := ts.PTY.Close(); err != nil {
			if !isExpectedFileClosed(err) {
				logger.Warnf("CloseContainerTerminal: failed to close PTY for (session=%s, container=%s): %v", sessionID, containerID, err)
				errs = append(errs, fmt.Errorf("failed to close PTY: %w", err))
			} else {
				logger.Debugf("CloseContainerTerminal: PTY already closed for (session=%s, container=%s)", sessionID, containerID)
			}
		} else {
			logger.Debugf("CloseContainerTerminal: PTY closed for (session=%s, container=%s)", sessionID, containerID)
		}
	}

	// 3) Wait briefly; Kill if needed
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- ts.Cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil && !isExpectedWaitError(err) {
				logger.Warnf("CloseContainerTerminal: wait error (session=%s, container=%s): %v", sessionID, containerID, err)
				errs = append(errs, fmt.Errorf("failed to wait for Cmd: %w", err))
			} else {
				logger.Debugf("CloseContainerTerminal: Cmd exited (session=%s, container=%s)", sessionID, containerID)
			}
		case <-time.After(750 * time.Millisecond):
			_ = ts.Cmd.Process.Kill()
			if err := <-done; err != nil && !isExpectedWaitError(err) {
				logger.Warnf("CloseContainerTerminal: forced kill wait error (session=%s, container=%s): %v", sessionID, containerID, err)
				errs = append(errs, fmt.Errorf("forced kill wait: %w", err))
			} else {
				logger.Debugf("CloseContainerTerminal: process killed (session=%s, container=%s)", sessionID, containerID)
			}
		}
	}

	if len(errs) > 0 {
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
