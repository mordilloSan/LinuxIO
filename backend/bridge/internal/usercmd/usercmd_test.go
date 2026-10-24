package usercmd

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

func TestLoginShellArgvPassesArgumentsPositionally(t *testing.T) {
	argv := LoginShellArgv("npx", []string{"pkg", "--acp"}, []string{"--cli", "auth login"})
	require.Len(t, argv, 10)
	require.Contains(t, []string{"bash", "sh"}, argv[0])
	// Interactive AND login: ~/.bashrc returns early for non-interactive
	// shells, and that is where nvm-style version managers put themselves.
	require.Equal(t, []string{"-i", "-l", "-c", `exec "$0" "$@"`, "npx", "pkg", "--acp", "--cli", "auth login"}, argv[1:])
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

	path, err := Path(context.Background(), me)
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

	found, err := LookPathIn(dir+":/nonexistent", "tool")
	require.NoError(t, err)
	require.Equal(t, script, found)

	_, err = LookPathIn("/nonexistent", "tool")
	require.Error(t, err)

	// A command given with a slash is used as is, like exec.LookPath.
	found, err = LookPathIn("/nonexistent", script)
	require.NoError(t, err)
	require.Equal(t, script, found)
}
