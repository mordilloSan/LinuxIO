package logguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSource(t *testing.T, body string) string {
	t.Helper()
	return writeSourceIn(t, "", body)
}

// writeSourceIn writes sample.go in the folder dir of a temporary tree.
func writeSourceIn(t *testing.T, dir, body string) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), dir)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "sample.go")
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

// A Docker update pass runs from a timer, and the saved output of a run shows
// messages without their attributes, so its lines about one container end
// with its name, and keep the attribute.
func TestMessageStyleLetsADockerUpdateLineNameItsContainer(t *testing.T) {
	const update = "backend/common/dockerupdate"
	tests := []struct {
		name string
		dir  string
		call string
		want string // empty for a clean call
	}{
		{"named", update, `slog.Info("updated Compose Docker container "+p, "container", p)`, ""},
		{"named with slog.String", update, `slog.Warn("failed to record Docker update alert for container "+p, slog.String("container", p), "error", err)`, ""},
		{"no attribute", update, `slog.Info("updated Compose Docker container "+p, "image", p)`, `"container" attribute`},
		// The name in the message is the value of the "container" attribute.
		{"an error appended", update, `slog.Warn("failed to update Docker container "+err.Error(), "container", p)`, `"container" attribute`},
		{"another variable", update, `slog.Info("updated Compose Docker container "+q, "container", p)`, `"container" attribute`},
		{"a key variable before it", update, `slog.Info("updated Compose Docker container "+p, key, "container")`, `"container" attribute`},
		{"after a key variable", update, `slog.Info("updated Compose Docker container "+p, key, q, "container", p)`, ""},
		{"not a container's name", update, `slog.Info("updated "+p, "container", p)`, "built with +"},
		{"more after the name", update, `slog.Info("updated Compose Docker container "+p+" now", "container", p)`, "built with +"},
		{"capital", update, `slog.Info("Updated container "+p, "container", p)`, "capital"},
		{"another package", "backend/bridge/handlers/docker", `slog.Info("updated Docker container "+p, "container", p)`, "built with +"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSourceIn(t, tt.dir, "func f(p, q, key string, err error) {\n\t"+tt.call+"\n}\n")
			got, err := MessageStyle(path)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("got %q, want no violation", got)
				}
				return
			}
			// The reason, after the position: the temporary path holds the
			// test's name.
			if len(got) != 1 {
				t.Fatalf("got %q, want one violation containing %q", got, tt.want)
			}
			if _, reason, _ := strings.Cut(got[0], "sample.go:"); !strings.Contains(reason, tt.want) {
				t.Fatalf("got %q, want one violation containing %q", got, tt.want)
			}
		})
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
