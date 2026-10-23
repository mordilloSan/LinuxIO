package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestRequireDockerMountOrderingPrivilege(t *testing.T) {
	enabled := true
	cfg := &userconfig.Settings{
		Docker: userconfig.Docker{
			Folders:                 []userconfig.AbsolutePath{"/srv/docker"},
			RequireMountsForFolders: true,
		},
	}

	err := requireDockerMountOrderingPrivilege(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{Folders: []string{"/opt/docker"}},
	}, false)
	require.ErrorContains(t, err, "privileged session")

	err = requireDockerMountOrderingPrivilege(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{RequireMountsForFolders: &enabled},
	}, false)
	require.ErrorContains(t, err, "privileged session")

	require.NoError(t, requireDockerMountOrderingPrivilege(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{Folders: []string{"/opt/docker"}},
	}, true))
}

func TestShouldSyncDockerMountOrdering(t *testing.T) {
	enabled := true
	cfg := &userconfig.Settings{
		Docker: userconfig.Docker{RequireMountsForFolders: true},
	}

	require.True(t, shouldSyncDockerMountOrdering(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{RequireMountsForFolders: &enabled},
	}, userconfig.StorageModeHome))
	require.True(t, shouldSyncDockerMountOrdering(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{Folders: []string{"/srv/docker"}},
	}, userconfig.StorageModeFallback))
	require.False(t, shouldSyncDockerMountOrdering(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{RequireMountsForFolders: &enabled},
	}, userconfig.StorageModeMemory))

	cfg.Docker.RequireMountsForFolders = false
	require.False(t, shouldSyncDockerMountOrdering(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{Folders: []string{"/srv/docker"}},
	}, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerMountOrdering(cfg, &apischema.ConfigSetPayload{}, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerMountOrdering(cfg, nil, userconfig.StorageModeHome))
}
