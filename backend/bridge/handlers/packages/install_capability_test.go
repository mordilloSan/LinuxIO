package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/capabilities"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestCapabilityInstallReporterOmitsOutputRecords(t *testing.T) {
	registry := bridgeipc.NewTaskService()
	task, _, err := registry.ClaimForOwnerWithIdentity("system.install_capability", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
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

func lastInstallProgress(t *testing.T, task *bridgeipc.Task) InstallCapabilityProgress {
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

func TestRunCapabilityProcessCancelStopsPipeHoldingChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo started; sleep 30 & wait")
	seen := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- runCapabilityProcess(ctx, cmd, "sh", func(InstallCapabilityOutput) {
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
		t.Fatal("canceled command did not exit while a child held the pipes")
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
			capabilityDetectWithRetry = func(_ context.Context, spec capabilities.CapabilitySpec, _ session.User, _ time.Duration) (bool, string) {
				order = append(order, "detect:"+spec.Name)
				return true, ""
			}

			if _, err := installCapability(context.Background(), nil, session.User{}, "avahi"); err != nil {
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
	capabilityDetectWithRetry = func(_ context.Context, spec capabilities.CapabilitySpec, _ session.User, _ time.Duration) (bool, string) {
		order = append(order, "detect:"+spec.Name)
		return true, ""
	}

	registry := bridgeipc.NewTaskService()
	task, _, err := registry.ClaimForOwnerWithIdentity("system.install_capability", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	result, err := installCapability(context.Background(), task, session.User{}, "avahi")
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
		&capabilities.InstallSpec{OptionalPackageRHEL: "example-extra"},
	)
	if err != nil {
		t.Fatalf("installOptionalCapabilityPackage: %v", err)
	}
	if want := "Optional package example-extra was not installed; continuing without it."; warning != want {
		t.Fatalf("warning = %q, want %q", warning, want)
	}
}

func assertOptionalPackageWarningProgress(t *testing.T, replay []bridgeipc.TaskEvent) {
	t.Helper()
	var lastPercentage *int
	warningOutput := false
	for _, event := range replay {
		progress, ok := event.Progress.(bridgeipc.TaskProgress)
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
	spec, ok := capabilities.CapabilitySpecByName(name)
	if !ok || spec.Install == nil {
		t.Fatalf("capability %q has no install specification", name)
	}
	return spec.Install.OptionalPackageRHELFailureWarning
}

// hookRun records what an install with a fake zram package hook did.
type hookRun struct {
	order []string
	// afterCtxErr is the context error the step after the package saw.
	afterCtxErr error
	// onAfter runs in the step after the package, as a cancel that lands
	// while it runs.
	onAfter func()
}

// captureLogs sends the default logger's lines, Debug ones included, to a
// buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

// withZramHook installs zram on family through fakes: the registry's zram
// spec with its hook replaced by one whose step after the package returns
// afterWarning and afterErr, and a package step that returns packageErr.
func withZramHook(t *testing.T, family string, packageStep func(ctx context.Context) error, afterWarning string, afterErr error) *hookRun {
	t.Helper()
	originalFamily := capabilityDistroFamily
	originalPackage := capabilityInstallPackage
	originalDetect := capabilityDetectWithRetry
	originalSpec := capabilitySpecByName
	t.Cleanup(func() {
		capabilityDistroFamily = originalFamily
		capabilityInstallPackage = originalPackage
		capabilityDetectWithRetry = originalDetect
		capabilitySpecByName = originalSpec
	})
	run := &hookRun{}
	capabilitySpecByName = func(name string) (capabilities.CapabilitySpec, bool) {
		spec, ok := originalSpec(name)
		install := *spec.Install
		install.PackageHookDebian = func(context.Context) (func(context.Context) (string, error), error) {
			run.order = append(run.order, "hook:before")
			return func(ctx context.Context) (string, error) {
				run.order = append(run.order, "hook:after")
				run.afterCtxErr = ctx.Err()
				if run.onAfter != nil {
					run.onAfter()
				}
				return afterWarning, afterErr
			}, nil
		}
		spec.Install = &install
		return spec, ok
	}
	capabilityDistroFamily = func() string { return family }
	capabilityInstallPackage = func(ctx context.Context, name string, _ pkgUpdateReporter) error {
		run.order = append(run.order, "package:"+name)
		if packageStep != nil {
			return packageStep(ctx)
		}
		return nil
	}
	capabilityDetectWithRetry = func(_ context.Context, spec capabilities.CapabilitySpec, _ session.User, _ time.Duration) (bool, string) {
		run.order = append(run.order, "detect:"+spec.Name)
		return true, ""
	}
	return run
}

// zram's package hook runs around the package step only where zram-tools is
// the package installed: it reads the host before the package and acts
// after it, before the capability is detected again, and its warning is the
// result's. Fedora's and RHEL's zram-generator starts nothing as it
// installs, so their install runs no hook.
func TestInstallCapabilityRunsThePackageHookOnDebianOnly(t *testing.T) {
	if spec, ok := capabilities.CapabilitySpecByName("zram"); !ok || spec.Install == nil || spec.Install.PackageHookDebian == nil {
		t.Fatal("zram's install has no Debian package hook")
	}

	tests := []struct {
		family      string
		wantOrder   []string
		wantWarning string
	}{
		{family: "debian", wantOrder: []string{"hook:before", "package:zram-tools", "hook:after", "detect:zram"}, wantWarning: "zram was left on"},
		{family: "rhel", wantOrder: []string{"package:zram-generator", "detect:zram"}},
		{family: "fedora", wantOrder: []string{"package:zram-generator", "detect:zram"}},
	}
	for _, test := range tests {
		t.Run(test.family, func(t *testing.T) {
			run := withZramHook(t, test.family, nil, "zram was left on", nil)

			result, err := installCapability(context.Background(), nil, session.User{}, "zram")
			if err != nil {
				t.Fatalf("installCapability: %v", err)
			}
			if !slices.Equal(run.order, test.wantOrder) {
				t.Fatalf("operation order = %v, want %v", run.order, test.wantOrder)
			}
			if got := result.Warning; (got == nil) != (test.wantWarning == "") || (got != nil && *got != test.wantWarning) {
				t.Fatalf("result.Warning = %v, want %q", got, test.wantWarning)
			}
		})
	}
}

// A hook step that fails after the package is in fails the install, saying
// the package was installed.
func TestInstallCapabilityFailsWhenThePackageHookFails(t *testing.T) {
	withZramHook(t, "debian", nil, "", errors.New("disable failed"))

	_, err := installCapability(context.Background(), nil, session.User{}, "zram")
	if err == nil || !strings.Contains(err.Error(), "zram-tools was installed") || !strings.Contains(err.Error(), "disable failed") {
		t.Fatalf("err = %v, want the hook's error after the package was installed", err)
	}
}

// The step after the package always runs, so what the hook holds is let go:
// after a package step that failed, whose error is the install's, joined
// with the hook's on one line, since the Capabilities row and the toast show
// it as text.
func TestInstallCapabilityRunsThePackageHookAfterAFailedPackageStep(t *testing.T) {
	packageErr := errors.New("dpkg failed")
	hookErr := errors.New("reload failed")
	run := withZramHook(t, "debian", func(context.Context) error { return packageErr }, "", hookErr)

	_, err := installCapability(context.Background(), nil, session.User{}, "zram")
	if !errors.Is(err, packageErr) || !errors.Is(err, hookErr) {
		t.Fatalf("err = %v, want the package's error and the hook's", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("err = %q, want one line", err)
	}
	if want := []string{"hook:before", "package:zram-tools", "hook:after"}; !slices.Equal(run.order, want) {
		t.Fatalf("operation order = %v, want %v", run.order, want)
	}
}

// A cancelled install still runs the step after the package, on a context
// the cancel does not reach, and ends cancelled. A cancelled job reports
// only that it was cancelled, so the step's error is logged.
func TestInstallCapabilityRunsThePackageHookAfterACancel(t *testing.T) {
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := withZramHook(t, "debian", func(ctx context.Context) error {
		cancel()
		return ctx.Err()
	}, "", errors.New("reload failed"))

	_, err := runInstallCapabilityTask(ctx, nil, session.User{}, apischema.CapabilityRequest{Capability: "zram"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	if want := []string{"hook:before", "package:zram-tools", "hook:after"}; !slices.Equal(run.order, want) {
		t.Fatalf("operation order = %v, want %v", run.order, want)
	}
	if run.afterCtxErr != nil {
		t.Fatalf("the step after the package ran on a done context: %v", run.afterCtxErr)
	}
	if !strings.Contains(logs.String(), "package hook failed after a cancelled install") || !strings.Contains(logs.String(), "reload failed") {
		t.Fatalf("logs = %q, want the hook's error recorded", logs.String())
	}
}

// A cancel while PackageKit installs zram-tools runs the step after the
// package only once PackageKit's transaction has ended, so the hold and the
// lock it releases outlive the package's scripts.
func TestInstallCapabilityRunsThePackageHookAfterTheCancelledTransactionEnds(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setResolvable("zram-tools", fakeResolvable{id: hungPackageID})
	service.setInstallHang(hungPackageID)
	run := withZramHook(t, "debian", func(ctx context.Context) error {
		return InstallByNameWithProgress(ctx, "zram-tools", nil)
	}, "", nil)
	afterRan := make(chan struct{})
	run.onAfter = func() { close(afterRan) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runInstallCapabilityTask(ctx, nil, session.User{}, apischema.CapabilityRequest{Capability: "zram"})
		done <- err
	}()
	var tx *fakeTransaction
	select {
	case tx = <-service.installStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("PackageKit was not asked to install zram-tools")
	}

	cancel()
	waitCancelCall(t, service)
	select {
	case <-afterRan:
		t.Fatal("the step after the package ran while PackageKit's transaction still ran")
	case <-time.After(200 * time.Millisecond):
	}
	tx.emit("Finished", uint32(1), uint32(0))

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("install = %v, want canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the install did not return")
	}
	if want := []string{"hook:before", "package:zram-tools", "hook:after"}; !slices.Equal(run.order, want) {
		t.Fatalf("operation order = %v, want %v", run.order, want)
	}
}

// A cancel that lands while the step after a successful package step runs,
// and a step that fails, does not let the install go on to end completed:
// the job ends cancelled, and the step's failure is recorded in the log.
func TestInstallCapabilityDoesNotCompleteWhenThePackageHookFailsDuringACancel(t *testing.T) {
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := withZramHook(t, "debian", nil, "", errors.New("disable unit files zramswap.service: timed out; zramswap.service starts zram at the next boot"))
	run.onAfter = cancel

	result, err := runInstallCapabilityTask(ctx, nil, session.User{}, apischema.CapabilityRequest{Capability: "zram"})
	if err == nil {
		t.Fatalf("the install completed with %+v, want it to end cancelled", result)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	if want := []string{"hook:before", "package:zram-tools", "hook:after"}; !slices.Equal(run.order, want) {
		t.Fatalf("operation order = %v, want %v: the install went on", run.order, want)
	}
	if !strings.Contains(logs.String(), "package hook failed after a cancelled install") || !strings.Contains(logs.String(), "starts zram at the next boot") {
		t.Fatalf("logs = %q, want the hook's error recorded", logs.String())
	}
}

// A hook that fails before the package step stops the install there.
func TestInstallCapabilityStopsWhenThePackageHookCannotPrepare(t *testing.T) {
	run := withZramHook(t, "debian", nil, "", nil)
	hooked := capabilitySpecByName
	capabilitySpecByName = func(name string) (capabilities.CapabilitySpec, bool) {
		spec, ok := hooked(name)
		spec.Install.PackageHookDebian = func(context.Context) (func(context.Context) (string, error), error) {
			run.order = append(run.order, "hook:before")
			return nil, errors.New("bus closed")
		}
		return spec, ok
	}

	_, err := installCapability(context.Background(), nil, session.User{}, "zram")
	if err == nil || !strings.Contains(err.Error(), "prepare the zram-tools install") || !strings.Contains(err.Error(), "bus closed") {
		t.Fatalf("err = %v, want the hook's error before the package", err)
	}
	if want := []string{"hook:before"}; !slices.Equal(run.order, want) {
		t.Fatalf("operation order = %v, want %v", run.order, want)
	}
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
	capabilityDetectWithRetry = func(_ context.Context, spec capabilities.CapabilitySpec, _ session.User, _ time.Duration) (bool, string) {
		order = append(order, "detect:"+spec.Name)
		return true, ""
	}

	if _, err := installCapability(context.Background(), nil, session.User{}, "lm_sensors"); err != nil {
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
