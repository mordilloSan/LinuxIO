package logs

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

const StreamTypeGeneralLogs = "general-logs"

// HandleGeneralLogsStream streams general journal logs in real-time.
// Args: [lines] where lines is the number of initial lines (default "100")
func HandleGeneralLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	lines := "100"
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		lines = strings.TrimSpace(args[0])
	}

	logger.Debugf("[GeneralLogs] Starting stream lines=%s", lines)

	// Create a context that we can cancel when the stream closes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run journalctl with -f (follow) mode
	// -n <lines>: show last N lines initially
	// -f: follow (stream new logs)
	// --no-pager: don't use a pager
	// -o short-iso: use a compact timestamp format
	cmd := exec.CommandContext(ctx, "journalctl", "-n", lines, "-f", "--no-pager", "-o", "short-iso")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Errorf("[GeneralLogs] failed to create stdout pipe: %v", err)
		sendStreamClose(stream)
		return err
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("[GeneralLogs] failed to start journalctl: %v", err)
		sendStreamClose(stream)
		return err
	}

	// Monitor for client disconnect in background
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			logger.Debugf("[GeneralLogs] client closed stream")
			cancel()
		}
	}()

	// Stream journalctl output to client
	reader := bufio.NewReader(stdout)
	for {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			logger.Debugf("[GeneralLogs] context cancelled, stopping stream")
			_ = cmd.Process.Kill()
			sendStreamClose(stream)
			return nil
		default:
		}

		// Read a line from journalctl
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF || errors.Is(err, context.Canceled) {
				break
			}
			logger.Debugf("[GeneralLogs] read error: %v", err)
			break
		}

		// Send to client as stream data
		frame := &ipc.StreamFrame{
			Opcode:   ipc.OpStreamData,
			StreamID: 1,
			Payload:  []byte(line),
		}
		if err := ipc.WriteRelayFrame(stream, frame); err != nil {
			logger.Debugf("[GeneralLogs] write to stream failed: %v", err)
			break
		}
	}

	// Wait for command to finish
	_ = cmd.Wait()

	sendStreamClose(stream)
	logger.Infof("[GeneralLogs] Stream closed")
	return nil
}
