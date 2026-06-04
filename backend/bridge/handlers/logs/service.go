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

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var streamTypeServiceLogs = RouteServiceFollow.Route

// runServiceLogsJob streams service logs from journalctl through the bridge job lifecycle.
func runServiceLogsJob(ctx context.Context, _ runtime.Runtime, job *bridgeipc.Job, req apischema.ServiceLogsFollowRequest) (any, error) {
	serviceName, lines, err := parseServiceLogsRequest(req)
	if err != nil {
		return nil, err
	}
	slog.Debug("starting service log job",
		"component", "logs",
		"route", streamTypeServiceLogs,
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
			"route", streamTypeServiceLogs,
			"job_id", job.ID(),
			"service", serviceName,
			"error", err)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start service log job",
			"component", "logs",
			"route", streamTypeServiceLogs,
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
		job.ReportData(line)
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
