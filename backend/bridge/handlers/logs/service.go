package logs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const StreamTypeServiceLogs = "service-logs"

func writeServiceLogsError(stream net.Conn, message string, code int) {
	if err := ipc.WriteResultErrorAndClose(stream, 0, message, code); err != nil {
		slog.Debug("failed to write service log error frame",
			"component", "logs",
			"stream_type", StreamTypeServiceLogs,
			"error", err)
	}
}

// HandleServiceLogsStream streams service logs from journalctl in real-time.
// Args: [serviceName, lines] where lines is the number of initial lines (default "100")
func HandleServiceLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	serviceName, lines, err := parseServiceLogsArgs(stream, args)
	if err != nil {
		return err
	}
	slog.Debug("starting service log stream",
		"component", "logs",
		"stream_type", StreamTypeServiceLogs,
		"service", serviceName,
		"lines", lines)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "journalctl", "-u", serviceName, "-n", lines, "-f", "--no-pager", "-o", "short-iso")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to create service log stream pipe",
			"component", "logs",
			"stream_type", StreamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		writeServiceLogsError(stream, "Failed to prepare log stream", 500)
		return err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start service log stream",
			"component", "logs",
			"stream_type", StreamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		writeServiceLogsError(stream, "Failed to start log stream", 500)
		return err
	}

	monitorLogsStreamDisconnect(stream, cancel)
	sentData := streamServiceLogs(ctx, stream, stdout, cmd)
	if err := waitForServiceLogsCommand(ctx, cmd, &stderr, stream, sentData); err != nil {
		return err
	}
	closeLogsStream(stream, "[ServiceLogs]")
	return nil
}

func parseServiceLogsArgs(stream net.Conn, args []string) (string, string, error) {
	if len(args) < 1 {
		slog.Error("[ServiceLogs] missing service name")
		writeServiceLogsError(stream, "missing service name", 400)
		return "", "", errors.New("missing service name")
	}
	serviceName := strings.TrimSpace(args[0])
	if serviceName == "" {
		slog.Error("[ServiceLogs] empty service name")
		writeServiceLogsError(stream, "empty service name", 400)
		return "", "", errors.New("empty service name")
	}
	if strings.Contains(serviceName, "@.") {
		err := fmt.Errorf("template unit %s does not have logs until instantiated", serviceName)
		slog.Debug("service log request rejected for template unit",
			"component", "logs",
			"stream_type", StreamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		writeServiceLogsError(
			stream,
			"Logs are unavailable for template unit files. Select an instantiated unit instead.",
			400,
		)
		return "", "", err
	}
	lines := "100"
	if len(args) >= 2 && args[1] != "" {
		lines = args[1]
	}
	return serviceName, lines, nil
}

func streamServiceLogs(ctx context.Context, stream net.Conn, stdout io.Reader, cmd *exec.Cmd) bool {
	reader := bufio.NewReader(stdout)
	sentData := false
	for {
		if handleLogsContextCancellation(ctx, cmd, stream, "[ServiceLogs]") {
			return sentData
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				slog.Debug("service log stream read error",
					"component", "logs",
					"stream_type", StreamTypeServiceLogs,
					"error", err)
			}
			return sentData
		}
		sentData = true
		if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamData,
			StreamID: 1,
			Payload:  []byte(line),
		}); err != nil {
			return sentData
		}
	}
}

func waitForServiceLogsCommand(
	ctx context.Context,
	cmd *exec.Cmd,
	stderr *bytes.Buffer,
	stream net.Conn,
	sentData bool,
) error {
	if err := cmd.Wait(); err != nil {
		slog.Debug("service log command exited with error",
			"component", "logs",
			"stream_type", StreamTypeServiceLogs,
			"error", err)
		if ctx.Err() == nil && !sentData {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = "Failed to load service logs"
			}
			writeServiceLogsError(stream, message, 500)
			return err
		}
	}
	return nil
}
