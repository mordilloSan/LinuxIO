package terminal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// StreamTerminalSession manages a direct PTY-to-stream connection.
type StreamTerminalSession struct {
	PTY      *os.File
	Cmd      *exec.Cmd
	Stream   net.Conn // yamux stream
	mu       sync.Mutex
	closed   bool
	doneChan chan struct{}
}

// RegisterStreamHandlers registers all terminal stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers["terminal"] = HandleTerminalStream
	handlers["container"] = HandleContainerTerminalStream
}

// HandleTerminalStream handles a yamux stream dedicated to terminal I/O.
// This bypasses all JSON encoding - raw PTY bytes flow directly.
// Protocol:
//   - First frame from client: OpStreamOpen with args [cols, rows]
//   - Subsequent frames from client: OpStreamData with input bytes
//   - Server sends: OpStreamData with PTY output bytes
//   - Either side can send OpStreamClose to terminate
func HandleTerminalStream(sess *session.Session, stream net.Conn, args []string) error {
	logger.Debugf("[StreamTerminal] Starting for user=%s args=%v", sess.User.Username, args)

	// Parse initial size from args (cols, rows)
	cols, rows := 120, 32
	if len(args) >= 2 {
		if c, err := strconv.Atoi(args[0]); err == nil && c > 0 {
			cols = c
		}
		if r, err := strconv.Atoi(args[1]); err == nil && r > 0 {
			rows = r
		}
	}

	// Look up user for environment setup
	u, err := user.LookupId(strconv.FormatUint(uint64(sess.User.UID), 10))
	if err != nil {
		logger.Errorf("[StreamTerminal] lookup user %s failed: %v", sess.User.Username, err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil { // streamID 1 for terminal
			logger.Debugf("[StreamTerminal] failed to write stream close frame: %v", closeErr)
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

	cmd := exec.Command(shellPath, "-i", "-l")
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
		logger.Errorf("[StreamTerminal] pty start failed: %v", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[StreamTerminal] failed to write stream close frame: %v", closeErr)
		}
		return err
	}

	// Set initial size
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: safeUint16(cols),
		Rows: safeUint16(rows),
	}); err != nil {
		logger.Debugf("[StreamTerminal] failed to set initial PTY size: %v", err)
	}

	sts := &StreamTerminalSession{
		PTY:      ptmx,
		Cmd:      cmd,
		Stream:   stream,
		doneChan: make(chan struct{}),
	}

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
func (sts *StreamTerminalSession) relayPTYToStream() {
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
				logger.Debugf("[StreamTerminal] failed to write stream close frame: %v", closeErr)
			}
			return
		}
	}
}

// relayStreamToPTY reads stream frames and writes input to PTY.
func (sts *StreamTerminalSession) relayStreamToPTY() {
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
					logger.Debugf("[StreamTerminal] failed to resize PTY: %v", err)
				}
			}

		default:
			// Unknown opcode - ignore
		}
	}
}

func (sts *StreamTerminalSession) cleanup() {
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
			logger.Debugf("[StreamTerminal] failed to send SIGHUP to process group: %v", err)
		}
	}

	// Close PTY
	if sts.PTY != nil {
		if err := sts.PTY.Close(); err != nil {
			logger.Debugf("[StreamTerminal] failed to close PTY: %v", err)
		}
	}

	// Close stream
	if sts.Stream != nil {
		if err := sts.Stream.Close(); err != nil {
			logger.Debugf("[StreamTerminal] failed to close stream: %v", err)
		}
	}

	// Wait for process
	if sts.Cmd != nil {
		if err := sts.Cmd.Wait(); err != nil {
			logger.Debugf("[StreamTerminal] process exited with error: %v", err)
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

// HandleContainerTerminalStream handles a yamux stream for container terminal I/O.
// Args: [containerID, shell, cols, rows]
func HandleContainerTerminalStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 2 {
		logger.Errorf("[ContainerTerminal] missing containerID or shell")
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[ContainerTerminal] failed to write stream close frame: %v", closeErr)
		}
		return fmt.Errorf("missing containerID or shell")
	}

	containerID := args[0]
	shell := args[1]

	logger.Debugf("[ContainerTerminal] Starting for container=%s shell=%s user=%s", containerID, shell, sess.User.Username)

	cols, rows := 120, 32
	if len(args) >= 4 {
		if c, err := strconv.Atoi(args[2]); err == nil && c > 0 {
			cols = c
		}
		if r, err := strconv.Atoi(args[3]); err == nil && r > 0 {
			rows = r
		}
	}

	var shellArgs []string
	switch shell {
	case "bash":
		shellArgs = []string{"bash", "-il"}
	default:
		shellArgs = []string{shell, "-i"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Errorf("[ContainerTerminal] docker client error: %v", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[ContainerTerminal] failed to write stream close frame: %v", closeErr)
		}
		return err
	}
	defer cli.Close()

	consoleSize := [2]uint{uint(rows), uint(cols)}
	execResp, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"TERM=xterm-256color"},
		ConsoleSize:  &consoleSize,
		Cmd:          shellArgs,
	})
	if err != nil {
		logger.Errorf("[ContainerTerminal] exec create failed: %v", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[ContainerTerminal] failed to write stream close frame: %v", closeErr)
		}
		return err
	}

	attachResp, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty:         true,
		ConsoleSize: &consoleSize,
	})
	if err != nil {
		logger.Errorf("[ContainerTerminal] exec attach failed: %v", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[ContainerTerminal] failed to write stream close frame: %v", closeErr)
		}
		return err
	}
	defer attachResp.Close()

	if resizeErr := cli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
		Width:  uint(cols),
		Height: uint(rows),
	}); resizeErr != nil {
		logger.Debugf("[ContainerTerminal] failed to set initial exec size: %v", resizeErr)
	}

	done := make(chan error, 2)

	// Docker exec output -> stream
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := attachResp.Reader.Read(buf)
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
	}()

	// Stream input/resize -> docker exec
	go func() {
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
				if _, writeErr := attachResp.Conn.Write(frame.Payload); writeErr != nil {
					done <- writeErr
					return
				}
			case ipc.OpStreamResize:
				if len(frame.Payload) < 4 {
					continue
				}
				resizeCols := binary.BigEndian.Uint16(frame.Payload[0:2])
				resizeRows := binary.BigEndian.Uint16(frame.Payload[2:4])
				if resizeErr := cli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
					Width:  uint(resizeCols),
					Height: uint(resizeRows),
				}); resizeErr != nil {
					logger.Debugf("[ContainerTerminal] failed to resize exec tty: %v", resizeErr)
				}
			case ipc.OpStreamClose:
				done <- nil
				return
			}
		}
	}()

	err = <-done
	cancel()
	if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
		logger.Debugf("[ContainerTerminal] failed to write stream close frame: %v", closeErr)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		logger.Debugf("[ContainerTerminal] session ended with error: %v", err)
	}
	return nil
}
