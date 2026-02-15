package updates

import (
	"context"
	"fmt"
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
	return fileExists("/usr/bin/apt") && (fileExists("/usr/bin/unattended-upgrades") || fileExists("/usr/bin/unattended-upgrade"))
}

func (b *aptBackend) Read() (AutoUpdateState, error) {
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
	sd, err := systemd.New()
	if err != nil {
		return err
	}
	defer sd.Close()

	/* 1) Write 20auto-upgrades - controls periodic update checks */
	upd, dl, uu := "0", "0", "0"
	if o.Enabled {
		upd = "1"
		if o.DownloadOnly {
			dl, uu = "1", "0"
		} else {
			dl, uu = "1", "1"
		}
	}
	content20 := fmt.Sprintf(`APT::Periodic::Update-Package-Lists "%s";
APT::Periodic::Download-Upgradeable-Packages "%s";
APT::Periodic::Unattended-Upgrade "%s";
`, upd, dl, uu)
	if err2 := fsutil.WriteFileAtomic("/etc/apt/apt.conf.d/20auto-upgrades", []byte(content20), 0o644); err2 != nil {
		return fmt.Errorf("failed to write 20auto-upgrades: %w", err2)
	}

	/* 2) Write 50unattended-upgrades - controls allowed origins, reboot, and excludes */
	origins := []string{`${distro_id}:${distro_codename}-security`}
	if o.Scope == "updates" || o.Scope == "all" {
		origins = append(origins, `${distro_id}:${distro_codename}-updates`)
	}
	if o.Scope == "all" {
		origins = append(origins, `${distro_id}:${distro_codename}-backports`)
	}

	var bl strings.Builder
	for _, p := range o.ExcludePkgs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		bl.WriteString(`        "` + p + `";` + "\n")
	}

	reboot := "false"
	if o.RebootPolicy == "always" || o.RebootPolicy == "if_needed" {
		reboot = "true"
	}

	content50 := fmt.Sprintf(`Unattended-Upgrade::Allowed-Origins {
%s};
Unattended-Upgrade::Package-Blacklist {
%s};
Unattended-Upgrade::Automatic-Reboot "%s";
Unattended-Upgrade::Automatic-Reboot-Time "03:30";
`, formatOrigins(origins), bl.String(), reboot)
	if err3 := fsutil.WriteFileAtomic("/etc/apt/apt.conf.d/50unattended-upgrades", []byte(content50), 0o644); err3 != nil {
		return fmt.Errorf("failed to write 50unattended-upgrades: %w", err3)
	}

	/* 3) Write timer drop-ins - controls schedule frequency */
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

	/* 4) Reload systemd daemon to pick up drop-in changes */
	if err := sd.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	/* 5) Enable/disable and start/stop timers based on settings */
	if o.Enabled {
		// Always enable and start apt-daily.timer (handles update checks and downloads)
		if err := sd.Enable(ctx, "apt-daily.timer"); err != nil {
			return fmt.Errorf("failed to enable apt-daily.timer: %w", err)
		}
		if err := sd.Start(ctx, "apt-daily.timer"); err != nil {
			return fmt.Errorf("failed to start apt-daily.timer: %w", err)
		}

		// Only enable upgrade timer if not in download-only mode
		if !o.DownloadOnly {
			if err := sd.Enable(ctx, "apt-daily-upgrade.timer"); err != nil {
				return fmt.Errorf("failed to enable apt-daily-upgrade.timer: %w", err)
			}
			if err := sd.Start(ctx, "apt-daily-upgrade.timer"); err != nil {
				return fmt.Errorf("failed to start apt-daily-upgrade.timer: %w", err)
			}
		} else {
			// Download-only mode: stop and disable upgrade timer
			_ = sd.Stop(ctx, "apt-daily-upgrade.timer")
			_ = sd.Disable(ctx, "apt-daily-upgrade.timer")
		}
	} else {
		// Auto-updates disabled: stop and disable both timers
		_ = sd.Stop(ctx, "apt-daily.timer")
		_ = sd.Stop(ctx, "apt-daily-upgrade.timer")
		_ = sd.Disable(ctx, "apt-daily.timer")
		_ = sd.Disable(ctx, "apt-daily-upgrade.timer")
	}

	/* 6) Restart timers to apply new schedules immediately */
	if o.Enabled {
		if err := sd.Restart(ctx, "apt-daily.timer"); err != nil {
			// Log but don't fail - timer will restart on next boot
			_ = err
		}
		if !o.DownloadOnly {
			if err := sd.Restart(ctx, "apt-daily-upgrade.timer"); err != nil {
				_ = err
			}
		}
	}

	return nil
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
