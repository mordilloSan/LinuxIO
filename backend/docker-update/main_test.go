package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
)

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{name: "default", want: docker.DockerUpdateConfigPath, ok: true},
		{name: "custom", args: []string{"--config", "/tmp/update.json"}, want: "/tmp/update.json", ok: true},
		{name: "missing path", args: []string{"--config"}, ok: false},
		{name: "blank path", args: []string{"--config", "  "}, ok: false},
		{name: "unknown argument", args: []string{"--verbose"}, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got, ok := parseRunArgs(tt.args, &stderr)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("parseRunArgs(%v) = %q, %v; want %q, %v", tt.args, got, ok, tt.want, tt.ok)
			}
			if !tt.ok && stderr.Len() == 0 {
				t.Fatal("expected a usage error")
			}
		})
	}
}

func TestParseOperationArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{name: "valid", args: []string{"--id", "00000000-0000-4000-8000-000000000001"}, want: "00000000-0000-4000-8000-000000000001", ok: true},
		{name: "missing", ok: false},
		{name: "blank", args: []string{"--id", "  "}, ok: false},
		{name: "unknown", args: []string{"--verbose"}, ok: false},
		{name: "duplicate", args: []string{"--id", "one", "--id", "two"}, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got, ok := parseOperationArgs(tt.args, &stderr)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("parseOperationArgs(%v) = %q, %v; want %q, %v", tt.args, got, ok, tt.want, tt.ok)
			}
			if !tt.ok && stderr.Len() == 0 {
				t.Fatal("expected a usage error")
			}
		})
	}
}

func TestRunCLIDispatchesInjectedOperations(t *testing.T) {
	originalRun := runUpdates
	originalOperation := runOperation
	t.Cleanup(func() { runUpdates = originalRun })
	t.Cleanup(func() { runOperation = originalOperation })

	var gotConfig string
	runUpdates = func(ctx context.Context, configPath string) error {
		gotConfig = configPath
		return nil
	}
	if code := runCLI([]string{"linuxio-docker-update", "run", "--config", "/tmp/custom"}, context.Background(), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("run exit code = %d, want 0", code)
	}
	if gotConfig != "/tmp/custom" {
		t.Fatalf("run config = %q, want custom path", gotConfig)
	}
	var gotOperation string
	runOperation = func(ctx context.Context, operationID string) error {
		gotOperation = operationID
		return nil
	}
	if code := runCLI([]string{"linuxio-docker-update", "run-operation", "--id", "00000000-0000-4000-8000-000000000001"}, context.Background(), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("run-operation exit code = %d, want 0", code)
	}
	if gotOperation == "" {
		t.Fatal("run-operation did not receive an operation ID")
	}
}

func TestRunCLIExitCodesAndErrors(t *testing.T) {
	originalRun := runUpdates
	originalOperation := runOperation
	t.Cleanup(func() {
		runUpdates = originalRun
		runOperation = originalOperation
	})
	runUpdates = func(context.Context, string) error { return errors.New("docker unavailable") }

	var stderr bytes.Buffer
	if code := runCLI([]string{"linuxio-docker-update", "run"}, context.Background(), &bytes.Buffer{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "run failed: docker unavailable") {
		t.Fatalf("run failure = code %d, stderr %q", code, stderr.String())
	}
	stderr.Reset()
	if code := runCLI([]string{"linuxio-docker-update", "bogus"}, context.Background(), &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown command = code %d, stderr %q", code, stderr.String())
	}
	stderr.Reset()
	if code := runCLI([]string{"linuxio-docker-update", "run", "--config"}, context.Background(), &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires a path") {
		t.Fatalf("missing config = code %d, stderr %q", code, stderr.String())
	}
	runOperation = func(context.Context, string) error { return errors.New("operation failed") }
	stderr.Reset()
	if code := runCLI([]string{"linuxio-docker-update", "run-operation", "--id", "00000000-0000-4000-8000-000000000001"}, context.Background(), &bytes.Buffer{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "run-operation failed") {
		t.Fatalf("run-operation failure = code %d, stderr %q", code, stderr.String())
	}
}

func TestRunWorkerConfiguresLoggingBeforeDispatch(t *testing.T) {
	originalConfigure := configureLogging
	originalRun := runUpdates
	t.Cleanup(func() {
		configureLogging = originalConfigure
		runUpdates = originalRun
	})

	configured := false
	configureLogging = func(identifier string, verbose bool) error {
		if identifier != "linuxio-docker-update" || verbose {
			t.Fatalf("logging config = %q, %v", identifier, verbose)
		}
		configured = true
		return nil
	}
	runUpdates = func(context.Context, string) error {
		if !configured {
			t.Fatal("update dispatched before logging was configured")
		}
		return nil
	}
	if code := runWorker([]string{"linuxio-docker-update", "run"}, context.Background(), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("runWorker exit code = %d, want 0", code)
	}

	configureLogging = func(string, bool) error { return errors.New("journald unavailable") }
	var stderr bytes.Buffer
	if code := runWorker([]string{"linuxio-docker-update", "run"}, context.Background(), &bytes.Buffer{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "initialize logging") {
		t.Fatalf("logging failure = code %d, stderr %q", code, stderr.String())
	}
}
