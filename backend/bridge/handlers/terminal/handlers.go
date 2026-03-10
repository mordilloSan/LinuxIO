package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/creack/pty"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(sess *session.Session) {
	// Terminal - bidirectional PTY stream
	ipc.Register("terminal", "bash", &terminalHandler{sess: sess})
	ipc.Register("terminal", "sh", &terminalHandler{sess: sess, shell: "sh"})

	// Simple JSON handlers
	ipc.RegisterFunc("terminal", "list_shells", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return emit.Result([]string{})
		}
		shells, err := ListContainerShells(args[0])
		if err != nil {
			return err
		}
		return emit.Result(shells)
	})
}

// terminalHandler implements BidirectionalHandler for terminal PTY sessions
type terminalHandler struct {
	sess  *session.Session
	shell string // Optional shell override ("bash", "sh", etc.)
}

func (h *terminalHandler) Execute(ctx context.Context, args []string, emit ipc.Events) error {
	return fmt.Errorf("terminal requires bidirectional stream")
}

func (h *terminalHandler) ExecuteWithInput(ctx context.Context, args []string, emit ipc.Events, input <-chan []byte) error {
	cols, rows := parseTerminalSize(args)
	logger.Debugf("[Terminal] Starting for user=%s size=%dx%d", h.sess.User.Username, cols, rows)

	terminalUser, err := lookupTerminalUser(h.sess)
	if err != nil {
		return err
	}
	shellPath := resolveShellPath(h.shell)
	cmd, ptmx, err := startTerminalCommand(h.sess, terminalUser, shellPath)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	setTerminalSize(ptmx, cols, rows)

	logger.Infof("[Terminal] Started for user=%s pid=%d", h.sess.User.Username, cmd.Process.Pid)

	resizeChan, _ := ipc.ResizeChannel(ctx)
	done := relayTerminalOutput(ptmx, emit)
	manageTerminalSession(ctx, input, resizeChan, ptmx, cmd, done, h.sess.User.Username)
	exitCode := waitTerminalCommand(cmd)

	logger.Infof("[Terminal] Closed for user=%s exitCode=%d", h.sess.User.Username, exitCode)

	return emit.Result(map[string]any{
		"exit_code": exitCode,
	})
}

func parseTerminalSize(args []string) (int, int) {
	cols, rows := 120, 32
	if len(args) >= 2 {
		if c, err := strconv.Atoi(args[0]); err == nil && c > 0 {
			cols = c
		}
		if r, err := strconv.Atoi(args[1]); err == nil && r > 0 {
			rows = r
		}
	}
	return cols, rows
}

func lookupTerminalUser(sess *session.Session) (*user.User, error) {
	terminalUser, err := user.LookupId(strconv.FormatUint(uint64(sess.User.UID), 10))
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return terminalUser, nil
}

func resolveShellPath(shell string) string {
	shellPath := shell
	if shellPath == "" {
		shellPath = "bash"
	}
	if _, err := exec.LookPath(shellPath); err == nil {
		return shellPath
	}
	return "sh"
}

func startTerminalCommand(
	sess *session.Session,
	terminalUser *user.User,
	shellPath string,
) (*exec.Cmd, *os.File, error) {
	cmd := exec.Command(shellPath, "-i", "-l")
	cmd.Dir = terminalUser.HomeDir
	cmd.Env = buildTerminalEnv(sess, terminalUser, shellPath)
	cmd.SysProcAttr = terminalSysProcAttr(sess)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("pty start failed: %w", err)
	}
	return cmd, ptmx, nil
}

func buildTerminalEnv(sess *session.Session, terminalUser *user.User, shellPath string) []string {
	return append(os.Environ(),
		"HOME="+terminalUser.HomeDir,
		"USER="+sess.User.Username,
		"LOGNAME="+sess.User.Username,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HISTFILE="+terminalUser.HomeDir+"/.bash_history",
		"SHELL="+shellPath,
	)
}

func terminalSysProcAttr(sess *session.Session) *syscall.SysProcAttr {
	sysAttr := &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if os.Geteuid() == 0 {
		sysAttr.Credential = &syscall.Credential{
			Uid: sess.User.UID,
			Gid: sess.User.GID,
		}
	}
	return sysAttr
}

func setTerminalSize(ptmx *os.File, cols, rows int) {
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: safeUint16(cols),
		Rows: safeUint16(rows),
	}); err != nil {
		logger.Debugf("[Terminal] failed to set initial PTY size: %v", err)
	}
}

func relayTerminalOutput(ptmx *os.File, emit ipc.Events) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if emitErr := emit.Data(buf[:n]); emitErr != nil {
					return
				}
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				logger.Errorf("[Terminal] PTY read error: %v", err)
				return
			}
		}
	}()
	return done
}

func manageTerminalSession(
	ctx context.Context,
	input <-chan []byte,
	resizeChan <-chan ipc.ResizeEvent,
	ptmx *os.File,
	cmd *exec.Cmd,
	done <-chan struct{},
	username string,
) {
	currentResizeChan := resizeChan
	for {
		select {
		case chunk, ok := <-input:
			if !ok {
				logger.Debugf("[Terminal] Client closed connection")
				killTerminalProcess(cmd.Process, "[Terminal] failed to kill process after client close")
				return
			}
			if _, err := ptmx.Write(chunk); err != nil {
				logger.Errorf("[Terminal] PTY write error: %v", err)
				return
			}
		case ev, ok := <-currentResizeChan:
			if !ok {
				currentResizeChan = nil
				continue
			}
			logger.Debugf("[Terminal] Resize %dx%d for user=%s", ev.Cols, ev.Rows, username)
			if err := pty.Setsize(ptmx, &pty.Winsize{Cols: ev.Cols, Rows: ev.Rows}); err != nil {
				logger.Debugf("[Terminal] failed to resize PTY: %v", err)
			}
		case <-ctx.Done():
			logger.Debugf("[Terminal] Context cancelled")
			killTerminalProcess(cmd.Process, "[Terminal] failed to kill process on context cancel")
			return
		case <-done:
			logger.Debugf("[Terminal] PTY closed")
			return
		}
	}
}

func killTerminalProcess(proc *os.Process, message string) {
	if err := proc.Kill(); err != nil {
		logger.Debugf("%s: %v", message, err)
	}
}

func waitTerminalCommand(cmd *exec.Cmd) int {
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return exitCode
}

// Use safeUint16 from stream.go (already defined)
