package autoupdate

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	aptPeriodicPath   = "/etc/apt/apt.conf.d/52linuxio-periodic"
	aptUnattendedPath = "/etc/apt/apt.conf.d/52linuxio-unattended-upgrades"
	aptVendorConfig   = "/etc/apt/apt.conf.d/50unattended-upgrades"
)

type aptFlavor uint8

const (
	aptUbuntu aptFlavor = iota
	aptDebian
)

type aptBackend struct {
	host   backendHost
	flavor aptFlavor
}

func newAptBackend(host backendHost, flavor aptFlavor) UpdateBackend {
	return &aptBackend{host: host, flavor: flavor}
}

func (*aptBackend) Name() AutoUpdateBackend { return backendAPT }

func aptSupport() AutoUpdateOptionSupport {
	return AutoUpdateOptionSupport{
		DownloadOnly:    true,
		ExcludePackages: true,
		Frequencies:     []AutoUpdateFrequency{"hourly", "daily", "weekly"},
		RebootPolicies:  []AutoUpdateRebootPolicy{"never", "if_needed"},
		Scopes:          []AutoUpdateScope{"security", "updates", "all"},
	}
}

func (b *aptBackend) Read(ctx context.Context) (AutoUpdateState, error) {
	if ctx == nil {
		return AutoUpdateState{}, fmt.Errorf("nil context")
	}
	if err := ctx.Err(); err != nil {
		return AutoUpdateState{}, err
	}

	state := AutoUpdateState{
		Backend:      b.Name(),
		CanConfigure: b.installed(),
		Options: AutoUpdateOptions{
			Frequency:       AutoUpdateFrequency(readTimerFrequency(b.host, "apt-daily.timer")),
			Scope:           "security",
			RebootPolicy:    "never",
			ExcludePackages: []string{},
		},
		Support: aptSupport(),
	}
	if !state.CanConfigure {
		state.Notes = []string{"Install unattended-upgrades to configure automatic updates"}
		return state, nil
	}

	periodic := readAptPeriodicConfiguration(b.host)
	state.Options.Enabled, state.Options.DownloadOnly = readAptPeriodic(periodic)
	unattended, err := b.host.readFile(aptUnattendedPath)
	if err != nil {
		unattended, err = b.host.readFile(aptVendorConfig)
	}
	if err == nil {
		state.Options.Scope = readAptScope(unattended)
		state.Options.RebootPolicy = readAptRebootPolicy(unattended)
		state.Options.ExcludePackages = readAptExclusions(unattended)
	}

	dailyEnabled, err := timerEnabled(ctx, b.host, "apt-daily.timer")
	if err != nil {
		return AutoUpdateState{}, fmt.Errorf("read apt-daily.timer state: %w", err)
	}
	upgradeEnabled, err := timerEnabled(ctx, b.host, "apt-daily-upgrade.timer")
	if err != nil {
		return AutoUpdateState{}, fmt.Errorf("read apt-daily-upgrade.timer state: %w", err)
	}
	if state.Options.Enabled && (!dailyEnabled || (!state.Options.DownloadOnly && !upgradeEnabled)) {
		state.Options.Enabled = false
		state.Notes = append(state.Notes, "Automatic updates are configured, but a required APT timer is disabled")
	}
	return state, nil
}

func (b *aptBackend) Apply(ctx context.Context, options AutoUpdateOptions) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateOptions(options, aptSupport()); err != nil {
		return err
	}
	if !b.installed() {
		return fmt.Errorf("unattended-upgrades is not installed")
	}
	onCalendar, err := onCalendarFor(string(options.Frequency))
	if err != nil {
		return err
	}

	if err := b.host.writeFileAtomic(aptPeriodicPath, renderAptPeriodic(options), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", aptPeriodicPath, err)
	}
	if err := b.host.writeFileAtomic(aptUnattendedPath, b.renderUnattended(options), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", aptUnattendedPath, err)
	}
	if err := writeTimerDropIn(b.host, "apt-daily.timer", onCalendar); err != nil {
		return fmt.Errorf("write apt-daily.timer schedule: %w", err)
	}
	if err := writeTimerDropIn(b.host, "apt-daily-upgrade.timer", onCalendar); err != nil {
		return fmt.Errorf("write apt-daily-upgrade.timer schedule: %w", err)
	}
	if err := b.host.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd after configuring APT updates: %w", err)
	}
	if !options.Enabled {
		return b.disableUpgradeTimer(ctx)
	}
	if err := b.enableTimer(ctx, "apt-daily.timer"); err != nil {
		return err
	}
	if options.DownloadOnly {
		return b.disableUpgradeTimer(ctx)
	}
	return b.enableTimer(ctx, "apt-daily-upgrade.timer")
}

func (b *aptBackend) installed() bool {
	return b.host.fileExists("/usr/bin/unattended-upgrades") || b.host.fileExists("/usr/bin/unattended-upgrade")
}

func (b *aptBackend) enableTimer(ctx context.Context, timer string) error {
	if err := b.host.enableUnit(ctx, timer); err != nil {
		return fmt.Errorf("enable %s: %w", timer, err)
	}
	if err := b.host.startUnit(ctx, timer); err != nil {
		return fmt.Errorf("start %s: %w", timer, err)
	}
	return nil
}

func (b *aptBackend) disableUpgradeTimer(ctx context.Context) error {
	const timer = "apt-daily-upgrade.timer"
	if err := b.host.stopUnit(ctx, timer); err != nil {
		return fmt.Errorf("stop %s: %w", timer, err)
	}
	if err := b.host.disableUnit(ctx, timer); err != nil {
		return fmt.Errorf("disable %s: %w", timer, err)
	}
	return nil
}

func (*aptBackend) ApplyOfflineNow(context.Context) error {
	return fmt.Errorf("not implemented for apt; use packagekit backend")
}

func renderAptPeriodic(options AutoUpdateOptions) []byte {
	update, download, unattended := aptPeriodicValues(options)
	return []byte(fmt.Sprintf(`# Managed by LinuxIO.
APT::Periodic::Update-Package-Lists "%s";
APT::Periodic::Download-Upgradeable-Packages "%s";
APT::Periodic::Unattended-Upgrade "%s";
`, update, download, unattended))
}

func aptPeriodicValues(options AutoUpdateOptions) (string, string, string) {
	if !options.Enabled {
		return "0", "0", "0"
	}
	interval := aptPeriodicInterval(options.Frequency)
	if options.DownloadOnly {
		return interval, interval, "0"
	}
	return interval, interval, interval
}

func aptPeriodicInterval(frequency AutoUpdateFrequency) string {
	switch frequency {
	case "hourly":
		return "1h"
	case "weekly":
		return "7"
	default:
		return "1"
	}
}

func (b *aptBackend) renderUnattended(options AutoUpdateOptions) []byte {
	var content strings.Builder
	content.WriteString("# Managed by LinuxIO.\n")
	content.WriteString("#clear Unattended-Upgrade::Allowed-Origins;\n")
	content.WriteString("#clear Unattended-Upgrade::Origins-Pattern;\n")
	content.WriteString("#clear Unattended-Upgrade::Package-Blacklist;\n")
	if b.flavor == aptUbuntu {
		content.WriteString("Unattended-Upgrade::Allowed-Origins {\n")
	} else {
		content.WriteString("Unattended-Upgrade::Origins-Pattern {\n")
	}
	content.WriteString(formatAptList(b.origins(options.Scope)))
	content.WriteString("};\nUnattended-Upgrade::Package-Blacklist {\n")
	content.WriteString(formatAptList(options.ExcludePackages))
	content.WriteString("};\nUnattended-Upgrade::Automatic-Reboot \"")
	content.WriteString(aptRebootSetting(options.RebootPolicy))
	content.WriteString("\";\nUnattended-Upgrade::Automatic-Reboot-Time \"03:30\";\n")
	return []byte(content.String())
}

func (b *aptBackend) origins(scope AutoUpdateScope) []string {
	if b.flavor == aptUbuntu {
		return ubuntuOrigins(scope)
	}
	return debianOrigins(scope)
}

func ubuntuOrigins(scope AutoUpdateScope) []string {
	origins := []string{
		`${distro_id}:${distro_codename}`,
		`${distro_id}:${distro_codename}-security`,
		`${distro_id}ESMApps:${distro_codename}-apps-security`,
		`${distro_id}ESM:${distro_codename}-infra-security`,
	}
	if scope == "updates" || scope == "all" {
		origins = append(origins, `${distro_id}:${distro_codename}-updates`)
	}
	if scope == "all" {
		origins = append(origins, `${distro_id}:${distro_codename}-backports`)
	}
	return origins
}

func debianOrigins(scope AutoUpdateScope) []string {
	origins := []string{
		`origin=Debian,codename=${distro_codename},label=Debian`,
		`origin=Debian,codename=${distro_codename},label=Debian-Security`,
		`origin=Debian,codename=${distro_codename}-security,label=Debian-Security`,
	}
	if scope == "updates" || scope == "all" {
		origins = append(origins, `origin=Debian,codename=${distro_codename}-updates`)
	}
	if scope == "all" {
		origins = append(origins, `origin=Debian Backports,codename=${distro_codename}-backports,label=Debian Backports`)
	}
	return origins
}

func formatAptList(values []string) string {
	var formatted strings.Builder
	for _, value := range values {
		formatted.WriteString(`        "`)
		formatted.WriteString(value)
		formatted.WriteString("\";\n")
	}
	return formatted.String()
}

func aptRebootSetting(policy AutoUpdateRebootPolicy) string {
	if policy == "if_needed" {
		return "true"
	}
	return "false"
}

var aptPeriodicSetting = regexp.MustCompile(`(?m)^\s*APT::Periodic::([A-Za-z-]+)\s+"([^"]*)";`)

func readAptPeriodic(data []byte) (enabled, downloadOnly bool) {
	settings := make(map[string]string)
	for _, match := range aptPeriodicSetting.FindAllSubmatch(data, -1) {
		settings[string(match[1])] = string(match[2])
	}
	update := settings["Update-Package-Lists"]
	download := settings["Download-Upgradeable-Packages"]
	unattended := settings["Unattended-Upgrade"]
	hasDownload := download != "" && download != "0"
	hasUnattended := unattended != "" && unattended != "0"
	enabled = update != "" && update != "0" && (hasDownload || hasUnattended)
	downloadOnly = enabled && hasDownload && !hasUnattended
	return enabled, downloadOnly
}

func readAptPeriodicConfiguration(host backendHost) []byte {
	paths := []string{
		"/etc/apt/apt.conf.d/10periodic",
		"/etc/apt/apt.conf.d/20auto-upgrades",
		aptPeriodicPath,
	}
	var configuration strings.Builder
	for _, path := range paths {
		data, err := host.readFile(path)
		if err != nil {
			continue
		}
		configuration.Write(data)
		configuration.WriteByte('\n')
	}
	return []byte(configuration.String())
}

func readAptScope(data []byte) AutoUpdateScope {
	content := activeAptConfiguration(data)
	if strings.Contains(content, "-backports") {
		return "all"
	}
	if strings.Contains(content, "-updates") {
		return "updates"
	}
	return "security"
}

func readAptRebootPolicy(data []byte) AutoUpdateRebootPolicy {
	if regexp.MustCompile(`Unattended-Upgrade::Automatic-Reboot\s+"true"`).MatchString(activeAptConfiguration(data)) {
		return "if_needed"
	}
	return "never"
}

func readAptExclusions(data []byte) []string {
	block := regexp.MustCompile(`(?s)Unattended-Upgrade::Package-Blacklist\s*\{(.*?)\};`).FindStringSubmatch(activeAptConfiguration(data))
	if len(block) < 2 {
		return []string{}
	}
	matches := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(block[1], -1)
	exclusions := make([]string, 0, len(matches))
	for _, match := range matches {
		exclusions = append(exclusions, match[1])
	}
	return exclusions
}

func activeAptConfiguration(data []byte) string {
	var active strings.Builder
	inBlockComment := false
	for line := range strings.SplitSeq(string(data), "\n") {
		for {
			if inBlockComment {
				_, after, found := strings.Cut(line, "*/")
				if !found {
					line = ""
					break
				}
				line = after
				inBlockComment = false
			}
			before, after, found := strings.Cut(line, "/*")
			if !found {
				break
			}
			line = before
			if _, remainder, closed := strings.Cut(after, "*/"); closed {
				line += remainder
				continue
			}
			inBlockComment = true
			break
		}
		line, _, _ = strings.Cut(line, "//")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		active.WriteString(line)
		active.WriteByte('\n')
	}
	return active.String()
}
