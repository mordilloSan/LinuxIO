package main

import "slices"

import "testing"

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

func TestFormatJournalEntryUsesSyslogIdentifier(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-bridge","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"bridge started"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := "linuxio-bridge[4321]:"; !containsSubstring(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
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
