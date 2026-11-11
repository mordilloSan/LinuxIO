package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{name: "default", want: dockerupdate.ConfigPath, ok: true},
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

func TestRunCLIDispatchesInjectedOperations(t *testing.T) {
	originalRun := runUpdates
	t.Cleanup(func() { runUpdates = originalRun })

	var gotConfig string
	runUpdates = func(ctx context.Context, configPath string, _ func(string)) error {
		gotConfig = configPath
		return nil
	}
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run", "--config", "/tmp/custom"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("run exit code = %d, want 0", code)
	}
	if gotConfig != "/tmp/custom" {
		t.Fatalf("run config = %q, want custom path", gotConfig)
	}
}

func TestRunCLIExitCodesAndErrors(t *testing.T) {
	originalRun := runUpdates
	t.Cleanup(func() { runUpdates = originalRun })
	runUpdates = func(context.Context, string, func(string)) error { return errors.New("docker unavailable") }

	var stderr bytes.Buffer
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "run failed: docker unavailable") {
		t.Fatalf("run failure = code %d, stderr %q", code, stderr.String())
	}
	// A pass whose every failure has its container's alert says so, and the
	// end hook raises no run alert; one failure without an alert undoes that.
	reported := scheduledrun.Reported(errors.New("web: pull failed"))
	runUpdates = func(context.Context, string, func(string)) error { return reported }
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &stderr); code != scheduledrun.ExitReported {
		t.Fatalf("reported failure exit code = %d, want %d", code, scheduledrun.ExitReported)
	}
	runUpdates = func(context.Context, string, func(string)) error {
		return errors.Join(reported, errors.New("a scheduled container is gone"))
	}
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &stderr); code != 1 {
		t.Fatalf("partly reported failure exit code = %d, want 1", code)
	}
	runUpdates = func(context.Context, string, func(string)) error {
		return scheduledrun.Skipped(errors.New("another Docker update is already in progress"))
	}
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &stderr); code != scheduledrun.ExitSkipped {
		t.Fatalf("skipped pass exit code = %d, want %d", code, scheduledrun.ExitSkipped)
	}
	stderr.Reset()
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "bogus"}, &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown command = code %d, stderr %q", code, stderr.String())
	}
	stderr.Reset()
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run", "--config"}, &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires a path") {
		t.Fatalf("missing config = code %d, stderr %q", code, stderr.String())
	}
}

func TestRunCLIReportsUpdateStepsAsUnknownPercentage(t *testing.T) {
	originalRun, originalReport := runUpdates, reportProgress
	t.Cleanup(func() { runUpdates, reportProgress = originalRun, originalReport })

	runUpdates = func(_ context.Context, _ string, report func(string)) error {
		report("Checking for updates")
		return nil
	}
	var percentage int
	var message string
	reportProgress = func(p int, m string) { percentage, message = p, m }
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("run exit code = %d, want 0", code)
	}
	if percentage != -1 || message != "Checking for updates" {
		t.Fatalf("reported (%d, %q), want (-1, %q)", percentage, message, "Checking for updates")
	}
}

func TestRunWorkerConfiguresLoggingBeforeDispatch(t *testing.T) {
	originalConfigure := configureLogging
	originalRun := runUpdates
	originalReporter, originalReport := newReporter, reportProgress
	t.Cleanup(func() {
		configureLogging = originalConfigure
		runUpdates = originalRun
		newReporter, reportProgress = originalReporter, originalReport
	})

	configured := false
	configureLogging = func(identifier string, verbose bool) error {
		if identifier != "linuxio-docker-update" || verbose {
			t.Fatalf("logging config = %q, %v", identifier, verbose)
		}
		configured = true
		return nil
	}
	// The reporter logs a record it cannot read, so it is built once the
	// logger is configured.
	reporterBuilt := false
	newReporter = func() func(int, string) {
		if !configured {
			t.Fatal("progress reporter built before logging was configured")
		}
		reporterBuilt = true
		return func(int, string) {}
	}
	runUpdates = func(context.Context, string, func(string)) error {
		if !configured {
			t.Fatal("update dispatched before logging was configured")
		}
		return nil
	}
	if code := runWorker(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("runWorker exit code = %d, want 0", code)
	}
	if !reporterBuilt {
		t.Fatal("runWorker did not build the progress reporter")
	}

	configureLogging = func(string, bool) error { return errors.New("journald unavailable") }
	var stderr bytes.Buffer
	if code := runWorker(context.Background(), []string{"linuxio-docker-update", "run"}, &bytes.Buffer{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "initialize logging") {
		t.Fatalf("logging failure = code %d, stderr %q", code, stderr.String())
	}
}
