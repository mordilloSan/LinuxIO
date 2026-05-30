package logs

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestParseServiceLogsRequestRejectsTemplateUnits(t *testing.T) {
	_, _, err := parseServiceLogsRequest(apischema.ServiceLogsFollowRequest{
		ServiceName: "apport-coredump-hook@.service",
	})
	if err == nil || !strings.Contains(err.Error(), "template unit") {
		t.Fatalf("handler err = %v, want template-unit error", err)
	}
}
