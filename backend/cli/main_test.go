package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseLogsArgsIndexer(t *testing.T) {
	mode, lines := parseLogsArgs([]string{"indexer", "25"})
	if mode != "indexer" || lines != 25 {
		t.Fatalf("parseLogsArgs = %q, %d; want indexer, 25", mode, lines)
	}
}

func TestVerboseDropinState(t *testing.T) {
	original := verboseDropins
	t.Cleanup(func() { verboseDropins = original })
	dir := t.TempDir()
	verboseDropins = []struct {
		path    string
		content string
	}{
		{path: filepath.Join(dir, "webserver.conf")},
		{path: filepath.Join(dir, "indexer.conf")},
	}

	if verboseEnabled() || verbosePartiallyEnabled() {
		t.Fatal("missing drop-ins reported enabled")
	}
	if err := os.WriteFile(verboseDropins[0].path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if verboseEnabled() || !verbosePartiallyEnabled() {
		t.Fatal("one drop-in did not report partial state")
	}
	if err := os.WriteFile(verboseDropins[1].path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !verboseEnabled() {
		t.Fatal("both drop-ins did not report enabled")
	}
}

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
			name: "indexer",
			mode: "indexer",
			wantIn: []string{
				"SYSLOG_IDENTIFIER=linuxio-indexer",
				"_SYSTEMD_UNIT=linuxio-indexer.service",
				"_SYSTEMD_UNIT=linuxio-indexer-index.service",
				"_SYSTEMD_UNIT=linuxio-indexer-index.timer",
			},
		},
		{
			name: "monitoring",
			mode: "monitoring",
			wantIn: []string{
				"SYSLOG_IDENTIFIER=linuxio-monitoring",
				"_SYSTEMD_UNIT=linuxio-monitoring.service",
			},
			wantMiss: []string{"SYSLOG_IDENTIFIER=linuxio-indexer"},
		},
		{
			name: "all",
			mode: "all",
			wantIn: []string{
				"SYSLOG_IDENTIFIER=linuxio-webserver",
				"SYSLOG_IDENTIFIER=linuxio-bridge",
				"SYSLOG_IDENTIFIER=linuxio-auth",
				"SYSLOG_IDENTIFIER=linuxio-indexer",
				"SYSLOG_IDENTIFIER=linuxio-monitoring",
				"_SYSTEMD_UNIT=linuxio-monitoring.service",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := journalTermsForMode(tt.mode)
			for _, term := range tt.wantIn {
				if !slices.Contains(got, term) {
					t.Fatalf("journalTermsForMode(%q) missing %q in %v", tt.mode, term, got)
				}
			}
			for _, term := range tt.wantMiss {
				if slices.Contains(got, term) {
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
	timestamp := time.Unix(0, 1_700_000_000_000_000*1_000).Format("2006/01/02 15:04:05")
	if want := timestamp + " \033[32m[INFO]\033[0m bridge bridge started"; !strings.Contains(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
}

func TestFormatJournalEntryPrefersSyslogIdentifierOverUnit(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","_SYSTEMD_UNIT":"linuxio-auth@miguelmariz.service","SYSLOG_IDENTIFIER":"linuxio-bridge","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"bridge started"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := "\033[32m[INFO]\033[0m bridge bridge started"; !strings.Contains(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
	if strings.Contains(got, "\033[32m[INFO]\033[0m auth bridge started") {
		t.Fatalf("formatJournalEntry() = %q, unexpectedly used systemd unit", got)
	}
}

func TestFormatJournalEntryIncludesLinuxIOFields(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-webserver","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"auth daemon: bridge spawned","LINUXIO_USER":"miguelmariz","LINUXIO_PRIVILEGED":"true","LINUXIO_VERBOSE":"false"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := "auth daemon: bridge spawned [privileged=true user=miguelmariz verbose=false]"; !strings.Contains(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
}

func TestFormatJournalEntryIncludesErrorField(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-bridge","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"NFS server unavailable","LINUXIO_ERROR":"exportfs not found (install nfs-kernel-server or nfs-utils)"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if want := `NFS server unavailable [error="exportfs not found (install nfs-kernel-server or nfs-utils)"]`; !strings.Contains(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
}

func TestFormatJournalEntryIncludesBridgeRouteFields(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-bridge","SYSLOG_PID":"4321","PRIORITY":"7","MESSAGE":"route completed","LINUXIO_ROUTE":"system.get_timezones","LINUXIO_MODE":"query","LINUXIO_OUTCOME":"success","LINUXIO_DURATION":"3.2ms","LINUXIO_ARG_COUNT":"0","LINUXIO_USER":"miguelmariz"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	want := `route completed [arg_count=0 duration=3.2ms mode=query outcome=success route=system.get_timezones user=miguelmariz]`
	if !strings.Contains(got, want) {
		t.Fatalf("formatJournalEntry() = %q, want substring %q", got, want)
	}
}

func TestFormatJournalEntryOmitsHiddenLinuxIOFields(t *testing.T) {
	got := formatJournalEntry(`{"__REALTIME_TIMESTAMP":"1700000000000000","SYSLOG_IDENTIFIER":"linuxio-auth","SYSLOG_PID":"4321","PRIORITY":"6","MESSAGE":"bridge exec failed","LINUXIO_SESSION_ID":"abc123","LINUXIO_COMPONENT":"auth"}`)
	if got == "" {
		t.Fatal("formatJournalEntry returned empty string")
	}
	if !strings.Contains(got, "bridge exec failed") {
		t.Fatalf("formatJournalEntry() = %q, want message preserved", got)
	}
	if strings.Contains(got, "abc123") || strings.Contains(got, "component=") {
		t.Fatalf("formatJournalEntry() = %q, unexpectedly included hidden fields", got)
	}
}
