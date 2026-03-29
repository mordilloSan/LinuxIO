package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		if err := r.Close(); err != nil {
			t.Fatalf("close read pipe: %v", err)
		}
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String()
}

func TestParseLogsArgsSupportsMonitoring(t *testing.T) {
	mode, lines := parseLogsArgs([]string{"monitoring", "250"})

	if mode != "monitoring" {
		t.Fatalf("mode = %q, want monitoring", mode)
	}
	if lines != 250 {
		t.Fatalf("lines = %d, want 250", lines)
	}
}

func TestJournalTermsForModeMonitoring(t *testing.T) {
	got := journalTermsForMode("monitoring")
	want := []string{"_SYSTEMD_UNIT=" + monitoringUnitName}

	if len(got) != len(want) {
		t.Fatalf("len(journalTermsForMode) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("journalTermsForMode[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunSystemctlUsesReadablePastTense(t *testing.T) {
	oldExec := execCommand
	defer func() { execCommand = oldExec }()

	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string{}, args...)
		return exec.Command("true")
	}

	out := captureStdout(t, func() {
		runSystemctl("stop", monitoringUnitName)
	})

	if gotName != "systemctl" {
		t.Fatalf("command = %q, want systemctl", gotName)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "stop" || gotArgs[1] != monitoringUnitName {
		t.Fatalf("args = %#v, want stop %s", gotArgs, monitoringUnitName)
	}
	if !strings.Contains(out, "Successfully stopped "+monitoringUnitName) {
		t.Fatalf("stdout = %q, want stopped message", out)
	}
}

func TestShowHelpIncludesMonitoringCommand(t *testing.T) {
	out := captureStdout(t, showHelp)

	if !strings.Contains(out, "monitoring  Manage monitoring stack") {
		t.Fatalf("showHelp output missing monitoring command: %q", out)
	}
	if !strings.Contains(out, "linuxio monitoring status") {
		t.Fatalf("showHelp output missing monitoring example: %q", out)
	}
}
