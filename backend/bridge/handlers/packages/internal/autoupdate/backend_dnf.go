package autoupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/ini.v1"
)

const dnfAutomaticConfig = "/etc/dnf/automatic.conf"

type dnfGeneration uint8

const (
	dnf4 dnfGeneration = iota + 1
	dnf5
)

type dnfBackend struct {
	host       backendHost
	generation dnfGeneration
}

func newDNFBackend(host backendHost, generation dnfGeneration) UpdateBackend {
	return &dnfBackend{host: host, generation: generation}
}

func (b *dnfBackend) Name() AutoUpdateBackend {
	if b.generation == dnf5 {
		return backendDNF5
	}
	return backendDNF4
}

func (b *dnfBackend) timer() string {
	if b.generation == dnf5 {
		return "dnf5-automatic.timer"
	}
	return "dnf-automatic.timer"
}

func (b *dnfBackend) providerInstalled() bool {
	return timerArtifactExists(b.host.fileExists, b.timer())
}

func (b *dnfBackend) Read(ctx context.Context) (AutoUpdateState, error) {
	if ctx == nil {
		return AutoUpdateState{}, fmt.Errorf("nil context")
	}
	if err := ctx.Err(); err != nil {
		return AutoUpdateState{}, err
	}

	config, err := loadDNFConfig(b.host)
	if err != nil {
		return AutoUpdateState{}, err
	}
	support := b.support(config)
	state := AutoUpdateState{
		Backend:      b.Name(),
		CanConfigure: b.providerInstalled(),
		Options: AutoUpdateOptions{
			DownloadOnly:    true,
			Enabled:         false,
			ExcludePackages: []string{},
			Frequency:       "daily",
			RebootPolicy:    "never",
			Scope:           "all",
		},
		Support: support,
	}
	if !state.CanConfigure {
		state.Notes = []string{b.installNote()}
		return state, nil
	}

	state.Options.Frequency = AutoUpdateFrequency(readTimerFrequency(b.host, b.timer()))
	downloadsDisabled, err := readDNFOptions(config, &state.Options)
	if err != nil {
		return AutoUpdateState{}, err
	}
	if downloadsDisabled {
		state.Notes = append(
			state.Notes,
			"DNF Automatic is configured to check without downloading; saving will switch it to download-only mode.",
		)
	}
	if !dnfSupportsReboot(support, state.Options.RebootPolicy) {
		state.Options.RebootPolicy = "never"
	}

	enabledTimers, err := b.enabledTimers(ctx)
	if err != nil {
		return AutoUpdateState{}, err
	}
	state.Options.Enabled = len(enabledTimers) > 0
	b.applyTimerOverrides(enabledTimers, &state)
	return state, nil
}

func (b *dnfBackend) installNote() string {
	if b.generation == dnf5 {
		return "Install DNF5 Automatic to enable: sudo dnf install dnf5-plugin-automatic"
	}
	return "Install DNF Automatic to enable: sudo dnf install dnf-automatic"
}

func (b *dnfBackend) support(config *ini.File) AutoUpdateOptionSupport {
	reboots := []AutoUpdateRebootPolicy{"never"}
	if b.generation == dnf5 || dnf4RebootSupported(config) {
		reboots = append(reboots, "if_needed", "always")
	}
	return AutoUpdateOptionSupport{
		DownloadOnly:    true,
		ExcludePackages: true,
		Frequencies:     []AutoUpdateFrequency{"hourly", "daily", "weekly"},
		RebootPolicies:  reboots,
		Scopes:          []AutoUpdateScope{"security", "all"},
	}
}

func dnf4RebootSupported(config *ini.File) bool {
	key, err := config.Section("commands").GetKey("reboot")
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(key.String())) {
	case "never", "when-changed", "when-needed":
		return true
	default:
		return false
	}
}

func dnfSupportsReboot(support AutoUpdateOptionSupport, policy AutoUpdateRebootPolicy) bool {
	return slices.Contains(support.RebootPolicies, policy)
}

func (b *dnfBackend) Apply(ctx context.Context, options AutoUpdateOptions) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.providerInstalled() {
		return errors.New(b.installNote())
	}

	config, err := loadDNFConfig(b.host)
	if err != nil {
		return err
	}
	if validationErr := validateOptions(options, b.support(config)); validationErr != nil {
		return validationErr
	}
	onCalendar, err := onCalendarFor(string(options.Frequency))
	if err != nil {
		return err
	}
	if err := updateDNFConfig(config, options, b.generation); err != nil {
		return err
	}
	if err := writeDNFConfig(b.host, config); err != nil {
		return err
	}
	if err := writeTimerDropIn(b.host, b.timer(), onCalendar); err != nil {
		return fmt.Errorf("write %s schedule: %w", b.timer(), err)
	}
	if err := b.host.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd after configuring %s: %w", b.timer(), err)
	}
	return b.applyTimerState(ctx, options.Enabled)
}

func (b *dnfBackend) ApplyOfflineNow(context.Context) error {
	return errors.New("not implemented for dnf; use packagekit backend")
}

func loadDNFConfig(host backendHost) (*ini.File, error) {
	options := ini.LoadOptions{PreserveSurroundedQuote: true, SpaceBeforeInlineComment: true}
	if !host.fileExists(dnfAutomaticConfig) {
		return ini.Empty(options), nil
	}
	data, err := host.readFile(dnfAutomaticConfig)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dnfAutomaticConfig, err)
	}
	config, err := ini.LoadSources(options, data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", dnfAutomaticConfig, err)
	}
	return config, nil
}

func updateDNFConfig(config *ini.File, options AutoUpdateOptions, generation dnfGeneration) error {
	commands, err := config.NewSection("commands")
	if err != nil {
		return fmt.Errorf("create DNF commands section: %w", err)
	}
	values := map[string]string{
		"download_updates": "True",
		"apply_updates":    dnfApplyUpdatesValue(options.DownloadOnly),
		"upgrade_type":     dnfUpgradeType(options.Scope),
	}
	if generation == dnf5 || dnf4RebootSupported(config) {
		values["reboot"] = dnfRebootValue(options.RebootPolicy)
	}
	for key, value := range values {
		commands.Key(key).SetValue(value)
	}

	base, err := config.NewSection("base")
	if err != nil {
		return fmt.Errorf("create DNF base section: %w", err)
	}
	base.DeleteKey("exclude")
	base.Key("excludepkgs").SetValue(strings.Join(options.ExcludePackages, ","))
	return nil
}

func writeDNFConfig(host backendHost, config *ini.File) error {
	var output bytes.Buffer
	if _, err := config.WriteTo(&output); err != nil {
		return fmt.Errorf("encode %s: %w", dnfAutomaticConfig, err)
	}
	if err := host.writeFileAtomic(dnfAutomaticConfig, output.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dnfAutomaticConfig, err)
	}
	return nil
}

func dnfApplyUpdatesValue(downloadOnly bool) string {
	if downloadOnly {
		return "False"
	}
	return "True"
}

func dnfUpgradeType(scope AutoUpdateScope) string {
	if scope == "security" {
		return "security"
	}
	return "default"
}

func dnfRebootValue(policy AutoUpdateRebootPolicy) string {
	switch policy {
	case "always":
		return "when-changed"
	case "if_needed":
		return "when-needed"
	default:
		return "never"
	}
}

func readDNFOptions(config *ini.File, options *AutoUpdateOptions) (bool, error) {
	commands := config.Section("commands")
	applyUpdates, err := readDNFBool(commands, "apply_updates", false)
	if err != nil {
		return false, err
	}
	downloadUpdates, err := readDNFBool(commands, "download_updates", true)
	if err != nil {
		return false, err
	}
	options.DownloadOnly = !applyUpdates

	scope, err := readDNFScope(commands)
	if err != nil {
		return false, err
	}
	options.Scope = scope
	options.RebootPolicy = readDNFRebootPolicy(commands)
	options.ExcludePackages = readDNFExcludes(config.Section("base"))
	return !applyUpdates && !downloadUpdates, nil
}

func readDNFBool(section *ini.Section, name string, fallback bool) (bool, error) {
	key, err := section.GetKey(name)
	if err != nil {
		return fallback, nil
	}
	value, err := key.Bool()
	if err != nil {
		return false, fmt.Errorf("parse DNF option %s: %w", name, err)
	}
	return value, nil
}

func readDNFScope(commands *ini.Section) (AutoUpdateScope, error) {
	value := strings.ToLower(strings.TrimSpace(commands.Key("upgrade_type").String()))
	switch value {
	case "", "default":
		return "all", nil
	case "security":
		return "security", nil
	default:
		return "", fmt.Errorf("unsupported DNF upgrade_type %q", value)
	}
}

func readDNFRebootPolicy(commands *ini.Section) AutoUpdateRebootPolicy {
	switch strings.ToLower(strings.TrimSpace(commands.Key("reboot").String())) {
	case "when-changed":
		return "always"
	case "when-needed":
		return "if_needed"
	default:
		return "never"
	}
}

func readDNFExcludes(base *ini.Section) []string {
	value := base.Key("excludepkgs").String()
	if value == "" {
		value = base.Key("exclude").String()
	}
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}

func (b *dnfBackend) enabledTimers(ctx context.Context) ([]string, error) {
	enabled := make([]string, 0, len(b.managedTimers()))
	for _, timer := range b.managedTimers() {
		if timer != b.timer() && !timerArtifactExists(b.host.fileExists, timer) {
			continue
		}
		isEnabled, err := timerEnabled(ctx, b.host, timer)
		if err != nil {
			return nil, fmt.Errorf("read %s state: %w", timer, err)
		}
		if isEnabled {
			enabled = append(enabled, timer)
		}
	}
	return enabled, nil
}

func (b *dnfBackend) managedTimers() []string {
	if b.generation == dnf5 {
		return []string{"dnf5-automatic.timer", "dnf-automatic.timer"}
	}
	return []string{
		"dnf-automatic.timer",
		"dnf-automatic-download.timer",
		"dnf-automatic-install.timer",
		"dnf-automatic-notifyonly.timer",
	}
}

func (b *dnfBackend) applyTimerOverrides(enabled []string, state *AutoUpdateState) {
	conflicts := make([]string, 0, len(enabled))
	for _, timer := range enabled {
		if timer == b.timer() {
			continue
		}
		conflicts = append(conflicts, timer)
		switch timer {
		case "dnf-automatic-install.timer":
			state.Options.DownloadOnly = false
		case "dnf-automatic-download.timer", "dnf-automatic-notifyonly.timer":
			state.Options.DownloadOnly = true
		}
	}
	if len(conflicts) > 0 {
		state.Notes = append(
			state.Notes,
			"Conflicting DNF automatic timers are enabled: "+strings.Join(conflicts, ", ")+". Save to consolidate them under LinuxIO management.",
		)
	}
}

func (b *dnfBackend) applyTimerState(ctx context.Context, enabled bool) error {
	for _, timer := range b.managedTimers() {
		if timer == b.timer() && enabled {
			continue
		}
		if !timerArtifactExists(b.host.fileExists, timer) {
			continue
		}
		if err := b.stopAndDisable(ctx, timer); err != nil {
			return err
		}
	}
	if !enabled {
		return nil
	}
	if err := b.host.enableUnit(ctx, b.timer()); err != nil {
		return fmt.Errorf("enable %s: %w", b.timer(), err)
	}
	if err := b.host.startUnit(ctx, b.timer()); err != nil {
		return fmt.Errorf("start %s: %w", b.timer(), err)
	}
	if err := b.host.restartUnit(ctx, b.timer()); err != nil {
		return fmt.Errorf("restart %s: %w", b.timer(), err)
	}
	return nil
}

func (b *dnfBackend) stopAndDisable(ctx context.Context, timer string) error {
	if err := b.host.stopUnit(ctx, timer); err != nil {
		return fmt.Errorf("stop %s: %w", timer, err)
	}
	if err := b.host.disableUnit(ctx, timer); err != nil {
		return fmt.Errorf("disable %s: %w", timer, err)
	}
	return nil
}
