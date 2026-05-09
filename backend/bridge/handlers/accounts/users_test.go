package accounts

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestValidateChpasswdInput(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "plain values allowed",
			username: "alice",
			password: "hunter2",
		},
		{
			name:     "username rejects colon",
			username: "ali:ce",
			password: "hunter2",
			wantErr:  true,
		},
		{
			name:     "password rejects newline",
			username: "alice",
			password: "line1\nline2",
			wantErr:  true,
		},
		{
			name:     "password rejects colon",
			username: "alice",
			password: "pass:word",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateChpasswdInput(tc.username, tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateChpasswdInput() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestIsNonLoginShellRecognizesDebianAndRHELPaths(t *testing.T) {
	tests := []struct {
		shell string
		want  bool
	}{
		{shell: "/usr/sbin/nologin", want: true},
		{shell: "/sbin/nologin", want: true},
		{shell: "/bin/false", want: true},
		{shell: "/usr/bin/false", want: true},
		{shell: "/bin/bash", want: false},
	}

	for _, tc := range tests {
		if got := isNonLoginShell(tc.shell); got != tc.want {
			t.Fatalf("isNonLoginShell(%q) = %v, want %v", tc.shell, got, tc.want)
		}
	}
}

func TestGetProcessSummaryErrorKeepsTopAsArray(t *testing.T) {
	summary := getProcessSummary(context.Background(), "__linuxio_missing_process_owner__")
	if summary.Error == "" {
		t.Skip("ps accepted the synthetic account name on this system")
	}
	if summary.Top == nil {
		t.Fatal("getProcessSummary() returned a nil Top slice on error")
	}

	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal(UserProcessSummary) error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(UserProcessSummary) error = %v", err)
	}
	if _, ok := decoded["top"].([]any); !ok {
		t.Fatalf("encoded top = %T (%v), want JSON array", decoded["top"], decoded["top"])
	}
}

func TestGetProcessSummaryNoRowsIsNotAnError(t *testing.T) {
	summary := getProcessSummary(context.Background(), "nobody")
	if strings.Contains(summary.Error, "does not exist") {
		t.Skip("nobody account is not available on this system")
	}
	if summary.Count > 0 {
		t.Skip("nobody owns processes on this system")
	}
	if summary.Error != "" {
		t.Fatalf("getProcessSummary() error = %q, want no error for an empty process list", summary.Error)
	}
	if summary.Top == nil {
		t.Fatal("getProcessSummary() returned a nil Top slice for an empty process list")
	}
}

func TestParseWhoUserSessionHandlesSSHPTYLine(t *testing.T) {
	line := "miguelmariz sshd pts/0   2026-05-09 11:23 00:04     2618794 (192.168.1.239)"

	session, ok := parseWhoUserSession(line, "miguelmariz")
	if !ok {
		t.Fatal("parseWhoUserSession() ok = false, want true")
	}
	if session.Terminal != "sshd pts/0" {
		t.Fatalf("Terminal = %q, want %q", session.Terminal, "sshd pts/0")
	}
	if session.StartedAt != "2026-05-09 11:23" {
		t.Fatalf("StartedAt = %q, want %q", session.StartedAt, "2026-05-09 11:23")
	}
	if session.Idle != "00:04" {
		t.Fatalf("Idle = %q, want %q", session.Idle, "00:04")
	}
	if session.PID != 2618794 {
		t.Fatalf("PID = %d, want %d", session.PID, 2618794)
	}
	if session.Source != "192.168.1.239" {
		t.Fatalf("Source = %q, want %q", session.Source, "192.168.1.239")
	}
}

func TestParseWhoUserSessionHandlesSSHWithoutTTY(t *testing.T) {
	line := "miguelmariz sshd         2026-05-09 11:13   ?       2606470 (192.168.1.239)"

	session, ok := parseWhoUserSession(line, "miguelmariz")
	if !ok {
		t.Fatal("parseWhoUserSession() ok = false, want true")
	}
	if session.Terminal != "sshd" {
		t.Fatalf("Terminal = %q, want %q", session.Terminal, "sshd")
	}
	if session.StartedAt != "2026-05-09 11:13" {
		t.Fatalf("StartedAt = %q, want %q", session.StartedAt, "2026-05-09 11:13")
	}
	if session.Idle != "?" {
		t.Fatalf("Idle = %q, want %q", session.Idle, "?")
	}
	if session.PID != 2606470 {
		t.Fatalf("PID = %d, want %d", session.PID, 2606470)
	}
	if session.Source != "192.168.1.239" {
		t.Fatalf("Source = %q, want %q", session.Source, "192.168.1.239")
	}
}

func TestParseWhoUserSessionSkipsDifferentUser(t *testing.T) {
	line := "other sshd pts/0   2026-05-09 11:23 00:04     2618794 (192.168.1.239)"

	if _, ok := parseWhoUserSession(line, "miguelmariz"); ok {
		t.Fatal("parseWhoUserSession() ok = true, want false")
	}
}

func TestIsEmptyProcessListExit(t *testing.T) {
	if !isEmptyProcessListExit(nil, &exec.ExitError{}) {
		t.Fatal("isEmptyProcessListExit() = false, want true for empty ps output and empty stderr")
	}
	if isEmptyProcessListExit([]byte("123 bash 0.0 0.1\n"), &exec.ExitError{}) {
		t.Fatal("isEmptyProcessListExit() = true, want false when ps output contains rows")
	}
	if isEmptyProcessListExit(nil, &exec.ExitError{Stderr: []byte("user name does not exist")}) {
		t.Fatal("isEmptyProcessListExit() = true, want false when ps reports stderr")
	}
}
