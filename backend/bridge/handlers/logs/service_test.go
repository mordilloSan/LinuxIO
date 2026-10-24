package logs

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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
