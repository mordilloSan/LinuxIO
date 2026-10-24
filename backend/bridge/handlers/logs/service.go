package logs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
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

	args := []string{"-u", serviceName, "-n", lines, "-f", "--no-pager", "-o", "short-iso"}
	err = followJournal(ctx, stream, args, func(line string) string { return line })
	if err != nil && !errors.Is(err, errJournalFollowEnded) {
		return writeLogErrorUnlessCanceled(ctx, stream, err)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

func parseServiceLogsRequest(req apischema.ServiceLogsFollowRequest) (string, string, error) {
	serviceName := strings.TrimSpace(req.ServiceName)
	if serviceName == "" {
		return "", "", fmt.Errorf("%w: service name", bridgeipc.ErrInvalidArgs)
	}
	if strings.Contains(serviceName, "@.") {
		err := fmt.Errorf("template unit %s does not have logs until instantiated", serviceName)
		slog.Debug("service log request rejected for template unit",
			"component", "logs",
			"route", streamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		return "", "", fmt.Errorf("%w: logs are unavailable for template unit files, select an instantiated unit instead", bridgeipc.ErrInvalidArgs)
	}
	lines := "100"
	if req.Lines != nil && *req.Lines != "" {
		lines = *req.Lines
	}
	return serviceName, lines, nil
}
