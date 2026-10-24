package hwprobe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jaypipes/ghw/pkg/gpu"
	"github.com/jaypipes/ghw/pkg/pci"
	"github.com/jaypipes/ghw/pkg/topology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePCIAddress(t *testing.T) {
	cases := map[string]string{
		"0000:01:00.0":     "0000:01:00.0",
		"00000000:01:00.0": "0000:01:00.0",
		"  0000:0A:00.0 ":  "0000:0a:00.0", //nolint:gocritic // the padding is the input under test
		"01:00.0":          "01:00.0",
		"":                 "",
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizePCIAddress(in), in)
	}
}

func TestSanitizers(t *testing.T) {
	assert.Empty(t, SanitizeValue(" N/A "))
	assert.Empty(t, SanitizeValue("[Not Supported]"))
	assert.Equal(t, "x", SanitizeValue(" x "))
	assert.Empty(t, SanitizeUnknown(" Unknown "))
	assert.Equal(t, "8 GT/s", SanitizeUnknown("8 GT/s"))
	assert.Empty(t, SanitizeZero("0"))
	assert.Equal(t, "16", SanitizeZero("16"))
}

func TestSysfsReaders(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		return path
	}
	num := write("num", " 42\n")
	bad := write("bad", "x")

	assert.Equal(t, "42", ReadSysfsString(num))
	assert.Empty(t, ReadSysfsString(filepath.Join(dir, "missing")))

	for content, want := range map[string]bool{"1": true, "Y": true, "yes": true, "TRUE": true, "0": false, "n": false, "No": false} {
		got, ok := ReadSysfsBool(write("bool", content))
		assert.True(t, ok, content)
		assert.Equal(t, want, got, content)
	}
	_, ok := ReadSysfsBool(bad)
	assert.False(t, ok)

	i, ok := ReadSysfsIntAny("", bad, num)
	assert.True(t, ok)
	assert.Equal(t, 42, i)
	u, ok := ReadSysfsUint64Any(bad, num)
	assert.True(t, ok)
	assert.Equal(t, uint64(42), u)
	f, ok := ReadSysfsFloatAny(bad, write("float", "1.5"))
	assert.True(t, ok)
	assert.InDelta(t, 1.5, f, 0)
	_, ok = ReadSysfsIntAny("", bad)
	assert.False(t, ok)
}

func TestFindGPUCardDir(t *testing.T) {
	root := t.TempDir()
	devices := filepath.Join(root, "devices", "0000:01:00.0")
	require.NoError(t, os.MkdirAll(devices, 0o755))
	drm := filepath.Join(root, "drm")
	card := filepath.Join(drm, "card1")
	require.NoError(t, os.MkdirAll(card, 0o755))
	require.NoError(t, os.Symlink(devices, filepath.Join(card, "device")))
	// Connector entries and non-card entries must be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(drm, "card1-HDMI-A-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(drm, "renderD128"), 0o755))

	name, cardDir, pciDir, ok := findGPUCardDir(drm, "00000000:01:00.0")
	require.True(t, ok)
	assert.Equal(t, "card1", name)
	assert.Equal(t, card, cardDir)
	assert.Equal(t, filepath.Join(card, "device"), pciDir)

	_, _, _, ok = findGPUCardDir(drm, "0000:02:00.0")
	assert.False(t, ok)
	_, _, _, ok = findGPUCardDir(filepath.Join(root, "missing"), "0000:01:00.0")
	assert.False(t, ok)
}

func TestDriverModule(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "module", "amdgpu")
	require.NoError(t, os.MkdirAll(module, 0o755))
	pciDir := filepath.Join(root, "dev")
	require.NoError(t, os.MkdirAll(filepath.Join(pciDir, "driver"), 0o755))
	assert.Empty(t, DriverModule(pciDir))
	require.NoError(t, os.Symlink(module, filepath.Join(pciDir, "driver", "module")))
	assert.Equal(t, "amdgpu", DriverModule(pciDir))
}

func TestBuildGPUEntry(t *testing.T) {
	card := &gpu.GraphicsCard{
		Address: "00000000:01:00.0",
		DeviceInfo: &pci.Device{
			Driver:   "amdgpu",
			Revision: "N/A",
		},
		Node: &topology.Node{ID: 1},
	}
	entry := BuildGPUEntry(card)
	assert.Equal(t, map[string]any{
		"address":   "0000:01:00.0",
		"driver":    "amdgpu",
		"numa_node": 1,
	}, entry)
}
