package capabilities

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents/acpagentstest"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// TestCapabilityRegistryEntriesAreValid checks what the response and the
// generated contract rely on: every entry has a unique, non-empty name, which
// keys its status on the wire, and exactly one detector.
func TestCapabilityRegistryEntriesAreValid(t *testing.T) {
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
}

// TestCapabilityNamesArePublished checks that the generated CapabilityName
// union is the registry's names, in registry order.
func TestCapabilityNamesArePublished(t *testing.T) {
	want := make([]string, 0, len(capabilityRegistry))
	for _, spec := range capabilityRegistry {
		want = append(want, spec.Name)
	}
	if got := apischema.StringEnums["CapabilityName"]; !slices.Equal(got, want) {
		t.Fatalf("StringEnums[CapabilityName] = %q, want the registry names %q", got, want)
	}
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

func TestClaudeCodeCapabilityInstallsAnthropicsPackage(t *testing.T) {
	spec, ok := CapabilitySpecByName("claude_code")
	if !ok {
		t.Fatal("claude_code capability not registered")
	}
	if spec.Install == nil || spec.Install.OptionalComponent != "claude_code" ||
		spec.Install.PackageDebian != "claude-code" || spec.Install.PackageRHEL != "claude-code" {
		t.Fatalf("claude_code must add Anthropic's repository and install claude-code, got %+v", spec.Install)
	}
	if spec.Detect != nil || spec.DetectAsUser == nil {
		t.Fatal("claude_code must detect through the session user's login shell, like the agent that runs it")
	}
	available, message := spec.DetectAsUser(context.Background(), session.User{Username: "linuxio-no-such-user"})
	if available || message == "" {
		t.Fatalf("expected unavailable with a reason for an unknown user, got available=%v message=%q", available, message)
	}
}

func TestAssistantAgentCapabilitiesCoverTheCatalogue(t *testing.T) {
	for _, id := range acpagents.IDs() {
		name := AssistantAgentPrefix + id
		spec, ok := CapabilitySpecByName(name)
		if !ok {
			t.Errorf("agent %q has no %s capability", id, name)
			continue
		}
		if spec.Install == nil || spec.Install.OptionalComponent != name ||
			spec.Install.PackageDebian != "" || spec.Install.PackageRHEL != "" {
			t.Errorf("%s must install only its own component, got %+v", name, spec.Install)
		}
		if spec.Detect != nil || spec.DetectAsUser == nil {
			t.Errorf("%s must detect the session user's own install", name)
		}
	}
}

func TestAssistantAgentCapabilityReportsTheInstalledVersion(t *testing.T) {
	agent, ok := acpagents.Lookup("claude")
	if !ok {
		t.Fatal("claude agent missing from the catalogue")
	}
	spec, ok := CapabilitySpecByName("assistant_claude")
	if !ok {
		t.Fatal("assistant_claude capability not registered")
	}
	home := t.TempDir()
	orig := agentHome
	agentHome = func(string) (string, error) { return home, nil }
	t.Cleanup(func() { agentHome = orig })
	user := session.User{Username: "alice"}

	if available, message := spec.DetectAsUser(context.Background(), user); available || !strings.Contains(message, "is not installed") {
		t.Fatalf("nothing installed: got available=%v message=%q", available, message)
	}

	acpagentstest.Install(t, home, "claude", agent, "0.0.1", "#!/bin/sh\n")
	if available, message := spec.DetectAsUser(context.Background(), user); available || !strings.Contains(message, "0.0.1 is installed, LinuxIO needs "+agent.Version) {
		t.Fatalf("other version installed: got available=%v message=%q", available, message)
	}

	acpagentstest.Install(t, home, "claude", agent, agent.Version, "#!/bin/sh\n")
	if available, message := spec.DetectAsUser(context.Background(), user); !available || message != "" {
		t.Fatalf("pinned version installed: got available=%v message=%q", available, message)
	}
}
