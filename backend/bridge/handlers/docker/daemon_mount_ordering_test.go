package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestRenderDockerMountOrderingDropIn(t *testing.T) {
	content, err := renderDockerMountOrderingDropIn([]userconfig.AbsolutePath{
		"/srv/docker",
		"/mnt/docker stacks",
	})
	require.NoError(t, err)

	require.Contains(t, content, "[Unit]")
	require.Contains(t, content, "Wants=network-online.target")
	require.Contains(t, content, "After=network-online.target remote-fs.target")
	require.Contains(t, content, "RequiresMountsFor=/srv/docker")
	require.Contains(t, content, `RequiresMountsFor="/mnt/docker stacks"`)
}

func TestRenderDockerMountOrderingDropInRejectsInvalidFolders(t *testing.T) {
	_, err := renderDockerMountOrderingDropIn(nil)
	require.ErrorContains(t, err, "docker folders cannot be empty")

	_, err = renderDockerMountOrderingDropIn([]userconfig.AbsolutePath{"/srv/docker\nbad"})
	require.ErrorContains(t, err, "docker folder cannot contain a newline")
}

func TestEnsureSystemdDropInDirSetsReadablePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.service.d")
	require.NoError(t, os.MkdirAll(path, 0o700))

	require.NoError(t, ensureSystemdDropInDir(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestSystemdUnitValueOnlyQuotesWhenNeeded(t *testing.T) {
	require.Equal(t, "/srv/docker", systemdUnitValue("/srv/docker"))
	quoted := systemdUnitValue("/srv/docker stacks")
	require.True(t, strings.HasPrefix(quoted, `"`))
	require.True(t, strings.HasSuffix(quoted, `"`))
}
