package logs

import (
	"strings"
	"testing"
)

func TestParseServiceLogsArgsRejectsTemplateUnits(t *testing.T) {
	_, _, err := parseServiceLogsArgs([]string{"apport-coredump-hook@.service"})
	if err == nil || !strings.Contains(err.Error(), "template unit") {
		t.Fatalf("handler err = %v, want template-unit error", err)
	}
}
