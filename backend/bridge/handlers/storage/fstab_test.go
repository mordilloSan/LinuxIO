package storage

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
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

func TestAddToFstabPass(t *testing.T) {
	useTempFstab(t, "")

	require.NoError(t, addToFstab("UUID=a", "/srv/a", "ext4", []string{"defaults", "nofail"}))
	require.NoError(t, addToFstab("UUID=b", "/srv/b", "btrfs", []string{"defaults", "nofail"}))

	assert.Equal(t, "UUID=a /srv/a ext4 defaults,nofail 0 2\nUUID=b /srv/b btrfs defaults,nofail 0 0\n", readFstab(t))
}

func TestUnmountRemovesFstab(t *testing.T) {
	origFind, origUnmount := mountedPartition, unmountPath
	t.Cleanup(func() { mountedPartition, unmountPath = origFind, origUnmount })
	mountedPartition = func(mountpoint string) (*disk.PartitionStat, error) {
		return &disk.PartitionStat{Device: "/dev/sdb1", Mountpoint: mountpoint, Fstype: "ext4"}, nil
	}
	unmountPath = func(string, int) error { return nil }

	const root, data = "UUID=a / ext4 defaults 0 1\n", "UUID=u1 /srv/data ext4 defaults,nofail 0 2\n"
	for _, removeFstab := range []bool{false, true} {
		useTempFstab(t, root+data)
		_, err := UnmountFilesystem(context.Background(), "/srv/data", removeFstab)
		require.NoError(t, err)
		want := root + data
		if removeFstab {
			want = root
		}
		assert.Equal(t, want, readFstab(t), "removeFstab=%t", removeFstab)
	}
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
