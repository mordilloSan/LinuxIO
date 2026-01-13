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
// Args: [lines, timePeriod, priority, identifier]
// - lines: number of initial lines (default "100")
// - timePeriod: time range like "1h", "24h", "7d" (optional)
// - priority: max priority level 0-7 (optional, empty = all)
// - identifier: filter by SYSLOG_IDENTIFIER (optional, empty = all)
func HandleGeneralLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	lines := "100"
	timePeriod := ""
	priority := ""
	identifier := ""

	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		lines = strings.TrimSpace(args[0])
	}
	if len(args) >= 2 && strings.TrimSpace(args[1]) != "" {
		timePeriod = strings.TrimSpace(args[1])
	}
	if len(args) >= 3 && strings.TrimSpace(args[2]) != "" {
		priority = strings.TrimSpace(args[2])
	}
	if len(args) >= 4 && strings.TrimSpace(args[3]) != "" {
		identifier = strings.TrimSpace(args[3])
	}

	logger.Debugf("[GeneralLogs] Starting stream lines=%s timePeriod=%s priority=%s identifier=%s",
		lines, timePeriod, priority, identifier)

	// Create a context that we can cancel when the stream closes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build journalctl command with filters
	// -n <lines>: show last N lines initially
	// -f: follow (stream new logs)
	// --no-pager: don't use a pager
	// -o json: output in JSON format with metadata including priority
	cmdArgs := []string{"-n", lines, "-f", "--no-pager", "-o", "json"}

	// Add time period filter if specified
	if timePeriod != "" {
		cmdArgs = append(cmdArgs, "--since", timePeriod+" ago")
	}

	// Add priority filter if specified
	if priority != "" {
		cmdArgs = append(cmdArgs, "-p", priority)
	}

	// Add identifier filter if specified
	if identifier != "" {
		cmdArgs = append(cmdArgs, "-t", identifier)
	}

	cmd := exec.CommandContext(ctx, "journalctl", cmdArgs...)

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
