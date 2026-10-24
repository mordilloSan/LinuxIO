package terminal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"syscall"

	"github.com/creack/pty"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
)

// ptyBufSize keeps terminal output frames small so bursts stay interactive.
const ptyBufSize = 4096

// HandleTerminalSession handles a yamux stream dedicated to terminal I/O.
// This bypasses result/progress JSON encoding after the JSON request envelope opens the stream.
func HandleTerminalSession(ctx context.Context, rt runtime.Runtime, stream net.Conn, req apischema.TerminalOpenRequest) error {
	sess := rt.Session
	slog.Debug("starting terminal stream", "user", sess.User.Username, "cols", req.Cols, "rows", req.Rows)

	cols, rows := initialSize(req.Cols, req.Rows)

	loginAgent, err := lookupLoginAgent(stream, req.Agent)
	if err != nil {
		return err
	}

	shellPath := usercmd.LoginShell()
	argv := []string{shellPath, "-i", "-l"}
	if req.Agent != "" {
		argv = usercmd.LoginShellArgv(loginAgent.Command, loginAgent.Args, req.Args)
	}
	cmd, err := usercmd.Command(ctx, sess.User, argv[0], argv[1:]...)
	if err != nil {
		streamrelay.WriteClose(stream)
		return fmt.Errorf("look up terminal user %s: %w", sess.User.Username, err)
	}
	cmd.Env = append(cmd.Env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HISTFILE="+cmd.Dir+"/.bash_history",
		"SHELL="+shellPath,
	)
	if req.Agent != "" {
		// CLI login flows print the URL and prompt for a code instead of
		// trying to open a browser on the server.
		cmd.Env = append(cmd.Env, "NO_BROWSER=1")
	}
	// usercmd already starts a new session and drops privileges when the
	// bridge runs as root; the PTY must also become the controlling terminal.
	cmd.SysProcAttr.Setctty = true

	ptmx, err := pty.Start(cmd)
	if err != nil {
		streamrelay.WriteClose(stream)
		return fmt.Errorf("start terminal PTY for user %s: %w", sess.User.Username, err)
	}

	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: safeUint16(cols), Rows: safeUint16(rows)}); err != nil {
		slog.Debug("failed to set initial PTY size", "cols", cols, "rows", rows, "error", err)
	}

	// Either relay direction ending (or ctx) tears the session down, which
	// unblocks the other side: a dead stream must not leave the PTY relay
	// stuck in an idle read, and vice versa.
	streamrelay.Run(ctx, stream, streamrelay.Local{
		Reader:  ptmx,
		Writer:  ptmx,
		BufSize: ptyBufSize,
		Resize: func(cols, rows uint16) {
			if err := pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
				slog.Debug("failed to resize terminal PTY", "cols", cols, "rows", rows, "error", err)
			}
		},
		Teardown: func() {
			if cmd.Process != nil {
				if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP); err != nil {
					slog.Debug("failed to send SIGHUP to terminal process group", "error", err)
				}
			}
			if err := ptmx.Close(); err != nil {
				slog.Debug("failed to close terminal PTY", "error", err)
			}
		},
	})

	if err := cmd.Wait(); err != nil {
		slog.Debug("terminal process exited with error", "error", err)
	}
	return nil
}

// initialSize returns the requested terminal size, defaulting unset values.
func initialSize(reqCols, reqRows int) (cols, rows int) {
	cols, rows = 120, 32
	if reqCols > 0 {
		cols = reqCols
	}
	if reqRows > 0 {
		rows = reqRows
	}
	return cols, rows
}

// lookupLoginAgent resolves the requested agent id; an empty id means the
// plain login shell. An unknown id closes the stream and returns an error.
func lookupLoginAgent(stream net.Conn, id string) (acpagents.Agent, error) {
	if id == "" {
		return acpagents.Agent{}, nil
	}
	agent, ok := acpagents.Lookup(id)
	if !ok {
		streamrelay.WriteClose(stream)
		return acpagents.Agent{}, fmt.Errorf("terminal: unknown agent %q", id)
	}
	return agent, nil
}

func safeUint16(val int) uint16 {
	if val < 0 {
		return 0
	}
	if val > 65535 {
		return 65535
	}
	return uint16(val)
}
