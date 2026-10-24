package logging

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
)

func TestReportFatalLogsAtErrorAndPrintsOnlyOffJournal(t *testing.T) {
	var logs bytes.Buffer
	prevLogger, prevConfigured := slog.Default(), configured
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prevLogger)
		configured = prevConfigured
	})

	// report runs ReportFatal and returns what it wrote to stderr.
	report := func(t *testing.T) string {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		prevStderr := os.Stderr
		os.Stderr = w
		defer func() { os.Stderr = prevStderr }()

		logs.Reset()
		ReportFatal(errors.New("boom"))
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}

	t.Run("under journal", func(t *testing.T) {
		configured = true
		t.Setenv("JOURNAL_STREAM", "1:2")
		if got := report(t); got != "" {
			t.Fatalf("stderr = %q, want empty", got)
		}
		for _, want := range []string{"level=ERROR", "daemon exited", "boom"} {
			if !bytes.Contains(logs.Bytes(), []byte(want)) {
				t.Fatalf("log %q missing %q", logs.String(), want)
			}
		}
	})

	t.Run("interactive", func(t *testing.T) {
		configured = true
		t.Setenv("JOURNAL_STREAM", "")
		if got := report(t); got != "boom\n" {
			t.Fatalf("stderr = %q, want %q", got, "boom\n")
		}
	})

	t.Run("logger setup failed", func(t *testing.T) {
		configured = false
		t.Setenv("JOURNAL_STREAM", "")
		if got := report(t); got != "" {
			t.Fatalf("stderr = %q, want empty", got)
		}
	})
}

func TestSetDebugIsIdempotent(t *testing.T) {
	t.Cleanup(func() { SetDebug(false) })

	SetDebug(true)
	SetDebug(true)
	if !DebugEnabled() || level.Level() != slog.LevelDebug {
		t.Fatalf("level = %v, want debug", level.Level())
	}
	SetDebug(false)
	if DebugEnabled() || level.Level() != slog.LevelInfo {
		t.Fatalf("level = %v, want info", level.Level())
	}
}
