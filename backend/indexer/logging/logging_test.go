package logging

import (
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortSourceKeepsAtMostTwoPathComponents(t *testing.T) {
	tests := []struct {
		name string
		file string
		line int
		want string
	}{
		{name: "absolute path", file: "/home/user/indexer/cmd/daemon.go", line: 462, want: "cmd/daemon.go:462"},
		{name: "two components", file: "cmd/daemon.go", line: 17, want: "cmd/daemon.go:17"},
		{name: "one component", file: "daemon.go", line: 8, want: "daemon.go:8"},
		{name: "root file", file: "/daemon.go", line: 1, want: "daemon.go:1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shortSource(test.file, test.line); got != test.want {
				t.Fatalf("shortSource(%q, %d) = %q, want %q", test.file, test.line, got, test.want)
			}
		})
	}
}

func TestConfigureFallbackHonorsLevelAndBridgesStandardLog(t *testing.T) {
	infoOutput := captureConfiguredOutput(t, false, func() {
		slog.Debug("hidden debug")
		slog.Info("structured message", "request_id", 42)
		log.Print("legacy message")
	})

	if strings.Contains(infoOutput, "hidden debug") {
		t.Fatalf("info logger emitted a debug record:\n%s", infoOutput)
	}
	for _, want := range []string{
		`msg="structured message"`,
		"request_id=42",
		`msg="legacy message"`,
		"source=logging/logging_test.go:",
	} {
		if !strings.Contains(infoOutput, want) {
			t.Errorf("fallback output does not contain %q:\n%s", want, infoOutput)
		}
	}
	if strings.Contains(infoOutput, "time=") || strings.Contains(infoOutput, "level=") {
		t.Errorf("fallback output contains fields configured for suppression:\n%s", infoOutput)
	}

	debugOutput := captureConfiguredOutput(t, true, func() {
		slog.Debug("visible debug")
	})
	if !strings.Contains(debugOutput, `msg="visible debug"`) {
		t.Fatalf("verbose logger did not emit debug record:\n%s", debugOutput)
	}
}

func captureConfiguredOutput(t *testing.T, verbose bool, emit func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr.log")
	output, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log capture: %v", err)
	}

	originalStderr := os.Stderr
	originalDefault := slog.Default()
	originalLogWriter := log.Writer()
	originalLogFlags := log.Flags()
	originalLogPrefix := log.Prefix()
	restored := false
	restore := func() {
		if restored {
			return
		}
		slog.SetDefault(originalDefault)
		log.SetOutput(originalLogWriter)
		log.SetFlags(originalLogFlags)
		log.SetPrefix(originalLogPrefix)
		os.Stderr = originalStderr
		restored = true
	}
	closed := false
	t.Cleanup(func() {
		restore()
		if !closed {
			if closeErr := output.Close(); closeErr != nil {
				t.Errorf("close log capture during cleanup: %v", closeErr)
			}
		}
	})

	os.Stderr = output
	Configure(" ", verbose)
	emit()
	restore()

	if syncErr := output.Sync(); syncErr != nil {
		t.Fatalf("sync log capture: %v", syncErr)
	}
	if closeErr := output.Close(); closeErr != nil {
		t.Fatalf("close log capture: %v", closeErr)
	}
	closed = true

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log capture: %v", err)
	}
	return string(data)
}
