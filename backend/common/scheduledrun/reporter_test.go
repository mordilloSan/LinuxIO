package scheduledrun

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestReporterWritesTheRunsProgress(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	t.Setenv("INVOCATION_ID", testInvocation)

	r := ReporterFromEnv(h.Store)
	if r == nil {
		t.Fatal("ReporterFromEnv = nil, want a reporter")
	}
	r.Report(43, "Scrubbing")

	got, err := h.Store.ReadProgress(testRunID)
	if err != nil {
		t.Fatalf("ReadProgress: %v", err)
	}
	if want := `{"percentage":43,"message":"Scrubbing"}`; string(got) != want {
		t.Fatalf("progress = %s, want %s", got, want)
	}
}

func TestReporterOmitsAnUnknownPercentage(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	t.Setenv("INVOCATION_ID", testInvocation)

	ReporterFromEnv(h.Store).Report(-1, "Taking snapshots")

	got, err := h.Store.ReadProgress(testRunID)
	if err != nil {
		t.Fatalf("ReadProgress: %v", err)
	}
	if want := `{"message":"Taking snapshots"}`; string(got) != want {
		t.Fatalf("progress = %s, want %s", got, want)
	}
}

func TestReporterWithoutARunDoesNothing(t *testing.T) {
	h := newHooks(t)

	t.Setenv("INVOCATION_ID", "")
	if r := ReporterFromEnv(h.Store); r != nil {
		t.Errorf("unset INVOCATION_ID gave %v, want nil", r)
	} else {
		r.Report(10, "ignored")
	}

	// A valid invocation whose Begin never ran has no record.
	t.Setenv("INVOCATION_ID", testInvocation)
	if r := ReporterFromEnv(h.Store); r != nil {
		t.Errorf("a run without a record gave %v, want nil", r)
	} else {
		r.Report(10, "ignored")
	}
	if _, err := h.Store.ReadProgress(testRunID); err == nil {
		t.Error("progress was written without a record")
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestReporterLogsARecordItCannotRead(t *testing.T) {
	h := newHooks(t)
	t.Setenv("INVOCATION_ID", testInvocation)

	logs := captureLogs(t)
	if r := ReporterFromEnv(h.Store); r != nil {
		t.Fatalf("a run without a record gave %v, want nil", r)
	}
	if logs.Len() != 0 {
		t.Errorf("a missing record was logged: %s", logs)
	}

	mustBegin(t, h, testUnit, testInvocation)
	record := strings.TrimSuffix(h.Store.OutputPath(testRunID), ".log") + ".json"
	if err := os.WriteFile(record, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := ReporterFromEnv(h.Store); r != nil {
		t.Fatalf("an unreadable record gave %v, want nil", r)
	}
	line := logs.String()
	for _, want := range []string{"level=DEBUG", `msg="scheduled run record unreadable"`, "run=" + testRunID, "error="} {
		if !strings.Contains(line, want) {
			t.Errorf("log %q lacks %q", line, want)
		}
	}
}
