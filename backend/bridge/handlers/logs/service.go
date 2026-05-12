package logs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const StreamTypeServiceLogs = "logs.service.follow"

// runServiceLogsJob streams service logs from journalctl through the bridge job lifecycle.
// Args: [serviceName, lines] where lines is the number of initial lines (default "100")
func runServiceLogsJob(ctx context.Context, _ runtime.Runtime, job *bridgeipc.Job, args []string) (any, error) {
	serviceName, lines, err := parseServiceLogsArgs(args)
	if err != nil {
		return nil, err
	}
	slog.Debug("starting service log job",
		"component", "logs",
		"route", StreamTypeServiceLogs,
		"job_id", job.ID(),
		"service", serviceName,
		"lines", lines)

	cmd := exec.CommandContext(ctx, "journalctl", "-u", serviceName, "-n", lines, "-f", "--no-pager", "-o", "short-iso")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to create service log job pipe",
			"component", "logs",
			"route", StreamTypeServiceLogs,
			"job_id", job.ID(),
			"service", serviceName,
			"error", err)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start service log job",
			"component", "logs",
			"route", StreamTypeServiceLogs,
			"job_id", job.ID(),
			"service", serviceName,
			"error", err)
		return nil, err
	}

	sentData, readErr := streamServiceLogs(ctx, job, stdout, cmd)
	if readErr != nil {
		return nil, readErr
	}
	if waitErr := waitForServiceLogsCommand(ctx, cmd, &stderr, sentData); waitErr != nil {
		return nil, waitErr
	}
	return map[string]any{"status": "stopped"}, nil
}

func parseServiceLogsArgs(args []string) (string, string, error) {
	if len(args) < 1 {
		slog.Error("[ServiceLogs] missing service name")
		return "", "", bridgeipc.NewError("missing service name", 400)
	}
	serviceName := strings.TrimSpace(args[0])
	if serviceName == "" {
		slog.Error("[ServiceLogs] empty service name")
		return "", "", bridgeipc.NewError("empty service name", 400)
	}
	if strings.Contains(serviceName, "@.") {
		err := fmt.Errorf("template unit %s does not have logs until instantiated", serviceName)
		slog.Debug("service log request rejected for template unit",
			"component", "logs",
			"route", StreamTypeServiceLogs,
			"service", serviceName,
			"error", err)
		return "", "", bridgeipc.NewError(
			"Logs are unavailable for template unit files. Select an instantiated unit instead.",
			400,
		)
	}
	lines := "100"
	if len(args) >= 2 && args[1] != "" {
		lines = args[1]
	}
	return serviceName, lines, nil
}

func streamServiceLogs(ctx context.Context, job *bridgeipc.Job, stdout io.Reader, cmd *exec.Cmd) (bool, error) {
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
					"route", StreamTypeServiceLogs,
					"error", err)
				return sentData, err
			}
			if ctx.Err() != nil {
				return sentData, ctx.Err()
			}
			return sentData, nil
		}
		sentData = true
		job.ReportProgress(map[string]any{"type": "data", "data": line})
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
			"route", StreamTypeServiceLogs,
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
