package packages

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/capabilities"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const claudeCodeAptSource = "deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/latest latest main\n"

// withClaudeCodeInstall serves key as Anthropic's signing key, roots the
// repository files under a temporary directory laid out for family, and
// records the steps that follow. It returns that root and the step log.
func withClaudeCodeInstall(t *testing.T, family string, key http.HandlerFunc) (string, *[]string) {
	t.Helper()
	server := httptest.NewServer(key)
	root := t.TempDir()
	configDir := "etc/apt"
	if family == "rhel" {
		configDir = "etc/yum.repos.d"
	}
	if err := os.MkdirAll(filepath.Join(root, configDir), 0o755); err != nil {
		t.Fatal(err)
	}

	originalURL, originalRoot := claudeCodeKeyURL, claudeCodeRoot
	originalFamily := capabilityDistroFamily
	originalRefresh := capabilityRefreshPackageCache
	originalInstall := capabilityInstallPackage
	originalDetect := capabilityDetectWithRetry
	originalLookPath, originalExec := capabilityCommandLookPath, capabilityCommandExec
	t.Cleanup(func() {
		server.Close()
		claudeCodeKeyURL, claudeCodeRoot = originalURL, originalRoot
		capabilityDistroFamily = originalFamily
		capabilityRefreshPackageCache = originalRefresh
		capabilityInstallPackage = originalInstall
		capabilityDetectWithRetry = originalDetect
		capabilityCommandLookPath, capabilityCommandExec = originalLookPath, originalExec
	})

	var order []string
	claudeCodeKeyURL, claudeCodeRoot = server.URL, root
	capabilityDistroFamily = func() string { return family }
	capabilityRefreshPackageCache = func(context.Context) error {
		order = append(order, "refresh")
		return nil
	}
	capabilityInstallPackage = func(_ context.Context, name string, _ pkgUpdateReporter) error {
		order = append(order, "install:"+name)
		return nil
	}
	capabilityDetectWithRetry = func(_ context.Context, spec capabilities.CapabilitySpec, _ session.User, _ time.Duration) (bool, string) {
		order = append(order, "detect:"+spec.Name)
		return true, ""
	}
	capabilityCommandLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	capabilityCommandExec = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		order = append(order, "run:"+name+" "+strings.Join(args, " "))
		return exec.CommandContext(ctx, "true")
	}
	return root, &order
}

func anthropicKey(t *testing.T) []byte {
	t.Helper()
	key, err := os.ReadFile("testdata/claude-code.asc")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func serveBytes(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}
}

func readRooted(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInstallCapabilityAddsAnthropicsAptRepositoryThenInstallsClaudeCode(t *testing.T) {
	key := anthropicKey(t)
	root, order := withClaudeCodeInstall(t, "debian", serveBytes(key))

	result, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "claude_code")
	if err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if !result.Available {
		t.Fatal("result.Available = false, want true")
	}
	if got := readRooted(t, root, "etc/apt/keyrings/claude-code.asc"); got != string(key) {
		t.Fatal("the stored signing key differs from the one served")
	}
	if got := readRooted(t, root, "etc/apt/sources.list.d/claude-code.list"); got != claudeCodeAptSource {
		t.Fatalf("apt source = %q, want %q", got, claudeCodeAptSource)
	}
	for _, path := range []string{"etc/apt/keyrings/claude-code.asc", "etc/apt/sources.list.d/claude-code.list"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("%s mode = %v, want 0644 so apt can read it", path, info.Mode().Perm())
		}
	}
	// The package only resolves once the cache has seen the new repository.
	want := []string{"refresh", "install:claude-code", "detect:claude_code"}
	if !slices.Equal(*order, want) {
		t.Fatalf("steps = %v, want %v", *order, want)
	}
}

func TestInstallCapabilityAddsAnthropicsRPMRepository(t *testing.T) {
	key := anthropicKey(t)
	root, order := withClaudeCodeInstall(t, "rhel", serveBytes(key))

	if _, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "claude_code"); err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if got := readRooted(t, root, "etc/pki/rpm-gpg/RPM-GPG-KEY-claude-code"); got != string(key) {
		t.Fatal("the stored signing key differs from the one served")
	}
	wantRepo := "[claude-code]\n" +
		"name=Claude Code\n" +
		"baseurl=https://downloads.claude.ai/claude-code/rpm/latest\n" +
		"enabled=1\n" +
		"gpgcheck=1\n" +
		"gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-claude-code\n"
	if got := readRooted(t, root, "etc/yum.repos.d/claude-code.repo"); got != wantRepo {
		t.Fatalf("repo file = %q, want %q", got, wantRepo)
	}
	// rpm must trust the verified key before dnf sees the repository, so no
	// prompt asks anyone to confirm a fingerprint.
	want := []string{
		"run:/usr/bin/rpm --import " + filepath.Join(root, "etc/pki/rpm-gpg/RPM-GPG-KEY-claude-code"),
		"refresh", "install:claude-code", "detect:claude_code",
	}
	if !slices.Equal(*order, want) {
		t.Fatalf("steps = %v, want %v", *order, want)
	}
}

func TestInstallCapabilityRejectsAnUnexpectedClaudeCodeSigningKey(t *testing.T) {
	anthropic := anthropicKey(t)
	impostor := armor(oldFormatPacket(6, fakePublicKeyBody(7)))
	tests := []struct {
		name string
		key  []byte
		want string
	}{
		{name: "another key", key: impostor, want: "signing key fingerprint is"},
		{name: "an extra key beside Anthropic's", key: append(slices.Clone(anthropic), impostor...), want: "unexpected data after the OpenPGP public key block"},
		{name: "not a key", key: []byte("<html>blocked</html>"), want: "no OpenPGP public key block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, order := withClaudeCodeInstall(t, "debian", serveBytes(tt.key))

			_, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "claude_code")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one containing %q", err, tt.want)
			}
			for _, path := range []string{"etc/apt/keyrings/claude-code.asc", "etc/apt/sources.list.d/claude-code.list"} {
				if _, statErr := os.Stat(filepath.Join(root, path)); !os.IsNotExist(statErr) {
					t.Fatalf("%s must not be written for an untrusted key (stat: %v)", path, statErr)
				}
			}
			if len(*order) != 0 {
				t.Fatalf("no step may follow an untrusted key, got %v", *order)
			}
		})
	}
}

func TestInstallCapabilityKeepsAnExistingClaudeCodeRepository(t *testing.T) {
	root, order := withClaudeCodeInstall(t, "debian", func(http.ResponseWriter, *http.Request) {
		t.Error("the signing key must not be downloaded when a repository is already configured")
	})
	existing := "deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/stable stable main\n"
	source := filepath.Join(root, "etc/apt/sources.list.d/claude-code.list")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "claude_code"); err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if got := readRooted(t, root, "etc/apt/sources.list.d/claude-code.list"); got != existing {
		t.Fatalf("apt source = %q, want the administrator's %q left alone", got, existing)
	}
	want := []string{"refresh", "install:claude-code", "detect:claude_code"}
	if !slices.Equal(*order, want) {
		t.Fatalf("steps = %v, want %v", *order, want)
	}
}

func TestInstallCapabilityRefusesClaudeCodeWithoutAKnownPackageManager(t *testing.T) {
	root, order := withClaudeCodeInstall(t, "debian", serveBytes(anthropicKey(t)))
	if err := os.RemoveAll(filepath.Join(root, "etc/apt")); err != nil {
		t.Fatal(err)
	}

	_, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "claude_code")
	if err == nil || !strings.Contains(err.Error(), "no Claude Code package repository") {
		t.Fatalf("error = %v, want a refusal for a host without apt or dnf", err)
	}
	if len(*order) != 0 {
		t.Fatalf("no step may run, got %v", *order)
	}
}
