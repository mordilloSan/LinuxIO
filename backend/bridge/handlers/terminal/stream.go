package terminal

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/mordilloSan/go_logger/v2/logger"

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
		sendStreamClose(stream, 1) // streamID 1 for terminal
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
		sendStreamClose(stream, 1)
		return err
	}

	// Set initial size
	_ = pty.Setsize(ptmx, &pty.Winsize{
		Cols: safeUint16(cols),
		Rows: safeUint16(rows),
	})

	sts := &StreamTerminalSession{
		PTY:      ptmx,
		Cmd:      cmd,
		Stream:   stream,
		doneChan: make(chan struct{}),
	}

	// Start bidirectional relay
	var wg sync.WaitGroup
	wg.Add(2)

	// PTY → Stream (output)
	go func() {
		defer wg.Done()
		sts.relayPTYToStream()
	}()

	// Stream → PTY (input)
	go func() {
		defer wg.Done()
		sts.relayStreamToPTY()
	}()

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
			sendStreamClose(sts.Stream, 1)
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
				_ = pty.Setsize(sts.PTY, &pty.Winsize{Cols: cols, Rows: rows})
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
		_ = syscall.Kill(-sts.Cmd.Process.Pid, syscall.SIGHUP)
	}

	// Close PTY
	if sts.PTY != nil {
		_ = sts.PTY.Close()
	}

	// Close stream
	if sts.Stream != nil {
		_ = sts.Stream.Close()
	}

	// Wait for process
	if sts.Cmd != nil {
		_ = sts.Cmd.Wait()
	}
}

func sendStreamClose(stream net.Conn, streamID uint32) {
	frame := &ipc.StreamFrame{
		Opcode:   ipc.OpStreamClose,
		StreamID: streamID,
	}
	_ = ipc.WriteRelayFrame(stream, frame)
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
		sendStreamClose(stream, 1)
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
		shellArgs = []string{"exec", "-it", "-e", "TERM=xterm-256color", containerID, "bash", "-il"}
	default:
		shellArgs = []string{"exec", "-it", "-e", "TERM=xterm-256color", containerID, shell, "-i"}
	}

	cmd := exec.Command("docker", shellArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		logger.Errorf("[ContainerTerminal] pty start failed: %v", err)
		sendStreamClose(stream, 1)
		return err
	}

	_ = pty.Setsize(ptmx, &pty.Winsize{
		Cols: safeUint16(cols),
		Rows: safeUint16(rows),
	})

	sts := &StreamTerminalSession{
		PTY:      ptmx,
		Cmd:      cmd,
		Stream:   stream,
		doneChan: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sts.relayPTYToStream()
	}()

	go func() {
		defer wg.Done()
		sts.relayStreamToPTY()
	}()

	wg.Wait()
	sts.cleanup()
	return nil
}
