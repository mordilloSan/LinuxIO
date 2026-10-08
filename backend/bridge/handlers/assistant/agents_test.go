package assistant

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestLookupAgentKnowsTheThreeAgents(t *testing.T) {
	for _, id := range []string{"claude", "gemini", "codex"} {
		agent, ok := LookupAgent(id)
		require.True(t, ok, id)
		require.Equal(t, "npx", agent.Command, id)
		require.NotEmpty(t, agent.Args, id)
	}
	_, ok := LookupAgent("opencode")
	require.False(t, ok)
	require.ElementsMatch(t, []string{"claude", "codex", "gemini"}, AgentIDs())
}

func TestLoginShellArgvPassesArgumentsPositionally(t *testing.T) {
	argv := LoginShellArgv(Agent{Command: "npx", Args: []string{"pkg", "--acp"}}, []string{"--cli", "auth login"})
	require.Len(t, argv, 10)
	require.Contains(t, []string{"bash", "sh"}, argv[0])
	// Interactive AND login: ~/.bashrc returns early for non-interactive
	// shells, and that is where nvm-style version managers put themselves.
	require.Equal(t, []string{"-i", "-l", "-c", `exec "$0" "$@"`, "npx", "pkg", "--acp", "--cli", "auth login"}, argv[1:])
}

func TestOverrideAgentRestores(t *testing.T) {
	restore := OverrideAgent("fake", Agent{Command: "/bin/true"})
	_, ok := LookupAgent("fake")
	require.True(t, ok)
	restore()
	_, ok = LookupAgent("fake")
	require.False(t, ok)
}

func TestProbeLoginShellFindsCommandsOnTheUsersPath(t *testing.T) {
	u, err := user.Current()
	require.NoError(t, err)
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric uid %q", u.Uid)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric gid %q", u.Gid)
	}
	me := session.User{Username: u.Username, UID: uint32(uid), GID: uint32(gid)}

	path, err := UserPath(context.Background(), me)
	require.NoError(t, err)
	require.Contains(t, path, "/usr/bin")

	require.NoError(t, ProbeLoginShell(context.Background(), me, "sh"))

	err = ProbeLoginShell(context.Background(), me, "linuxio-definitely-missing-command")
	require.Error(t, err)
	require.Contains(t, err.Error(), "linuxio-definitely-missing-command")

	err = ProbeLoginShell(context.Background(), session.User{Username: "linuxio-no-such-user"}, "sh")
	require.Error(t, err)
}

func TestLookPathIn(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700))

	found, err := lookPathIn(dir+":/nonexistent", "tool")
	require.NoError(t, err)
	require.Equal(t, script, found)

	_, err = lookPathIn("/nonexistent", "tool")
	require.Error(t, err)

	// A command given with a slash is used as is, like exec.LookPath.
	found, err = lookPathIn("/nonexistent", script)
	require.NoError(t, err)
	require.Equal(t, script, found)
}
