package capabilities

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// TestCapabilityRegistryCoversWireFields ensures every *_available field on
// session.CapabilitiesAvailable (embedded in the wire response) has a matching
// capabilityRegistry entry (and vice versa).
// Without this check, adding a wire field but forgetting the registry entry
// would silently leave the field at its zero value, and adding a registry entry
// without the matching wire field would panic at runtime via setCapabilityField.
func TestCapabilityRegistryCoversWireFields(t *testing.T) {
	wireNames := wireAvailableFields(t)

	registryNames := make(map[string]bool, len(capabilityRegistry))
	for _, spec := range capabilityRegistry {
		if spec.Name == "" {
			t.Fatalf("registry entry with empty Name: %+v", spec)
		}
		if registryNames[spec.Name] {
			t.Fatalf("duplicate registry entry %q", spec.Name)
		}
		if spec.Detect == nil && spec.DetectAsUser == nil {
			t.Errorf("capability %q has neither Detect nor DetectAsUser", spec.Name)
		}
		if spec.Detect != nil && spec.DetectAsUser != nil {
			t.Errorf("capability %q sets both Detect and DetectAsUser", spec.Name)
		}
		registryNames[spec.Name] = true
	}

	for name := range wireNames {
		if !registryNames[name] {
			t.Errorf("wire field %q_available has no registry entry", name)
		}
	}
	for name := range registryNames {
		if !wireNames[name] {
			t.Errorf("registry entry %q has no matching wire field %q_available", name, name)
		}
	}
}

// wireAvailableFields returns the set of wire prefixes derived from JSON tags
// shaped `<prefix>_available` on session.CapabilitiesAvailable.
func wireAvailableFields(t *testing.T) map[string]bool {
	t.Helper()
	return availableTagsOf(reflect.TypeFor[session.CapabilitiesAvailable]())
}

// availableTagsOf returns the set of wire prefixes derived from JSON tags shaped
// `<prefix>_available` on the given struct type.
func availableTagsOf(typ reflect.Type) map[string]bool {
	names := make(map[string]bool)
	for field := range typ.Fields() {
		tag := field.Tag.Get("json")
		tag, _, _ = strings.Cut(tag, ",")
		prefix, ok := strings.CutSuffix(tag, "_available")
		if !ok {
			continue
		}
		names[prefix] = true
	}
	return names
}

func TestMonitoringCapabilityIsNotInstallable(t *testing.T) {
	spec, ok := CapabilitySpecByName("monitoring")
	if !ok {
		t.Fatal("monitoring capability not registered")
	}
	if spec.Install != nil {
		t.Fatalf("install spec = %#v, want nil: the daemon ships with LinuxIO", spec.Install)
	}
}

func TestCapabilityInstallPackageSelection(t *testing.T) {
	tests := []struct {
		name             string
		wantDebian       string
		wantRHEL         string
		wantOptionalRHEL string
		wantWarning      string
	}{
		{name: "lm_sensors", wantDebian: "lm-sensors", wantRHEL: "lm_sensors"},
		{name: "rsync", wantDebian: "rsync", wantRHEL: "rsync"},
		{name: "e2fsprogs", wantDebian: "e2fsprogs", wantRHEL: "e2fsprogs"},
		{name: "xfsprogs", wantDebian: "xfsprogs", wantRHEL: "xfsprogs"},
		{
			name:             "avahi",
			wantDebian:       "avahi-daemon libnss-mdns",
			wantRHEL:         "avahi",
			wantOptionalRHEL: "nss-mdns",
			wantWarning:      "nss-mdns was not installed. Avahi is running, but this host may need EPEL for .local name resolution.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, ok := CapabilitySpecByName(test.name)
			if !ok || spec.Install == nil {
				t.Fatalf("capability %q is not installable", test.name)
			}
			if got := spec.Install.PackageDebian; got != test.wantDebian {
				t.Errorf("debian package = %q, want %q", got, test.wantDebian)
			}
			if got := spec.Install.PackageRHEL; got != test.wantRHEL {
				t.Errorf("rhel package = %q, want %q", got, test.wantRHEL)
			}
			if got := spec.Install.OptionalPackageRHEL; got != test.wantOptionalRHEL {
				t.Errorf("optional rhel package = %q, want %q", got, test.wantOptionalRHEL)
			}
			if got := spec.Install.OptionalPackageRHELFailureWarning; got != test.wantWarning {
				t.Errorf("optional rhel package warning = %q, want %q", got, test.wantWarning)
			}
		})
	}
}

func TestRsyncCapabilityDetection(t *testing.T) {
	spec, ok := CapabilitySpecByName("rsync")
	if !ok {
		t.Fatal("rsync capability is not registered")
	}
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	if available, message := spec.Detect(context.Background()); available || !strings.Contains(message, "rsync not found") {
		t.Fatalf("missing rsync: available=%v, message=%q", available, message)
	}
	if err := os.WriteFile(filepath.Join(binDir, "rsync"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if available, message := spec.Detect(context.Background()); !available || message != "" {
		t.Fatalf("installed rsync: available=%v, message=%q", available, message)
	}
}

func TestNodeCapabilityProbesTheSessionUsersLoginShell(t *testing.T) {
	spec, ok := CapabilitySpecByName("node")
	if !ok {
		t.Fatal("node capability is not registered")
	}
	if spec.Install == nil || spec.Install.OptionalComponent != "node" || spec.Install.PackageDebian != "" || spec.Install.PackageRHEL != "" {
		t.Fatalf("install spec = %#v, want only the per-user nvm component", spec.Install)
	}
	if spec.Detect != nil || spec.DetectAsUser == nil {
		t.Fatal("node must detect through the session user's login shell, not the bridge PATH")
	}
	// A user that does not exist has no login shell to probe; the message must
	// say why rather than silently reporting "available".
	available, message := spec.DetectAsUser(context.Background(), session.User{Username: "linuxio-no-such-user"})
	if available || message == "" {
		t.Fatalf("unknown user: available=%v, message=%q", available, message)
	}
}

func TestLMSensorsInstallPostInstallCommand(t *testing.T) {
	spec, ok := CapabilitySpecByName("lm_sensors")
	if !ok || spec.Install == nil || spec.Install.PostInstall == nil {
		t.Fatal("lm_sensors post-install command is not registered")
	}
	if spec.Install.PostInstall.Name != "sensors-detect" {
		t.Fatalf("post-install command = %q, want sensors-detect", spec.Install.PostInstall.Name)
	}
	if got, want := spec.Install.PostInstall.Args, []string{"--auto"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("post-install args = %v, want %v", got, want)
	}
}
