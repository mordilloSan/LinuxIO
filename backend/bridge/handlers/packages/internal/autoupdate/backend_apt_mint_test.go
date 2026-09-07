package autoupdate

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

type fakeAutoUpdateHost struct {
	calls []string
	files map[string][]byte
	units map[string]string
}

func newFakeAutoUpdateHost() (*fakeAutoUpdateHost, backendHost) {
	fake := &fakeAutoUpdateHost{files: make(map[string][]byte), units: make(map[string]string)}
	host := backendHost{
		readFile: func(path string) ([]byte, error) {
			data, ok := fake.files[path]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return append([]byte(nil), data...), nil
		},
		writeFileAtomic: func(path string, data []byte, _ fs.FileMode, _ ...int) error {
			fake.calls = append(fake.calls, "write:"+path)
			fake.files[path] = append([]byte(nil), data...)
			return nil
		},
		removeFile: func(path string) error {
			fake.calls = append(fake.calls, "remove:"+path)
			if _, ok := fake.files[path]; !ok {
				return fs.ErrNotExist
			}
			delete(fake.files, path)
			return nil
		},
		fileExists: func(path string) bool {
			_, ok := fake.files[path]
			return ok
		},
		getUnitFileState: func(_ context.Context, name string) (string, error) {
			state, ok := fake.units[name]
			if !ok {
				return "", errors.New("unit not found")
			}
			return state, nil
		},
		enableUnit: func(_ context.Context, name string) error {
			fake.calls = append(fake.calls, "enable:"+name)
			fake.units[name] = "enabled"
			return nil
		},
		disableUnit: func(_ context.Context, name string) error {
			fake.calls = append(fake.calls, "disable:"+name)
			fake.units[name] = "disabled"
			return nil
		},
		startUnit: func(_ context.Context, name string) error {
			fake.calls = append(fake.calls, "start:"+name)
			return nil
		},
		stopUnit: func(_ context.Context, name string) error {
			fake.calls = append(fake.calls, "stop:"+name)
			return nil
		},
		restartUnit: func(_ context.Context, name string) error {
			fake.calls = append(fake.calls, "restart:"+name)
			return nil
		},
		daemonReload: func(context.Context) error {
			fake.calls = append(fake.calls, "daemon-reload")
			return nil
		},
	}
	return fake, host
}

func TestAptOriginsAreDistributionSpecific(t *testing.T) {
	tests := []struct {
		flavor  aptFlavor
		scope   AutoUpdateScope
		want    []string
		notWant []string
	}{
		{
			flavor: aptUbuntu,
			scope:  "security",
			want: []string{
				"Unattended-Upgrade::Allowed-Origins",
				"${distro_id}:${distro_codename}",
				"${distro_id}:${distro_codename}-security",
				"${distro_id}ESMApps:${distro_codename}-apps-security",
			},
			notWant: []string{"-updates", "-backports"},
		},
		{
			flavor: aptUbuntu,
			scope:  "all",
			want:   []string{"${distro_codename}-updates", "${distro_codename}-backports"},
		},
		{
			flavor: aptDebian,
			scope:  "security",
			want: []string{
				"Unattended-Upgrade::Origins-Pattern",
				"origin=Debian,codename=${distro_codename},label=Debian-Security",
				"origin=Debian,codename=${distro_codename}-security,label=Debian-Security",
			},
			notWant: []string{"codename=${distro_codename}-updates", "-backports"},
		},
		{
			flavor: aptDebian,
			scope:  "all",
			want: []string{
				"origin=Debian,codename=${distro_codename}-updates",
				"origin=Debian Backports,codename=${distro_codename}-backports,label=Debian Backports",
			},
		},
	}
	for _, test := range tests {
		backend := &aptBackend{flavor: test.flavor}
		content := string(backend.renderUnattended(AutoUpdateOptions{
			Frequency: "daily", RebootPolicy: "never", Scope: test.scope,
		}))
		for _, want := range append(test.want, "#clear Unattended-Upgrade::Allowed-Origins;") {
			if !strings.Contains(content, want) {
				t.Errorf("flavor %d scope %q: missing %q in:\n%s", test.flavor, test.scope, want, content)
			}
		}
		for _, unwanted := range test.notWant {
			if strings.Contains(content, unwanted) {
				t.Errorf("flavor %d scope %q: unexpectedly contains %q", test.flavor, test.scope, unwanted)
			}
		}
	}
}

func TestAptPeriodicFrequencyIntervals(t *testing.T) {
	tests := []struct {
		frequency AutoUpdateFrequency
		want      string
	}{
		{frequency: "hourly", want: "1h"},
		{frequency: "daily", want: "1"},
		{frequency: "weekly", want: "7"},
	}
	for _, test := range tests {
		update, download, unattended := aptPeriodicValues(AutoUpdateOptions{
			Enabled: true, Frequency: test.frequency,
		})
		if update != test.want || download != test.want || unattended != test.want {
			t.Errorf("%q intervals = (%q, %q, %q), want %q", test.frequency, update, download, unattended, test.want)
		}
	}
	_, _, unattended := aptPeriodicValues(AutoUpdateOptions{
		DownloadOnly: true, Enabled: true, Frequency: "weekly",
	})
	if unattended != "0" {
		t.Fatalf("download-only unattended interval = %q, want 0", unattended)
	}
}

func TestAptApplyUsesOwnedFilesAndKeepsDailyTimerEnabled(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	fake.files["/usr/bin/unattended-upgrades"] = []byte{}
	backend := &aptBackend{host: host, flavor: aptUbuntu}
	err := backend.Apply(context.Background(), AutoUpdateOptions{
		Enabled: true, ExcludePackages: []string{"linux-*"}, Frequency: "weekly",
		RebootPolicy: "if_needed", Scope: "all",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := fake.files[aptPeriodicPath]; !ok {
		t.Fatalf("%s was not written", aptPeriodicPath)
	}
	if _, ok := fake.files[aptUnattendedPath]; !ok {
		t.Fatalf("%s was not written", aptUnattendedPath)
	}
	for path := range fake.files {
		if strings.HasSuffix(path, "/20auto-upgrades") || strings.HasSuffix(path, "/50unattended-upgrades") {
			t.Fatalf("package-owned file was written: %s", path)
		}
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "disable:") || strings.HasPrefix(call, "stop:") {
			t.Fatalf("APT timer was disabled: %s", call)
		}
	}
	timer := string(fake.files["/etc/systemd/system/apt-daily.timer.d/linuxio.conf"])
	if !strings.Contains(timer, "OnCalendar=\nOnCalendar=weekly") {
		t.Fatalf("APT timer does not reset the vendor calendar:\n%s", timer)
	}
}

func TestAptDownloadOnlyDisablesOnlyUpgradeTimer(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	fake.files["/usr/bin/unattended-upgrades"] = []byte{}
	backend := &aptBackend{host: host, flavor: aptDebian}
	err := backend.Apply(context.Background(), AutoUpdateOptions{
		DownloadOnly: true, Enabled: true, Frequency: "daily",
		RebootPolicy: "never", Scope: "security",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, forbidden := range []string{"stop:apt-daily.timer", "disable:apt-daily.timer"} {
		if slices.Contains(fake.calls, forbidden) {
			t.Fatalf("APT daily maintenance timer was changed: %v", fake.calls)
		}
	}
	for _, required := range []string{"stop:apt-daily-upgrade.timer", "disable:apt-daily-upgrade.timer"} {
		if !slices.Contains(fake.calls, required) {
			t.Fatalf("missing %q in calls: %v", required, fake.calls)
		}
	}
}

func TestAptValidationHappensBeforeWrites(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	fake.files["/usr/bin/unattended-upgrades"] = []byte{}
	backend := &aptBackend{host: host, flavor: aptUbuntu}
	err := backend.Apply(context.Background(), AutoUpdateOptions{
		Frequency: "daily", RebootPolicy: "always", Scope: "security",
	})
	if err == nil {
		t.Fatal("Apply accepted unsupported reboot policy")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("invalid options caused mutations: %v", fake.calls)
	}
}

func TestAptReadReportsUnavailablePackageAndSupport(t *testing.T) {
	_, host := newFakeAutoUpdateHost()
	state, err := (&aptBackend{host: host, flavor: aptDebian}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if state.CanConfigure {
		t.Fatal("APT backend is configurable without unattended-upgrades")
	}
	if !state.Support.DownloadOnly || !state.Support.ExcludePackages {
		t.Fatalf("APT support was not reported: %+v", state.Support)
	}
	if len(state.Notes) == 0 {
		t.Fatal("missing unattended-upgrades note was not reported")
	}
}

func TestAptReadFallsBackToDistributionConfiguration(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	fake.files["/usr/bin/unattended-upgrades"] = []byte{}
	fake.files["/etc/apt/apt.conf.d/20auto-upgrades"] = []byte(`
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Download-Upgradeable-Packages "1";
APT::Periodic::Unattended-Upgrade "1";
`)
	fake.files[aptVendorConfig] = []byte(`
Unattended-Upgrade::Allowed-Origins {
        "${distro_id}:${distro_codename}-security";
        "${distro_id}:${distro_codename}-updates";
};
`)
	fake.units["apt-daily.timer"] = "enabled"
	fake.units["apt-daily-upgrade.timer"] = "enabled"
	state, err := (&aptBackend{host: host, flavor: aptUbuntu}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !state.Options.Enabled || state.Options.Scope != "updates" {
		t.Fatalf("distribution configuration was not read: %+v", state.Options)
	}
}

func TestAptReadIgnoresCommentedVendorExamples(t *testing.T) {
	configuration := []byte(`
// Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Origins-Pattern {
        "origin=Debian,codename=${distro_codename}-security,label=Debian-Security";
        // "origin=Debian,codename=${distro_codename}-updates";
        /* "origin=Debian Backports,codename=${distro_codename}-backports"; */
};
Unattended-Upgrade::Package-Blacklist {
        // "commented-package";
        "linux-*";
};
`)
	if scope := readAptScope(configuration); scope != "security" {
		t.Fatalf("scope = %q, want security", scope)
	}
	if policy := readAptRebootPolicy(configuration); policy != "never" {
		t.Fatalf("reboot policy = %q, want never", policy)
	}
	if exclusions := readAptExclusions(configuration); !slices.Equal(exclusions, []string{"linux-*"}) {
		t.Fatalf("exclusions = %v, want only active entry", exclusions)
	}
}

func TestMintRejectsUnsupportedOptionsBeforeWrites(t *testing.T) {
	tests := []AutoUpdateOptions{
		{DownloadOnly: true, Frequency: "daily", RebootPolicy: "never", Scope: "security"},
		{Frequency: "daily", RebootPolicy: "if_needed", Scope: "security"},
		{Frequency: "daily", RebootPolicy: "never", Scope: "updates"},
	}
	for _, options := range tests {
		fake, host := newFakeAutoUpdateHost()
		installMint(fake)
		backend := &mintBackend{host: host}
		if err := backend.Apply(context.Background(), options); err == nil {
			t.Fatalf("Apply accepted unsupported options: %+v", options)
		}
		if len(fake.calls) != 0 {
			t.Fatalf("invalid options caused mutations: %v", fake.calls)
		}
	}
}

func TestMintApplyPreservesNativeConfigurationAndEnablesLast(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	installMint(fake)
	fake.files[mintOptionsPath] = []byte("# native option\n--keep-configuration\n")
	fake.files[mintBlacklistPath] = []byte("# native exclusions\nnative-package\n" +
		mintManagedBegin + "\nold-linuxio-package\n" + mintManagedEnd + "\n")
	backend := &mintBackend{host: host}
	err := backend.Apply(context.Background(), AutoUpdateOptions{
		Enabled: true, ExcludePackages: []string{"linux-*", "docker.io"}, Frequency: "hourly",
		RebootPolicy: "never", Scope: "security",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	options := string(fake.files[mintOptionsPath])
	for _, want := range []string{"# native option", "--keep-configuration", "--only-security"} {
		if !strings.Contains(options, want) {
			t.Errorf("options file missing %q:\n%s", want, options)
		}
	}
	blacklist := string(fake.files[mintBlacklistPath])
	for _, want := range []string{"native-package", "linux-*", "docker.io"} {
		if !strings.Contains(blacklist, want) {
			t.Errorf("blacklist missing %q:\n%s", want, blacklist)
		}
	}
	if strings.Contains(blacklist, "old-linuxio-package") {
		t.Fatalf("old managed exclusion was retained:\n%s", blacklist)
	}
	timer := string(fake.files["/etc/systemd/system/"+mintTimer+".d/linuxio.conf"])
	if !strings.Contains(timer, "OnCalendar=\nOnCalendar=hourly") {
		t.Fatalf("Mint timer does not reset the vendor calendar:\n%s", timer)
	}
	markerWrite := slices.Index(fake.calls, "write:"+mintMarkerPath)
	timerStart := slices.Index(fake.calls, "start:"+mintTimer)
	if markerWrite < 0 || timerStart < 0 || markerWrite > timerStart {
		t.Fatalf("marker must be written before the timer starts: %v", fake.calls)
	}
}

func TestMintEnableRollsBackMarkerWhenTimerStartFails(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	installMint(fake)
	host.startUnit = func(_ context.Context, name string) error {
		fake.calls = append(fake.calls, "start:"+name)
		return errors.New("start failed")
	}
	err := (&mintBackend{host: host}).enable(context.Background())
	if err == nil {
		t.Fatal("enable succeeded when timer start failed")
	}
	if _, exists := fake.files[mintMarkerPath]; exists {
		t.Fatal("marker was not rolled back")
	}
	markerWrite := slices.Index(fake.calls, "write:"+mintMarkerPath)
	markerRemove := slices.Index(fake.calls, "remove:"+mintMarkerPath)
	if markerWrite < 0 || markerRemove < markerWrite {
		t.Fatalf("marker rollback was not ordered safely: %v", fake.calls)
	}
}

func TestMintDisableRemovesMarkerBeforeStoppingTimer(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	installMint(fake)
	fake.files[mintMarkerPath] = []byte{}
	backend := &mintBackend{host: host}
	if err := backend.disable(context.Background()); err != nil {
		t.Fatalf("disable: %v", err)
	}
	remove := slices.Index(fake.calls, "remove:"+mintMarkerPath)
	stop := slices.Index(fake.calls, "stop:"+mintTimer)
	disable := slices.Index(fake.calls, "disable:"+mintTimer)
	if remove < 0 || stop < 0 || disable < 0 || !(remove < stop && stop < disable) {
		t.Fatalf("unsafe disable order: %v", fake.calls)
	}
}

func TestMintReadRequiresMarkerAndTimer(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	installMint(fake)
	fake.files[mintMarkerPath] = []byte{}
	fake.files[mintOptionsPath] = []byte("--only-security\n")
	fake.files[mintBlacklistPath] = []byte("# native\nnative-package\n" +
		mintManagedBegin + "\nlinux-*\n" + mintManagedEnd + "\n")
	fake.units[mintTimer] = "disabled"
	state, err := (&mintBackend{host: host}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if state.Options.Enabled {
		t.Fatal("Mint automation is enabled with a disabled timer")
	}
	if !state.CanConfigure {
		t.Fatal("installed Mint backend is not configurable")
	}
	if !slices.Equal(state.Support.Scopes, []AutoUpdateScope{"security", "all"}) {
		t.Fatalf("Mint scopes = %v", state.Support.Scopes)
	}
	if state.Options.Scope != "security" {
		t.Fatalf("scope = %q, want security", state.Options.Scope)
	}
	if !slices.Equal(state.Options.ExcludePackages, []string{"linux-*"}) {
		t.Fatalf("exclusions = %v", state.Options.ExcludePackages)
	}
	if len(state.Notes) == 0 {
		t.Fatal("marker/timer mismatch was not reported")
	}
}

func installMint(fake *fakeAutoUpdateHost) {
	fake.files["/usr/bin/mintupdate-cli"] = []byte{}
	fake.files["/usr/lib/systemd/system/"+mintTimer] = []byte{}
	fake.units[mintTimer] = "enabled"
}
