package config

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveFileOwnershipUsesAuthenticatedIDs(t *testing.T) {
	uid := uint32(os.Geteuid())
	gid := uint32(os.Getegid())

	owner, err := resolveFileOwnership(uid, gid)
	require.NoError(t, err)
	require.Equal(t, int(uid), owner.uid)
	require.Equal(t, int(gid), owner.gid)
	require.True(t, owner.enforce)
}

func TestResolveFileOwnershipRejectsMismatchedUnprivilegedTarget(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may target another user")
	}

	otherUID := uint32(os.Geteuid()) + 1
	_, err := resolveFileOwnership(otherUID, uint32(os.Getegid()))
	require.ErrorContains(t, err, "does not match target uid")

	otherGID := uint32(os.Getegid()) + 1
	_, err = resolveFileOwnership(uint32(os.Geteuid()), otherGID)
	require.ErrorContains(t, err, "does not match target gid")
}

func TestHomedirDoesNotFallBackToBridgeHome(t *testing.T) {
	_, err := Homedir("linuxio-user-that-does-not-exist")
	require.Error(t, err)
}

func TestOwnedConfigArtifactsUseTargetOwnership(t *testing.T) {
	base := t.TempDir()
	owner := currentProcessFileOwnership()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	lockPath := cfgPath + ".lock"
	uiLockPath := uiPath + ".lock"

	err := withConfigLocksOwned(context.Background(), lockPath, uiLockPath, owner, func() error {
		return initializeLockedOwned(cfgPath, uiPath, base, owner)
	})
	require.NoError(t, err)

	ui := DefaultUIPreferences()
	store := newUserStore("miguel", cfgPath, uiPath, DefaultSettings(base), &ui)
	_, err = store.Update(context.Background(), func(cfg *Settings) error {
		cfg.AppSettings.ChunkSizeMB++
		return nil
	})
	require.NoError(t, err)
	_, err = store.UpdateUI(context.Background(), func(ui *UIPreferences) error {
		ui.Theme = ThemeDark
		return nil
	})
	require.NoError(t, err)

	for _, path := range []string{cfgPath, uiPath, lockPath, uiLockPath} {
		requireFileOwnership(t, path, owner.uid, owner.gid)
	}
}

func TestOwnedConfigStoreRejectsWrongHomeOwner(t *testing.T) {
	owner := currentProcessFileOwnership()
	owner.uid++

	err := owner.ensureDirectory(t.TempDir())
	require.ErrorContains(t, err, "is owned by uid")
}

func requireFileOwnership(t *testing.T, path string, uid, gid int) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "stat type = %T", info.Sys())
	require.Equal(t, uid, int(stat.Uid), "uid for %s", path)
	require.Equal(t, gid, int(stat.Gid), "gid for %s", path)
}
