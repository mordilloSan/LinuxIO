package autoupdate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type (
	AutoUpdateBackend       = apischema.AutoUpdateBackend
	AutoUpdateFrequency     = apischema.AutoUpdateFrequency
	AutoUpdateOptions       = apischema.AutoUpdateOptions
	AutoUpdateOptionSupport = apischema.AutoUpdateOptionSupport
	AutoUpdateRebootPolicy  = apischema.AutoUpdateRebootPolicy
	AutoUpdateScope         = apischema.AutoUpdateScope
	AutoUpdateState         = apischema.AutoUpdateState
)

const (
	backendAPT  AutoUpdateBackend = "apt-unattended"
	backendMint AutoUpdateBackend = "mintupdate-automation"
	backendDNF4 AutoUpdateBackend = "dnf-automatic"
	backendDNF5 AutoUpdateBackend = "dnf5-automatic"
)

type UpdateBackend interface {
	Name() AutoUpdateBackend
	Read(context.Context) (AutoUpdateState, error)
	Apply(context.Context, AutoUpdateOptions) error
	ApplyOfflineNow(context.Context) error
}

type backendHost struct {
	readFile         func(string) ([]byte, error)
	writeFileAtomic  func(string, []byte, fs.FileMode) error
	removeFile       func(string) error
	fileExists       func(string) bool
	getUnitFileState func(context.Context, string) (string, error)
	enableUnit       func(context.Context, string) error
	disableUnit      func(context.Context, string) error
	startUnit        func(context.Context, string) error
	stopUnit         func(context.Context, string) error
	restartUnit      func(context.Context, string) error
	daemonReload     func(context.Context) error
}

func systemHost() backendHost {
	return backendHost{
		readFile:         os.ReadFile,
		writeFileAtomic:  utils.WriteFileAtomic,
		removeFile:       os.Remove,
		fileExists:       utils.FileExists,
		getUnitFileState: systemd.GetUnitFileState,
		enableUnit:       systemd.EnableUnit,
		disableUnit:      systemd.DisableUnit,
		startUnit:        systemd.StartUnit,
		stopUnit:         systemd.StopUnit,
		restartUnit:      systemd.RestartUnit,
		daemonReload:     systemd.DaemonReload,
	}
}

func SelectBackend(ctx context.Context) UpdateBackend {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	host := systemHost()
	platform, err := readPlatform(host.readFile, "/etc/os-release")
	if err != nil {
		return nil
	}
	return selectBackend(platform, host)
}

func selectBackend(platform hostPlatform, host backendHost) UpdateBackend {
	switch platform.ID {
	case "linuxmint":
		return newMintBackend(host)
	case "ubuntu":
		return newAptBackend(host, aptUbuntu)
	case "debian":
		return newAptBackend(host, aptDebian)
	case "fedora", "rhel", "rocky", "almalinux":
		if platformUsesDNF5(platform, host.fileExists) {
			return newDNFBackend(host, dnf5)
		}
		return newDNFBackend(host, dnf4)
	default:
		return nil
	}
}

func platformUsesDNF5(platform hostPlatform, exists func(string) bool) bool {
	if timerArtifactExists(exists, "dnf5-automatic.timer") {
		return true
	}
	if timerArtifactExists(exists, "dnf-automatic.timer") || exists("/usr/bin/dnf-automatic") {
		return false
	}
	if platform.ID == "fedora" {
		return platform.VersionMajor >= 41
	}
	return false
}

func timerArtifactExists(exists func(string) bool, name string) bool {
	return exists("/usr/lib/systemd/system/"+name) || exists("/lib/systemd/system/"+name)
}

func NewPkgKitBackendIfAvailable(ctx context.Context) UpdateBackend {
	b := newPkgKitBackend()
	if b.Detect(ctx) {
		return b
	}
	return nil
}

func validateOptions(options AutoUpdateOptions, support AutoUpdateOptionSupport) error {
	if !slices.Contains(support.Frequencies, options.Frequency) {
		return fmt.Errorf("frequency %q is not supported by this update backend", options.Frequency)
	}
	if !slices.Contains(support.Scopes, options.Scope) {
		return fmt.Errorf("scope %q is not supported by this update backend", options.Scope)
	}
	if !slices.Contains(support.RebootPolicies, options.RebootPolicy) {
		return fmt.Errorf("reboot policy %q is not supported by this update backend", options.RebootPolicy)
	}
	if options.DownloadOnly && !support.DownloadOnly {
		return fmt.Errorf("download-only mode is not supported by this update backend")
	}
	if len(options.ExcludePackages) > 0 && !support.ExcludePackages {
		return fmt.Errorf("package exclusions are not supported by this update backend")
	}
	return validatePackagePatterns(options.ExcludePackages)
}

var packagePattern = regexp.MustCompile(`^[[:alnum:]][[:alnum:]+._:*?\[\]-]*$`)

func validatePackagePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if !packagePattern.MatchString(pattern) {
			return fmt.Errorf("invalid package exclusion %q", pattern)
		}
	}
	return nil
}

func timerEnabled(ctx context.Context, host backendHost, name string) (bool, error) {
	state, err := host.getUnitFileState(ctx, name)
	if err != nil {
		return false, err
	}
	return state == "enabled" || state == "enabled-runtime" || state == "linked" || state == "linked-runtime", nil
}
