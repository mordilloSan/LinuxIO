package packages

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/assistant"
	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// nodeComponent is the InstallSpec.OptionalComponent value that installs nvm
// and Node.js LTS into the session user's home directory.
const nodeComponent = "node"

var (
	nvmInstallScriptURL   = "https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.8/install.sh"
	capabilityUserCommand = assistant.UserCommand
)

// nvmInstallNodeScript loads the freshly installed nvm and installs the
// current LTS as the default, so a new login shell resolves npx.
const nvmInstallNodeScript = `. "$HOME/.nvm/nvm.sh" && nvm install --lts && nvm alias default 'lts/*'`

// installNodeForUser runs nvm's install script and then `nvm install --lts`
// as the session user, never as root. nvm adds itself to the user's
// ~/.bashrc, which is where the node capability's login-shell probe and the
// Assistant's agent spawn both pick it up.
func installNodeForUser(ctx context.Context, task *bridgetask.Task, user session.User) error {
	if user.Username == "" {
		return fmt.Errorf("no session user to install Node.js for")
	}

	reportProgress(task, stageResolve, fmt.Sprintf("Downloading nvm install script from %s", nvmInstallScriptURL), pctResolve)
	script, err := fetchInstallScript(ctx, nvmInstallScriptURL)
	if err != nil {
		return fmt.Errorf("download nvm install script: %w", err)
	}
	reportProgress(task, stageResolve, fmt.Sprintf("Downloaded nvm install script (%d bytes, sha256 %x)", len(script), sha256.Sum256(script)), pctResolve)

	steps := []struct {
		message string
		args    []string
		stdin   []byte
		pct     uint32
	}{
		{message: "Installing nvm for " + user.Username, args: []string{"-s"}, stdin: script, pct: pctInstallStart},
		{message: "Installing Node.js LTS for " + user.Username, args: []string{"-c", nvmInstallNodeScript}, pct: (pctInstallStart + pctInstallEnd) / 2},
	}
	for _, step := range steps {
		cmd, err := capabilityUserCommand(ctx, user, "bash", step.args...)
		if err != nil {
			return err
		}
		// nvm picks the profile file to edit from SHELL; bash's ~/.bashrc is
		// the one the login-shell probe reads.
		cmd.Env = append(cmd.Env, "SHELL="+cmd.Path)
		if step.stdin != nil {
			cmd.Stdin = bytes.NewReader(step.stdin)
		}
		reportProgress(task, stageInstallPackage, step.message, step.pct)
		slog.Info("Installing Node.js for the session user.", "user", user.Username, "step", step.message)
		if err := runCapabilityProcess(ctx, cmd, "bash", func(output InstallCapabilityOutput) {
			reportOutput(task, stageInstallPackage, step.message, step.pct, output)
		}); err != nil {
			return fmt.Errorf("%s: %w", step.message, err)
		}
	}
	reportProgress(task, stageInstallPackage, "Node.js installed", pctInstallEnd)
	return nil
}
