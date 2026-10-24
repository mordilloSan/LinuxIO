package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTempFstab(t *testing.T, content string) {
	t.Helper()
	old := fstabPath
	t.Cleanup(func() { fstabPath = old })
	fstabPath = filepath.Join(t.TempDir(), "fstab")
	require.NoError(t, os.WriteFile(fstabPath, []byte(content), 0o644))
}

func TestAddToFstabIgnoresCommentsAndTerminatesLastLine(t *testing.T) {
	useTempFstab(t, "# /mnt/data was here\nUUID=a / ext4 defaults 0 1")

	require.NoError(t, addToFstab("srv:/x", "/mnt/data", "nfs", []string{"ro"}))

	assert.Equal(t, "# /mnt/data was here\nUUID=a / ext4 defaults 0 1\nsrv:/x /mnt/data nfs ro 0 0\n", readFstab(t))
}

func TestRemoveFromFstabKeepsComments(t *testing.T) {
	useTempFstab(t, "# /mnt/data old disk\nsrv:/x /mnt/data nfs ro 0 0\nUUID=a / ext4 defaults 0 1\n")

	require.NoError(t, removeFromFstab("/mnt/data"))

	assert.Equal(t, "# /mnt/data old disk\nUUID=a / ext4 defaults 0 1\n", readFstab(t))
}

func TestUpdateFstabEntryWritesANewFile(t *testing.T) {
	useTempFstab(t, "srv:/x /mnt/data nfs ro 0 0\n")
	before, err := os.Stat(fstabPath)
	require.NoError(t, err)

	require.NoError(t, updateFstabEntry("/mnt/data", "srv:/x", "nfs", []string{"rw"}))

	after, err := os.Stat(fstabPath)
	require.NoError(t, err)
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.NotEqual(t, beforeStat.Ino, afterStat.Ino, "the table is replaced, never truncated in place")
	assert.Equal(t, "srv:/x /mnt/data nfs rw 0 0\n", readFstab(t))
}

func TestCleanMountOptions(t *testing.T) {
	got, err := cleanMountOptions([]string{" ro ", "", "nofail,_netdev"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ro", "nofail", "_netdev"}, got)

	for _, bad := range []string{"ro nofail", "ro\n/dev/sdb1 / ext4 defaults 0 0", "ro\tx"} {
		_, badErr := cleanMountOptions([]string{bad})
		require.Error(t, badErr, "%q must be rejected", bad)
	}

	split, err := cleanMountOptions([]string{"ro,password=x"})
	require.NoError(t, err)
	assert.Error(t, rejectSensitiveCustomOptions(split), "a comma-joined credential is seen once split")
}
