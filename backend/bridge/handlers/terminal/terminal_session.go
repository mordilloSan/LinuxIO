package terminal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// TerminalSession manages a direct PTY-to-duplex connection.
type TerminalSession struct {
	PTY      *os.File
	Cmd      *exec.Cmd
	Stream   net.Conn // yamux stream
	mu       sync.Mutex
	closed   bool
	doneChan chan struct{}
}

// HandleTerminalSession handles a yamux stream dedicated to terminal I/O.
// This bypasses result/progress JSON encoding after the JSON request envelope opens the stream.
func HandleTerminalSession(ctx context.Context, rt runtime.Runtime, stream net.Conn, req apischema.TerminalOpenRequest) error {
	sess := rt.Session
	slog.Debug("starting terminal stream", "user", sess.User.Username, "cols", req.Cols, "rows", req.Rows)

	// Parse initial size from args (cols, rows)
	cols, rows := 120, 32
	if req.Cols > 0 {
		cols = req.Cols
	}
	if req.Rows > 0 {
		rows = req.Rows
	}

	// Look up user for environment setup
	u, err := user.LookupId(strconv.FormatUint(uint64(sess.User.UID), 10))
	if err != nil {
		slog.Error("failed to look up terminal user", "user", sess.User.Username, "error", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			// streamID 1 for terminal
			slog.Debug("failed to write terminal stream close frame", "error", closeErr)
		}
		return fmt.Errorf("lookup user: %w", err)
	}

	// Build environment
	env := append(os.Environ(),
		"HOME="+u.HomeDir,
		"USER="+sess.User.Username,
		"LOGNAME="+sess.User.Username,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HISTFILE="+u.HomeDir+"/.bash_history",
	)

	// Prefer bash, fall back to sh
	shellPath := "bash"
	if _, lookErr := exec.LookPath(shellPath); lookErr != nil {
		shellPath = "sh"
	}

	cmd := exec.CommandContext(ctx, shellPath, "-i", "-l")
	cmd.Dir = u.HomeDir
	cmd.Env = append(env, "SHELL="+shellPath)

	sysAttr := &syscall.SysProcAttr{Setsid: true, Setctty: true}

	// Drop privileges if running as root
	if os.Geteuid() == 0 {
		sysAttr.Credential = &syscall.Credential{
			Uid: sess.User.UID,
			Gid: sess.User.GID,
		}
	}
	cmd.SysProcAttr = sysAttr

	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Error("failed to start terminal PTY", "user", sess.User.Username, "error", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			slog.Debug("failed to write terminal stream close frame", "error", closeErr)
		}
		return err
	}

	// Set initial size
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: safeUint16(cols),
		Rows: safeUint16(rows),
	}); err != nil {
		slog.Debug("failed to set initial PTY size", "cols", cols, "rows", rows, "error", err)
	}

	sts := &TerminalSession{
		PTY:      ptmx,
		Cmd:      cmd,
		Stream:   stream,
		doneChan: make(chan struct{}),
	}
	go func() {
		<-ctx.Done()
		sts.cleanup()
	}()

	// Start bidirectional relay
	var wg sync.WaitGroup

	// PTY → Stream (output)
	wg.Go(func() {
		sts.relayPTYToStream()
	})

	// Stream → PTY (input)
	wg.Go(func() {
		sts.relayStreamToPTY()
	})

	wg.Wait()
	sts.cleanup()
	return nil
}

// relayPTYToStream reads PTY output and sends it as stream data frames.
func (sts *TerminalSession) relayPTYToStream() {
	buf := make([]byte, 4096)
	for {
		n, err := sts.PTY.Read(buf)
		if n > 0 {
			frame := &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 1, // Terminal always uses streamID 1
				Payload:  buf[:n],
			}
			if werr := ipc.WriteRelayFrame(sts.Stream, frame); werr != nil {
				return
			}
		}
		if err != nil {
			// Send close frame
			if closeErr := ipc.WriteStreamClose(sts.Stream, 1); closeErr != nil {
				slog.Debug("failed to write terminal stream close frame", "error", closeErr)
			}
			return
		}
	}
}

// relayStreamToPTY reads stream frames and writes input to PTY.
func (sts *TerminalSession) relayStreamToPTY() {
	for {
		frame, err := ipc.ReadRelayFrame(sts.Stream)
		if err != nil {
			return
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			// Write input to PTY
			if len(frame.Payload) > 0 {
				if _, werr := sts.PTY.Write(frame.Payload); werr != nil {
					return
				}
			}

		case ipc.OpStreamClose:
			return

		case ipc.OpStreamResize:
			// Payload: [cols:2][rows:2]
			if len(frame.Payload) >= 4 {
				cols := binary.BigEndian.Uint16(frame.Payload[0:2])
				rows := binary.BigEndian.Uint16(frame.Payload[2:4])
				if err := pty.Setsize(sts.PTY, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
					slog.Debug("failed to resize terminal PTY", "cols", cols, "rows", rows, "error", err)
				}
			}

		default:
			// Unknown opcode - ignore
		}
	}
}

func (sts *TerminalSession) cleanup() {
	sts.mu.Lock()
	if sts.closed {
		sts.mu.Unlock()
		return
	}
	sts.closed = true
	sts.mu.Unlock()

	// Signal PTY process to terminate
	if sts.Cmd != nil && sts.Cmd.Process != nil {
		if err := syscall.Kill(-sts.Cmd.Process.Pid, syscall.SIGHUP); err != nil {
			slog.Debug("failed to send SIGHUP to terminal process group", "error", err)
		}
	}

	// Close PTY
	if sts.PTY != nil {
		if err := sts.PTY.Close(); err != nil {
			slog.Debug("failed to close terminal PTY", "error", err)
		}
	}

	// Close stream
	if sts.Stream != nil {
		if err := sts.Stream.Close(); err != nil {
			slog.Debug("failed to close terminal stream", "error", err)
		}
	}

	// Wait for process
	if sts.Cmd != nil {
		if err := sts.Cmd.Wait(); err != nil {
			slog.Debug("terminal process exited with error", "error", err)
		}
	}
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

// HandleContainerTerminalSession handles a yamux stream for container terminal I/O.
func HandleContainerTerminalSession(parent context.Context, rt runtime.Runtime, stream net.Conn, req apischema.ContainerOpenRequest) error {
	sess := rt.Session
	if req.ContainerID == "" || req.Shell == "" {
		err := fmt.Errorf("missing containerID or shell")
		slog.Error("invalid container terminal request", "error", err)
		writeContainerTerminalClose(stream)
		return err
	}
	cols, rows := 120, 32
	if req.Cols > 0 {
		cols = req.Cols
	}
	if req.Rows > 0 {
		rows = req.Rows
	}
	slog.Debug("starting container terminal stream", "container", req.ContainerID, "shell", req.Shell, "user", sess.User.Username)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		slog.Error("container terminal docker client error", "error", err)
		writeContainerTerminalClose(stream)
		return err
	}
	defer cli.Close()

	execResp, err := createContainerExec(ctx, cli, req.ContainerID, req.Shell, cols, rows)
	if err != nil {
		slog.Error("container terminal exec create failed", "container", req.ContainerID, "shell", req.Shell, "error", err)
		writeContainerTerminalClose(stream)
		return err
	}

	attachResp, err := attachContainerExec(ctx, cli, execResp.ID, cols, rows)
	if err != nil {
		slog.Error("container terminal exec attach failed", "container", req.ContainerID, "exec_id", execResp.ID, "error", err)
		writeContainerTerminalClose(stream)
		return err
	}
	defer attachResp.Close()

	resizeContainerExec(ctx, cli, execResp.ID, cols, rows)

	done := make(chan error, 2)
	go streamContainerExecOutput(attachResp.Reader, stream, done)
	go streamContainerExecInput(ctx, cli, execResp.ID, attachResp.Conn, stream, done)

	return waitForContainerTerminalEnd(ctx, cancel, attachResp.Close, stream, done)
}

func createContainerExec(ctx context.Context, cli *client.Client, containerID, shell string, cols, rows int) (client.ExecCreateResult, error) {
	consoleSize := client.ConsoleSize{Height: uint(rows), Width: uint(cols)}
	return cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		TTY:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"TERM=xterm-256color"},
		ConsoleSize:  consoleSize,
		Cmd:          containerShellArgs(shell),
	})
}

func attachContainerExec(ctx context.Context, cli *client.Client, execID string, cols, rows int) (client.HijackedResponse, error) {
	consoleSize := client.ConsoleSize{Height: uint(rows), Width: uint(cols)}
	result, err := cli.ExecAttach(ctx, execID, client.ExecAttachOptions{
		TTY:         true,
		ConsoleSize: consoleSize,
	})
	return result.HijackedResponse, err
}

func containerShellArgs(shell string) []string {
	if shell == "bash" {
		return []string{"bash", "-il"}
	}
	return []string{shell, "-i"}
}

func resizeContainerExec(ctx context.Context, cli *client.Client, execID string, cols, rows int) {
	if _, err := cli.ExecResize(ctx, execID, client.ExecResizeOptions{
		Width:  uint(cols),
		Height: uint(rows),
	}); err != nil {
		slog.Debug("failed to resize container exec tty", "exec_id", execID, "cols", cols, "rows", rows, "error", err)
	}
}

func streamContainerExecOutput(reader io.Reader, stream net.Conn, done chan<- error) {
	buf := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			frame := &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 1,
				Payload:  payload,
			}
			if writeErr := ipc.WriteRelayFrame(stream, frame); writeErr != nil {
				done <- writeErr
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				done <- nil
			} else {
				done <- readErr
			}
			return
		}
	}
}

func streamContainerExecInput(ctx context.Context, cli *client.Client, execID string, conn net.Conn, stream net.Conn, done chan<- error) {
	for {
		frame, readErr := ipc.ReadRelayFrame(stream)
		if readErr != nil {
			done <- readErr
			return
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) == 0 {
				continue
			}
			if _, writeErr := conn.Write(frame.Payload); writeErr != nil {
				done <- writeErr
				return
			}
		case ipc.OpStreamResize:
			cols, rows, ok := parseResizePayload(frame.Payload)
			if !ok {
				continue
			}
			resizeContainerExec(ctx, cli, execID, cols, rows)
		case ipc.OpStreamClose:
			done <- nil
			return
		}
	}
}

func parseResizePayload(payload []byte) (int, int, bool) {
	if len(payload) < 4 {
		return 0, 0, false
	}
	return int(binary.BigEndian.Uint16(payload[0:2])), int(binary.BigEndian.Uint16(payload[2:4])), true
}

func waitForContainerTerminalEnd(ctx context.Context, cancel context.CancelFunc, closeAttach func(), stream net.Conn, done <-chan error) error {
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	cancel()
	if closeAttach != nil {
		closeAttach()
	}
	writeContainerTerminalClose(stream)
	select {
	case secondErr := <-done:
		if secondErr != nil && !errors.Is(secondErr, io.EOF) && !errors.Is(secondErr, net.ErrClosed) {
			slog.Debug("container terminal ended with secondary error", "error", secondErr)
		}
	case <-time.After(100 * time.Millisecond):
	}
	if err != nil && !errors.Is(err, io.EOF) {
		slog.Debug("container terminal ended with error", "error", err)
	}
	return nil
}

func writeContainerTerminalClose(stream net.Conn) {
	if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
		slog.Debug("failed to write container terminal stream close frame", "error", closeErr)
	}
}
