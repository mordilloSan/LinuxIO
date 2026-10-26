package logs

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestParseGeneralLogsRequestDefaults(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{})
	if req.lines != "100" {
		t.Errorf("lines = %q, want default 100", req.lines)
	}
	if !req.follow {
		t.Error("follow = false, want default true")
	}
}

func TestParseGeneralLogsRequestFollowFalse(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Follow: new(false),
	})
	if req.follow {
		t.Error("follow = true, want false")
	}
}

func TestParseGeneralLogsRequestAfterCursor(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		AfterCursor: new("  s=abc;i=42  "),
	})
	if req.afterCursor != "s=abc;i=42" {
		t.Errorf("afterCursor = %q, want trimmed cursor", req.afterCursor)
	}
}

func TestParseGeneralLogsRequestRejectsInvalidFieldFilters(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		FieldFilters: []string{"PRIORITY=3", "-rf", "foo=bar", "_SYSTEMD_UNIT=ssh.service"},
	})
	want := []string{"PRIORITY=3", "_SYSTEMD_UNIT=ssh.service"}
	if !slices.Equal(req.fieldFilters, want) {
		t.Errorf("fieldFilters = %v, want %v", req.fieldFilters, want)
	}
}

func TestJournalMatchBranchesIncludeUnitAndInvocationProducers(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Unit:         new("linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service"),
		InvocationID: new("0123456789abcdef0123456789abcdef"),
		FieldFilters: []string{"PRIORITY=3"},
	})
	branches := journalMatchBranches(req)
	if len(branches) != 3 {
		t.Fatalf("branches = %d, want 3", len(branches))
	}
	joined := strings.Join([]string{
		strings.Join(branches[0], " "),
		strings.Join(branches[1], " "),
		strings.Join(branches[2], " "),
	}, " + ")
	for _, want := range []string{
		"_SYSTEMD_UNIT=linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service _SYSTEMD_INVOCATION_ID=0123456789abcdef0123456789abcdef PRIORITY=3",
		"UNIT=linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service _PID=1 INVOCATION_ID=0123456789abcdef0123456789abcdef PRIORITY=3",
		"OBJECT_SYSTEMD_UNIT=linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service _UID=0 OBJECT_SYSTEMD_INVOCATION_ID=0123456789abcdef0123456789abcdef PRIORITY=3",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("branches missing %q: %s", want, joined)
		}
	}
}

func TestParseGeneralLogsRequestRejectsUnsafeScheduleScope(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Unit:         new("bad unit.service"),
		InvocationID: new("not-an-invocation"),
	})
	if req.unit != "" || req.invocationID != "" {
		t.Fatalf("unsafe scope accepted: unit=%q invocation=%q", req.unit, req.invocationID)
	}
	if req.scopeError == nil {
		t.Fatal("unsafe scope did not produce a validation error")
	}
}

func TestUnitScopeUsesJournalctlNativeMatcher(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Unit: new("linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service"),
	})
	args := strings.Join(backlogArgs(req), " ")
	if !strings.Contains(args, "--unit linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service") {
		t.Fatalf("unit scope did not use native matcher: %s", args)
	}
	if strings.Contains(args, "_SYSTEMD_UNIT=") {
		t.Fatalf("unit scope rebuilt native matcher: %s", args)
	}
}

func TestBacklogArgs(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		Lines:      new("200"),
		TimePeriod: new("24h"),
		Priority:   new("4"),
		Identifier: new("sshd"),
	})
	args := backlogArgs(req)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--reverse", "-n 200", "--since 24h ago", "-p 4", "-t sshd", "-o json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("backlogArgs missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "--follow") || strings.Contains(joined, "-f ") {
		t.Errorf("backlog must not follow: %q", joined)
	}
}

func TestBacklogArgsAllLinesHasNoTailFlag(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{Lines: new("all")})
	joined := strings.Join(backlogArgs(req), " ")
	if strings.Contains(joined, "-n ") {
		t.Errorf("lines=all must not pass -n: %q", joined)
	}
}

func TestFollowArgsAnchorsOnCursor(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{TimePeriod: new("24h")})
	joined := strings.Join(followArgs(req, "s=abc;i=1"), " ")
	if !strings.Contains(joined, "--follow") {
		t.Errorf("follow args missing --follow: %q", joined)
	}
	if !strings.Contains(joined, "--after-cursor s=abc;i=1") {
		t.Errorf("follow args missing --after-cursor: %q", joined)
	}
	if strings.Contains(joined, "--since") {
		t.Errorf("cursor-anchored follow must not re-apply --since: %q", joined)
	}
}

func TestFollowArgsWithoutCursorTailsZero(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{})
	joined := strings.Join(followArgs(req, ""), " ")
	if !strings.Contains(joined, "-n 0") {
		t.Errorf("cursorless windowless follow must tail 0 lines: %q", joined)
	}
}

func TestFollowArgsWithoutCursorRecoversWindow(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{TimePeriod: new("24h")})
	joined := strings.Join(followArgs(req, ""), " ")
	if !strings.Contains(joined, "--since 24h ago") || !strings.Contains(joined, "--no-tail") {
		t.Errorf("empty-backlog follow must re-cover the window to close the gap: %q", joined)
	}
	if strings.Contains(joined, "-n 0") {
		t.Errorf("window fallback must not also tail 0: %q", joined)
	}
}

func TestGeneralPageArgsDoNotCombineCursorAndSince(t *testing.T) {
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		TimePeriod: new("24h"),
		Priority:   new("4"),
		Identifier: new("sshd"),
	})
	joined := strings.Join(generalPageArgs(req, "s=abc;i=1"), " ")
	if !strings.Contains(joined, "--cursor s=abc;i=1") {
		t.Errorf("page args missing cursor: %q", joined)
	}
	if strings.Contains(joined, "--since") {
		t.Errorf("journalctl rejects --cursor with --since: %q", joined)
	}
	for _, want := range []string{"--reverse", "-p 4", "-t sshd", "-o json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("page args missing %q in %q", want, joined)
		}
	}
}

func TestIsValidJournalCursor(t *testing.T) {
	cases := []struct {
		cursor string
		want   bool
	}{
		{"s=81164172d30a41bf;i=1c937;b=c40aeda9", true},
		{"", false},
		{"has\nnewline", false},
		{"has\x00nul", false},
		{strings.Repeat("a", 1025), false},
		{strings.Repeat("a", 1024), true},
	}
	for _, tc := range cases {
		if got := isValidJournalCursor(tc.cursor); got != tc.want {
			t.Errorf("isValidJournalCursor(%.20q...) = %v, want %v", tc.cursor, got, tc.want)
		}
	}
}

func TestParseGeneralLogsRequestRejectsInvalidLines(t *testing.T) {
	for _, lines := range []string{"all", "0", "200", "999999"} {
		if req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{Lines: &lines}); req.scopeError != nil {
			t.Errorf("lines %q: unexpected error %v", lines, req.scopeError)
		}
	}
	for _, lines := range []string{"--version", "--vacuum-time=1s", "-5", "1000000", "1 2", "ALL"} {
		req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{Lines: &lines})
		if !errors.Is(req.scopeError, bridgeipc.ErrInvalidArgs) {
			t.Errorf("lines %q: scopeError = %v, want ErrInvalidArgs", lines, req.scopeError)
		}
	}
}
