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
	serviceName, invocationID, lines, err := parseServiceLogsRequest(req)
	if err != nil {
		return writeLogError(stream, err)
	}
	slog.Debug("starting service log channel",
		"component", "logs",
		"route", streamTypeServiceLogs,
		"service", serviceName,
		"invocation", invocationID,
		"lines", lines)

	err = followJournal(ctx, stream, serviceJournalArgs(serviceName, invocationID, lines), func(line string) string { return line })
	if err != nil && !errors.Is(err, errJournalFollowEnded) {
		return writeLogErrorUnlessCanceled(ctx, stream, err)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

// serviceJournalArgs builds the journalctl arguments for a service stream. With
// an invocation it matches on the journal fields, so only that run's own
// processes show, and passes --all so lines holding control characters are
// shown rather than replaced with a blob placeholder.
func serviceJournalArgs(serviceName, invocationID, lines string) []string {
	var args []string
	if invocationID == "" {
		args = []string{"-u", serviceName}
	} else {
		args = []string{"_SYSTEMD_UNIT=" + serviceName, "_SYSTEMD_INVOCATION_ID=" + invocationID}
	}
	if lines != "all" {
		args = append(args, "-n", lines)
	}
	args = append(args, "-f", "--no-pager")
	if invocationID != "" {
		args = append(args, "--all")
	}
	return append(args, "-o", "short-iso")
}

func parseServiceLogsRequest(req apischema.ServiceLogsFollowRequest) (string, string, string, error) {
	serviceName := strings.TrimSpace(req.ServiceName)
	if serviceName == "" {
		return "", "", "", fmt.Errorf("%w: service name", bridgeipc.ErrInvalidArgs)
	}
	if strings.Contains(serviceName, "@.") {
		err := fmt.Errorf("template unit %s does not have logs until instantiated", serviceName)
		slog.Debug("service log request rejected for template unit",
			"component", "logs",
			"route", streamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		return "", "", "", fmt.Errorf("%w: logs are unavailable for template unit files, select an instantiated unit instead", bridgeipc.ErrInvalidArgs)
	}
	lines := "100"
	if req.Lines != nil && *req.Lines != "" {
		lines = *req.Lines
	}
	if err := validateLines(lines); err != nil {
		return "", "", "", err
	}
	invocationID := ""
	if req.InvocationID != nil {
		invocationID = strings.TrimSpace(*req.InvocationID)
	}
	if invocationID != "" && !invocationIDMatch.MatchString(invocationID) {
		return "", "", "", fmt.Errorf("%w: invocation ID", bridgeipc.ErrInvalidArgs)
	}
	return serviceName, invocationID, lines, nil
}
