package system

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestFetchSystemInfoRoutesGHWWarningsThroughDefaultLogger(t *testing.T) {
	t.Setenv("GHW_CHROOT", t.TempDir())
	for _, key := range []string{"GHW_DISABLE_WARNINGS", "GHW_LOG_LEVEL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetenv(%q): %v", key, err)
		}
	}

	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)

	handler := &capturingSlogHandler{}
	slog.SetDefault(slog.New(handler))

	if _, err := FetchSystemInfo(context.Background()); err != nil {
		t.Fatalf("FetchSystemInfo() error = %v", err)
	}

	var warnings []slog.Record
	for _, record := range handler.records {
		if record.Level == slog.LevelWarn {
			warnings = append(warnings, record)
		}
	}

	// chassis (5), product (7), and BIOS (3) each query the missing DMI tree.
	if len(warnings) != 15 {
		t.Fatalf("warning record count = %d, want 15", len(warnings))
	}
	for _, record := range warnings {
		if !strings.HasPrefix(record.Message, "Unable to read ") {
			t.Errorf("warning message = %q, want ghw DMI warning", record.Message)
		}
	}
}

type capturingSlogHandler struct {
	records []slog.Record
}

func (h *capturingSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *capturingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record)
	return nil
}

func (h *capturingSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *capturingSlogHandler) WithGroup(string) slog.Handler {
	return h
}
