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

func TestResolveHomePathFollowsSymlinkAndChecksResolvedOwner(t *testing.T) {
	target := filepath.Join(t.TempDir(), "home-target")
	require.NoError(t, os.Mkdir(target, 0o755))
	link := filepath.Join(filepath.Dir(target), "home-link")
	require.NoError(t, os.Symlink(target, link))

	resolved, err := resolveHomePath(link, uint32(os.Getuid()))
	require.NoError(t, err)
	require.Equal(t, target, resolved)
	require.ErrorContains(t, func() error {
		_, err := resolveHomePath(link, uint32(os.Getuid()+1))
		return err
	}(), "owned by uid")
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
	replacement := DefaultUIPreferences()
	replacement.Theme = ThemeDark
	_, err = store.ReplaceUI(context.Background(), replacement)
	require.NoError(t, err)

	for _, path := range []string{cfgPath, uiPath, lockPath, uiLockPath} {
		requireFileOwnership(t, path, owner.uid, owner.gid)
	}
	for _, path := range []string{cfgPath, uiPath} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(filePerm), info.Mode().Perm(), "mode for %s", path)
	}
}

func TestOwnedConfigStoreRejectsWrongHomeOwner(t *testing.T) {
	owner := currentProcessFileOwnership()
	owner.uid++

	err := owner.ensureDirectory(t.TempDir())
	require.ErrorContains(t, err, "is owned by uid")
}

func TestOwnedConfigArtifactsCanTargetDifferentIDsWhenPrivileged(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires privilege to create artifacts for a different target ID")
	}
	base := t.TempDir()
	targetUID := uint32(os.Getuid() + 1)
	targetGID := uint32(os.Getgid() + 1)
	require.NoError(t, os.Chown(base, int(targetUID), int(targetGID)))
	owner, err := resolveFileOwnership(targetUID, targetGID)
	require.NoError(t, err)

	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	require.NoError(t, withConfigLocksOwned(context.Background(), cfgPath+".lock", uiPath+".lock", owner, func() error {
		return initializeLockedOwned(cfgPath, uiPath, base, owner)
	}))
	requireFileOwnership(t, cfgPath, int(targetUID), int(targetGID))
	requireFileOwnership(t, uiPath, int(targetUID), int(targetGID))
	requireFileOwnership(t, cfgPath+".lock", int(targetUID), int(targetGID))
	requireFileOwnership(t, uiPath+".lock", int(targetUID), int(targetGID))
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
