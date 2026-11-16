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
	runUpdates = func(ctx context.Context, configPath string, _ func(string)) (dockerupdate.PassReport, error) {
		gotConfig = configPath
		return dockerupdate.PassReport{}, nil
	}
	if code := runCLI(context.Background(), []string{"linuxio-docker-update", "run", "--config", "/tmp/custom"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("run exit code = %d, want 0", code)
	}
	if gotConfig != "/tmp/custom" {
		t.Fatalf("run config = %q, want custom path", gotConfig)
	}
}

// passHarness runs "linuxio-docker-update run" over a stubbed pass and a
// stubbed summary store.
type passHarness struct {
	stored   []scheduledrun.Summary
	storeErr error
}

func newPassHarness(t *testing.T) *passHarness {
	t.Helper()
	originalRun, originalSummarize := runUpdates, summarize
	t.Cleanup(func() { runUpdates, summarize = originalRun, originalSummarize })
	h := &passHarness{}
	summarize = func(_ context.Context, summary scheduledrun.Summary) error {
		if h.storeErr != nil {
			return h.storeErr
		}
		h.stored = append(h.stored, summary)
		return nil
	}
	return h
}

// run runs one pass that ends with report and err.
func (h *passHarness) run(report dockerupdate.PassReport, err error) (code int, stdout, stderr string) {
	runUpdates = func(context.Context, string, func(string)) (dockerupdate.PassReport, error) { return report, err }
	var out, errOut bytes.Buffer
	h.stored = nil
	code = runCLI(context.Background(), []string{"linuxio-docker-update", "run"}, &out, &errOut)
	return code, out.String(), errOut.String()
}

func failedProxyPass() (dockerupdate.PassReport, error, error) {
	cause := errors.New("container keeps restarting")
	return dockerupdate.PassReport{
		Mode:    "update",
		Checked: dockerupdate.DockerUpdateCheckResult{Checked: 3, Updates: 1},
		Containers: []dockerupdate.ContainerOutcome{
			{Name: "docker-proxy", Image: "example/proxy:latest", OldImageID: "sha256:old", Err: cause},
		},
	}, scheduledrun.Reported(cause), cause
}

func TestRunCLIFailsOnlyForWhatNoAlertReports(t *testing.T) {
	h := newPassHarness(t)

	// A pass that ended before it checked anything prints and stores nothing.
	code, stdout, stderr := h.run(dockerupdate.PassReport{}, errors.New("docker unavailable"))
	if code != 1 || stdout != "" || len(h.stored) != 0 || !strings.Contains(stderr, "run failed: docker unavailable") {
		t.Fatalf("run failure = code %d, stdout %q, stderr %q, stored %+v", code, stdout, stderr, h.stored)
	}

	// A container that failed raised its own alert: the pass did its work, so
	// the unit does not fail, and the report and the summary name the failure.
	report, reported, _ := failedProxyPass()
	code, stdout, stderr = h.run(report, reported)
	if code != 0 || stderr != "" {
		t.Fatalf("reported failure = code %d, stderr %q; want 0 and no run failure", code, stderr)
	}
	if !strings.Contains(stdout, "Container updates: 3 checked, 1 failed") || !strings.Contains(stdout, "docker-proxy") || !strings.Contains(stdout, "container keeps restarting") {
		t.Fatalf("stdout = %q, want the totals and the failed container", stdout)
	}
	if len(h.stored) != 1 || h.stored[0].Message != "1 failed" || h.stored[0].Failures != 1 || h.stored[0].Skipped {
		t.Fatalf("stored summary = %+v, want one failure", h.stored)
	}

	// One failure without an alert fails the run, and only it is printed as
	// the run's failure: the report already names the container's.
	code, _, stderr = h.run(report, errors.Join(reported, errors.New("a scheduled container is gone")))
	if code != 1 || !strings.Contains(stderr, "run failed: a scheduled container is gone") || strings.Contains(stderr, "keeps restarting") {
		t.Fatalf("partly reported failure = code %d, stderr %q", code, stderr)
	}
}

func TestRunCLISkippedPassCompletes(t *testing.T) {
	h := newPassHarness(t)
	skipped := dockerupdate.PassReport{Skipped: "another Docker update was running"}

	// A pass another update held off did no work: it completes and says so.
	code, stdout, stderr := h.run(skipped, nil)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "skipped, another Docker update was running") {
		t.Fatalf("skipped pass = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(h.stored) != 1 || !h.stored[0].Skipped || h.stored[0].Failures != 0 {
		t.Fatalf("stored summary = %+v, want a skipped run", h.stored)
	}

	// Without its summary the end hook would read the skipped pass as one
	// that ran, so the exit status says it instead; a pass that ran still
	// exits with 0, since its completed run is read correctly without one.
	h.storeErr = scheduledrun.ErrNoRun
	if code, _, _ := h.run(skipped, nil); code != scheduledrun.ExitSkipped {
		t.Fatalf("skipped pass without a stored summary = code %d, want %d", code, scheduledrun.ExitSkipped)
	}
	report, reported, _ := failedProxyPass()
	if code, _, _ := h.run(report, reported); code != 0 {
		t.Fatalf("reported failure without a stored summary = code %d, want 0", code)
	}
}

func TestRunCLIRejectsBadArguments(t *testing.T) {
	var stderr bytes.Buffer
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

	runUpdates = func(_ context.Context, _ string, report func(string)) (dockerupdate.PassReport, error) {
		report("Checking for updates")
		return dockerupdate.PassReport{}, nil
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
	originalReporter, originalReport, originalSummarize := newReporter, reportProgress, summarize
	t.Cleanup(func() {
		configureLogging = originalConfigure
		runUpdates = originalRun
		newReporter, reportProgress, summarize = originalReporter, originalReport, originalSummarize
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
	newReporter = func() (func(int, string), func(context.Context, scheduledrun.Summary) error) {
		if !configured {
			t.Fatal("progress reporter built before logging was configured")
		}
		reporterBuilt = true
		return func(int, string) {}, func(context.Context, scheduledrun.Summary) error { return nil }
	}
	runUpdates = func(context.Context, string, func(string)) (dockerupdate.PassReport, error) {
		if !configured {
			t.Fatal("update dispatched before logging was configured")
		}
		return dockerupdate.PassReport{}, nil
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
