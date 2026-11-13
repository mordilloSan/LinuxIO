package packages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/capabilities"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

var (
	capabilityUserPath = usercmd.Path
	capabilityUserHome = userconfig.Homedir
)

// installAssistantAgentScript replaces an agent's install directory with a
// fresh `npm install` of one exact package version. The directory and npm's
// arguments arrive as positional parameters; nothing is interpolated. The new
// copy is built beside the old one and swapped in only once npm succeeded, so
// a failed or interrupted install leaves the working copy alone.
const installAssistantAgentScript = `set -eu
dir=$1
shift
staging="$dir.new"
rm -rf -- "$staging"
mkdir -p -- "$staging"
npm install --prefix "$staging" --no-audit --no-fund --no-progress "$@"
rm -rf -- "$dir"
mv -- "$staging" "$dir"
`

// assistantAgentID returns the acpagents id an "assistant_<agent>" optional
// component names.
func assistantAgentID(component string) (string, bool) {
	return strings.CutPrefix(component, capabilities.AssistantAgentPrefix)
}

// installAssistantAgent installs the pinned version of one assistant agent
// into the session user's home, as that user and never as root, with the npm
// on that user's login-shell PATH. This is the only place an agent is ever
// downloaded: starting a chat runs what is installed and nothing else.
func installAssistantAgent(ctx context.Context, task *bridgeipc.Task, user session.User, id string) error {
	if user.Username == "" {
		return fmt.Errorf("no session user to install the assistant agent for")
	}
	agent, ok := acpagents.Lookup(id)
	if !ok {
		return fmt.Errorf("unknown assistant agent %q", id)
	}
	userPath, err := capabilityUserPath(ctx, user)
	if err != nil {
		return err
	}
	if _, lookErr := usercmd.LookPathIn(userPath, "npm"); lookErr != nil {
		return fmt.Errorf("npm not found on %s's login-shell PATH; install Node.js first", user.Username)
	}
	home, err := capabilityUserHome(user.Username)
	if err != nil {
		return err
	}

	args := []string{"-c", installAssistantAgentScript, "sh", acpagents.Dir(home, id)}
	args = append(args, agent.InstallArgs...)
	args = append(args, agent.Spec())
	cmd, err := capabilityUserCommand(ctx, user, "sh", args...)
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, "PATH="+userPath)

	message := fmt.Sprintf("Installing %s for %s", agent.Spec(), user.Username)
	reportProgress(task, stageInstallPackage, message, pctInstallStart)
	slog.Debug("installing assistant agent for the session user", "agent", id, "package", agent.Spec(), "user", user.Username)
	if err := runCapabilityProcess(ctx, cmd, "npm", func(output InstallCapabilityOutput) {
		reportOutput(task, stageInstallPackage, message, pctInstallStart, output)
	}); err != nil {
		return fmt.Errorf("install %s: %w", agent.Spec(), err)
	}
	reportProgress(task, stageInstallPackage, fmt.Sprintf("Installed %s", agent.Spec()), pctInstallEnd)
	return nil
}
