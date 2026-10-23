package logs

import (
	"encoding/json"
	"testing"
)

func TestTrimJournalLineKeepsAllowlistAndLinuxioFields(t *testing.T) {
	line := `{"__CURSOR":"s=abc;i=1","__REALTIME_TIMESTAMP":"1700000000000000",` +
		`"MESSAGE":"hello","PRIORITY":"6","SYSLOG_IDENTIFIER":"sshd",` +
		`"LINUXIO_SESSION":"x1","_CMDLINE":"/usr/sbin/sshd -D","_EXE":"/usr/sbin/sshd",` +
		`"_HOSTNAME":"box","CODE_FUNC":"doThing"}`
	trimmed, cursor := trimJournalLine(line)
	if cursor != "s=abc;i=1" {
		t.Errorf("cursor = %q, want s=abc;i=1", cursor)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		t.Fatalf("trimmed output is not valid JSON: %v", err)
	}
	for _, want := range []string{"__CURSOR", "__REALTIME_TIMESTAMP", "MESSAGE", "PRIORITY", "SYSLOG_IDENTIFIER", "LINUXIO_SESSION", "CODE_FUNC"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("trimmed output missing %q", want)
		}
	}
	for _, dropped := range []string{"_CMDLINE", "_EXE", "_HOSTNAME"} {
		if _, ok := fields[dropped]; ok {
			t.Errorf("trimmed output should have dropped %q", dropped)
		}
	}
}

func TestTrimJournalLinePassesThroughInvalidJSON(t *testing.T) {
	trimmed, cursor := trimJournalLine("not json at all")
	if trimmed != "not json at all" || cursor != "" {
		t.Errorf("invalid JSON should pass through, got %q / %q", trimmed, cursor)
	}
}
