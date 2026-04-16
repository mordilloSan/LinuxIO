package updates

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/internal/fsutil"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/internal/systemd"
)

type aptBackend struct{}

func newAptBackend() Backend     { return &aptBackend{} }
func (*aptBackend) Name() string { return "apt-unattended" }
func (*aptBackend) Detect() bool {
	return fileExists("/usr/bin/apt")
}

func unattendedUpgradesInstalled() bool {
	return fileExists("/usr/bin/unattended-upgrades") || fileExists("/usr/bin/unattended-upgrade")
}

func (b *aptBackend) Read() (AutoUpdateState, error) {
	if !unattendedUpgradesInstalled() {
		return AutoUpdateState{
			Backend: b.Name(),
			Options: AutoUpdateOptions{
				Enabled:      false,
				Frequency:    "daily",
				Scope:        "security",
				RebootPolicy: "never",
				ExcludePkgs:  []string{},
			},
			Notes: []string{"Install unattended-upgrades to enable: sudo apt install unattended-upgrades"},
		}, nil
	}

	st := AutoUpdateState{
		Backend: b.Name(),
		Options: AutoUpdateOptions{
			Enabled:      timerEnabled("apt-daily-upgrade.timer") || timerEnabled("apt-daily.timer"),
			Frequency:    readTimerFrequency("apt-daily.timer"),
			Scope:        readScope(),
			DownloadOnly: !timerEnabled("apt-daily-upgrade.timer") && timerEnabled("apt-daily.timer"),
			RebootPolicy: readRebootPolicy(),
			ExcludePkgs:  readExcludePackages(),
		},
	}
	return st, nil
}

func (b *aptBackend) Apply(ctx context.Context, o AutoUpdateOptions) error {
	if !unattendedUpgradesInstalled() {
		return fmt.Errorf("unattended-upgrades is not installed; run: sudo apt install unattended-upgrades")
	}

	sd, err := systemd.New()
	if err != nil {
		return err
	}
	defer sd.Close()

	if writeErr := writeAptAutoUpgradeConfig(o); writeErr != nil {
		return fmt.Errorf("failed to write 20auto-upgrades: %w", writeErr)
	}

	if writeErr := writeAptUnattendedConfig(o); writeErr != nil {
		return fmt.Errorf("failed to write 50unattended-upgrades: %w", writeErr)
	}

	oncal, err := systemd.OnCalendarFor(o.Frequency)
	if err != nil {
		return fmt.Errorf("invalid frequency: %w", err)
	}
	if err := writeTimerDropIn("apt-daily.timer", oncal); err != nil {
		return fmt.Errorf("failed to write apt-daily.timer drop-in: %w", err)
	}
	if err := writeTimerDropIn("apt-daily-upgrade.timer", oncal); err != nil {
		return fmt.Errorf("failed to write apt-daily-upgrade.timer drop-in: %w", err)
	}

	if err := sd.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	if err := applyAptTimerState(ctx, sd, o); err != nil {
		return err
	}

	restartAptTimers(ctx, sd, o)
	return nil
}

func writeAptAutoUpgradeConfig(o AutoUpdateOptions) error {
	upd, dl, uu := aptPeriodicValues(o)
	content := fmt.Sprintf(`APT::Periodic::Update-Package-Lists "%s";
APT::Periodic::Download-Upgradeable-Packages "%s";
APT::Periodic::Unattended-Upgrade "%s";
`, upd, dl, uu)
	return fsutil.WriteFileAtomic("/etc/apt/apt.conf.d/20auto-upgrades", []byte(content), 0o644)
}

func aptPeriodicValues(o AutoUpdateOptions) (string, string, string) {
	if !o.Enabled {
		return "0", "0", "0"
	}
	if o.DownloadOnly {
		return "1", "1", "0"
	}
	return "1", "1", "1"
}

func writeAptUnattendedConfig(o AutoUpdateOptions) error {
	content := fmt.Sprintf(`Unattended-Upgrade::Allowed-Origins {
%s};
Unattended-Upgrade::Package-Blacklist {
%s};
Unattended-Upgrade::Automatic-Reboot "%s";
Unattended-Upgrade::Automatic-Reboot-Time "03:30";
`, formatOrigins(aptAllowedOrigins(o.Scope)), formatAptBlacklist(o.ExcludePkgs), aptRebootSetting(o.RebootPolicy))
	return fsutil.WriteFileAtomic("/etc/apt/apt.conf.d/50unattended-upgrades", []byte(content), 0o644)
}

func aptAllowedOrigins(scope string) []string {
	origins := []string{`${distro_id}:${distro_codename}-security`}
	if scope == "updates" || scope == "all" {
		origins = append(origins, `${distro_id}:${distro_codename}-updates`)
	}
	if scope == "all" {
		origins = append(origins, `${distro_id}:${distro_codename}-backports`)
	}
	return origins
}

func formatAptBlacklist(packages []string) string {
	var blacklist strings.Builder
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		blacklist.WriteString(`        "` + pkg + `";` + "\n")
	}
	return blacklist.String()
}

func aptRebootSetting(policy string) string {
	if policy == "always" || policy == "if_needed" {
		return "true"
	}
	return "false"
}

func applyAptTimerState(ctx context.Context, sd *systemd.Client, o AutoUpdateOptions) error {
	if o.Enabled {
		if err := enableAptDailyTimer(ctx, sd); err != nil {
			return err
		}
		if o.DownloadOnly {
			disableAptUpgradeTimer(ctx, sd, "in download-only mode")
			return nil
		}
		if err := enableAptUpgradeTimer(ctx, sd); err != nil {
			return err
		}
		return nil
	}

	disableAptDailyTimer(ctx, sd, "while disabling auto-updates")
	disableAptUpgradeTimer(ctx, sd, "while disabling auto-updates")
	return nil
}

func enableAptDailyTimer(ctx context.Context, sd *systemd.Client) error {
	if err := sd.Enable(ctx, "apt-daily.timer"); err != nil {
		return fmt.Errorf("failed to enable apt-daily.timer: %w", err)
	}
	if err := sd.Start(ctx, "apt-daily.timer"); err != nil {
		return fmt.Errorf("failed to start apt-daily.timer: %w", err)
	}
	return nil
}

func enableAptUpgradeTimer(ctx context.Context, sd *systemd.Client) error {
	if err := sd.Enable(ctx, "apt-daily-upgrade.timer"); err != nil {
		return fmt.Errorf("failed to enable apt-daily-upgrade.timer: %w", err)
	}
	if err := sd.Start(ctx, "apt-daily-upgrade.timer"); err != nil {
		return fmt.Errorf("failed to start apt-daily-upgrade.timer: %w", err)
	}
	return nil
}

func disableAptDailyTimer(ctx context.Context, sd *systemd.Client, reason string) {
	if err := sd.Stop(ctx, "apt-daily.timer"); err != nil {
		slog.Debug("failed to stop apt-daily.timer", "component", "dbus", "subsystem", "updates", "service", "apt-daily.timer", "mode", reason, "error", err)
	}
	if err := sd.Disable(ctx, "apt-daily.timer"); err != nil {
		slog.Debug("failed to disable apt-daily.timer", "component", "dbus", "subsystem", "updates", "service", "apt-daily.timer", "mode", reason, "error", err)
	}
}

func disableAptUpgradeTimer(ctx context.Context, sd *systemd.Client, reason string) {
	if err := sd.Stop(ctx, "apt-daily-upgrade.timer"); err != nil {
		slog.Debug("failed to stop apt-daily-upgrade.timer", "component", "dbus", "subsystem", "updates", "service", "apt-daily-upgrade.timer", "mode", reason, "error", err)
	}
	if err := sd.Disable(ctx, "apt-daily-upgrade.timer"); err != nil {
		slog.Debug("failed to disable apt-daily-upgrade.timer", "component", "dbus", "subsystem", "updates", "service", "apt-daily-upgrade.timer", "mode", reason, "error", err)
	}
}

func restartAptTimers(ctx context.Context, sd *systemd.Client, o AutoUpdateOptions) {
	if !o.Enabled {
		return
	}
	if err := sd.Restart(ctx, "apt-daily.timer"); err != nil {
		slog.Debug("failed to restart apt-daily.timer", "component", "dbus", "subsystem", "updates", "service", "apt-daily.timer", "error", err)
	}
	if o.DownloadOnly {
		return
	}
	if err := sd.Restart(ctx, "apt-daily-upgrade.timer"); err != nil {
		slog.Debug("failed to restart apt-daily-upgrade.timer", "component", "dbus", "subsystem", "updates", "service", "apt-daily-upgrade.timer", "error", err)
	}
}

func (b *aptBackend) ApplyOfflineNow() error {
	return fmt.Errorf("not implemented for apt; use packagekit backend")
}

/* ===== HELPER FUNCTIONS ===== */

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func timerEnabled(name string) bool {
	wants := []string{
		"/etc/systemd/system/timers.target.wants/" + name,
		"/lib/systemd/system/timers.target.wants/" + name,
	}
	return slices.ContainsFunc(wants, fileExists)
}

func writeTimerDropIn(timer, oncal string) error {
	path := filepath.Join("/etc/systemd/system", timer+".d", "linuxio.conf")
	body := "[Timer]\nOnCalendar=" + oncal + "\nRandomizedDelaySec=30m\n"
	return fsutil.WriteFileAtomic(path, []byte(body), 0o644)
}

func formatOrigins(list []string) string {
	var b strings.Builder
	for _, s := range list {
		b.WriteString(`        "` + s + `";` + "\n")
	}
	return b.String()
}

// readTimerFrequency reads the configured schedule from timer drop-in
func readTimerFrequency(timer string) string {
	path := filepath.Join("/etc/systemd/system", timer+".d", "linuxio.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return "daily" // default fallback
	}

	lines := strings.SplitSeq(string(data), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "OnCalendar="); ok {
			value := after
			return strings.TrimSpace(value)
		}
	}

	return "daily"
}

// readScope determines update scope from allowed origins
func readScope() string {
	data, err := os.ReadFile("/etc/apt/apt.conf.d/50unattended-upgrades")
	if err != nil {
		return "security" // default
	}

	content := string(data)
	hasSecurity := strings.Contains(content, "-security")
	hasUpdates := strings.Contains(content, "-updates")
	hasBackports := strings.Contains(content, "-backports")

	if hasSecurity && hasUpdates && hasBackports {
		return "all"
	}
	if hasSecurity && hasUpdates {
		return "updates"
	}
	return "security"
}

// readRebootPolicy reads automatic reboot configuration
func readRebootPolicy() string {
	data, err := os.ReadFile("/etc/apt/apt.conf.d/50unattended-upgrades")
	if err != nil {
		return "never" // default
	}

	content := string(data)
	re := regexp.MustCompile(`Unattended-Upgrade::Automatic-Reboot\s+"(true|false)"`)
	matches := re.FindStringSubmatch(content)

	if len(matches) > 1 {
		if matches[1] == "true" {
			return "if_needed"
		}
		return "never"
	}

	return "never"
}

// readExcludePackages reads package blacklist
func readExcludePackages() []string {
	data, err := os.ReadFile("/etc/apt/apt.conf.d/50unattended-upgrades")
	if err != nil {
		return []string{}
	}

	content := string(data)
	re := regexp.MustCompile(`Unattended-Upgrade::Package-Blacklist\s*\{([^}]*)\}`)
	matches := re.FindStringSubmatch(content)

	if len(matches) < 2 {
		return []string{}
	}

	blacklistContent := matches[1]
	pkgRe := regexp.MustCompile(`"([^"]+)"`)
	pkgMatches := pkgRe.FindAllStringSubmatch(blacklistContent, -1)

	var packages []string
	for _, m := range pkgMatches {
		if len(m) > 1 {
			pkg := strings.TrimSpace(m[1])
			if pkg != "" {
				packages = append(packages, pkg)
			}
		}
	}

	return packages
}
