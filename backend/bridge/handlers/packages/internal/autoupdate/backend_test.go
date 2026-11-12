package autoupdate

import (
	"slices"
	"testing"
)

// installedBackend is a provider's backend on a fake host where it is
// installed, with the host's files to see what Apply wrote.
type installedBackend struct {
	name    string
	service string // the upgrade service the notify drop-in belongs to
	backend UpdateBackend
	files   map[string][]byte
}

func installedBackends() []installedBackend {
	apt, aptHost := newFakeAutoUpdateHost()
	apt.files["/usr/bin/unattended-upgrades"] = []byte{}
	apt.units["apt-daily.timer"], apt.units["apt-daily-upgrade.timer"] = "enabled", "enabled"
	mint, mintHost := newFakeAutoUpdateHost()
	installMint(mint)
	dnf4Host, dnf5Host := newFakeDNFHost(), newFakeDNFHost()
	dnf4Host.installTimer("dnf-automatic.timer")
	dnf5Host.installTimer("dnf5-automatic.timer")
	return []installedBackend{
		{name: "apt", service: aptUpgradeService, backend: &aptBackend{host: aptHost, flavor: aptDebian}, files: apt.files},
		{name: "mint", service: mintUpgradeService, backend: &mintBackend{host: mintHost}, files: mint.files},
		{name: "dnf4", service: "dnf-automatic.service", backend: newDNFBackend(dnf4Host.host(), dnf4), files: dnf4Host.files},
		{name: "dnf5", service: "dnf5-automatic.service", backend: newDNFBackend(dnf5Host.host(), dnf5), files: dnf5Host.files},
	}
}

func TestOfferedPoliciesHaveNoNever(t *testing.T) {
	// Only unattended-upgrades says what a run installed; DNF and Mint give
	// the service result alone, so "on install" cannot differ from "on failure".
	want := map[string][]AutoUpdateNotify{
		"apt":  {AutoUpdateNotifyOnFailure, AutoUpdateNotifyOnChange, AutoUpdateNotifyAlways},
		"mint": {AutoUpdateNotifyOnFailure, AutoUpdateNotifyAlways},
		"dnf4": {AutoUpdateNotifyOnFailure, AutoUpdateNotifyAlways},
		"dnf5": {AutoUpdateNotifyOnFailure, AutoUpdateNotifyAlways},
	}
	for _, tc := range installedBackends() {
		t.Run(tc.name, func(t *testing.T) {
			state, err := tc.backend.Read(t.Context())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if !slices.Equal(state.Support.NotifyPolicies, want[tc.name]) {
				t.Fatalf("NotifyPolicies = %v, want %v", state.Support.NotifyPolicies, want[tc.name])
			}
		})
	}
}

// An older build offered on_change everywhere. Where only the service result
// is known it runs as on_failure, so it reads as that, and the options the
// page sends back are not refused.
func TestAnOlderOnChangeReadsAsWhatItRuns(t *testing.T) {
	for _, tc := range installedBackends() {
		t.Run(tc.name, func(t *testing.T) {
			tc.files[notifyDropInPath(tc.service)] = []byte("[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider x --policy on_change\n")
			want := AutoUpdateNotifyOnFailure
			if tc.name == "apt" {
				want = AutoUpdateNotifyOnChange
			}
			state := wantNotify(t, tc.backend, want)
			if err := tc.backend.Apply(t.Context(), state.Options); err != nil {
				t.Fatalf("Apply of the options read back: %v", err)
			}
		})
	}
}

// A host with no notify drop-in reports failures only: the run-record hook
// reports a failed update there. A "never" an older page sends still saves and
// installs no drop-in, and reads back as what it runs as.
func TestNoNotifyDropInReadsAsFailuresOnly(t *testing.T) {
	for _, tc := range installedBackends() {
		t.Run(tc.name, func(t *testing.T) {
			wantNotify(t, tc.backend, AutoUpdateNotifyOnFailure)

			options := AutoUpdateOptions{ExcludePackages: []string{}, Frequency: "daily", Notify: AutoUpdateNotifyNever, RebootPolicy: "never", Scope: "security"}
			if err := tc.backend.Apply(t.Context(), options); err != nil {
				t.Fatalf("Apply never: %v", err)
			}
			if _, ok := tc.files[notifyDropInPath(tc.service)]; ok {
				t.Fatal("never wrote the notify drop-in")
			}
			wantNotify(t, tc.backend, AutoUpdateNotifyOnFailure)

			options.Notify = AutoUpdateNotifyOnFailure
			if err := tc.backend.Apply(t.Context(), options); err != nil {
				t.Fatalf("Apply on_failure: %v", err)
			}
			if _, ok := tc.files[notifyDropInPath(tc.service)]; !ok {
				t.Fatal("on_failure wrote no notify drop-in")
			}
			wantNotify(t, tc.backend, AutoUpdateNotifyOnFailure)
		})
	}
}

// wantNotify reads backend's state and fails unless it holds notify.
func wantNotify(t *testing.T, backend UpdateBackend, notify AutoUpdateNotify) AutoUpdateState {
	t.Helper()
	state, err := backend.Read(t.Context())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if state.Options.Notify != notify {
		t.Fatalf("notify %q, want %q", state.Options.Notify, notify)
	}
	return state
}

func TestParsePlatform(t *testing.T) {
	platform := parsePlatform([]byte(`ID=linuxmint
ID_LIKE="ubuntu debian"
VERSION_ID="22.3"
`))
	if platform.ID != "linuxmint" || platform.VersionMajor != 22 {
		t.Fatalf("unexpected platform: %+v", platform)
	}
	if len(platform.IDLike) != 2 || platform.IDLike[0] != "ubuntu" || platform.IDLike[1] != "debian" {
		t.Fatalf("unexpected ID_LIKE: %v", platform.IDLike)
	}
}

func TestSelectBackendByExactDistribution(t *testing.T) {
	host := backendHost{fileExists: func(string) bool { return false }}
	tests := []struct {
		name     string
		platform hostPlatform
		want     AutoUpdateBackend
	}{
		{name: "ubuntu", platform: hostPlatform{ID: "ubuntu"}, want: backendAPT},
		{name: "debian", platform: hostPlatform{ID: "debian"}, want: backendAPT},
		{name: "mint before parent IDs", platform: hostPlatform{ID: "linuxmint", IDLike: []string{"ubuntu", "debian"}}, want: backendMint},
		{name: "fedora dnf5", platform: hostPlatform{ID: "fedora", VersionMajor: 41}, want: backendDNF5},
		{name: "rocky dnf4", platform: hostPlatform{ID: "rocky", VersionMajor: 9}, want: backendDNF4},
		{name: "alma dnf4", platform: hostPlatform{ID: "almalinux", VersionMajor: 9}, want: backendDNF4},
		{name: "rhel dnf4", platform: hostPlatform{ID: "rhel", VersionMajor: 9}, want: backendDNF4},
		{name: "rhel without provider artifact stays dnf4", platform: hostPlatform{ID: "rhel", VersionMajor: 10}, want: backendDNF4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := selectBackend(test.platform, host)
			if backend == nil || backend.Name() != test.want {
				t.Fatalf("backend=%v, want %q", backend, test.want)
			}
		})
	}
	if backend := selectBackend(hostPlatform{ID: "arch"}, host); backend != nil {
		t.Fatalf("unsupported distribution selected %q", backend.Name())
	}
}

func TestDNF5ArtifactTakesPrecedence(t *testing.T) {
	exists := func(path string) bool {
		return path == "/usr/lib/systemd/system/dnf5-automatic.timer" ||
			path == "/usr/lib/systemd/system/dnf-automatic.timer"
	}
	if !platformUsesDNF5(hostPlatform{ID: "fedora", VersionMajor: 40}, exists) {
		t.Fatal("DNF5 timer artifact did not take precedence")
	}
}

func TestValidateOptionsRejectsUnsupportedAndUnsafeValues(t *testing.T) {
	support := AutoUpdateOptionSupport{
		Frequencies:    []AutoUpdateFrequency{"daily"},
		Scopes:         []AutoUpdateScope{"security"},
		RebootPolicies: []AutoUpdateRebootPolicy{"never"},
	}
	base := AutoUpdateOptions{Frequency: "daily", Scope: "security", RebootPolicy: "never"}
	if err := validateOptions(base, support); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
	bad := base
	bad.DownloadOnly = true
	if err := validateOptions(bad, support); err == nil {
		t.Fatal("unsupported download-only mode accepted")
	}
	bad = base
	bad.ExcludePackages = []string{"safe\nAPT::Injected"}
	if err := validateOptions(bad, support); err == nil {
		t.Fatal("unsafe package pattern accepted")
	}
}
