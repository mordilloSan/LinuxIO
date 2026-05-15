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

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// journaldFieldMatch matches journald-style KEY=VALUE operands. The key must
// start with an uppercase letter or underscore and contain only uppercase
// letters, digits, and underscores. Anything else is rejected to keep
// untrusted UI input from being passed straight to journalctl.
var journaldFieldMatch = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=.*$`)

const StreamTypeGeneralLogs = "logs.general.follow"

type generalLogsRequest struct {
	lines        string
	timePeriod   string
	priority     string
	identifier   string
	fieldFilters []string
}

// runGeneralLogsJob streams general journal logs through the bridge job lifecycle.
// Args: [lines, timePeriod, priority, identifier, fieldMatches...]
// - lines: number of initial lines (default "100")
// - timePeriod: time range like "1h", "24h", "7d" (optional)
// - priority: max priority level 0-7 (optional, empty = all)
// - identifier: filter by SYSLOG_IDENTIFIER (optional, empty = all)
// - fieldMatches: optional KEY=VALUE journald match operands (ANDed)
func runGeneralLogsJob(ctx context.Context, _ runtime.Runtime, job *bridgeipc.Job, args []string) (any, error) {
	req := parseGeneralLogsRequest(args)
	slog.Debug("starting general log job",
		"component", "logs",
		"route", StreamTypeGeneralLogs,
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
			"route", StreamTypeGeneralLogs,
			"job_id", job.ID(),
			"error", err)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start general log job",
			"component", "logs",
			"route", StreamTypeGeneralLogs,
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
	for _, raw := range args[min(4, len(args)):] {
		f := strings.TrimSpace(raw)
		if f == "" || !journaldFieldMatch.MatchString(f) {
			continue
		}
		req.fieldFilters = append(req.fieldFilters, f)
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
		job.ReportProgress(map[string]any{"type": "data", "data": line})
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
