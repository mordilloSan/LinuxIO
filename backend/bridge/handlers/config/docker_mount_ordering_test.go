package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func TestRenderDockerMountOrderingDropIn(t *testing.T) {
	content, err := renderDockerMountOrderingDropIn([]bridgeconfig.AbsolutePath{
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

	_, err = renderDockerMountOrderingDropIn([]bridgeconfig.AbsolutePath{"/srv/docker\nbad"})
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

func TestRequireDockerMountOrderingPrivilege(t *testing.T) {
	enabled := true
	cfg := &bridgeconfig.Settings{
		Docker: bridgeconfig.Docker{
			Folders:                 []bridgeconfig.AbsolutePath{"/srv/docker"},
			RequireMountsForFolders: true,
		},
	}

	err := requireDockerMountOrderingPrivilege(cfg, &configSetPayload{
		Docker: &configDockerPayload{Folders: []string{"/opt/docker"}},
	}, false)
	require.ErrorContains(t, err, "privileged session")

	err = requireDockerMountOrderingPrivilege(cfg, &configSetPayload{
		Docker: &configDockerPayload{RequireMountsForFolders: &enabled},
	}, false)
	require.ErrorContains(t, err, "privileged session")

	require.NoError(t, requireDockerMountOrderingPrivilege(cfg, &configSetPayload{
		Docker: &configDockerPayload{Folders: []string{"/opt/docker"}},
	}, true))
}

func TestShouldSyncDockerMountOrdering(t *testing.T) {
	enabled := true
	cfg := &bridgeconfig.Settings{
		Docker: bridgeconfig.Docker{RequireMountsForFolders: true},
	}

	require.True(t, shouldSyncDockerMountOrdering(cfg, &configSetPayload{
		Docker: &configDockerPayload{RequireMountsForFolders: &enabled},
	}))
	require.True(t, shouldSyncDockerMountOrdering(cfg, &configSetPayload{
		Docker: &configDockerPayload{Folders: []string{"/srv/docker"}},
	}))

	cfg.Docker.RequireMountsForFolders = false
	require.False(t, shouldSyncDockerMountOrdering(cfg, &configSetPayload{
		Docker: &configDockerPayload{Folders: []string{"/srv/docker"}},
	}))
	require.False(t, shouldSyncDockerMountOrdering(cfg, &configSetPayload{}))
	require.False(t, shouldSyncDockerMountOrdering(cfg, nil))
}

func TestSystemdUnitValueOnlyQuotesWhenNeeded(t *testing.T) {
	require.Equal(t, "/srv/docker", systemdUnitValue("/srv/docker"))
	quoted := systemdUnitValue("/srv/docker stacks")
	require.True(t, strings.HasPrefix(quoted, `"`))
	require.True(t, strings.HasSuffix(quoted, `"`))
}
