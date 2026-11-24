package smart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmmcSysfs points the eMMC reader at a sysfs tree holding mmcblk0,
// serial 01234567, whose pre-EOL info is a warning.
func fakeEmmcSysfs(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	prev := emmcSysfsRoot
	emmcSysfsRoot = tmp
	t.Cleanup(func() { emmcSysfsRoot = prev })

	mmcDeviceDir := filepath.Join(tmp, "class", "block", "mmcblk0", "device")
	mmcQueueDir := filepath.Join(tmp, "class", "block", "mmcblk0", "queue")
	require.NoError(t, os.MkdirAll(mmcDeviceDir, 0o755))
	require.NoError(t, os.MkdirAll(mmcQueueDir, 0o755))

	write := func(path, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write(filepath.Join(mmcDeviceDir, "pre_eol_info"), "0x02\n")
	write(filepath.Join(mmcDeviceDir, "life_time"), "0x04 0x05\n")
	write(filepath.Join(mmcDeviceDir, "name"), "H26M52103FMR\n")
	write(filepath.Join(mmcDeviceDir, "serial"), "01234567\n")
	write(filepath.Join(mmcDeviceDir, "prv"), "0x08\n")
	write(filepath.Join(mmcQueueDir, "logical_block_size"), "4096\n")        // size is always 512-byte units
	write(filepath.Join(tmp, "class", "block", "mmcblk0", "size"), "1024\n") // sectors
}

func TestEmmcMockSysfsScanAndCollect(t *testing.T) {
	fakeEmmcSysfs(t)

	devs := scanEmmcDevices()
	require.Equal(t, []string{"/dev/mmcblk0"}, devs)

	key, got, ok := readEmmcSmartData(devs[0])
	require.True(t, ok)
	assert.Equal(t, "01234567", key)
	assert.Equal(t, "emmc", got.DiskType)
	assert.Equal(t, "/dev/mmcblk0", got.DiskName)
	assert.Equal(t, "WARNING", got.SmartStatus)
	assert.Equal(t, "01234567", got.SerialNumber)
	assert.NotEmpty(t, got.ModelName)
	assert.Equal(t, uint64(1024*512), got.Capacity)
	assert.GreaterOrEqual(t, len(got.Attributes), 3)
}
