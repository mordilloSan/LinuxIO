package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
)

// StartTerminal starts an interactive login shell for the session's user.
func StartTerminal(sess *session.Session) error {
	if ts := getOrNil(sess.SessionID); ts != nil && ts.PTY != nil && ts.Open {
		// already started
		logger.Debugf("terminal already running for session=%s", sess.SessionID)
		return nil
	}

	u, err := user.LookupId(sess.User.UID)
	if err != nil {
		logger.Errorf("lookup user %s failed: %v", sess.User.Username, err)
		return fmt.Errorf("lookup user %s: %w", sess.User.Username, err)
	}
	userHome := u.HomeDir

	// Build a clean, user-oriented environment for the interactive shell.
	// This ensures PS1 shows the correct user and history lands in the user's HOME.
	env := append(os.Environ(),
		"HOME="+userHome,
		"USER="+sess.User.Username,
		"LOGNAME="+sess.User.Username,
		// Encourage colorized output
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HISTFILE="+userHome+"/.bash_history",
	)

	// Prefer bash, fall back to sh if not available.
	shellPath := "bash"
	if _, lookErr := exec.LookPath(shellPath); lookErr != nil {
		shellPath = "sh"
	}

	cmd := exec.Command(shellPath, "-i", "-l")
	cmd.Dir = userHome
	cmd.Env = append(env, "SHELL="+shellPath)

	// Always create a new session and set controlling TTY.
	sysAttr := &syscall.SysProcAttr{Setsid: true, Setctty: true}

	// If the bridge is running as root (privileged session), drop the PTY shell to the session user.
	// This keeps privileged abilities in the bridge for API calls while the interactive shell
	// runs as the actual user (expected username in prompt). Users can still sudo as needed.
	if os.Geteuid() == 0 {
		if uid, uerr := strconv.Atoi(sess.User.UID); uerr == nil {
			if gid, gerr := strconv.Atoi(sess.User.GID); gerr == nil {
				sysAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
			}
		}
	}
	cmd.SysProcAttr = sysAttr

	ptmx, err := pty.Start(cmd)
	if err != nil {
		logger.Errorf("StartTerminal: pty start failed: %v", err)
		return err
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 32})

	ts := &TerminalSession{PTY: ptmx, Cmd: cmd, Open: true, notify: make(chan struct{}, 1)}
	setMain(sess.SessionID, ts)

	go pumpPTY(ts)
	logger.Infof("Started terminal for session=%s user=%s uid=%s", sess.SessionID, sess.User.Username, sess.User.UID)
	return nil
}

// WriteToTerminal writes input data to the user's main terminal.
func WriteToTerminal(sessionID, data string) error {
	ts := getOrNil(sessionID)
	if ts == nil || ts.PTY == nil || !ts.Open {
		return errors.New("terminal not running")
	}
	_, err := ts.PTY.Write([]byte(data))
	return err
}

// ResizeTerminal resizes the user's main terminal PTY.
func ResizeTerminal(sessionID string, cols, rows int) error {
	ts := getOrNil(sessionID)
	if ts == nil || ts.PTY == nil || !ts.Open {
		return errors.New("terminal not running")
	}
	return pty.Setsize(ts.PTY, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// ReadTerminal returns buffered output (draining it). If buffer empty, waits up to waitMs for new data.
// Returns (data, closed, error).
func ReadTerminal(sessionID string, waitMs int) (string, bool, error) {
	ts := getOrNil(sessionID)
	if ts == nil {
		return "", true, errors.New("terminal not running")
	}

	// quick path: immediate data
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

	// wait for notify or timeout
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

// ReadTerminalBacklog returns the retained scrollback for the user's main terminal
// without draining it. Useful when the UI reconnects and needs prior context.
func ReadTerminalBacklog(sessionID string) (string, error) {
	ts := getOrNil(sessionID)
	if ts == nil {
		return "", errors.New("terminal not running")
	}
	return ts.snapshotBacklog(), nil
}

// CloseTerminal terminates the main terminal and cleans up.
func CloseTerminal(sessionID string) error {
	ts := delMain(sessionID)
	if ts == nil {
		return fmt.Errorf("no terminal found for session %s", sessionID)
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
		return fmt.Errorf("terminal cleanup had errors: %v", errs)
	}
	return nil
}

func isExpectedWaitError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "signal: hangup") ||
		strings.Contains(msg, "signal: terminated") ||
		strings.Contains(msg, "signal: killed") ||
		strings.Contains(strings.ToLower(msg), "status 129") ||
		strings.Contains(strings.ToLower(msg), "status 137")
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

func pumpPTY(ts *TerminalSession) {
	buf := make([]byte, 4096)
	for {
		n, err := ts.PTY.Read(buf)
		if n > 0 {
			ts.appendOutput(buf[:n])
		}
		if err != nil {
			ts.Mu.Lock()
			ts.Open = false
			ts.Mu.Unlock()
			return
		}
	}
}
