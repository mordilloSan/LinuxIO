package smart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMdraidSysfs points the md reader at a sysfs tree holding md0, a
// two-disk raid1 that is resyncing.
func fakeMdraidSysfs(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	prev := mdraidSysfsRoot
	mdraidSysfsRoot = tmp
	t.Cleanup(func() { mdraidSysfsRoot = prev })

	mdDir := filepath.Join(tmp, "block", "md0", "md")
	queueDir := filepath.Join(tmp, "block", "md0", "queue")
	require.NoError(t, os.MkdirAll(mdDir, 0o755))
	require.NoError(t, os.MkdirAll(queueDir, 0o755))

	write := func(path, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write(filepath.Join(mdDir, "array_state"), "active\n")
	write(filepath.Join(mdDir, "level"), "raid1\n")
	write(filepath.Join(mdDir, "raid_disks"), "2\n")
	write(filepath.Join(mdDir, "degraded"), "0\n")
	write(filepath.Join(mdDir, "sync_action"), "resync\n")
	write(filepath.Join(mdDir, "sync_completed"), "10%\n")
	write(filepath.Join(mdDir, "sync_speed"), "100M\n")
	write(filepath.Join(mdDir, "mismatch_cnt"), "0\n")
	write(filepath.Join(queueDir, "logical_block_size"), "4096\n") // size is always 512-byte units
	write(filepath.Join(tmp, "block", "md0", "size"), "2048\n")
}

func TestMdraidMockSysfsScanAndCollect(t *testing.T) {
	fakeMdraidSysfs(t)

	devs := scanMdraidDevices()
	require.Equal(t, []string{"/dev/md0"}, devs)

	key, got, ok := readMdraidSmartData(devs[0])
	require.True(t, ok)
	assert.Equal(t, "mdraid:md0", key)
	assert.Equal(t, "mdraid", got.DiskType)
	assert.Equal(t, "/dev/md0", got.DiskName)
	assert.Equal(t, "WARNING", got.SmartStatus)
	assert.NotEmpty(t, got.ModelName)
	assert.Equal(t, uint64(2048*512), got.Capacity)
	assert.GreaterOrEqual(t, len(got.Attributes), 5)
}

func TestMdraidSmartStatus(t *testing.T) {
	if got := mdraidSmartStatus(mdraidHealth{arrayState: "inactive"}); got != "FAILED" {
		t.Fatalf("mdraidSmartStatus(inactive) = %q, want FAILED", got)
	}
	if got := mdraidSmartStatus(mdraidHealth{arrayState: "active", degraded: 1, syncAction: "recover"}); got != "WARNING" {
		t.Fatalf("mdraidSmartStatus(degraded+recover) = %q, want WARNING", got)
	}
	if got := mdraidSmartStatus(mdraidHealth{arrayState: "active", degraded: 1}); got != "FAILED" {
		t.Fatalf("mdraidSmartStatus(degraded) = %q, want FAILED", got)
	}
	if got := mdraidSmartStatus(mdraidHealth{arrayState: "active", syncAction: "recover"}); got != "WARNING" {
		t.Fatalf("mdraidSmartStatus(recover) = %q, want WARNING", got)
	}
	if got := mdraidSmartStatus(mdraidHealth{arrayState: "clean"}); got != "PASSED" {
		t.Fatalf("mdraidSmartStatus(clean) = %q, want PASSED", got)
	}
	if got := mdraidSmartStatus(mdraidHealth{arrayState: "unknown"}); got != "UNKNOWN" {
		t.Fatalf("mdraidSmartStatus(unknown) = %q, want UNKNOWN", got)
	}
}
