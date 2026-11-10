package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const (
	scheduledTestUnit       = "linuxio-docker-update.service"
	scheduledTestInvocation = "1234567890abcdef1234567890abcdef"
	scheduledTestRunID      = "12345678-90ab-cdef-1234-567890abcdef"
)

func scheduledTestHooks(t *testing.T) scheduledrun.Hooks {
	t.Helper()
	dir := t.TempDir()
	return scheduledrun.Hooks{
		Store:       jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run")),
		ScheduleDir: filepath.Join(dir, "schedules"),
		Now:         func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) },
		BootID:      func() (string, error) { return "11111111-2222-3333-4444-555555555555", nil },
		Output:      func(context.Context, string, io.Writer) error { return nil },
		Stopping:    func(context.Context) bool { return false },
		Alert:       func(context.Context, alerts.Observation) error { return nil },
	}
}

func scheduledTestEnv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestScheduledRunBegin(t *testing.T) {
	hooks := scheduledTestHooks(t)
	var stderr bytes.Buffer
	getenv := scheduledTestEnv(map[string]string{"INVOCATION_ID": scheduledTestInvocation})

	if code := runScheduledRun(t.Context(), []string{"begin", "--unit", scheduledTestUnit}, getenv, hooks, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	record, err := hooks.Store.Get(t.Context(), scheduledTestRunID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != jobs.StateRunning {
		t.Errorf("state = %q, want running", record.State)
	}
}

func TestScheduledRunEnd(t *testing.T) {
	hooks := scheduledTestHooks(t)
	var stderr bytes.Buffer
	begin := scheduledTestEnv(map[string]string{"INVOCATION_ID": scheduledTestInvocation})
	end := scheduledTestEnv(map[string]string{
		"INVOCATION_ID":  scheduledTestInvocation,
		"SERVICE_RESULT": "exit-code",
		"EXIT_CODE":      "exited",
		"EXIT_STATUS":    "3",
	})

	if code := runScheduledRun(t.Context(), []string{"begin", "--unit", scheduledTestUnit}, begin, hooks, &stderr); code != 0 {
		t.Fatalf("begin code = %d, stderr = %q", code, stderr.String())
	}
	if code := runScheduledRun(t.Context(), []string{"end", "--unit", scheduledTestUnit}, end, hooks, &stderr); code != 0 {
		t.Fatalf("end code = %d, stderr = %q", code, stderr.String())
	}
	record, err := hooks.Store.Get(t.Context(), scheduledTestRunID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != jobs.StateFailed || record.Error == nil || record.Error.Message != "exited with status 3" {
		t.Errorf("state = %q, error = %+v; want failed, exited with status 3", record.State, record.Error)
	}
}

func TestScheduledRunUsage(t *testing.T) {
	const usage = "Usage: linuxio scheduled-run begin|end --unit <unit>"
	getenv := scheduledTestEnv(map[string]string{"INVOCATION_ID": scheduledTestInvocation})
	for name, args := range map[string][]string{
		"no arguments":  nil,
		"unknown verb":  {"start", "--unit", scheduledTestUnit},
		"missing unit":  {"begin"},
		"empty unit":    {"end", "--unit", ""},
		"stray operand": {"begin", "--unit", scheduledTestUnit, "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := runScheduledRun(t.Context(), args, getenv, scheduledTestHooks(t), &stderr); code != 2 {
				t.Errorf("code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), usage) {
				t.Errorf("stderr = %q, want %q", stderr.String(), usage)
			}
		})
	}
}

func TestScheduledRunNeedsAnInvocation(t *testing.T) {
	var stderr bytes.Buffer
	code := runScheduledRun(t.Context(), []string{"begin", "--unit", scheduledTestUnit}, scheduledTestEnv(nil), scheduledTestHooks(t), &stderr)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "INVOCATION_ID is not set") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestScheduledRunRefusesAnUnknownUnit(t *testing.T) {
	hooks := scheduledTestHooks(t)
	var stderr bytes.Buffer
	getenv := scheduledTestEnv(map[string]string{"INVOCATION_ID": scheduledTestInvocation})

	if code := runScheduledRun(t.Context(), []string{"begin", "--unit", "ssh.service"}, getenv, hooks, &stderr); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Error("nothing printed for a refused unit")
	}
	records, err := hooks.Store.List(t.Context())
	if err != nil || len(records) != 0 {
		t.Errorf("List = %v, %v; want no records", records, err)
	}
}
