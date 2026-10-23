package userconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
