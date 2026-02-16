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
	// Parse terminal size from args (cols, rows)
	cols, rows := 120, 32
	if len(args) >= 2 {
		if c, err := strconv.Atoi(args[0]); err == nil && c > 0 {
			cols = c
		}
		if r, err := strconv.Atoi(args[1]); err == nil && r > 0 {
			rows = r
		}
	}

	logger.Debugf("[Terminal] Starting for user=%s size=%dx%d", h.sess.User.Username, cols, rows)

	// Look up user for environment
	u, err := user.LookupId(strconv.FormatUint(uint64(h.sess.User.UID), 10))
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}

	// Build environment
	env := append(os.Environ(),
		"HOME="+u.HomeDir,
		"USER="+h.sess.User.Username,
		"LOGNAME="+h.sess.User.Username,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HISTFILE="+u.HomeDir+"/.bash_history",
	)

	// Determine shell
	shellPath := h.shell
	if shellPath == "" {
		shellPath = "bash"
	}
	if _, lookErr := exec.LookPath(shellPath); lookErr != nil {
		shellPath = "sh"
	}

	// Create command
	cmd := exec.Command(shellPath, "-i", "-l")
	cmd.Dir = u.HomeDir
	cmd.Env = append(env, "SHELL="+shellPath)

	sysAttr := &syscall.SysProcAttr{Setsid: true, Setctty: true}

	// Drop privileges if running as root
	if os.Geteuid() == 0 {
		sysAttr.Credential = &syscall.Credential{
			Uid: h.sess.User.UID,
			Gid: h.sess.User.GID,
		}
	}
	cmd.SysProcAttr = sysAttr

	// Start PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start failed: %w", err)
	}
	defer ptmx.Close()

	// Set initial size
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: safeUint16(cols),
		Rows: safeUint16(rows),
	}); err != nil {
		logger.Debugf("[Terminal] failed to set initial PTY size: %v", err)
	}

	logger.Infof("[Terminal] Started for user=%s pid=%d", h.sess.User.Username, cmd.Process.Pid)

	resizeChan, _ := ipc.ResizeChannel(ctx)

	// Start PTY output relay (PTY → client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				// Send PTY output to client
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

	// Process client input (client → PTY)
	for {
		select {
		case chunk, ok := <-input:
			if !ok {
				// Client closed connection
				logger.Debugf("[Terminal] Client closed connection")
				if err := cmd.Process.Kill(); err != nil {
					logger.Debugf("[Terminal] failed to kill process after client close: %v", err)
				}
				goto cleanup
			}

			if _, err := ptmx.Write(chunk); err != nil {
				logger.Errorf("[Terminal] PTY write error: %v", err)
				goto cleanup
			}

		case ev, ok := <-resizeChan:
			if !ok {
				resizeChan = nil
				continue
			}
			logger.Debugf("[Terminal] Resize %dx%d for user=%s", ev.Cols, ev.Rows, h.sess.User.Username)
			if err := pty.Setsize(ptmx, &pty.Winsize{
				Cols: ev.Cols,
				Rows: ev.Rows,
			}); err != nil {
				logger.Debugf("[Terminal] failed to resize PTY: %v", err)
			}

		case <-ctx.Done():
			// Context cancelled
			logger.Debugf("[Terminal] Context cancelled")
			if err := cmd.Process.Kill(); err != nil {
				logger.Debugf("[Terminal] failed to kill process on context cancel: %v", err)
			}
			goto cleanup

		case <-done:
			// PTY closed (command exited)
			logger.Debugf("[Terminal] PTY closed")
			goto cleanup
		}
	}

cleanup:
	// Wait for command to finish
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	logger.Infof("[Terminal] Closed for user=%s exitCode=%d", h.sess.User.Username, exitCode)

	return emit.Result(map[string]any{
		"exit_code": exitCode,
	})
}

// Use safeUint16 from stream.go (already defined)
