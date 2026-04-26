package main

import (
	"slices"
	"testing"
)

func TestJournalTermsForMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		wantIn   []string
		wantMiss []string
	}{
		{
			name:   "bridge",
			mode:   "bridge",
			wantIn: []string{"SYSLOG_IDENTIFIER=linuxio-bridge"},
			wantMiss: []string{
				"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service",
				"SYSLOG_IDENTIFIER=linuxio-auth",
			},
		},
		{
			name: "webserver",
			mode: "webserver",
			wantIn: []string{
				"SYSLOG_IDENTIFIER=linuxio-webserver",
				"_SYSTEMD_UNIT=linuxio-webserver.service",
				"_SYSTEMD_UNIT=linuxio-webserver.socket",
			},
		},
		{
			name: "auth",
			mode: "auth",
			wantIn: []string{
				"SYSLOG_IDENTIFIER=linuxio-auth",
				"_SYSTEMD_UNIT=linuxio-auth.socket",
				"_SYSTEMD_UNIT=linuxio-auth@.service",
			},
		},
		{
			name: "all",
			mode: "all",
			wantIn: []string{
				"SYSLOG_IDENTIFIER=linuxio-webserver",
				"SYSLOG_IDENTIFIER=linuxio-bridge",
				"SYSLOG_IDENTIFIER=linuxio-auth",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := journalTermsForMode(tt.mode)
			for _, term := range tt.wantIn {
				if !containsString(got, term) {
					t.Fatalf("journalTermsForMode(%q) missing %q in %v", tt.mode, term, got)
				}
			}
			for _, term := range tt.wantMiss {
				if containsString(got, term) {
					t.Fatalf("journalTermsForMode(%q) unexpectedly contains %q in %v", tt.mode, term, got)
				}
			}
		})
	}
}

func TestJournalctlCommandUsesSgForPendingGroup(t *testing.T) {
	cmd := journalctlCommand([]string{"SYSLOG_IDENTIFIER=linuxio-auth", "+", "-n", "25", "--no-pager"}, "systemd-journal")

	want := []string{"sg", "systemd-journal", "-c", "journalctl SYSLOG_IDENTIFIER=linuxio-auth + -n 25 --no-pager"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("journalctlCommand() args = %v, want %v", cmd.Args, want)
	}
}

func TestJournalctlShellCommandQuotesUnsafeArgs(t *testing.T) {
	got := journalctlShellCommand([]string{"MESSAGE=can't stop", "-n", "10"})
	want := `journalctl 'MESSAGE=can'"'"'t stop' -n 10`
	if got != want {
		t.Fatalf("journalctlShellCommand() = %q, want %q", got, want)
	}
}

func TestFormatJournalEntryUsesSyslogIdentifier(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-bridge","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"bridge started"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := "linuxio-bridge[4321]:"; !containsSubstring(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
}

func TestFormatJournalEntryPrefersSyslogIdentifierOverUnit(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","_SYSTEMD_UNIT":"linuxio-auth@miguelmariz.service","SYSLOG_IDENTIFIER":"linuxio-bridge","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"bridge started"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := "linuxio-bridge[4321]:"; !containsSubstring(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
	if containsSubstring(got, "linuxio-auth[4321]:") {
		t.Fatalf("formatJournalEntry() = %q, unexpectedly used systemd unit", got)
	}
}

func TestFormatJournalEntryIncludesLinuxIOFields(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-webserver","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"auth daemon: bridge spawned","LINUXIO_USER":"miguelmariz","LINUXIO_PRIVILEGED":"true"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := "auth daemon: bridge spawned privileged=true user=miguelmariz"; !containsSubstring(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
}

func TestFormatJournalEntryOmitsHiddenLinuxIOFields(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-auth","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"bridge exec failed","LINUXIO_ERROR":"permission denied","LINUXIO_SESSION_ID":"abc123","LINUXIO_COMPONENT":"auth"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if !containsSubstring(got, "bridge exec failed") {
		t.Fatalf("formatJournalEntry() = %q, want message preserved", got)
	}
	if containsSubstring(got, "permission denied") || containsSubstring(got, "abc123") || containsSubstring(got, "component=") {
		t.Fatalf("formatJournalEntry() = %q, unexpectedly included hidden fields", got)
	}
}

func containsString(values []string, target string) bool {
	return slices.Contains(values, target)
}

func containsSubstring(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}())
}
