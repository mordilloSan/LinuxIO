package logguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSource(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.go")
	src := "package sample\n\nimport (\n\t\"fmt\"\n\t\"log/slog\"\n)\n\nvar _ = fmt.Sprint\n\n" + body
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMessageStyleFlagsEachRule(t *testing.T) {
	tests := []struct {
		name string
		call string
		want string
	}{
		{"capital", `slog.Info("Unmounted filesystem")`, "starts with a capital letter"},
		{"tag", `slog.Warn("[proxy] failed")`, "[tag]"},
		{"prefix", `slog.Warn("status: failed")`, "Prefix: "},
		{"acronym prefix", `slog.Warn("NVML: failed")`, "Prefix: "},
		{"trailing period", `slog.Debug("done.")`, "ends with punctuation"},
		{"trailing colon", `slog.Debug("failed:")`, "ends with punctuation"},
		{"trailing bang", `slog.Debug("failed!")`, "ends with punctuation"},
		{"err key", `slog.Warn("failed", "path", p, "err", err)`, `"err"`},
		{"err key after attr", `slog.Warn("failed", slog.String("err", e))`, `"err"`},
		{"context variant", `slog.WarnContext(ctx, "Failed")`, "capital"},
		{"concatenation", `slog.Warn("failed " + p)`, "built with +"},
		{"sprintf", `slog.Warn(fmt.Sprintf("failed %s", p))`, "fmt.Sprintf"},
		{"logger receiver", `logger.Info("Started")`, "capital"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSource(t, "func f(ctx interface{}, p string, e string, err error, logger interface{ Info(string, ...any) }) {\n\t"+tt.call+"\n}\n")
			got, err := MessageStyle(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || !strings.Contains(got[0], tt.want) || !strings.Contains(got[0], "sample.go:") {
				t.Fatalf("got %q, want one violation containing %q", got, tt.want)
			}
		})
	}
}

func TestMessageStyleAcceptsCleanCalls(t *testing.T) {
	path := writeSource(t, `func f(ctx interface{}, p string, err error) {
	slog.Info("unmounted filesystem", "path", p)
	slog.Warn("NVML unavailable", "error", err)
	slog.Info("Samba share created", "name", p)
	slog.Info("SIGHUP received", "pid", 1)
	slog.Debug("D-Bus call failed", slog.String("error", p))
	slog.Error(msg, "path", p)
	slog.Info("")
}

var msg = "Dynamic"
`)
	got, err := MessageStyle(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("clean file reported %q", got)
	}
}

func TestLogThenReturn(t *testing.T) {
	path := writeSource(t, `func bad(err error) error {
	slog.Error("failed", "error", err)
	return err
}

func swallowed(err error) error {
	slog.Warn("best effort failed", "error", err)
	return nil
}
`)
	got, err := LogThenReturn(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "log or return, not both") {
		t.Fatalf("got %q, want one log-then-return violation", got)
	}
}
