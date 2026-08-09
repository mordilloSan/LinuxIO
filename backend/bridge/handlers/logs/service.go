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

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const streamTypeServiceLogs = "logs.service.follow"

// streamServiceLogsChannel streams service logs from journalctl through a direct channel.
func streamServiceLogsChannel(parent context.Context, stream net.Conn, _ runtime.Runtime, req apischema.ServiceLogsFollowRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()
	serviceName, lines, err := parseServiceLogsRequest(req)
	if err != nil {
		return writeLogError(stream, err)
	}
	slog.Debug("starting service log channel",
		"component", "logs",
		"route", streamTypeServiceLogs,
		"service", serviceName,
		"lines", lines)

	cmd := exec.CommandContext(ctx, "journalctl", "-u", serviceName, "-n", lines, "-f", "--no-pager", "-o", "short-iso")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to create service log channel pipe",
			"component", "logs",
			"route", streamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		return writeLogErrorUnlessCanceled(ctx, stream, err)
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start service log channel",
			"component", "logs",
			"route", streamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		return writeLogErrorUnlessCanceled(ctx, stream, err)
	}

	sentData, readErr := streamServiceLogs(ctx, stream, stdout, cmd)
	if readErr != nil {
		return writeLogErrorUnlessCanceled(ctx, stream, readErr)
	}
	if waitErr := waitForServiceLogsCommand(ctx, cmd, &stderr, sentData); waitErr != nil {
		return writeLogErrorUnlessCanceled(ctx, stream, waitErr)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

func parseServiceLogsRequest(req apischema.ServiceLogsFollowRequest) (string, string, error) {
	serviceName := strings.TrimSpace(req.ServiceName)
	if serviceName == "" {
		slog.Error("[ServiceLogs] empty service name")
		return "", "", bridgeipc.NewError("empty service name", 400)
	}
	if strings.Contains(serviceName, "@.") {
		err := fmt.Errorf("template unit %s does not have logs until instantiated", serviceName)
		slog.Debug("service log request rejected for template unit",
			"component", "logs",
			"route", streamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		return "", "", bridgeipc.NewError(
			"Logs are unavailable for template unit files. Select an instantiated unit instead.",
			400,
		)
	}
	lines := "100"
	if req.Lines != nil && *req.Lines != "" {
		lines = *req.Lines
	}
	return serviceName, lines, nil
}

func handleLogsContextCancellation(ctx context.Context, cmd *exec.Cmd, label string) bool {
	select {
	case <-ctx.Done():
		if killErr := cmd.Process.Kill(); killErr != nil {
			slog.Debug("failed to kill journalctl process",
				"component", "logs",
				"stream_label", label,
				"error", killErr)
		}
		return true
	default:
		return false
	}
}

func streamServiceLogs(ctx context.Context, stream net.Conn, stdout io.Reader, cmd *exec.Cmd) (bool, error) {
	reader := bufio.NewReader(stdout)
	sentData := false
	for {
		if handleLogsContextCancellation(ctx, cmd, "[ServiceLogs]") {
			return sentData, ctx.Err()
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				slog.Debug("service log stream read error",
					"component", "logs",
					"route", streamTypeServiceLogs,
					"error", err)
				return sentData, err
			}
			if ctx.Err() != nil {
				return sentData, ctx.Err()
			}
			return sentData, nil
		}
		sentData = true
		if err := relay.WriteRelayFrame(stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: []byte(line)}); err != nil {
			return sentData, err
		}
	}
}

func waitForServiceLogsCommand(
	ctx context.Context,
	cmd *exec.Cmd,
	stderr *bytes.Buffer,
	sentData bool,
) error {
	if err := cmd.Wait(); err != nil {
		slog.Debug("service log command exited with error",
			"component", "logs",
			"route", streamTypeServiceLogs,
			"error", err)
		if ctx.Err() == nil && !sentData {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = "Failed to load service logs"
			}
			return bridgeipc.NewError(message, 500)
		}
		return err
	}
	return nil
}
