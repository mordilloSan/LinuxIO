package updates

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mordilloSan/LinuxIO/bridge/handlers/dbus/internal/fsutil"
	"github.com/mordilloSan/LinuxIO/bridge/handlers/dbus/internal/systemd"
)

type dnfBackend struct{}

func newDnfBackend() Backend     { return &dnfBackend{} }
func (*dnfBackend) Name() string { return "dnf-automatic" }
func (*dnfBackend) Detect() bool {
	return fileExists("/usr/bin/dnf-automatic") || fileExists("/usr/lib/systemd/system/dnf-automatic.timer")
}

func (b *dnfBackend) Read(ctx context.Context) (AutoUpdateState, error) {
	return AutoUpdateState{
		Backend: b.Name(),
		Options: AutoUpdateOptions{
			Enabled:      timerEnabled("dnf-automatic.timer"),
			Frequency:    readTimerFrequency("dnf-automatic.timer"),
			Scope:        readDnfScope(),
			DownloadOnly: readDnfDownloadOnly(),
			RebootPolicy: "never", // DNF automatic doesn't have native reboot support
			ExcludePkgs:  readDnfExcludePackages(),
		},
	}, nil
}

func (b *dnfBackend) Apply(ctx context.Context, o AutoUpdateOptions) error {
	sd, err := systemd.New()
	if err != nil {
		return err
	}
	defer sd.Close()

	// Determine apply_updates setting
	apply := "True"
	if o.DownloadOnly || !o.Enabled {
		apply = "False"
	}

	// Determine upgrade_type
	upg := "security"
	if o.Scope == "updates" || o.Scope == "all" {
		upg = "default"
	}

	// Build exclude list
	var excludeLine string
	if len(o.ExcludePkgs) > 0 {
		// DNF uses space-separated list
		excludeLine = "exclude = " + strings.Join(o.ExcludePkgs, " ") + "\n"
	}

	// Write DNF automatic configuration
	conf := fmt.Sprintf(`[commands]
apply_updates = %s
upgrade_type = %s

[emitters]
emit_via = motd

[base]
%srandom_sleep = 0
`, apply, upg, excludeLine)

	if err := fsutil.WriteFileAtomic("/etc/dnf/automatic.conf", []byte(conf), 0o644); err != nil {
		return err
	}

	// Configure timer frequency
	timer := "dnf-automatic.timer"
	oncal, err := systemd.OnCalendarFor(o.Frequency)
	if err != nil {
		return err
	}
	if err := writeTimerDropIn(timer, oncal); err != nil {
		return err
	}

	// Reload systemd daemon to pick up changes
	if err := sd.Reload(ctx); err != nil {
		return err
	}

	// Enable/disable and start/stop timer based on settings
	if o.Enabled {
		if err := sd.Enable(ctx, timer); err != nil {
			return err
		}
		if err := sd.Start(ctx, timer); err != nil {
			return err
		}
		// Restart to apply new schedule
		_ = sd.Restart(ctx, timer)
	} else {
		_ = sd.Stop(ctx, timer)
		_ = sd.Disable(ctx, timer)
	}

	return nil
}

func (b *dnfBackend) ApplyOfflineNow(ctx context.Context) error {
	return fmt.Errorf("not implemented for dnf; use packagekit backend")
}

/* ===== DNF-SPECIFIC HELPER FUNCTIONS ===== */

// readDnfScope reads the upgrade_type from /etc/dnf/automatic.conf
func readDnfScope() string {
	data, err := os.ReadFile("/etc/dnf/automatic.conf")
	if err != nil {
		return "security" // default
	}

	content := string(data)

	// Look for upgrade_type setting
	re := regexp.MustCompile(`(?m)^\s*upgrade_type\s*=\s*(\S+)`)
	matches := re.FindStringSubmatch(content)

	if len(matches) > 1 {
		upgradeType := strings.TrimSpace(matches[1])
		if upgradeType == "default" {
			return "updates" // "default" means all updates, map to "updates"
		}
		if upgradeType == "security" {
			return "security"
		}
	}

	return "security"
}

// readDnfDownloadOnly reads the apply_updates setting from /etc/dnf/automatic.conf
func readDnfDownloadOnly() bool {
	data, err := os.ReadFile("/etc/dnf/automatic.conf")
	if err != nil {
		return false // default to applying updates
	}

	content := string(data)

	// Look for apply_updates setting
	re := regexp.MustCompile(`(?m)^\s*apply_updates\s*=\s*(\S+)`)
	matches := re.FindStringSubmatch(content)

	if len(matches) > 1 {
		value := strings.ToLower(strings.TrimSpace(matches[1]))
		// If apply_updates is False, then it's download-only
		return value == "false" || value == "no" || value == "0"
	}

	return false
}

// readDnfExcludePackages reads the exclude list from /etc/dnf/automatic.conf
func readDnfExcludePackages() []string {
	data, err := os.ReadFile("/etc/dnf/automatic.conf")
	if err != nil {
		return []string{}
	}

	content := string(data)

	// Look for exclude setting in [base] section
	// Format: exclude = package1 package2 package3
	re := regexp.MustCompile(`(?m)^\s*exclude\s*=\s*(.+)$`)
	matches := re.FindStringSubmatch(content)

	if len(matches) > 1 {
		excludeList := strings.TrimSpace(matches[1])
		if excludeList == "" {
			return []string{}
		}
		// Split by spaces
		packages := strings.Fields(excludeList)
		return packages
	}

	return []string{}
}
