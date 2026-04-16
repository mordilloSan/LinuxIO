package logs

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const StreamTypeGeneralLogs = "general-logs"

type generalLogsRequest struct {
	lines      string
	timePeriod string
	priority   string
	identifier string
}

// HandleGeneralLogsStream streams general journal logs in real-time.
// Args: [lines, timePeriod, priority, identifier]
// - lines: number of initial lines (default "100")
// - timePeriod: time range like "1h", "24h", "7d" (optional)
// - priority: max priority level 0-7 (optional, empty = all)
// - identifier: filter by SYSLOG_IDENTIFIER (optional, empty = all)
func HandleGeneralLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	req := parseGeneralLogsRequest(args)
	slog.Debug("starting general log stream",
		"component", "logs",
		"stream_type", StreamTypeGeneralLogs,
		"lines", req.lines,
		"time_period", req.timePeriod,
		"priority", req.priority,
		"identifier", req.identifier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd, stdout, err := startGeneralLogsCommand(ctx, req)
	if err != nil {
		slog.Error("failed to create general log stream pipe",
			"component", "logs",
			"stream_type", StreamTypeGeneralLogs,
			"error", err)
		closeLogsStream(stream, "[GeneralLogs]")
		return err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start general log stream",
			"component", "logs",
			"stream_type", StreamTypeGeneralLogs,
			"error", err)
		closeLogsStream(stream, "[GeneralLogs]")
		return err
	}

	monitorLogsStreamDisconnect(stream, cancel)
	streamJournalLines(ctx, stream, stdout, cmd, "[GeneralLogs]")
	waitForLogsCommand(cmd, "[GeneralLogs]")
	closeLogsStream(stream, "[GeneralLogs]")
	return nil
}

func parseGeneralLogsRequest(args []string) generalLogsRequest {
	req := generalLogsRequest{lines: "100"}
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		req.lines = strings.TrimSpace(args[0])
	}
	if len(args) >= 2 && strings.TrimSpace(args[1]) != "" {
		req.timePeriod = strings.TrimSpace(args[1])
	}
	if len(args) >= 3 && strings.TrimSpace(args[2]) != "" {
		req.priority = strings.TrimSpace(args[2])
	}
	if len(args) >= 4 && strings.TrimSpace(args[3]) != "" {
		req.identifier = strings.TrimSpace(args[3])
	}
	return req
}

func startGeneralLogsCommand(ctx context.Context, req generalLogsRequest) (*exec.Cmd, io.ReadCloser, error) {
	cmdArgs := []string{"-n", req.lines, "-f", "--no-pager", "-o", "json"}
	if req.timePeriod != "" {
		cmdArgs = append(cmdArgs, "--since", req.timePeriod+" ago")
	}
	if req.priority != "" {
		cmdArgs = append(cmdArgs, "-p", req.priority)
	}
	if req.identifier != "" {
		cmdArgs = append(cmdArgs, "-t", req.identifier)
	}
	cmd := exec.CommandContext(ctx, "journalctl", cmdArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

func monitorLogsStreamDisconnect(stream net.Conn, cancel context.CancelFunc) {
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			cancel()
		}
	}()
}

func streamJournalLines(ctx context.Context, stream net.Conn, stdout io.Reader, cmd *exec.Cmd, label string) {
	reader := bufio.NewReader(stdout)
	for {
		if handleLogsContextCancellation(ctx, cmd, stream, label) {
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				slog.Debug("log stream read error",
					"component", "logs",
					"stream_label", label,
					"error", err)
			}
			return
		}
		if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamData,
			StreamID: 1,
			Payload:  []byte(line),
		}); err != nil {
			return
		}
	}
}

func handleLogsContextCancellation(ctx context.Context, cmd *exec.Cmd, stream net.Conn, label string) bool {
	select {
	case <-ctx.Done():
		if killErr := cmd.Process.Kill(); killErr != nil {
			slog.Debug("failed to kill journalctl process",
				"component", "logs",
				"stream_label", label,
				"error", killErr)
		}
		closeLogsStream(stream, label)
		return true
	default:
		return false
	}
}

func waitForLogsCommand(cmd *exec.Cmd, label string) {
	if err := cmd.Wait(); err != nil {
		slog.Debug("journalctl exited with error",
			"component", "logs",
			"stream_label", label,
			"error", err)
	}
}

func closeLogsStream(stream net.Conn, label string) {
	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		slog.Debug("failed to write log stream close frame",
			"component", "logs",
			"stream_label", label,
			"error", err)
	}
}
