package acpagents_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents/acpagentstest"
)

const noopScript = "#!/bin/sh\n"

func TestLookupKnowsTheThreeAgents(t *testing.T) {
	for _, id := range []string{"claude", "gemini", "codex"} {
		agent, ok := acpagents.Lookup(id)
		require.True(t, ok, id)
		require.NotEmpty(t, agent.Package, id)
		require.Regexp(t, `^\d+\.\d+\.\d+$`, agent.Version, "%s must pin an exact version", id)
		require.NotEmpty(t, agent.Bin, id)
		require.Equal(t, agent.Package+"@"+agent.Version, agent.Spec(), id)
	}
	_, ok := acpagents.Lookup("opencode")
	require.False(t, ok)
	require.ElementsMatch(t, []string{"claude", "codex", "gemini"}, acpagents.IDs())
}

func TestClaudeUsesTheInstalledClaudeCode(t *testing.T) {
	agent, ok := acpagents.Lookup("claude")
	require.True(t, ok)
	require.Equal(t, "claude", agent.CLI)
	require.Equal(t, "CLAUDE_CODE_EXECUTABLE", agent.CLIEnv)
	// Its own copy of Claude Code is an optional dependency; leaving it out is
	// what keeps the install small.
	require.Contains(t, agent.InstallArgs, "--omit=optional")
}

func TestOverrideRestores(t *testing.T) {
	restore := acpagents.Override("fake", acpagents.Agent{Package: "fake", Version: "1.0.0", Bin: "fake"})
	_, ok := acpagents.Lookup("fake")
	require.True(t, ok)
	restore()
	_, ok = acpagents.Lookup("fake")
	require.False(t, ok)
}

func TestDirIsPerUserAndPerAgent(t *testing.T) {
	require.Equal(t, "/home/ana/.local/share/linuxio/agents/claude", acpagents.Dir("/home/ana", "claude"))
}

func TestInstalled(t *testing.T) {
	agent := acpagents.Agent{Package: "@scope/fake-agent", Version: "1.2.3", Bin: "fake-agent"}
	t.Cleanup(acpagents.Override("fake", agent))

	t.Run("unknown agent", func(t *testing.T) {
		require.ErrorContains(t, acpagents.Installed("nope", t.TempDir()), `unknown agent "nope"`)
	})
	t.Run("nothing installed", func(t *testing.T) {
		require.ErrorContains(t, acpagents.Installed("fake", t.TempDir()), "@scope/fake-agent is not installed")
	})
	t.Run("another version installed", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.2.2", noopScript)
		require.ErrorContains(t, acpagents.Installed("fake", home), "@scope/fake-agent 1.2.2 is installed, LinuxIO needs 1.2.3")
	})
	t.Run("unreadable manifest", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.2.3", noopScript)
		manifest := filepath.Join(acpagents.Dir(home, "fake"), "node_modules", "@scope", "fake-agent", "package.json")
		require.NoError(t, os.WriteFile(manifest, []byte("not json"), 0o644))
		require.ErrorContains(t, acpagents.Installed("fake", home), "@scope/fake-agent is not installed")
	})
	t.Run("pinned version installed", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.2.3", noopScript)
		require.NoError(t, acpagents.Installed("fake", home))
	})
}

func TestLaunch(t *testing.T) {
	t.Run("self-contained agent runs its installed bin", func(t *testing.T) {
		agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent", Args: []string{"--acp"}}
		t.Cleanup(acpagents.Override("fake", agent))
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)

		command, err := acpagents.Launch("fake", home, "/usr/bin")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(acpagents.Dir(home, "fake"), "node_modules", ".bin", "fake-agent"), command.Path)
		require.Equal(t, []string{"--acp"}, command.Args)
		require.Empty(t, command.Env)
	})

	t.Run("refuses an agent that is not installed", func(t *testing.T) {
		t.Cleanup(acpagents.Override("fake", acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent"}))
		_, err := acpagents.Launch("fake", t.TempDir(), "/usr/bin")
		require.ErrorContains(t, err, "fake-agent is not installed")
	})

	t.Run("points the agent at the CLI on the user's PATH", func(t *testing.T) {
		agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent", CLI: "fake-cli", CLIEnv: "FAKE_CLI_EXECUTABLE"}
		t.Cleanup(acpagents.Override("fake", agent))
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)
		bin := t.TempDir()
		cli := filepath.Join(bin, "fake-cli")
		require.NoError(t, os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755))

		command, err := acpagents.Launch("fake", home, "/nonexistent:"+bin)
		require.NoError(t, err)
		require.Equal(t, []string{"FAKE_CLI_EXECUTABLE=" + cli}, command.Env)
	})

	t.Run("refuses when the CLI is missing", func(t *testing.T) {
		agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent", CLI: "fake-cli", CLIEnv: "FAKE_CLI_EXECUTABLE"}
		t.Cleanup(acpagents.Override("fake", agent))
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)

		_, err := acpagents.Launch("fake", home, t.TempDir())
		require.ErrorContains(t, err, "fake-cli not found on PATH")
	})

	t.Run("refuses an install whose bin is gone", func(t *testing.T) {
		agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent"}
		t.Cleanup(acpagents.Override("fake", agent))
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)
		require.NoError(t, os.Remove(filepath.Join(acpagents.Dir(home, "fake"), "node_modules", ".bin", "fake-agent")))

		_, err := acpagents.Launch("fake", home, "/usr/bin")
		require.Error(t, err)
	})
}

func TestInstalledDoesNotTrustWhatTheAgentDirectoryHolds(t *testing.T) {
	agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent"}
	t.Cleanup(acpagents.Override("fake", agent))

	t.Run("a package symlinked from outside the agent directory", func(t *testing.T) {
		home, elsewhere := t.TempDir(), t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "package.json"), []byte(`{"version":"1.0.0"}`), 0o644))
		modules := filepath.Join(acpagents.Dir(home, "fake"), "node_modules")
		require.NoError(t, os.MkdirAll(modules, 0o755))
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(modules, "fake-agent")))

		require.ErrorContains(t, acpagents.Installed("fake", home), "fake-agent is not installed")
	})

	t.Run("a manifest that is not a regular file", func(t *testing.T) {
		home := t.TempDir()
		pkg := filepath.Join(acpagents.Dir(home, "fake"), "node_modules", "fake-agent")
		require.NoError(t, os.MkdirAll(pkg, 0o755))
		require.NoError(t, syscall.Mkfifo(filepath.Join(pkg, "package.json"), 0o644))

		done := make(chan error, 1)
		go func() { done <- acpagents.Installed("fake", home) }()
		select {
		case err := <-done:
			require.ErrorContains(t, err, "fake-agent is not installed")
		case <-time.After(5 * time.Second):
			t.Fatal("reading a FIFO as the manifest blocked")
		}
	})

	t.Run("an oversized manifest", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)
		manifest := filepath.Join(acpagents.Dir(home, "fake"), "node_modules", "fake-agent", "package.json")
		huge := `{"version":"1.0.0","padding":"` + strings.Repeat("x", 2<<20) + `"}`
		require.NoError(t, os.WriteFile(manifest, []byte(huge), 0o644))

		require.ErrorContains(t, acpagents.Installed("fake", home), "fake-agent is not installed")
	})

	t.Run("an agent directory reached through a symlinked parent still counts", func(t *testing.T) {
		home, disk := t.TempDir(), t.TempDir()
		require.NoError(t, os.Symlink(disk, filepath.Join(home, ".local")))
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)

		require.NoError(t, acpagents.Installed("fake", home))
	})
}

func TestLaunchChecksTheBinInsideTheAgentDirectory(t *testing.T) {
	agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent"}
	t.Cleanup(acpagents.Override("fake", agent))

	t.Run("npm's relative bin link is followed", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)
		modules := filepath.Join(acpagents.Dir(home, "fake"), "node_modules")
		require.NoError(t, os.WriteFile(filepath.Join(modules, "fake-agent", "cli.js"), []byte(noopScript), 0o755))
		bin := filepath.Join(modules, ".bin", "fake-agent")
		require.NoError(t, os.Remove(bin))
		require.NoError(t, os.Symlink("../fake-agent/cli.js", bin))

		command, err := acpagents.Launch("fake", home, "/usr/bin")
		require.NoError(t, err)
		require.Equal(t, bin, command.Path)
	})

	t.Run("a bin linked to a program outside the agent directory is refused", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)
		bin := filepath.Join(acpagents.Dir(home, "fake"), "node_modules", ".bin", "fake-agent")
		require.NoError(t, os.Remove(bin))
		require.NoError(t, os.Symlink("/bin/sh", bin))

		_, err := acpagents.Launch("fake", home, "/usr/bin")
		require.Error(t, err)
	})

	t.Run("a bin that is not executable is refused", func(t *testing.T) {
		home := t.TempDir()
		acpagentstest.Install(t, home, "fake", agent, "1.0.0", noopScript)
		bin := filepath.Join(acpagents.Dir(home, "fake"), "node_modules", ".bin", "fake-agent")
		require.NoError(t, os.Chmod(bin, 0o644))

		_, err := acpagents.Launch("fake", home, "/usr/bin")
		require.ErrorContains(t, err, "not executable")
	})
}
