package packages

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/capabilities"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents/acpagentstest"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// fakeNPM stands in for npm: it logs its arguments and lays out the requested
// package under --prefix the way `npm install` does. NPM_FAIL makes it fail
// after writing a partial tree.
const fakeNPM = `#!/bin/sh
echo "npm $*" >> "$HOME/calls"
prefix=""
spec=""
while [ $# -gt 0 ]; do
	case "$1" in
	--prefix) prefix=$2; shift ;;
	install | -*) ;;
	*) spec=$1 ;;
	esac
	shift
done
package=${spec%@*}
mkdir -p "$prefix/node_modules/$package" "$prefix/node_modules/.bin"
if [ -n "${NPM_FAIL:-}" ]; then
	echo "npm error network unreachable" >&2
	exit 1
fi
printf '{"version":"%s"}' "${spec##*@}" > "$prefix/node_modules/$package/package.json"
printf '#!/bin/sh\n' > "$prefix/node_modules/.bin/claude-agent-acp"
chmod +x "$prefix/node_modules/.bin/claude-agent-acp"
`

// withAssistantInstall gives the session user a temporary home and a PATH
// whose npm is fakeNPM, and makes detection read that home. It returns the
// home and the users the install commands ran as.
func withAssistantInstall(t *testing.T, env ...string) (string, *[]string) {
	t.Helper()
	home := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(fakeNPM), 0o755); err != nil {
		t.Fatal(err)
	}

	originalUserCommand := capabilityUserCommand
	originalUserPath := capabilityUserPath
	originalUserHome := capabilityUserHome
	originalDetect := capabilityDetectWithRetry
	t.Cleanup(func() {
		capabilityUserCommand = originalUserCommand
		capabilityUserPath = originalUserPath
		capabilityUserHome = originalUserHome
		capabilityDetectWithRetry = originalDetect
	})

	var commandUsers []string
	capabilityUserCommand = func(ctx context.Context, u session.User, name string, args ...string) (*exec.Cmd, error) {
		commandUsers = append(commandUsers, u.Username)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Env = append(cmd.Env, env...)
		return cmd, nil
	}
	capabilityUserPath = func(context.Context, session.User) (string, error) {
		return bin + ":" + os.Getenv("PATH"), nil
	}
	capabilityUserHome = func(string) (string, error) { return home, nil }
	capabilityDetectWithRetry = func(_ context.Context, spec capabilities.CapabilitySpec, _ session.User, _ time.Duration) (bool, string) {
		id := strings.TrimPrefix(spec.Name, capabilities.AssistantAgentPrefix)
		if err := acpagents.Installed(id, home); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
	return home, &commandUsers
}

func claudeAgent(t *testing.T) acpagents.Agent {
	t.Helper()
	agent, ok := acpagents.Lookup("claude")
	if !ok {
		t.Fatal("claude agent missing from the catalogue")
	}
	return agent
}

func TestInstallCapabilityInstallsThePinnedAssistantAgentAsTheSessionUser(t *testing.T) {
	home, commandUsers := withAssistantInstall(t)
	agent := claudeAgent(t)

	result, err := installCapability(context.Background(), nil, session.User{Username: "alice", UID: 1000, GID: 1000}, "assistant_claude")
	if err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if !result.Available {
		t.Fatalf("result.Available = false (%v), want true", result.Error)
	}
	if strings.Join(*commandUsers, ",") != "alice" {
		t.Fatalf("commands ran as %v, want one command as alice", *commandUsers)
	}
	dir := acpagents.Dir(home, "claude")
	calls, err := os.ReadFile(filepath.Join(home, "calls"))
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	// The exact version, never a tag: nothing else can be installed, and the
	// adapter's own copy of Claude Code is left out.
	want := "npm install --prefix " + dir + ".new --no-audit --no-fund --no-progress --omit=optional " + agent.Spec()
	if got := strings.TrimSpace(string(calls)); got != want {
		t.Fatalf("npm ran as %q, want %q", got, want)
	}
	if _, err := os.Stat(dir + ".new"); !os.IsNotExist(err) {
		t.Fatalf("the staging directory must be gone after a successful install (stat: %v)", err)
	}
}

func TestInstallCapabilityReplacesAnOlderAssistantAgent(t *testing.T) {
	home, _ := withAssistantInstall(t)
	agent := claudeAgent(t)
	acpagentstest.Install(t, home, "claude", agent, "0.0.1", "#!/bin/sh\n")
	leftover := filepath.Join(acpagents.Dir(home, "claude"), "node_modules", "left-from-the-old-version")
	if err := os.WriteFile(leftover, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "assistant_claude")
	if err != nil {
		t.Fatalf("installCapability: %v", err)
	}
	if !result.Available {
		t.Fatalf("result.Available = false (%v), want the pinned version installed", result.Error)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("the old install must be replaced, not merged into (stat: %v)", err)
	}
}

func TestInstallCapabilityKeepsTheWorkingAssistantAgentWhenNPMFails(t *testing.T) {
	home, _ := withAssistantInstall(t, "NPM_FAIL=1")
	agent := claudeAgent(t)
	acpagentstest.Install(t, home, "claude", agent, agent.Version, "#!/bin/sh\n")

	_, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "assistant_claude")
	if err == nil || !strings.Contains(err.Error(), "network unreachable") {
		t.Fatalf("error = %v, want npm's own message", err)
	}
	if err := acpagents.Installed("claude", home); err != nil {
		t.Fatalf("a failed install must leave the working copy in place: %v", err)
	}
}

func TestInstallCapabilityRejectsAnAssistantAgentWithoutNPM(t *testing.T) {
	withAssistantInstall(t)
	capabilityUserPath = func(context.Context, session.User) (string, error) { return t.TempDir(), nil }

	_, err := installCapability(context.Background(), nil, session.User{Username: "alice"}, "assistant_claude")
	if err == nil || !strings.Contains(err.Error(), "npm not found on alice's login-shell PATH") {
		t.Fatalf("error = %v, want a pointer to the missing npm", err)
	}
}

func TestInstallCapabilityRejectsAnAssistantAgentWithoutASessionUser(t *testing.T) {
	if _, err := installCapability(context.Background(), nil, session.User{}, "assistant_claude"); err == nil {
		t.Fatal("installCapability succeeded without a session user")
	}
}
