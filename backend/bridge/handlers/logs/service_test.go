package logs

import (
	"errors"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestParseServiceLogsRequestDefaultsLines(t *testing.T) {
	name, lines, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: " ssh.service "})
	if err != nil {
		t.Fatalf("parseServiceLogsRequest: %v", err)
	}
	if name != "ssh.service" || lines != "100" {
		t.Errorf("got %q / %q, want ssh.service / 100", name, lines)
	}
}

func TestParseServiceLogsRequestRejectsEmptyAndTemplateUnits(t *testing.T) {
	for _, name := range []string{"", "  ", "getty@.service"} {
		if _, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: name}); err == nil {
			t.Errorf("parseServiceLogsRequest(%q) succeeded, want error", name)
		}
	}
}

func TestParseServiceLogsRequestValidatesLines(t *testing.T) {
	for _, lines := range []string{"all", "0", "200", "999999"} {
		if _, got, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: "ssh.service", Lines: &lines}); err != nil || got != lines {
			t.Errorf("lines %q: got %q, %v, want accepted", lines, got, err)
		}
	}
	for _, lines := range []string{"--version", "--vacuum-time=1s", "-5", "1000000", "1 2", "ALL"} {
		_, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{ServiceName: "ssh.service", Lines: &lines})
		if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("lines %q: err = %v, want ErrInvalidArgs", lines, err)
		}
	}
}
