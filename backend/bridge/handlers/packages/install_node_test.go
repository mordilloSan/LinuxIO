package packages

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// fakeNVMScript stands in for nvm's install.sh: it records the profile shell
// and installs an nvm.sh whose nvm function logs its arguments.
const fakeNVMScript = `mkdir -p "$HOME/.nvm"
echo "shell=$SHELL" > "$HOME/calls"
echo 'nvm() { echo "nvm $*" >> "$HOME/calls"; }' > "$HOME/.nvm/nvm.sh"
`

func TestInstallCapabilityInstallsNodeWithNVMAsTheSessionUser(t *testing.T) {
	home := t.TempDir()
	server := httptest.NewServer(serveScript(fakeNVMScript))
	originalURL := nvmInstallScriptURL
	originalUserCommand := capabilityUserCommand
	originalDetect := capabilityDetectWithRetry
	t.Cleanup(func() {
		server.Close()
		nvmInstallScriptURL = originalURL
		capabilityUserCommand = originalUserCommand
		capabilityDetectWithRetry = originalDetect
	})

	user := session.User{Username: "alice", UID: 1000, GID: 1000}
	nvmInstallScriptURL = server.URL
	var commandUsers []string
	capabilityUserCommand = func(ctx context.Context, u session.User, name string, args ...string) (*exec.Cmd, error) {
		commandUsers = append(commandUsers, u.Username)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "HOME="+home)
		return cmd, nil
	}
	var detectedFor string
	capabilityDetectWithRetry = func(_ context.Context, spec system.CapabilitySpec, u session.User, _ time.Duration) (bool, string) {
		detectedFor = spec.Name + ":" + u.Username
		return true, ""
	}

	result, err := installCapability(context.Background(), nil, user, "node")
	if err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if !result.Available {
		t.Fatal("result.Available = false, want true")
	}
	if strings.Join(commandUsers, ",") != "alice,alice" {
		t.Fatalf("commands ran as %v, want both steps as alice", commandUsers)
	}
	if detectedFor != "node:alice" {
		t.Fatalf("detected %q, want node as alice", detectedFor)
	}
	calls, err := os.ReadFile(filepath.Join(home, "calls"))
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "/bash") || lines[1] != "nvm install --lts" || lines[2] != "nvm alias default lts/*" {
		t.Fatalf("calls = %q, want SHELL=bash, nvm install --lts, nvm alias default lts/*", lines)
	}
}

func TestInstallCapabilityRejectsNodeWithoutASessionUser(t *testing.T) {
	if _, err := installCapability(context.Background(), nil, session.User{}, "node"); err == nil {
		t.Fatal("installCapability succeeded without a session user")
	}
}
