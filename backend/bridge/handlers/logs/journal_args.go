package logs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// linesMatch limits the lines value to "all" or a plain count so it can never
// be parsed by journalctl as an option after -n.
var linesMatch = regexp.MustCompile(`^(all|[0-9]{1,6})$`)

func validateLines(lines string) error {
	if !linesMatch.MatchString(lines) {
		return fmt.Errorf("%w: lines must be \"all\" or a number", bridgeipc.ErrInvalidArgs)
	}
	return nil
}

// journaldFieldMatch matches journald-style KEY=VALUE operands. The key must
// start with an uppercase letter or underscore and contain only uppercase
// letters, digits, and underscores. Anything else is rejected to keep
// untrusted UI input from being passed straight to journalctl.
var journaldFieldMatch = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=.*$`)

var managedUnitMatch = regexp.MustCompile(`^[A-Za-z0-9:_.@%-]+\.service$`)

var invocationIDMatch = regexp.MustCompile(`^[0-9a-f]{32}$`)

// isValidJournalCursor bounds cursor lookups: journal cursors are printable
// ASCII key=value pairs joined by semicolons. The value is passed as a single
// execve argument (never through a shell), so printable + bounded is enough.
func isValidJournalCursor(cursor string) bool {
	if cursor == "" || len(cursor) > 1024 {
		return false
	}
	for i := 0; i < len(cursor); i++ {
		if cursor[i] < 0x20 || cursor[i] > 0x7e {
			return false
		}
	}
	return true
}

type generalLogsRequest struct {
	lines        string
	timePeriod   string
	priority     string
	identifier   string
	unit         string
	invocationID string
	scopeError   error
	fieldFilters []string
	follow       bool
	afterCursor  string
}

func parseGeneralLogsRequest(request apischema.GeneralLogsFollowRequest) generalLogsRequest {
	req := generalLogsRequest{lines: "100", follow: true}
	if request.TimePeriod != nil && strings.TrimSpace(*request.TimePeriod) != "" {
		req.timePeriod = strings.TrimSpace(*request.TimePeriod)
	}
	if request.Priority != nil && strings.TrimSpace(*request.Priority) != "" {
		req.priority = strings.TrimSpace(*request.Priority)
	}
	if request.Identifier != nil && strings.TrimSpace(*request.Identifier) != "" {
		req.identifier = strings.TrimSpace(*request.Identifier)
	}
	req.unit, req.invocationID, req.scopeError = parseLogScope(request.Unit, request.InvocationID)
	if request.Lines != nil && strings.TrimSpace(*request.Lines) != "" {
		req.lines = strings.TrimSpace(*request.Lines)
		if linesErr := validateLines(req.lines); linesErr != nil {
			req.scopeError = linesErr
		}
	}
	if request.Follow != nil {
		req.follow = *request.Follow
	}
	if request.AfterCursor != nil {
		req.afterCursor = strings.TrimSpace(*request.AfterCursor)
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

func parseLogScope(unitValue, invocationValue *string) (string, string, error) {
	unit, invocationID := "", ""
	if unitValue != nil {
		unit = strings.TrimSpace(*unitValue)
		switch {
		case unit == "":
		case len(unit) <= 255 && managedUnitMatch.MatchString(unit):
		default:
			return "", "", errors.New("invalid scheduled log unit")
		}
	}
	if invocationValue != nil {
		invocationID = strings.TrimSpace(*invocationValue)
		switch {
		case invocationID == "":
		case invocationIDMatch.MatchString(invocationID):
		default:
			return "", "", errors.New("invalid scheduled log invocation ID")
		}
	}
	if invocationID != "" && unit == "" {
		return "", "", errors.New("an invocation scope requires a unit")
	}
	return unit, invocationID, nil
}

func appendCommonFilters(args []string, req generalLogsRequest) []string {
	if req.priority != "" {
		args = append(args, "-p", req.priority)
	}
	if req.identifier != "" {
		args = append(args, "-t", req.identifier)
	}
	if req.unit != "" && req.invocationID == "" {
		return append(append(args, "--unit", req.unit), req.fieldFilters...)
	}
	branches := journalMatchBranches(req)
	if len(branches) == 0 {
		return append(args, req.fieldFilters...)
	}
	for index, branch := range branches {
		if index > 0 {
			args = append(args, "+")
		}
		args = append(args, branch...)
	}
	return args
}

// journalMatchBranches mirrors journalctl -u's unit and PID 1/object matches.
// Unit output and manager lifecycle records use different fields. Invocation
// correlation adds the corresponding field to each branch, preserving the OR
// relationship while keeping custom filters conjunctive within every branch.
func journalMatchBranches(req generalLogsRequest) [][]string {
	if req.unit == "" {
		return nil
	}
	unitBranches := [][]string{
		{"_SYSTEMD_UNIT=" + req.unit},
		{"UNIT=" + req.unit, "_PID=1"},
		{"OBJECT_SYSTEMD_UNIT=" + req.unit, "_UID=0"},
	}
	if req.invocationID != "" {
		unitBranches[0] = append(unitBranches[0], "_SYSTEMD_INVOCATION_ID="+req.invocationID)
		unitBranches[1] = append(unitBranches[1], "INVOCATION_ID="+req.invocationID)
		unitBranches[2] = append(unitBranches[2], "OBJECT_SYSTEMD_INVOCATION_ID="+req.invocationID)
	}
	for index, branch := range unitBranches {
		unitBranches[index] = append(branch, req.fieldFilters...)
	}
	return unitBranches
}

func backlogArgs(req generalLogsRequest) []string {
	args := []string{"-q", "--no-pager", "-o", "json", "--reverse"}
	if req.lines != "" && req.lines != "all" {
		args = append(args, "-n", req.lines)
	}
	if req.timePeriod != "" {
		args = append(args, "--since", req.timePeriod+" ago")
	}
	return appendCommonFilters(args, req)
}

func followArgs(req generalLogsRequest, afterCursor string) []string {
	args := []string{"-q", "--no-pager", "-o", "json", "--follow"}
	switch {
	case afterCursor != "":
		args = append(args, "--after-cursor", afterCursor)
	case req.timePeriod != "":
		// Empty backlog: re-cover the (proven empty) window so entries logged
		// between the two journalctl processes are not lost.
		args = append(args, "--no-tail", "--since", req.timePeriod+" ago")
	default:
		args = append(args, "-n", "0")
	}
	return appendCommonFilters(args, req)
}

func generalPageArgs(req generalLogsRequest, cursor string) []string {
	// journalctl treats --since, --cursor, --cursor-file and --after-cursor as
	// mutually exclusive seek positions. The selected time window is enforced
	// while reading entries below instead of being passed as --since.
	args := []string{
		"-q", "--no-pager", "-o", "json", "--reverse", "--cursor", cursor,
	}
	return appendCommonFilters(args, req)
}
