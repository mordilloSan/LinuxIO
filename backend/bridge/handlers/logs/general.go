package logs

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// journaldFieldMatch matches journald-style KEY=VALUE operands. The key must
// start with an uppercase letter or underscore and contain only uppercase
// letters, digits, and underscores. Anything else is rejected to keep
// untrusted UI input from being passed straight to journalctl.
var journaldFieldMatch = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=.*$`)

const streamTypeGeneralLogs = "logs.general.follow"

type generalLogsRequest struct {
	lines        string
	timePeriod   string
	priority     string
	identifier   string
	fieldFilters []string
}

// runGeneralLogsJob streams general journal logs through the bridge job lifecycle.
func runGeneralLogsJob(ctx context.Context, _ runtime.Runtime, job *bridgeipc.Job, request apischema.GeneralLogsFollowRequest) (any, error) {
	req := parseGeneralLogsRequest(request)
	slog.Debug("starting general log job",
		"component", "logs",
		"route", streamTypeGeneralLogs,
		"job_id", job.ID(),
		"lines", req.lines,
		"time_period", req.timePeriod,
		"priority", req.priority,
		"identifier", req.identifier,
		"field_filters", strings.Join(req.fieldFilters, " "))

	cmd, stdout, err := startGeneralLogsCommand(ctx, req)
	if err != nil {
		slog.Error("failed to create general log job pipe",
			"component", "logs",
			"route", streamTypeGeneralLogs,
			"job_id", job.ID(),
			"error", err)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start general log job",
			"component", "logs",
			"route", streamTypeGeneralLogs,
			"job_id", job.ID(),
			"error", err)
		return nil, err
	}

	readErr := streamJournalLinesToJob(ctx, job, stdout, cmd, "[GeneralLogs]")
	waitErr := waitForLogsCommand(cmd, "[GeneralLogs]")
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return map[string]any{"status": "stopped"}, nil
}

func parseGeneralLogsRequest(request apischema.GeneralLogsFollowRequest) generalLogsRequest {
	req := generalLogsRequest{lines: "100"}
	if request.Lines != nil && strings.TrimSpace(*request.Lines) != "" {
		req.lines = strings.TrimSpace(*request.Lines)
	}
	if request.TimePeriod != nil && strings.TrimSpace(*request.TimePeriod) != "" {
		req.timePeriod = strings.TrimSpace(*request.TimePeriod)
	}
	if request.Priority != nil && strings.TrimSpace(*request.Priority) != "" {
		req.priority = strings.TrimSpace(*request.Priority)
	}
	if request.Identifier != nil && strings.TrimSpace(*request.Identifier) != "" {
		req.identifier = strings.TrimSpace(*request.Identifier)
	}
	for _, raw := range request.FieldFilters {
		f := strings.TrimSpace(raw)
		if f == "" || !journaldFieldMatch.MatchString(f) {
			continue
		}
		req.fieldFilters = append(req.fieldFilters, f)
	}
	return req
}

func startGeneralLogsCommand(ctx context.Context, req generalLogsRequest) (*exec.Cmd, io.ReadCloser, error) {
	cmdArgs := []string{"-f", "--no-pager", "-o", "json"}
	if req.lines == "all" {
		// `-f` alone only emits the default tail (10 entries) of history before
		// following; `--no-tail` is needed to actually get every entry in the
		// (time-restricted) window.
		cmdArgs = append(cmdArgs, "--no-tail")
	} else if req.lines != "" {
		cmdArgs = append([]string{"-n", req.lines}, cmdArgs...)
	}
	if req.timePeriod != "" {
		cmdArgs = append(cmdArgs, "--since", req.timePeriod+" ago")
	}
	if req.priority != "" {
		cmdArgs = append(cmdArgs, "-p", req.priority)
	}
	if req.identifier != "" {
		cmdArgs = append(cmdArgs, "-t", req.identifier)
	}
	cmdArgs = append(cmdArgs, req.fieldFilters...)
	cmd := exec.CommandContext(ctx, "journalctl", cmdArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

func streamJournalLinesToJob(ctx context.Context, job *bridgeipc.Job, stdout io.Reader, cmd *exec.Cmd, label string) error {
	reader := bufio.NewReader(stdout)
	for {
		if handleLogsContextCancellation(ctx, cmd, label) {
			return ctx.Err()
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				slog.Debug("log stream read error",
					"component", "logs",
					"stream_label", label,
					"error", err)
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		job.ReportData(line)
	}
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

func waitForLogsCommand(cmd *exec.Cmd, label string) error {
	if err := cmd.Wait(); err != nil {
		slog.Debug("journalctl exited with error",
			"component", "logs",
			"stream_label", label,
			"error", err)
		return err
	}
	return nil
}
