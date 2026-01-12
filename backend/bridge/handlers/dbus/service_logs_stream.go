package dbus

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const StreamTypeServiceLogs = "service-logs"

// RegisterServiceLogsStreamHandler registers the service logs stream handler.
// Called from RegisterStreamHandlers in pkg_update_stream.go
func RegisterServiceLogsStreamHandler(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeServiceLogs] = HandleServiceLogsStream
}

// HandleServiceLogsStream streams service logs from journalctl in real-time.
// Args: [serviceName, lines] where lines is the number of initial lines (default "100")
func HandleServiceLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 1 {
		logger.Errorf("[ServiceLogs] missing service name")
		sendServiceLogsClose(stream)
		return errors.New("missing service name")
	}

	serviceName := strings.TrimSpace(args[0])
	if serviceName == "" {
		logger.Errorf("[ServiceLogs] empty service name")
		sendServiceLogsClose(stream)
		return errors.New("empty service name")
	}

	lines := "100"
	if len(args) >= 2 && args[1] != "" {
		lines = args[1]
	}

	logger.Debugf("[ServiceLogs] Starting stream for service=%s lines=%s", serviceName, lines)

	// Create a context that we can cancel when the stream closes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run journalctl with -f (follow) mode
	// -u <service>: filter by service unit
	// -n <lines>: show last N lines initially
	// -f: follow (stream new logs)
	// --no-pager: don't use a pager
	// -o short-iso: use a compact timestamp format
	cmd := exec.CommandContext(ctx, "journalctl", "-u", serviceName, "-n", lines, "-f", "--no-pager", "-o", "short-iso")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Errorf("[ServiceLogs] failed to create stdout pipe: %v", err)
		sendServiceLogsClose(stream)
		return err
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("[ServiceLogs] failed to start journalctl: %v", err)
		sendServiceLogsClose(stream)
		return err
	}

	// Monitor for client disconnect in background
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			logger.Debugf("[ServiceLogs] client closed stream")
			cancel()
		}
	}()

	// Stream journalctl output to client
	reader := bufio.NewReader(stdout)
	for {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			logger.Debugf("[ServiceLogs] context cancelled, stopping stream")
			_ = cmd.Process.Kill()
			sendServiceLogsClose(stream)
			return nil
		default:
		}

		// Read a line from journalctl
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF || errors.Is(err, context.Canceled) {
				break
			}
			logger.Debugf("[ServiceLogs] read error: %v", err)
			break
		}

		// Send to client as stream data
		frame := &ipc.StreamFrame{
			Opcode:   ipc.OpStreamData,
			StreamID: 1,
			Payload:  []byte(line),
		}
		if err := ipc.WriteRelayFrame(stream, frame); err != nil {
			logger.Debugf("[ServiceLogs] write to stream failed: %v", err)
			break
		}
	}

	// Wait for command to finish
	_ = cmd.Wait()

	sendServiceLogsClose(stream)
	logger.Infof("[ServiceLogs] Stream closed for service=%s", serviceName)
	return nil
}

func sendServiceLogsClose(stream net.Conn) {
	frame := &ipc.StreamFrame{
		Opcode:   ipc.OpStreamClose,
		StreamID: 1,
	}
	_ = ipc.WriteRelayFrame(stream, frame)
}
