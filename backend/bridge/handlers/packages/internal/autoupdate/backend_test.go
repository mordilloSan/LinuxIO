package autoupdate

import (
	"testing"
)

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
