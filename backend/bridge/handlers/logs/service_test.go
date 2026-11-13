package logs

import (
	"errors"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestParseServiceLogsRequestDefaultsLines(t *testing.T) {
	name, invocationID, lines, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: " ssh.service "})
	if err != nil {
		t.Fatalf("parseServiceLogsRequest: %v", err)
	}
	if name != "ssh.service" || invocationID != "" || lines != "100" {
		t.Errorf("got %q / %q / %q, want ssh.service / empty / 100", name, invocationID, lines)
	}
}

func TestParseServiceLogsRequestRejectsEmptyAndTemplateUnits(t *testing.T) {
	for _, name := range []string{"", "  ", "getty@.service"} {
		if _, _, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: name}); err == nil {
			t.Errorf("parseServiceLogsRequest(%q) succeeded, want error", name)
		}
	}
}

func TestParseServiceLogsRequestValidatesLines(t *testing.T) {
	for _, lines := range []string{"all", "0", "200", "999999"} {
		if _, _, got, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: "ssh.service", Lines: &lines}); err != nil || got != lines {
			t.Errorf("lines %q: got %q, %v, want accepted", lines, got, err)
		}
	}
	for _, lines := range []string{"--version", "--vacuum-time=1s", "-5", "1000000", "1 2", "ALL"} {
		_, _, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: "ssh.service", Lines: &lines})
		if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("lines %q: err = %v, want ErrInvalidArgs", lines, err)
		}
	}
}

func TestServiceLogsArgsWithoutAnInvocationAreUnchanged(t *testing.T) {
	got := serviceJournalArgs("ssh.service", "", "100")
	want := []string{"-u", "ssh.service", "-n", "100", "-f", "--no-pager", "-o", "short-iso"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
	got = serviceJournalArgs("ssh.service", "", "all")
	want = []string{"-u", "ssh.service", "-f", "--no-pager", "-o", "short-iso"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
}

func TestServiceLogsScopeToAnInvocation(t *testing.T) {
	const invocation = "1234567890abcdef1234567890abcdef"
	lines := "200"
	// Whitespace around the invocation is ignored.
	padded := " " + invocation + " "
	name, invocationID, gotLines, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{
		ServiceName:  "linuxio-schedule-x.service",
		InvocationID: &padded,
		Lines:        &lines,
	})
	if err != nil {
		t.Fatalf("parseServiceLogsRequest: %v", err)
	}
	got := serviceJournalArgs(name, invocationID, gotLines)
	want := []string{
		"_SYSTEMD_UNIT=linuxio-schedule-x.service",
		"_SYSTEMD_INVOCATION_ID=" + invocation,
		"-n", "200", "-f", "--no-pager", "--all", "-o", "short-iso",
	}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}

	got = serviceJournalArgs(name, invocationID, "all")
	want = []string{
		"_SYSTEMD_UNIT=linuxio-schedule-x.service",
		"_SYSTEMD_INVOCATION_ID=" + invocation,
		"-f", "--no-pager", "--all", "-o", "short-iso",
	}
	if !slices.Equal(got, want) {
		t.Errorf("args with all lines = %q, want %q", got, want)
	}
}

func TestServiceLogsTreatABlankInvocationAsNone(t *testing.T) {
	for _, blank := range []string{"", "  "} {
		_, invocationID, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{
			ServiceName:  "ssh.service",
			InvocationID: &blank,
		})
		if err != nil || invocationID != "" {
			t.Errorf("invocation %q: got %q, %v, want empty and no error", blank, invocationID, err)
		}
	}
}

func TestServiceLogsRejectABadInvocation(t *testing.T) {
	for _, bad := range []string{"nope", "1234567890ABCDEF1234567890ABCDEF", "--all", "1234567890abcdef1234567890abcdef0"} {
		_, _, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{
			ServiceName:  "ssh.service",
			InvocationID: &bad,
		})
		if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("invocation %q: err = %v, want ErrInvalidArgs", bad, err)
		}
	}
}
