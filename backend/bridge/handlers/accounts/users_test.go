package accounts

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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

func TestGetHomeHealthKeepsKnownOwnership(t *testing.T) {
	homeDir := t.TempDir()
	info, err := os.Stat(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("temporary directory has no syscall.Stat_t metadata")
	}

	health := getHomeHealth(apischema.AccountUser{HomeDir: homeDir, UID: int(stat.Uid)})
	if health.OwnerUID == nil || *health.OwnerUID != int(stat.Uid) {
		t.Fatalf("OwnerUID = %v, want known %d", health.OwnerUID, stat.Uid)
	}
	if health.GroupGID == nil || *health.GroupGID != int(stat.Gid) {
		t.Fatalf("GroupGID = %v, want known %d", health.GroupGID, stat.Gid)
	}
	if !health.OwnerMatches {
		t.Fatal("OwnerMatches = false, want true")
	}
}

func TestGetHomeHealthOmitsUnknownOwnership(t *testing.T) {
	health := getHomeHealth(apischema.AccountUser{HomeDir: t.TempDir() + "/missing"})
	if health.OwnerUID != nil || health.GroupGID != nil {
		t.Fatalf("unknown ownership must be omitted, got %#v", health)
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

func TestSessionFromLogindMapsSSHSession(t *testing.T) {
	now := time.Date(2026, 5, 9, 11, 27, 0, 0, time.UTC)
	props := map[string]godbus.Variant{
		"Id":            godbus.MakeVariant("32"),
		"Class":         godbus.MakeVariant("user"),
		"TTY":           godbus.MakeVariant("pts/0"),
		"Service":       godbus.MakeVariant("sshd"),
		"RemoteHost":    godbus.MakeVariant("192.168.1.239"),
		"Leader":        godbus.MakeVariant(uint32(2618794)),
		"Timestamp":     godbus.MakeVariant(uint64(now.Add(-4 * time.Minute).UnixMicro())),
		"IdleHint":      godbus.MakeVariant(true),
		"IdleSinceHint": godbus.MakeVariant(uint64(now.Add(-4 * time.Minute).UnixMicro())),
	}

	session, ok := sessionFromLogind(props, now)
	if !ok {
		t.Fatal("sessionFromLogind() ok = false, want true")
	}
	want := apischema.AccountActiveSession{
		SessionID: "32", Terminal: "pts/0", Source: "192.168.1.239",
		Idle: "00:04", StartedAt: now.Add(-4 * time.Minute).Local().Format("2006-01-02 15:04"),
	}
	pid := 2618794
	want.PID = &pid
	if *session.PID != pid || session.PID == nil {
		t.Fatalf("PID = %v, want %d", session.PID, pid)
	}
	session.PID = want.PID
	if session != want {
		t.Fatalf("session = %+v, want %+v", session, want)
	}
}

func TestSessionFromLogindWebSessionWithoutTTYIsActive(t *testing.T) {
	props := map[string]godbus.Variant{
		"Id":       godbus.MakeVariant("40"),
		"Class":    godbus.MakeVariant("user"),
		"Service":  godbus.MakeVariant("linuxio"),
		"IdleHint": godbus.MakeVariant(false),
	}

	session, ok := sessionFromLogind(props, time.Now())
	if !ok {
		t.Fatal("sessionFromLogind() ok = false, want true")
	}
	if session.Terminal != "linuxio" || session.Idle != "." || session.PID != nil {
		t.Fatalf("session = %+v, want service as terminal, active, no PID", session)
	}
}

func TestSessionFromLogindSkipsManagerClass(t *testing.T) {
	props := map[string]godbus.Variant{"Class": godbus.MakeVariant("manager")}
	if _, ok := sessionFromLogind(props, time.Now()); ok {
		t.Fatal("sessionFromLogind() ok = true, want false")
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
