package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, "privileged session")

	err = requireDockerMountOrderingPrivilege(cfg, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{RequireMountsForFolders: &enabled},
	}, false)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
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

// The host IP list has no use but the drop-in root writes, so both fields
// need sudo whatever is stored, unlike the folders.
func TestDockerHostIPWaitNeedsAPrivilegedSession(t *testing.T) {
	for name, cfg := range map[string]*userconfig.Settings{
		"setting off": {},
		"setting on":  {Docker: userconfig.Docker{WaitForHostIPs: true, HostIPs: []string{"192.168.1.66"}}},
	} {
		for field, docker := range map[string]*apischema.ConfigDockerPayload{
			"docker.waitForHostIPs": {WaitForHostIPs: new(false)},
			"docker.hostIPs":        {HostIPs: []string{"10.0.0.2"}},
			"docker.hostIPs, empty": {HostIPs: []string{}},
		} {
			payload := &apischema.ConfigSetPayload{Docker: docker}
			err := requireDockerMountOrderingPrivilege(cfg, payload, false)
			require.ErrorIs(t, err, bridgeipc.ErrForbidden, name+": "+field)
			require.ErrorContains(t, err, "privileged session", name+": "+field)
			require.NoError(t, requireDockerMountOrderingPrivilege(cfg, payload, true), name+": "+field)
		}
	}

	// The folders alone stay open while the mount setting is off.
	require.NoError(t, requireDockerMountOrderingPrivilege(&userconfig.Settings{}, &apischema.ConfigSetPayload{
		Docker: &apischema.ConfigDockerPayload{Folders: []string{"/opt/docker"}},
	}, false))
}

func TestShouldSyncDockerHostIPWait(t *testing.T) {
	on := &userconfig.Settings{Docker: userconfig.Docker{WaitForHostIPs: true, HostIPs: []string{"192.168.1.66"}}}
	off := &userconfig.Settings{Docker: userconfig.Docker{HostIPs: []string{"192.168.1.66"}}}
	setting := &apischema.ConfigSetPayload{Docker: &apischema.ConfigDockerPayload{WaitForHostIPs: new(false)}}
	list := &apischema.ConfigSetPayload{Docker: &apischema.ConfigDockerPayload{HostIPs: []string{"192.168.1.66"}}}
	folders := &apischema.ConfigSetPayload{Docker: &apischema.ConfigDockerPayload{Folders: []string{"/srv/docker"}}}

	// Switching off removes the drop-in, so the setting always syncs.
	require.True(t, shouldSyncDockerHostIPWait(off, setting, userconfig.StorageModeHome))
	require.True(t, shouldSyncDockerHostIPWait(on, setting, userconfig.StorageModeFallback))
	require.True(t, shouldSyncDockerHostIPWait(on, list, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerHostIPWait(off, list, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerHostIPWait(on, folders, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerHostIPWait(on, setting, userconfig.StorageModeMemory))
	require.False(t, shouldSyncDockerHostIPWait(on, list, userconfig.StorageModeMemory))
	require.False(t, shouldSyncDockerHostIPWait(on, &apischema.ConfigSetPayload{}, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerHostIPWait(on, nil, userconfig.StorageModeHome))

	// A host IP save leaves the mount drop-in alone.
	require.False(t, shouldSyncDockerMountOrdering(on, setting, userconfig.StorageModeHome))
	require.False(t, shouldSyncDockerMountOrdering(on, list, userconfig.StorageModeHome))
}
