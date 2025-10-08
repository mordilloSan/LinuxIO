package terminal

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
)

// StartContainerTerminal launches a shell inside a docker container.
func StartContainerTerminal(sess *session.Session, containerID, shell string) error {
	if shell == "" {
		return errors.New("no shell specified")
	}
	if ts := getContainer(sess.SessionID, containerID); ts != nil && ts.PTY != nil && ts.Open {
		logger.Debugf("container terminal already running (session=%s, container=%s)", sess.SessionID, containerID)
		return nil
	}

	var shellArgs []string
	switch shell {
	case "bash":
		shellArgs = []string{"exec", "-it", "-e", "TERM=xterm-256color", containerID, "bash", "-il"}
	default:
		shellArgs = []string{"exec", "-it", "-e", "TERM=xterm-256color", containerID, shell, "-i"}
	}
	cmd := exec.Command("docker", shellArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		logger.Errorf("start container pty failed: %v", err)
		return err
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 32})

	ts := &TerminalSession{PTY: ptmx, Cmd: cmd, Open: true, notify: make(chan struct{}, 1)}
	setContainer(sess.SessionID, containerID, ts)
	go pumpPTY(ts)
	logger.Infof("Started container terminal (session=%s, container=%s, shell=%s)", sess.SessionID, containerID, shell)
	return nil
}

func ListContainerShells(containerID string) ([]string, error) {
	shells := []string{"bash", "sh", "zsh", "ash", "dash"}
	available := []string{}
	for _, sh := range shells {
		cmd := exec.Command("docker", "exec", containerID, "which", sh)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			available = append(available, sh)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no known shell found")
	}
	return available, nil
}

func WriteToContainerTerminal(sessionID, containerID, data string) error {
	ts := getContainer(sessionID, containerID)
	if ts == nil || ts.PTY == nil || !ts.Open {
		return errors.New("container terminal not running")
	}
	_, err := ts.PTY.Write([]byte(data))
	return err
}

func ResizeContainerTerminal(sessionID, containerID string, cols, rows int) error {
	ts := getContainer(sessionID, containerID)
	if ts == nil || ts.PTY == nil || !ts.Open {
		return errors.New("container terminal not running")
	}

	// Validate terminal dimensions to prevent integer overflow
	if cols < 0 || cols > 65535 {
		return errors.New("invalid terminal width: must be between 0 and 65535")
	}
	if rows < 0 || rows > 65535 {
		return errors.New("invalid terminal height: must be between 0 and 65535")
	}

	return pty.Setsize(ts.PTY, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func ReadContainerTerminal(sessionID, containerID string, waitMs int) (string, bool, error) {
	ts := getContainer(sessionID, containerID)
	if ts == nil {
		return "", true, errors.New("container terminal not running")
	}
	ts.Mu.Lock()
	if len(ts.Buffer) > 0 {
		out := string(ts.Buffer)
		ts.Buffer = ts.Buffer[:0]
		closed := !ts.Open
		ts.Mu.Unlock()
		return out, closed, nil
	}
	closed := !ts.Open
	ts.Mu.Unlock()
	if closed {
		return "", true, nil
	}
	if waitMs <= 0 {
		waitMs = 750
	}
	timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
	select {
	case <-ts.notify:
		timer.Stop()
	case <-timer.C:
	}
	ts.Mu.Lock()
	out := string(ts.Buffer)
	ts.Buffer = ts.Buffer[:0]
	closed = !ts.Open
	ts.Mu.Unlock()
	return out, closed, nil
}

func CloseContainerTerminal(sessionID, containerID string) error {
	ts := delContainer(sessionID, containerID)
	if ts == nil {
		return fmt.Errorf("no container terminal found for session=%s container=%s", sessionID, containerID)
	}
	ts.Mu.Lock()
	ts.Open = false
	ts.Mu.Unlock()

	var errs []error
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		_ = syscall.Kill(-ts.Cmd.Process.Pid, syscall.SIGHUP)
	}
	if ts.PTY != nil {
		if err := ts.PTY.Close(); err != nil && !isExpectedFileClosed(err) {
			errs = append(errs, fmt.Errorf("pty close: %w", err))
		}
	}
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- ts.Cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil && !isExpectedWaitError(err) {
				errs = append(errs, fmt.Errorf("wait: %w", err))
			}
		case <-time.After(750 * time.Millisecond):
			_ = ts.Cmd.Process.Kill()
			if err := <-done; err != nil && !isExpectedWaitError(err) {
				errs = append(errs, fmt.Errorf("kill wait: %w", err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("container terminal cleanup errors: %v", errs)
	}
	return nil
}
