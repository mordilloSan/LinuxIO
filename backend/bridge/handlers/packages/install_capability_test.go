package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestCapabilityInstallReporterOmitsOutputRecords(t *testing.T) {
	registry := bridgetask.NewTaskService()
	task, err := registry.Create("system.install_capability", nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	reporter := capabilityInstallReporter(task, "samba", pctInstallStart, pctInstallEnd)
	pct := uint32(40)
	if err := reporter(&PkgUpdateProgress{Percentage: &pct, Status: "Downloading"}); err != nil {
		t.Fatalf("reporter: %v", err)
	}

	detail := lastInstallProgress(t, task)
	if detail.Output != nil {
		t.Fatalf("PackageKit tick attached output record %+v", *detail.Output)
	}
	if !strings.Contains(detail.Message, "Downloading") {
		t.Fatalf("progress message = %q, want PackageKit status", detail.Message)
	}

	reportProgress(task, stageInstallPackage, "Installed samba", pctInstallEnd)
	detail = lastInstallProgress(t, task)
	if detail.Output == nil || detail.Output.Stream != "status" || detail.Output.Text != "Installed samba" {
		t.Fatalf("stage progress output = %+v, want status record", detail.Output)
	}
}

func lastInstallProgress(t *testing.T, task *bridgetask.Task) InstallCapabilityProgress {
	t.Helper()
	progress := task.Snapshot().Progress
	if progress == nil {
		t.Fatal("task progress was not updated")
	}
	detail, ok := progress.Detail.(InstallCapabilityProgress)
	if !ok {
		t.Fatalf("progress detail = %T, want InstallCapabilityProgress", progress.Detail)
	}
	return detail
}

func TestRunCapabilityCommandForwardsSeparatedOutput(t *testing.T) {
	withCapabilityCommand(t, "output")

	var output []InstallCapabilityOutput
	err := runCapabilityCommand(context.Background(), "sensors-detect", []string{"--auto"}, func(record InstallCapabilityOutput) {
		output = append(output, record)
	})
	if err != nil {
		t.Fatalf("runCapabilityCommand: %v", err)
	}
	if len(output) < 3 {
		t.Fatalf("output records = %#v, want at least three records", output)
	}
	var stdout, stderr []string
	for _, record := range output {
		switch record.Stream {
		case "stdout":
			stdout = append(stdout, record.Text)
		case "stderr":
			stderr = append(stderr, record.Text)
		default:
			t.Fatalf("unexpected output stream %q", record.Stream)
		}
	}
	if !slices.Equal(stdout, []string{"stdout one\n", "stdout two\n"}) {
		t.Errorf("stdout records = %#v", stdout)
	}
	if !slices.Equal(stderr, []string{"stderr one\n"}) {
		t.Errorf("stderr records = %#v", stderr)
	}
}

func TestRunCapabilityCommandIncludesStderrOnFailure(t *testing.T) {
	withCapabilityCommand(t, "failure")

	err := runCapabilityCommand(context.Background(), "sensors-detect", []string{"--auto"}, nil)
	if err == nil || !strings.Contains(err.Error(), "sensors-detect") || !strings.Contains(err.Error(), "sensors-detect failed") {
		t.Fatalf("error = %v, want command and stderr", err)
	}
}

func TestRunCapabilityCommandBoundsIndividualOutputRecords(t *testing.T) {
	withCapabilityCommand(t, "long-output")

	var output strings.Builder
	maxRecordLength := 0
	err := runCapabilityCommand(context.Background(), "sensors-detect", []string{"--auto"}, func(record InstallCapabilityOutput) {
		if len(record.Text) > maxRecordLength {
			maxRecordLength = len(record.Text)
		}
		output.WriteString(record.Text)
	})
	if err != nil {
		t.Fatalf("runCapabilityCommand: %v", err)
	}
	want := strings.Repeat("x", capabilityOutputChunkBytes*2+17)
	if output.String() != want {
		t.Fatalf("reassembled output length = %d, want %d", output.Len(), len(want))
	}
	if maxRecordLength > capabilityOutputChunkBytes {
		t.Fatalf("output record length = %d, want at most %d", maxRecordLength, capabilityOutputChunkBytes)
	}
}

func TestRunCapabilityCommandReportsMissingCommand(t *testing.T) {
	original := capabilityCommandLookPath
	capabilityCommandLookPath = func(name string) (string, error) {
		return "", fmt.Errorf("%s is not installed", name)
	}
	t.Cleanup(func() { capabilityCommandLookPath = original })

	err := runCapabilityCommand(context.Background(), "sensors-detect", []string{"--auto"}, nil)
	if err == nil || !strings.Contains(err.Error(), "resolve sensors-detect") || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %v, want command resolution context", err)
	}
}

func TestRunCapabilityCommandHonorsCancellation(t *testing.T) {
	withCapabilityCommand(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- runCapabilityCommand(ctx, "sensors-detect", []string{"--auto"}, func(InstallCapabilityOutput) {
			select {
			case <-seen:
			default:
				close(seen)
			}
		})
	}()

	select {
	case <-seen:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("command did not produce output")
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled command did not exit")
	}
}

func TestInstallCapabilityInstallsAvahiNSSBeforeServiceActions(t *testing.T) {
	originalFamily := capabilityDistroFamily
	originalPackage := capabilityInstallPackage
	originalEnable := capabilityEnableService
	originalStart := capabilityStartService
	originalWait := capabilityWaitServiceActive
	originalDetect := capabilityDetectWithRetry
	t.Cleanup(func() {
		capabilityDistroFamily = originalFamily
		capabilityInstallPackage = originalPackage
		capabilityEnableService = originalEnable
		capabilityStartService = originalStart
		capabilityWaitServiceActive = originalWait
		capabilityDetectWithRetry = originalDetect
	})

	tests := []struct {
		family   string
		packages []string
	}{
		{family: "debian", packages: []string{"avahi-daemon", "libnss-mdns"}},
		{family: "rhel", packages: []string{"avahi", "nss-mdns"}},
		{family: "fedora", packages: []string{"avahi", "nss-mdns"}},
	}
	for _, test := range tests {
		t.Run(test.family, func(t *testing.T) {
			capabilityDistroFamily = func() string { return test.family }
			var order []string
			capabilityInstallPackage = func(_ context.Context, name string, _ pkgUpdateReporter) error {
				order = append(order, "package:"+name)
				return nil
			}
			capabilityEnableService = func(_ context.Context, service string) error {
				order = append(order, "enable:"+service)
				return nil
			}
			capabilityStartService = func(_ context.Context, service string) error {
				order = append(order, "start:"+service)
				return nil
			}
			capabilityWaitServiceActive = func(_ context.Context, service string, _ time.Duration) error {
				order = append(order, "wait:"+service)
				return nil
			}
			capabilityDetectWithRetry = func(_ context.Context, spec system.CapabilitySpec, _ time.Duration) (bool, string) {
				order = append(order, "detect:"+spec.Name)
				return true, ""
			}

			if _, err := installCapability(context.Background(), nil, "avahi"); err != nil {
				t.Fatalf("installCapability: %v", err)
			}
			want := make([]string, 0, len(test.packages)+4)
			for _, name := range test.packages {
				want = append(want, "package:"+name)
			}
			want = append(want,
				"enable:avahi-daemon.service",
				"start:avahi-daemon.service",
				"wait:avahi-daemon.service",
				"detect:avahi",
			)
			if !slices.Equal(order, want) {
				t.Fatalf("operation order = %v, want %v", order, want)
			}
		})
	}
}

func TestInstallCapabilityContinuesAvahiWhenRHELNSSIsUnavailable(t *testing.T) {
	originalFamily := capabilityDistroFamily
	originalPackage := capabilityInstallPackage
	originalEnable := capabilityEnableService
	originalStart := capabilityStartService
	originalWait := capabilityWaitServiceActive
	originalDetect := capabilityDetectWithRetry
	t.Cleanup(func() {
		capabilityDistroFamily = originalFamily
		capabilityInstallPackage = originalPackage
		capabilityEnableService = originalEnable
		capabilityStartService = originalStart
		capabilityWaitServiceActive = originalWait
		capabilityDetectWithRetry = originalDetect
	})

	capabilityDistroFamily = func() string { return "rhel" }
	var order []string
	capabilityInstallPackage = func(_ context.Context, name string, _ pkgUpdateReporter) error {
		order = append(order, "package:"+name)
		if name == "nss-mdns" {
			return errors.New("no enabled repository provides nss-mdns")
		}
		return nil
	}
	capabilityEnableService = func(_ context.Context, service string) error {
		order = append(order, "enable:"+service)
		return nil
	}
	capabilityStartService = func(_ context.Context, service string) error {
		order = append(order, "start:"+service)
		return nil
	}
	capabilityWaitServiceActive = func(_ context.Context, service string, _ time.Duration) error {
		order = append(order, "wait:"+service)
		return nil
	}
	capabilityDetectWithRetry = func(_ context.Context, spec system.CapabilitySpec, _ time.Duration) (bool, string) {
		order = append(order, "detect:"+spec.Name)
		return true, ""
	}

	registry := bridgetask.NewTaskService()
	task, err := registry.Create("system.install_capability", nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	result, err := installCapability(context.Background(), task, "avahi")
	if err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	_, replay, unsubscribe := task.SubscribeWithReplay(64)
	defer unsubscribe()
	wantOrder := []string{
		"package:avahi",
		"package:nss-mdns",
		"enable:avahi-daemon.service",
		"start:avahi-daemon.service",
		"wait:avahi-daemon.service",
		"detect:avahi",
	}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("operation order = %v, want %v", order, wantOrder)
	}
	if !result.Available {
		t.Fatal("result.Available = false, want true after responder detection")
	}
	if result.Error != nil {
		t.Fatalf("result.Error = %q, want nil for available responder", *result.Error)
	}
	if result.Warning == nil || *result.Warning != specOptionalRHELWarning(t, "avahi") {
		t.Fatalf("result.Warning = %v, want the Avahi-specific optional-package warning", result.Warning)
	}
	if strings.Contains(*result.Warning, "no enabled repository") {
		t.Fatalf("result.Warning contains raw package-manager error: %q", *result.Warning)
	}

	assertOptionalPackageWarningProgress(t, replay)
}

func TestInstallOptionalCapabilityPackageUsesGenericFallbackWarning(t *testing.T) {
	originalPackage := capabilityInstallPackage
	t.Cleanup(func() { capabilityInstallPackage = originalPackage })
	capabilityInstallPackage = func(_ context.Context, _ string, _ pkgUpdateReporter) error {
		return errors.New("package unavailable")
	}

	warning, err := installOptionalCapabilityPackage(
		context.Background(),
		nil,
		"rhel",
		"Example capability",
		&system.InstallSpec{OptionalPackageRHEL: "example-extra"},
	)
	if err != nil {
		t.Fatalf("installOptionalCapabilityPackage: %v", err)
	}
	if want := "Optional package example-extra was not installed; continuing without it."; warning != want {
		t.Fatalf("warning = %q, want %q", warning, want)
	}
}

func assertOptionalPackageWarningProgress(t *testing.T, replay []bridgetask.TaskEvent) {
	t.Helper()
	var lastPercentage *int
	warningOutput := false
	for _, event := range replay {
		progress, ok := event.Progress.(bridgetask.TaskProgress)
		if !ok {
			continue
		}
		if progress.Percentage != nil {
			percentage := *progress.Percentage
			if lastPercentage != nil && percentage < *lastPercentage {
				t.Fatalf("progress percentage moved backwards from %d to %d", *lastPercentage, percentage)
			}
			lastPercentage = &percentage
		}
		detail, ok := progress.Detail.(InstallCapabilityProgress)
		if ok && detail.Output != nil && detail.Output.Stream == "status" && strings.Contains(detail.Output.Text, "could not be installed") && strings.Contains(detail.Output.Text, "no enabled repository") {
			warningOutput = true
		}
	}
	if !warningOutput {
		t.Fatal("progress replay did not contain the optional-package warning")
	}
}

func specOptionalRHELWarning(t *testing.T, name string) string {
	t.Helper()
	spec, ok := system.CapabilitySpecByName(name)
	if !ok || spec.Install == nil {
		t.Fatalf("capability %q has no install specification", name)
	}
	return spec.Install.OptionalPackageRHELFailureWarning
}

func TestInstallCapabilityRunsSensorsDetectBeforeRedetection(t *testing.T) {
	withCapabilityCommand(t, "output")
	originalFamily := capabilityDistroFamily
	originalPackage := capabilityInstallPackage
	originalDetect := capabilityDetectWithRetry
	originalLookPath := capabilityCommandLookPath
	t.Cleanup(func() {
		capabilityDistroFamily = originalFamily
		capabilityInstallPackage = originalPackage
		capabilityDetectWithRetry = originalDetect
		capabilityCommandLookPath = originalLookPath
	})

	capabilityDistroFamily = func() string { return "debian" }
	var order []string
	capabilityInstallPackage = func(_ context.Context, name string, _ pkgUpdateReporter) error {
		order = append(order, "package:"+name)
		return nil
	}
	capabilityCommandLookPath = func(name string) (string, error) {
		order = append(order, "post-install:"+name)
		return originalLookPath(name)
	}
	capabilityDetectWithRetry = func(_ context.Context, spec system.CapabilitySpec, _ time.Duration) (bool, string) {
		order = append(order, "detect:"+spec.Name)
		return true, ""
	}

	if _, err := installCapability(context.Background(), nil, "lm_sensors"); err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	want := []string{"package:lm-sensors", "post-install:sensors-detect", "detect:lm_sensors"}
	if !slices.Equal(order, want) {
		t.Fatalf("operation order = %v, want %v", order, want)
	}
}

func withCapabilityCommand(t *testing.T, mode string) {
	t.Helper()
	originalLookPath := capabilityCommandLookPath
	originalExec := capabilityCommandExec
	capabilityCommandLookPath = func(name string) (string, error) {
		if name != "sensors-detect" {
			t.Fatalf("look path name = %q", name)
		}
		return "/usr/bin/sensors-detect", nil
	}
	capabilityCommandExec = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name != "/usr/bin/sensors-detect" {
			t.Fatalf("command name = %q", name)
		}
		if !slices.Equal(args, []string{"--auto"}) {
			t.Fatalf("command args = %v", args)
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperCapabilityCommand", "--")
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "CAPABILITY_COMMAND_MODE="+mode)
		return cmd
	}
	t.Cleanup(func() {
		capabilityCommandLookPath = originalLookPath
		capabilityCommandExec = originalExec
	})
}

func TestHelperCapabilityCommand(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	switch os.Getenv("CAPABILITY_COMMAND_MODE") {
	case "output":
		fmt.Fprint(os.Stdout, "stdout one\nstdout two\n")
		fmt.Fprint(os.Stderr, "stderr one\n")
	case "failure":
		fmt.Fprint(os.Stderr, "sensors-detect failed\n")
		os.Exit(7)
	case "long-output":
		fmt.Fprint(os.Stdout, strings.Repeat("x", capabilityOutputChunkBytes*2+17))
	case "cancel":
		for {
			fmt.Fprintln(os.Stdout, "still running")
			time.Sleep(10 * time.Millisecond)
		}
	default:
		t.Fatalf("unknown helper mode %q", os.Getenv("CAPABILITY_COMMAND_MODE"))
	}
	os.Exit(0)
}
