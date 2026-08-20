package autoupdate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

const (
	mintTimer         = "mintupdate-automation-upgrade.timer"
	mintMarkerPath    = "/var/lib/linuxmint/mintupdate-automatic-upgrades-enabled"
	mintOptionsPath   = "/etc/mintupdate-automatic-upgrades.conf"
	mintBlacklistPath = "/etc/mintupdate.blacklist"
	mintManagedBegin  = "# BEGIN LinuxIO managed exclusions"
	mintManagedEnd    = "# END LinuxIO managed exclusions"
)

type mintBackend struct {
	host backendHost
}

func newMintBackend(host backendHost) UpdateBackend { return &mintBackend{host: host} }

func (*mintBackend) Name() AutoUpdateBackend { return backendMint }

func mintSupport() AutoUpdateOptionSupport {
	return AutoUpdateOptionSupport{
		DownloadOnly:    false,
		ExcludePackages: true,
		Frequencies:     []AutoUpdateFrequency{"hourly", "daily", "weekly"},
		RebootPolicies:  []AutoUpdateRebootPolicy{"never"},
		Scopes:          []AutoUpdateScope{"security", "all"},
	}
}

func (b *mintBackend) Read(ctx context.Context) (AutoUpdateState, error) {
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
			Frequency:       AutoUpdateFrequency(readTimerFrequency(b.host, mintTimer)),
			Scope:           "all",
			RebootPolicy:    "never",
			ExcludePackages: []string{},
		},
		Support: mintSupport(),
	}
	if !state.CanConfigure {
		state.Notes = []string{"Linux Mint Update Manager automation is not installed"}
		return state, nil
	}

	markerEnabled := b.host.fileExists(mintMarkerPath)
	timerIsEnabled, err := timerEnabled(ctx, b.host, mintTimer)
	if err != nil {
		return AutoUpdateState{}, fmt.Errorf("read %s state: %w", mintTimer, err)
	}
	state.Options.Enabled = markerEnabled && timerIsEnabled
	if markerEnabled != timerIsEnabled {
		state.Notes = append(state.Notes, "Linux Mint automation marker and timer state do not match")
	}
	if data, err := b.host.readFile(mintOptionsPath); err == nil && mintSecurityOnly(data) {
		state.Options.Scope = "security"
	}
	if data, err := b.host.readFile(mintBlacklistPath); err == nil {
		state.Options.ExcludePackages = readMintManagedExclusions(data)
	}
	return state, nil
}

func (b *mintBackend) Apply(ctx context.Context, options AutoUpdateOptions) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateOptions(options, mintSupport()); err != nil {
		return err
	}
	if !b.installed() {
		return fmt.Errorf("linux mint update manager automation is not installed")
	}
	onCalendar, err := onCalendarFor(string(options.Frequency))
	if err != nil {
		return err
	}

	existingOptions, _ := b.host.readFile(mintOptionsPath)
	if err := b.host.writeFileAtomic(mintOptionsPath, updateMintOptions(existingOptions, options.Scope), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", mintOptionsPath, err)
	}
	existingBlacklist, _ := b.host.readFile(mintBlacklistPath)
	if err := b.host.writeFileAtomic(mintBlacklistPath, updateMintBlacklist(existingBlacklist, options.ExcludePackages), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", mintBlacklistPath, err)
	}
	if err := writeTimerDropIn(b.host, mintTimer, onCalendar); err != nil {
		return fmt.Errorf("write %s schedule: %w", mintTimer, err)
	}
	if err := b.host.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd after configuring Linux Mint updates: %w", err)
	}
	if !options.Enabled {
		return b.disable(ctx)
	}
	return b.enable(ctx)
}

func (b *mintBackend) installed() bool {
	return b.host.fileExists("/usr/bin/mintupdate-cli") && timerArtifactExists(b.host.fileExists, mintTimer)
}

func (b *mintBackend) enable(ctx context.Context) error {
	if err := b.host.enableUnit(ctx, mintTimer); err != nil {
		return fmt.Errorf("enable %s: %w", mintTimer, err)
	}
	if err := b.host.writeFileAtomic(mintMarkerPath, []byte{}, 0o644); err != nil {
		return fmt.Errorf("enable Linux Mint update marker: %w", err)
	}
	if err := b.host.startUnit(ctx, mintTimer); err != nil {
		if rollbackErr := b.host.removeFile(mintMarkerPath); rollbackErr != nil && !errors.Is(rollbackErr, fs.ErrNotExist) {
			return fmt.Errorf("start %s: %w (also failed to roll back update marker: %v)", mintTimer, err, rollbackErr)
		}
		return fmt.Errorf("start %s: %w", mintTimer, err)
	}
	return nil
}

func (b *mintBackend) disable(ctx context.Context) error {
	if err := b.host.removeFile(mintMarkerPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("disable Linux Mint update marker: %w", err)
	}
	if err := b.host.stopUnit(ctx, mintTimer); err != nil {
		return fmt.Errorf("stop %s: %w", mintTimer, err)
	}
	if err := b.host.disableUnit(ctx, mintTimer); err != nil {
		return fmt.Errorf("disable %s: %w", mintTimer, err)
	}
	return nil
}

func (*mintBackend) ApplyOfflineNow(context.Context) error {
	return fmt.Errorf("not implemented for Linux Mint; use packagekit backend")
}

func mintSecurityOnly(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "--only-security" {
			return true
		}
	}
	return false
}

func updateMintOptions(existing []byte, scope AutoUpdateScope) []byte {
	lines := make([]string, 0)
	for line := range strings.SplitSeq(strings.TrimRight(string(existing), "\n"), "\n") {
		if strings.TrimSpace(line) != "--only-security" && line != "" {
			lines = append(lines, line)
		}
	}
	if scope == "security" {
		lines = append(lines, "--only-security")
	}
	if len(lines) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func readMintManagedExclusions(data []byte) []string {
	exclusions := make([]string, 0)
	inManagedBlock := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case mintManagedBegin:
			inManagedBlock = true
			continue
		case mintManagedEnd:
			return exclusions
		}
		if !inManagedBlock || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		exclusions = append(exclusions, line)
	}
	return exclusions
}

func updateMintBlacklist(existing []byte, exclusions []string) []byte {
	lines := strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	result := make([]string, 0, len(lines)+len(exclusions)+2)
	inManagedBlock := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case mintManagedBegin:
			inManagedBlock = true
			continue
		case mintManagedEnd:
			inManagedBlock = false
			continue
		}
		if !inManagedBlock && line != "" {
			result = append(result, line)
		}
	}
	if len(exclusions) > 0 {
		result = append(result, mintManagedBegin)
		result = append(result, exclusions...)
		result = append(result, mintManagedEnd)
	}
	if len(result) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(result, "\n") + "\n")
}
