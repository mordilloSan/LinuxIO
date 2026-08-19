package autoupdate

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func TestDNFBackendReportsMissingProvider(t *testing.T) {
	tests := []struct {
		generation dnfGeneration
		name       AutoUpdateBackend
		note       string
	}{
		{generation: dnf4, name: backendDNF4, note: "dnf-automatic"},
		{generation: dnf5, name: backendDNF5, note: "dnf5-plugin-automatic"},
	}

	for _, tc := range tests {
		t.Run(string(tc.name), func(t *testing.T) {
			fake := newFakeDNFHost()
			backend := newDNFBackend(fake.host(), tc.generation)
			state, err := backend.Read(context.Background())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if state.Backend != tc.name {
				t.Fatalf("Backend = %q, want %q", state.Backend, tc.name)
			}
			if state.CanConfigure {
				t.Fatal("CanConfigure = true without an installed provider")
			}
			if len(state.Notes) != 1 || !strings.Contains(state.Notes[0], tc.note) {
				t.Fatalf("Notes = %#v, want installation hint containing %q", state.Notes, tc.note)
			}
			if state.Options.Enabled {
				t.Fatal("Enabled = true without an installed provider")
			}
		})
	}
}

func TestDNF5ReadMapsConfiguration(t *testing.T) {
	fake := newFakeDNFHost()
	fake.installTimer("dnf5-automatic.timer")
	fake.states["dnf5-automatic.timer"] = "enabled"
	fake.files[dnfAutomaticConfig] = []byte(`[commands]
upgrade_type = security
download_updates = yes
apply_updates = yes
reboot = when-needed

[base]
excludepkgs = kernel*, podman
`)
	fake.files["/etc/systemd/system/dnf5-automatic.timer.d/linuxio.conf"] = []byte(
		"[Timer]\nOnCalendar=\nOnCalendar=weekly\n",
	)

	state, err := newDNFBackend(fake.host(), dnf5).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !state.CanConfigure || !state.Options.Enabled {
		t.Fatalf("state = %#v, want configurable and enabled", state)
	}
	if state.Options.Frequency != "weekly" || state.Options.Scope != "security" {
		t.Fatalf("Options = %#v, want weekly security updates", state.Options)
	}
	if state.Options.DownloadOnly || state.Options.RebootPolicy != "if_needed" {
		t.Fatalf("Options = %#v, want install mode and if-needed reboot", state.Options)
	}
	if !slices.Equal(state.Options.ExcludePackages, []string{"kernel*", "podman"}) {
		t.Fatalf("ExcludePackages = %#v", state.Options.ExcludePackages)
	}
	if !slices.Equal(state.Support.RebootPolicies, []AutoUpdateRebootPolicy{"never", "if_needed", "always"}) {
		t.Fatalf("RebootPolicies = %#v", state.Support.RebootPolicies)
	}
}

func TestDNF4RebootSupportRequiresInstalledConfigurationOption(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantPolicy []AutoUpdateRebootPolicy
	}{
		{
			name:       "legacy",
			config:     "[commands]\nupgrade_type=default\napply_updates=no\n",
			wantPolicy: []AutoUpdateRebootPolicy{"never"},
		},
		{
			name:       "reboot capable",
			config:     "[commands]\nupgrade_type=default\napply_updates=no\nreboot=when-changed\n",
			wantPolicy: []AutoUpdateRebootPolicy{"never", "if_needed", "always"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeDNFHost()
			fake.installTimer("dnf-automatic.timer")
			fake.files[dnfAutomaticConfig] = []byte(tc.config)
			state, err := newDNFBackend(fake.host(), dnf4).Read(context.Background())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if !slices.Equal(state.Support.RebootPolicies, tc.wantPolicy) {
				t.Fatalf("RebootPolicies = %#v, want %#v", state.Support.RebootPolicies, tc.wantPolicy)
			}
		})
	}
}

func TestDNF5ApplyPreservesUnmanagedConfiguration(t *testing.T) {
	fake := newFakeDNFHost()
	fake.installTimer("dnf5-automatic.timer")
	fake.installTimer("dnf-automatic.timer")
	fake.files[dnfAutomaticConfig] = []byte(`# administrator comment
[commands]
upgrade_type = security
download_updates = no
apply_updates = no
network_online_timeout = 99

[emitters]
emit_via = email

[base]
exclude = old*
debuglevel = 2
`)

	options := AutoUpdateOptions{
		DownloadOnly:    false,
		Enabled:         true,
		ExcludePackages: []string{"kernel*", "podman"},
		Frequency:       "hourly",
		RebootPolicy:    "always",
		Scope:           "all",
	}
	if err := newDNFBackend(fake.host(), dnf5).Apply(context.Background(), options); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	config := string(fake.files[dnfAutomaticConfig])
	if !strings.Contains(config, "administrator comment") {
		t.Errorf("updated config does not preserve administrator comment:\n%s", config)
	}
	parsed, err := loadDNFConfig(fake.host())
	if err != nil {
		t.Fatalf("parse updated config: %v", err)
	}
	for _, preserved := range []struct {
		section string
		key     string
		value   string
	}{
		{section: "commands", key: "network_online_timeout", value: "99"},
		{section: "emitters", key: "emit_via", value: "email"},
		{section: "base", key: "debuglevel", value: "2"},
	} {
		if got := parsed.Section(preserved.section).Key(preserved.key).String(); got != preserved.value {
			t.Errorf("%s.%s = %q, want preserved value %q", preserved.section, preserved.key, got, preserved.value)
		}
	}
	for _, managed := range []struct {
		section string
		key     string
		value   string
	}{
		{section: "commands", key: "upgrade_type", value: "default"},
		{section: "commands", key: "download_updates", value: "True"},
		{section: "commands", key: "apply_updates", value: "True"},
		{section: "commands", key: "reboot", value: "when-changed"},
		{section: "base", key: "excludepkgs", value: "kernel*,podman"},
	} {
		if got := parsed.Section(managed.section).Key(managed.key).String(); got != managed.value {
			t.Errorf("%s.%s = %q, want %q", managed.section, managed.key, got, managed.value)
		}
	}
	if _, err := parsed.Section("base").GetKey("exclude"); err == nil {
		t.Fatalf("legacy exclude remains in updated config:\n%s", config)
	}

	wantSchedule := "[Timer]\nOnCalendar=\nOnCalendar=hourly\nRandomizedDelaySec=30m\n"
	if got := string(fake.files["/etc/systemd/system/dnf5-automatic.timer.d/linuxio.conf"]); got != wantSchedule {
		t.Fatalf("schedule = %q, want %q", got, wantSchedule)
	}
	for _, operation := range []string{
		"reload",
		"stop:dnf-automatic.timer",
		"disable:dnf-automatic.timer",
		"enable:dnf5-automatic.timer",
		"start:dnf5-automatic.timer",
		"restart:dnf5-automatic.timer",
	} {
		if !slices.Contains(fake.operations, operation) {
			t.Errorf("operations %#v do not contain %q", fake.operations, operation)
		}
	}
}

func TestDNF4DisableStopsEveryInstalledTimer(t *testing.T) {
	fake := newFakeDNFHost()
	for _, timer := range []string{
		"dnf-automatic.timer",
		"dnf-automatic-download.timer",
		"dnf-automatic-install.timer",
		"dnf-automatic-notifyonly.timer",
	} {
		fake.installTimer(timer)
	}
	fake.files[dnfAutomaticConfig] = []byte("[commands]\nupgrade_type=security\napply_updates=yes\n")

	options := AutoUpdateOptions{
		Enabled:      false,
		Frequency:    "daily",
		RebootPolicy: "never",
		Scope:        "security",
	}
	if err := newDNFBackend(fake.host(), dnf4).Apply(context.Background(), options); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	config, err := loadDNFConfig(fake.host())
	if err != nil {
		t.Fatalf("parse updated config: %v", err)
	}
	if got := config.Section("commands").Key("apply_updates").String(); got != "True" {
		t.Fatalf("apply_updates = %q, want install mode preserved while disabled", got)
	}
	for _, timer := range []string{
		"dnf-automatic.timer",
		"dnf-automatic-download.timer",
		"dnf-automatic-install.timer",
		"dnf-automatic-notifyonly.timer",
	} {
		for _, operation := range []string{"stop:" + timer, "disable:" + timer} {
			if !slices.Contains(fake.operations, operation) {
				t.Errorf("operations %#v do not contain %q", fake.operations, operation)
			}
		}
	}
}

func TestDNF4ReadReportsAlternateTimerConflict(t *testing.T) {
	fake := newFakeDNFHost()
	fake.installTimer("dnf-automatic.timer")
	fake.installTimer("dnf-automatic-install.timer")
	fake.states["dnf-automatic.timer"] = "disabled"
	fake.states["dnf-automatic-install.timer"] = "enabled"
	fake.files[dnfAutomaticConfig] = []byte("[commands]\nupgrade_type=security\napply_updates=no\n")

	state, err := newDNFBackend(fake.host(), dnf4).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !state.Options.Enabled || state.Options.DownloadOnly {
		t.Fatalf("Options = %#v, want enabled install mode", state.Options)
	}
	if len(state.Notes) != 1 || !strings.Contains(state.Notes[0], "dnf-automatic-install.timer") {
		t.Fatalf("Notes = %#v, want alternate timer warning", state.Notes)
	}
}

func TestDNFApplyRejectsUnsupportedOptionsBeforeWriting(t *testing.T) {
	fake := newFakeDNFHost()
	fake.installTimer("dnf-automatic.timer")
	fake.files[dnfAutomaticConfig] = []byte("[commands]\nupgrade_type=security\napply_updates=no\n")

	err := newDNFBackend(fake.host(), dnf4).Apply(context.Background(), AutoUpdateOptions{
		Frequency:    "daily",
		RebootPolicy: "never",
		Scope:        "updates",
	})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("Apply error = %v, want unsupported scope", err)
	}
	if len(fake.operations) != 0 {
		t.Fatalf("operations = %#v, want no writes or systemd calls", fake.operations)
	}
}

func TestDNFApplyPropagatesSystemdErrors(t *testing.T) {
	fake := newFakeDNFHost()
	fake.installTimer("dnf5-automatic.timer")
	fake.errors["restart:dnf5-automatic.timer"] = errors.New("restart failed")

	err := newDNFBackend(fake.host(), dnf5).Apply(context.Background(), AutoUpdateOptions{
		Enabled:      true,
		Frequency:    "daily",
		RebootPolicy: "never",
		Scope:        "all",
	})
	if err == nil || !strings.Contains(err.Error(), "restart dnf5-automatic.timer") {
		t.Fatalf("Apply error = %v, want restart failure", err)
	}
}

type fakeDNFHost struct {
	artifacts  map[string]bool
	errors     map[string]error
	files      map[string][]byte
	operations []string
	states     map[string]string
}

func newFakeDNFHost() *fakeDNFHost {
	return &fakeDNFHost{
		artifacts: make(map[string]bool),
		errors:    make(map[string]error),
		files:     make(map[string][]byte),
		states:    make(map[string]string),
	}
}

func (f *fakeDNFHost) installTimer(timer string) {
	f.artifacts["/usr/lib/systemd/system/"+timer] = true
}

func (f *fakeDNFHost) host() backendHost {
	return backendHost{
		readFile: func(path string) ([]byte, error) {
			data, ok := f.files[path]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return slices.Clone(data), nil
		},
		writeFileAtomic: func(path string, data []byte, _ fs.FileMode) error {
			f.operations = append(f.operations, "write:"+path)
			if err := f.errors["write:"+path]; err != nil {
				return err
			}
			f.files[path] = slices.Clone(data)
			return nil
		},
		removeFile: func(path string) error {
			delete(f.files, path)
			return nil
		},
		fileExists: func(path string) bool {
			return f.artifacts[path] || f.files[path] != nil
		},
		getUnitFileState: func(_ context.Context, timer string) (string, error) {
			if err := f.errors["state:"+timer]; err != nil {
				return "", err
			}
			if state := f.states[timer]; state != "" {
				return state, nil
			}
			return "disabled", nil
		},
		enableUnit:  f.systemOperation("enable"),
		disableUnit: f.systemOperation("disable"),
		startUnit:   f.systemOperation("start"),
		stopUnit:    f.systemOperation("stop"),
		restartUnit: f.systemOperation("restart"),
		daemonReload: func(context.Context) error {
			f.operations = append(f.operations, "reload")
			return f.errors["reload"]
		},
	}
}

func (f *fakeDNFHost) systemOperation(operation string) func(context.Context, string) error {
	return func(_ context.Context, timer string) error {
		key := operation + ":" + timer
		f.operations = append(f.operations, key)
		return f.errors[key]
	}
}
