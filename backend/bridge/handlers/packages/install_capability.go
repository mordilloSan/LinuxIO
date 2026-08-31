package packages

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// InstallCapabilityProgress is reported on the task event stream so the UI
// can show what stage we're in. Percentage is a single global 0-100 value that
// only moves forward across stages (it never resets per stage). Frontend
// mirrors this shape.
type InstallCapabilityProgress struct {
	Stage      string                   `json:"stage"`
	Message    string                   `json:"message"`
	Percentage *uint32                  `json:"percentage,omitempty"`
	Output     *InstallCapabilityOutput `json:"output,omitempty"`
}

// InstallCapabilityOutput is one ordered record from a capability installer.
// stdout/stderr records retain the bytes read from the command, including line
// endings. A status record is synthesized progress (including PackageKit
// transaction updates), never literal command output.
type InstallCapabilityOutput struct {
	Stream string `json:"stream"` // "status", "stdout", or "stderr"
	Text   string `json:"text"`
}

func (p InstallCapabilityProgress) ProgressEnvelope() bridgetask.TaskProgress {
	var percentage *int
	if p.Percentage != nil {
		value := int(*p.Percentage)
		percentage = &value
	}
	return bridgetask.TaskProgress{
		Percentage: percentage,
		Phase:      p.Stage,
		Message:    p.Message,
		Detail:     p,
	}
}

const (
	stageResolve        = "resolve"
	stageInstallAsset   = "install_asset"
	stageInstallPackage = "install_package"
	stagePostInstall    = "post_install"
	stageEnableService  = "enable_service"
	stageStartService   = "start_service"
	stageWaitActive     = "wait_service_active"
	stageDetect         = "detect"
)

// Global progress checkpoints (0-100). Each stage occupies a slice of the bar;
// the package step is the only one with sub-progress, with PackageKit's 0-100
// transaction percentage rescaled into [pctInstallStart, pctInstallEnd]. The
// final jump to 100 is owned by the task result handler on the frontend.
const (
	pctResolve      uint32 = 3
	pctInstallStart uint32 = 5
	pctInstallEnd   uint32 = 85
	pctPostInstall  uint32 = 86
	pctEnable       uint32 = 87
	pctStart        uint32 = 90
	pctWait         uint32 = 94
	pctDetect       uint32 = 98
)

const (
	serviceActiveTimeout = 15 * time.Second
	detectRetryTimeout   = 5 * time.Second
	detectRetryInterval  = 300 * time.Millisecond
)

var capabilityInstallRoutes = capabilityInstallBindings().Routes()

var (
	capabilityDistroFamily      = detectDistroFamily
	capabilityInstallPackage    = InstallByNameWithProgress
	capabilityEnableService     = systemd.EnableUnit
	capabilityStartService      = systemd.StartUnit
	capabilityWaitServiceActive = waitUnitActive
	capabilityDetectWithRetry   = detectWithRetry
)

func capabilityInstallBindings() apischema.BindingSet {
	policy := bridgetask.TaskSingletonSystem
	policy.Timeout = 10 * time.Minute
	return apischema.Bindings(
		apischema.TaskRunner[apischema.CapabilityRequest, apischema.InstallCapabilityResult]("system.install_capability", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[InstallCapabilityProgress](), apischema.WithTaskMetadata(func(req apischema.CapabilityRequest) bridgetask.TaskMetadata {
			return bridgetask.TaskMetadata{Identity: []string{req.Capability}, Label: "Installing " + req.Capability, Capability: req.Capability}
		})).Run(runInstallCapabilityTask, policy),
	)
}

// RegisterCapabilityTaskRoutes attaches the install_capability runner. It
// streams per-stage progress events to the UI and is registered alongside
// the other packages-package task runners from handlers.go.
func RegisterCapabilityTaskRoutes(router *bridgetask.Router) {
	capabilityInstallBindings().Register(router)
}

func runInstallCapabilityTask(ctx context.Context, task *bridgetask.Task, req apischema.CapabilityRequest) (apischema.InstallCapabilityResult, error) {
	name := strings.TrimSpace(req.Capability)
	if name == "" {
		return apischema.InstallCapabilityResult{}, bridgetask.NewError("capability name required", 400)
	}

	result, err := installCapability(ctx, task, name)
	if err != nil {
		if ctx.Err() != nil {
			return apischema.InstallCapabilityResult{}, context.Canceled
		}
		return apischema.InstallCapabilityResult{}, bridgetask.NewError(err.Error(), 500)
	}
	return result, nil
}

func installCapability(ctx context.Context, task *bridgetask.Task, name string) (apischema.InstallCapabilityResult, error) {
	spec, ok := system.CapabilitySpecByName(name)
	if !ok {
		return apischema.InstallCapabilityResult{}, fmt.Errorf("unknown capability %q", name)
	}
	if spec.Install == nil {
		return apischema.InstallCapabilityResult{}, fmt.Errorf("capability %q is not installable from the UI", name)
	}

	family := capabilityDistroFamily()
	pkg := pickByFamily(family, spec.Install.PackageDebian, spec.Install.PackageRHEL)
	service := pickByFamily(family, spec.Install.ServiceDebian, spec.Install.ServiceRHEL)

	if err := checkCapabilityInstallPrerequisites(ctx, task, spec); err != nil {
		return apischema.InstallCapabilityResult{}, err
	}

	if spec.Install.OptionalComponent != "" {
		if err := installOptionalComponent(ctx, task, spec); err != nil {
			return apischema.InstallCapabilityResult{}, err
		}
	}

	optionalPackageWarning, err := installCapabilityDependencies(ctx, task, family, name, spec.LogName, pkg, spec.Install)
	if err != nil {
		return apischema.InstallCapabilityResult{}, err
	}

	if spec.Install.PostInstall != nil {
		if err := runCapabilityPostInstall(ctx, task, spec.Install.PostInstall); err != nil {
			return apischema.InstallCapabilityResult{}, err
		}
	}

	if service != "" {
		if spec.Install.EnableService {
			reportProgress(task, stageEnableService, fmt.Sprintf("Enabling %s", service), pctEnable)
			slog.Info("Enabling capability service.", "capability", name, "unit", service)
			if err := capabilityEnableService(ctx, service); err != nil {
				return apischema.InstallCapabilityResult{}, fmt.Errorf("enable %s: %w", service, err)
			}
		}
		reportProgress(task, stageStartService, fmt.Sprintf("Starting %s", service), pctStart)
		slog.Info("Starting capability service.", "capability", name, "unit", service)
		if err := capabilityStartService(ctx, service); err != nil {
			return apischema.InstallCapabilityResult{}, fmt.Errorf("start %s: %w", service, err)
		}
		reportProgress(task, stageWaitActive, fmt.Sprintf("Waiting for %s to become active", service), pctWait)
		if err := capabilityWaitServiceActive(ctx, service, serviceActiveTimeout); err != nil {
			return apischema.InstallCapabilityResult{}, err
		}
	}

	reportProgress(task, stageDetect, fmt.Sprintf("Verifying %s", spec.LogName), pctDetect)
	available, errMsg := capabilityDetectWithRetry(ctx, spec, detectRetryTimeout)
	return apischema.InstallCapabilityResult{
		Available: available,
		Error:     utils.OptionalString(errMsg),
		Warning:   utils.OptionalString(optionalPackageWarning),
	}, nil
}

func installCapabilityDependencies(ctx context.Context, task *bridgetask.Task, family, name, logName, packageList string, spec *system.InstallSpec) (string, error) {
	if err := installCapabilityPackages(ctx, task, name, packageList); err != nil {
		return "", err
	}
	return installOptionalCapabilityPackage(ctx, task, family, logName, spec)
}

func installCapabilityPackages(ctx context.Context, task *bridgetask.Task, capabilityName string, packageList string) error {
	packages := strings.Fields(packageList)
	if len(packages) == 0 {
		return nil
	}
	reportProgress(task, stageResolve, fmt.Sprintf("Looking up %s", packageList), pctResolve)
	for idx, packageName := range packages {
		installStart, installEnd := packageInstallProgressRange(idx, len(packages))
		reportProgress(task, stageInstallPackage, fmt.Sprintf("Installing %s", packageName), installStart)
		slog.Info("Installing capability package.", "capability", capabilityName, "package", packageName)
		if err := capabilityInstallPackage(ctx, packageName, capabilityInstallReporter(task, packageName, installStart, installEnd)); err != nil {
			return fmt.Errorf("install %s: %w", packageName, err)
		}
		reportProgress(task, stageInstallPackage, fmt.Sprintf("Installed %s", packageName), installEnd)
	}
	return nil
}

func installOptionalCapabilityPackage(ctx context.Context, task *bridgetask.Task, family, capabilityName string, spec *system.InstallSpec) (string, error) {
	if !isRHELFamily(family) || spec == nil || strings.TrimSpace(spec.OptionalPackageRHEL) == "" {
		return "", nil
	}

	packageName := strings.TrimSpace(spec.OptionalPackageRHEL)
	reportProgress(task, stageInstallPackage, fmt.Sprintf("Installing optional package %s", packageName), pctInstallEnd)
	slog.Info("Installing optional capability package.", "capability", capabilityName, "package", packageName)
	err := capabilityInstallPackage(ctx, packageName, capabilityInstallReporter(task, packageName, pctInstallEnd, pctInstallEnd))
	if err == nil {
		reportProgress(task, stageInstallPackage, fmt.Sprintf("Installed optional package %s", packageName), pctInstallEnd)
		return "", nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	detail := fmt.Sprintf("Optional package %s could not be installed: %v", packageName, err)
	warning := strings.TrimSpace(spec.OptionalPackageRHELFailureWarning)
	if warning == "" {
		warning = fmt.Sprintf("Optional package %s was not installed; continuing without it.", packageName)
	}
	reportProgress(task, stageInstallPackage, detail+". "+warning, pctInstallEnd)
	return warning, nil
}

func runCapabilityPostInstall(ctx context.Context, task *bridgetask.Task, command *system.InstallCommand) error {
	if command == nil || strings.TrimSpace(command.Name) == "" {
		return nil
	}
	args := append([]string(nil), command.Args...)
	reportProgress(task, stagePostInstall, fmt.Sprintf("Running %s %s", command.Name, strings.Join(args, " ")), pctPostInstall)
	slog.Info("Running capability post-install command.", "command", command.Name, "args", args)
	if err := runCapabilityCommand(ctx, command.Name, args, func(output InstallCapabilityOutput) {
		reportOutput(task, stagePostInstall, fmt.Sprintf("Running %s", command.Name), pctPostInstall, output)
	}); err != nil {
		return fmt.Errorf("run %s %s: %w", command.Name, strings.Join(args, " "), err)
	}
	return nil
}

var (
	capabilityCommandLookPath = exec.LookPath
	capabilityCommandExec     = exec.CommandContext
)

// runCapabilityCommand executes a capability-owned command while forwarding
// stdout and stderr as separate records. Each pipe has an owner goroutine and
// the command is always context-bound, so cancellation closes the process and
// lets both readers exit before the function returns.
func runCapabilityCommand(ctx context.Context, name string, args []string, report func(InstallCapabilityOutput)) error {
	return runCapabilityProcess(ctx, name, args, nil, nil, report)
}

func runCapabilityScript(ctx context.Context, name string, args []string, script []byte, report func(InstallCapabilityOutput)) error {
	return runCapabilityProcess(ctx, name, args, bytes.NewReader(script), append(os.Environ(), "DEBIAN_FRONTEND=noninteractive"), report)
}

type capabilityProcessOutput struct {
	mu      sync.Mutex
	readErr error
	report  func(InstallCapabilityOutput)
	stderr  bytes.Buffer
}

const (
	capabilityOutputChunkBytes = 8 << 10
	capabilityErrorTailBytes   = 16 << 10
)

func (o *capabilityProcessOutput) appendStderr(chunk []byte) {
	if len(chunk) >= capabilityErrorTailBytes {
		o.stderr.Reset()
		_, _ = o.stderr.Write(chunk[len(chunk)-capabilityErrorTailBytes:])
		return
	}
	if overflow := o.stderr.Len() + len(chunk) - capabilityErrorTailBytes; overflow > 0 {
		retained := append([]byte(nil), o.stderr.Bytes()[overflow:]...)
		o.stderr.Reset()
		_, _ = o.stderr.Write(retained)
	}
	_, _ = o.stderr.Write(chunk)
}

func (o *capabilityProcessOutput) read(stream string, reader io.Reader) {
	buf := bufio.NewReaderSize(reader, capabilityOutputChunkBytes)
	for {
		chunk, err := buf.ReadSlice('\n')
		if len(chunk) > 0 {
			o.mu.Lock()
			if stream == "stderr" {
				o.appendStderr(chunk)
			}
			if o.report != nil {
				o.report(InstallCapabilityOutput{Stream: stream, Text: string(chunk)})
			}
			o.mu.Unlock()
		}
		if err == nil || err == bufio.ErrBufferFull {
			continue
		}
		if err != io.EOF {
			o.mu.Lock()
			if o.readErr == nil {
				o.readErr = err
			}
			o.mu.Unlock()
		}
		return
	}
}

func (o *capabilityProcessOutput) commandError(ctx context.Context, name string, waitErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.readErr != nil {
		return fmt.Errorf("read %s output: %w", name, o.readErr)
	}
	if waitErr == nil {
		return nil
	}
	message := strings.TrimSpace(o.stderr.String())
	if message != "" {
		return fmt.Errorf("%s: %w: %s", name, waitErr, message)
	}
	return fmt.Errorf("%s: %w", name, waitErr)
}

func runCapabilityProcess(ctx context.Context, name string, args []string, stdin io.Reader, env []string, report func(InstallCapabilityOutput)) error {
	path, err := capabilityCommandLookPath(name)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", name, err)
	}
	cmd := capabilityCommandExec(ctx, path, args...)
	if env != nil {
		cmd.Env = env
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("capture %s stdout: %w", name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("capture %s stderr: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}

	output := &capabilityProcessOutput{report: report}
	var readers sync.WaitGroup
	read := func(stream string, reader io.Reader) {
		defer readers.Done()
		output.read(stream, reader)
	}
	readers.Add(2)
	go read("stdout", stdout)
	go read("stderr", stderr)
	readers.Wait()
	waitErr := cmd.Wait()
	return output.commandError(ctx, name, waitErr)
}

func checkCapabilityInstallPrerequisites(ctx context.Context, task *bridgetask.Task, spec system.CapabilitySpec) error {
	if !spec.Install.RequiresDocker {
		return nil
	}
	reportProgress(task, stageResolve, "Checking Docker availability", pctResolve)
	available, err := docker.CheckDockerAvailability(ctx)
	if err != nil {
		return fmt.Errorf("docker is required to install %s: %w", spec.LogName, err)
	}
	if !available {
		return fmt.Errorf("docker is required to install %s", spec.LogName)
	}
	return nil
}

func installOptionalComponent(ctx context.Context, task *bridgetask.Task, spec system.CapabilitySpec) error {
	switch spec.Install.OptionalComponent {
	case system.OptionalComponentMonitoring:
		return installMonitoring(ctx, task)
	default:
		return fmt.Errorf("unknown optional component %q for capability %q", spec.Install.OptionalComponent, spec.Name)
	}
}

func reportProgress(task *bridgetask.Task, stage, message string, pct uint32) {
	reportProgressDetail(task, stage, message, pct, &InstallCapabilityOutput{Stream: "status", Text: message})
}

func reportOutput(task *bridgetask.Task, stage, message string, pct uint32, output InstallCapabilityOutput) {
	if task == nil {
		return
	}
	// Literal installer output belongs to direct task watchers. Keeping it
	// transient avoids flooding the app-wide task event stream while preserving
	// bounded replay for a dialog that reconnects to the running task.
	task.ReportTransientProgress(InstallCapabilityProgress{
		Stage:      stage,
		Message:    message,
		Percentage: &pct,
		Output:     &output,
	}.ProgressEnvelope())
}

func reportProgressDetail(task *bridgetask.Task, stage, message string, pct uint32, output *InstallCapabilityOutput) {
	if task == nil {
		return
	}
	task.ReportProgress(InstallCapabilityProgress{Stage: stage, Message: message, Percentage: &pct, Output: output})
}

func packageInstallProgressRange(index int, total int) (uint32, uint32) {
	if total <= 1 {
		return pctInstallStart, pctInstallEnd
	}
	span := pctInstallEnd - pctInstallStart
	start := pctInstallStart + uint32(index)*span/uint32(total)
	end := pctInstallStart + uint32(index+1)*span/uint32(total)
	return start, end
}

// scaleInstallPct maps PackageKit's 0-100 transaction percentage into one
// package's slice of the global package-step band.
func scaleInstallPct(pkgPct, start, end uint32) uint32 {
	if pkgPct > 100 {
		pkgPct = 100
	}
	return start + pkgPct*(end-start)/100
}

// capabilityInstallReporter adapts PackageKit update-signal frames (emitted by
// the shared awaitPackageUpdateSignals handlers) into the capability task's
// progress stream, carrying a single global percentage plus the current status.
func capabilityInstallReporter(task *bridgetask.Task, pkg string, installStart uint32, installEnd uint32) pkgUpdateReporter {
	lastGlobal := installStart
	lastStatus := ""
	return func(p *PkgUpdateProgress) error {
		changed := false
		if p.Percentage != nil && *p.Percentage <= 100 {
			lastGlobal = scaleInstallPct(*p.Percentage, installStart, installEnd)
			changed = true
		}
		if p.Status != "" {
			lastStatus = p.Status
			changed = true
		}
		if !changed {
			return nil
		}
		msg := fmt.Sprintf("Installing %s", pkg)
		if lastStatus != "" {
			msg = fmt.Sprintf("Installing %s (%s)", pkg, lastStatus)
		}
		// PackageKit ticks update the phase/percentage header only. A status
		// output record per tick would flood the raw-output panel with the
		// transaction's per-dependency status churn.
		reportProgressDetail(task, stageInstallPackage, msg, lastGlobal, nil)
		return nil
	}
}

// waitUnitActive polls systemd until the unit reports "active" or fails. The
// systemd StartUnit task returns once the unit transitions, but for services
// whose readiness depends on something beyond systemd (e.g. avahi-daemon
// claiming its D-Bus name) we still need this poll before re-detecting.
func waitUnitActive(ctx context.Context, unit string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastState string
	for {
		state, err := systemd.GetActiveState(ctx, unit)
		if err == nil {
			lastState = state
			switch state {
			case "active":
				return nil
			case "failed":
				return fmt.Errorf("unit %s entered failed state", unit)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("unit %s did not become active within %s (last state: %s)", unit, timeout, lastState)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(detectRetryInterval):
		}
	}
}

// detectWithRetry re-runs the capability's detect function for up to `timeout`
// while it still reports unavailable. This covers the small window between a
// service becoming "active" and its public surface (D-Bus name, listening
// socket, etc.) being reachable from the detector.
func detectWithRetry(ctx context.Context, spec system.CapabilitySpec, timeout time.Duration) (bool, string) {
	deadline := time.Now().Add(timeout)
	var available bool
	var errMsg string
	for {
		available, errMsg = spec.Detect(ctx)
		if available {
			return true, ""
		}
		if time.Now().After(deadline) {
			return available, errMsg
		}
		select {
		case <-ctx.Done():
			return available, errMsg
		case <-time.After(detectRetryInterval):
		}
	}
}

// pickByFamily returns the Debian-side value for Debian-family hosts, or the
// RHEL-side value for RHEL-family hosts. Falls back to whichever is non-empty.
func pickByFamily(family, debian, rhel string) string {
	if isRHELFamily(family) && rhel != "" {
		return rhel
	}
	if family == "debian" && debian != "" {
		return debian
	}
	if debian != "" {
		return debian
	}
	return rhel
}

func isRHELFamily(family string) bool {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "rhel", "fedora", "centos", "rocky", "almalinux", "ol", "amzn":
		return true
	default:
		return false
	}
}

// detectDistroFamily reads /etc/os-release and classifies the host as either
// "debian" or "rhel" (the two families we know how to install for). Anything
// else defaults to "debian" — the wrong package name will surface as a clear
// resolve-failed error from PackageKit, which is better than silently doing
// nothing.
func detectDistroFamily() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "debian"
	}

	values := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
	}

	ids := []string{values["ID"]}
	ids = append(ids, strings.Fields(values["ID_LIKE"])...)

	if slices.ContainsFunc(ids, isRHELFamily) {
		return "rhel"
	}
	return "debian"
}
