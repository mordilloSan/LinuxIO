package packages

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// InstallCapabilityProgress is reported on the job event stream so the UI
// can show what stage we're in. Percentage is a single global 0-100 value that
// only moves forward across stages (it never resets per stage). Frontend
// mirrors this shape.
type InstallCapabilityProgress struct {
	Stage      string  `json:"stage"`
	Message    string  `json:"message"`
	Percentage *uint32 `json:"percentage,omitempty"`
}

const (
	stageResolve        = "resolve"
	stageInstallAsset   = "install_asset"
	stageInstallPackage = "install_package"
	stageEnableService  = "enable_service"
	stageStartService   = "start_service"
	stageWaitActive     = "wait_service_active"
	stageDetect         = "detect"
)

// Global progress checkpoints (0-100). Each stage occupies a slice of the bar;
// the package step is the only one with sub-progress, with PackageKit's 0-100
// transaction percentage rescaled into [pctInstallStart, pctInstallEnd]. The
// final jump to 100 is owned by the job result handler on the frontend.
const (
	pctResolve      uint32 = 3
	pctInstallStart uint32 = 5
	pctInstallEnd   uint32 = 85
	pctEnable       uint32 = 86
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

func capabilityInstallBindings() apischema.BindingSet {
	policy := bridgejobs.SingletonSystem
	policy.Timeout = 10 * time.Minute
	return apischema.Bindings(
		apischema.Runner[apischema.CapabilityRequest, apischema.JobSnapshot]("system.install_capability", apischema.Privileged()).Run(runInstallCapabilityJob, policy),
	)
}

// RegisterCapabilityJobRoutes attaches the install_capability runner. It
// streams per-stage progress events to the UI and is registered alongside
// the other packages-package job runners from handlers.go.
func RegisterCapabilityJobRoutes(router *bridgejobs.Router) {
	capabilityInstallBindings().Register(router)
}

func runInstallCapabilityJob(ctx context.Context, job *bridgejobs.Job, req apischema.CapabilityRequest) (any, error) {
	name := strings.TrimSpace(req.Capability)
	if name == "" {
		return nil, bridgejobs.NewError("capability name required", 400)
	}

	result, err := installCapability(ctx, job, name)
	if err != nil {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		return nil, bridgejobs.NewError(err.Error(), 500)
	}
	return result, nil
}

func installCapability(ctx context.Context, job *bridgejobs.Job, name string) (apischema.InstallCapabilityResult, error) {
	spec, ok := system.CapabilitySpecByName(name)
	if !ok {
		return apischema.InstallCapabilityResult{}, fmt.Errorf("unknown capability %q", name)
	}
	if spec.Install == nil {
		return apischema.InstallCapabilityResult{}, fmt.Errorf("capability %q is not installable from the UI", name)
	}

	family := detectDistroFamily()
	pkg := pickByFamily(family, spec.Install.PackageDebian, spec.Install.PackageRHEL)
	service := pickByFamily(family, spec.Install.ServiceDebian, spec.Install.ServiceRHEL)

	if err := checkCapabilityInstallPrerequisites(ctx, job, spec); err != nil {
		return apischema.InstallCapabilityResult{}, err
	}

	if spec.Install.OptionalComponent != "" {
		if err := installOptionalComponent(ctx, job, spec); err != nil {
			return apischema.InstallCapabilityResult{}, err
		}
	}

	if err := installCapabilityPackages(ctx, job, name, pkg); err != nil {
		return apischema.InstallCapabilityResult{}, err
	}

	if service != "" {
		if spec.Install.EnableService {
			reportProgress(job, stageEnableService, fmt.Sprintf("Enabling %s", service), pctEnable)
			slog.Info("Enabling capability service.", "capability", name, "unit", service)
			if err := systemd.EnableUnit(ctx, service); err != nil {
				return apischema.InstallCapabilityResult{}, fmt.Errorf("enable %s: %w", service, err)
			}
		}
		reportProgress(job, stageStartService, fmt.Sprintf("Starting %s", service), pctStart)
		slog.Info("Starting capability service.", "capability", name, "unit", service)
		if err := systemd.StartUnit(ctx, service); err != nil {
			return apischema.InstallCapabilityResult{}, fmt.Errorf("start %s: %w", service, err)
		}
		reportProgress(job, stageWaitActive, fmt.Sprintf("Waiting for %s to become active", service), pctWait)
		if err := waitUnitActive(ctx, service, serviceActiveTimeout); err != nil {
			return apischema.InstallCapabilityResult{}, err
		}
	}

	reportProgress(job, stageDetect, fmt.Sprintf("Verifying %s", spec.LogName), pctDetect)
	available, errMsg := detectWithRetry(ctx, spec, detectRetryTimeout)
	return apischema.InstallCapabilityResult{Available: available, Error: utils.OptionalString(errMsg)}, nil
}

func installCapabilityPackages(ctx context.Context, job *bridgejobs.Job, capabilityName string, packageList string) error {
	packages := strings.Fields(packageList)
	if len(packages) == 0 {
		return nil
	}
	reportProgress(job, stageResolve, fmt.Sprintf("Looking up %s", packageList), pctResolve)
	for idx, packageName := range packages {
		installStart, installEnd := packageInstallProgressRange(idx, len(packages))
		reportProgress(job, stageInstallPackage, fmt.Sprintf("Installing %s", packageName), installStart)
		slog.Info("Installing capability package.", "capability", capabilityName, "package", packageName)
		if err := InstallByNameWithProgress(ctx, packageName, capabilityInstallReporter(job, packageName, installStart, installEnd)); err != nil {
			return fmt.Errorf("install %s: %w", packageName, err)
		}
		reportProgress(job, stageInstallPackage, fmt.Sprintf("Installed %s", packageName), installEnd)
	}
	return nil
}

func checkCapabilityInstallPrerequisites(ctx context.Context, job *bridgejobs.Job, spec system.CapabilitySpec) error {
	if !spec.Install.RequiresDocker {
		return nil
	}
	reportProgress(job, stageResolve, "Checking Docker availability", pctResolve)
	available, err := docker.CheckDockerAvailability(ctx)
	if err != nil {
		return fmt.Errorf("docker is required to install %s: %w", spec.LogName, err)
	}
	if !available {
		return fmt.Errorf("docker is required to install %s", spec.LogName)
	}
	return nil
}

func installOptionalComponent(ctx context.Context, job *bridgejobs.Job, spec system.CapabilitySpec) error {
	switch spec.Install.OptionalComponent {
	case system.OptionalComponentWatchtower:
		return installWatchtower(ctx, job)
	default:
		return fmt.Errorf("unknown optional component %q for capability %q", spec.Install.OptionalComponent, spec.Name)
	}
}

func reportProgress(job *bridgejobs.Job, stage, message string, pct uint32) {
	if job == nil {
		return
	}
	job.ReportProgress(InstallCapabilityProgress{Stage: stage, Message: message, Percentage: &pct})
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
// the shared awaitPackageUpdateSignals handlers) into the capability job's
// progress stream, carrying a single global percentage plus the current status.
func capabilityInstallReporter(job *bridgejobs.Job, pkg string, installStart uint32, installEnd uint32) pkgUpdateReporter {
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
		reportProgress(job, stageInstallPackage, msg, lastGlobal)
		return nil
	}
}

// waitUnitActive polls systemd until the unit reports "active" or fails. The
// systemd StartUnit job returns once the unit transitions, but for services
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

// pickByFamily returns the debian-side value when family is "debian", or the
// rhel-side value when family is "rhel". Falls back to whichever is non-empty.
func pickByFamily(family, debian, rhel string) string {
	if family == "rhel" && rhel != "" {
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

	rhelFamily := []string{"rhel", "fedora", "centos", "rocky", "almalinux", "ol", "amzn"}
	for _, id := range ids {
		if slices.Contains(rhelFamily, id) {
			return "rhel"
		}
	}
	return "debian"
}
