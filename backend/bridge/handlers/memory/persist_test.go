package memory

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// read returns a fixture file's content, failing the test when it is missing.
func (f fixture) read(name string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

// exists reports whether a fixture file exists.
func (f fixture) exists(name string) bool {
	f.t.Helper()
	_, err := os.Lstat(filepath.Join(f.root, name))
	return err == nil
}

func TestManagedFilesAreWrittenWithTheirExactContent(t *testing.T) {
	tests := []struct {
		name string
		path string
		data []byte
		want string
	}{
		{
			name: "sysctl drop-in",
			path: sysctlDropInPath,
			data: renderSysctlDropIn([]sysctlEntry{
				{key: "vm.swappiness", value: "180"},
				{key: "vm.page-cluster", value: "0"},
				{key: "vm.watermark_boost_factor", value: "0"},
				{key: "vm.watermark_scale_factor", value: "125"},
			}),
			want: "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
				"vm.swappiness = 180\n" +
				"vm.page-cluster = 0\n" +
				"vm.watermark_boost_factor = 0\n" +
				"vm.watermark_scale_factor = 125\n",
		},
		{
			name: "zswap tmpfiles",
			path: zswapTmpfilesPath,
			data: renderZswapTmpfiles("zstd"),
			want: "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
				"w /sys/module/zswap/parameters/compressor - - - - zstd\n" +
				"w /sys/module/zswap/parameters/enabled - - - - Y\n",
		},
		{
			name: "zram-tools config",
			path: zramswapConfigPath,
			data: renderZramswapConfig(8 * gib),
			want: "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
				"ALGO=zstd\n" +
				"SIZE=8192\n" +
				"PRIORITY=100\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			if err := f.host().writeFile(test.path, test.data, managedFileMode); err != nil {
				t.Fatal(err)
			}
			if got := f.read(test.path); got != test.want {
				t.Errorf("content = %q, want %q", got, test.want)
			}
			info, err := os.Stat(filepath.Join(f.root, test.path))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o644 {
				t.Errorf("mode = %v, want 0644", info.Mode().Perm())
			}
		})
	}
}

// The zram size is the planned size rounded down to whole MiB.
func TestZramswapConfigSizeIsInMiB(t *testing.T) {
	if got := string(renderZramswapConfig(3*gib + mib/2)); !slices.Contains(strings.Split(got, "\n"), "SIZE=3072") {
		t.Fatalf("config = %q, want a line SIZE=3072", got)
	}
}

func TestManagedFilesRefuseToWriteThroughASymlink(t *testing.T) {
	f := newFixture(t)
	f.write("/elsewhere", "x\n")
	f.mkdir("/etc/default")
	if err := os.Symlink("../../elsewhere", filepath.Join(f.root, zramswapConfigPath)); err != nil {
		t.Fatal(err)
	}
	if err := f.host().writeFile(zramswapConfigPath, renderZramswapConfig(gib), managedFileMode); err == nil {
		t.Fatal("want a refusal to write through a symlink")
	}
	if got := f.read("/elsewhere"); got != "x\n" {
		t.Fatalf("symlink target = %q, want it untouched", got)
	}
}

func TestRemovingAnAbsentManagedFileIsNotAnError(t *testing.T) {
	f := newFixture(t)
	if err := f.host().removeFile(zswapTmpfilesPath); err != nil {
		t.Fatalf("remove absent file: %v", err)
	}
	f.write(zswapTmpfilesPath, "w x - - - - y\n")
	if err := f.host().removeFile(zswapTmpfilesPath); err != nil {
		t.Fatal(err)
	}
	if f.exists(zswapTmpfilesPath) {
		t.Fatal("tmpfiles file still exists")
	}
}

func TestParseSysctlDropIn(t *testing.T) {
	text := "# Managed by LinuxIO\n; old comment\n\nvm.swappiness=10\n  vm.vfs_cache_pressure = 50  \nnot a setting\nvm.page-cluster = 0\n"
	want := []sysctlEntry{
		{key: "vm.swappiness", value: "10"},
		{key: "vm.vfs_cache_pressure", value: "50"},
		{key: "vm.page-cluster", value: "0"},
	}
	if got := parseSysctlDropIn(text); !slices.Equal(got, want) {
		t.Fatalf("entries = %+v, want %+v", got, want)
	}
}

// Changing one key keeps every other key the drop-in holds, in its place.
func TestMergeSysctlDropInKeepsOtherKeys(t *testing.T) {
	existing := []sysctlEntry{
		{key: "vm.vfs_cache_pressure", value: "50"},
		{key: "vm.swappiness", value: "60"},
		{key: "vm.page-cluster", value: "0"},
	}
	tests := []struct {
		name   string
		set    []sysctlSetting
		remove []string
		want   []sysctlEntry
	}{
		{
			name: "replace in place",
			set:  []sysctlSetting{{name: sysctlSwappiness, value: 100}},
			want: []sysctlEntry{
				{key: "vm.vfs_cache_pressure", value: "50"},
				{key: "vm.swappiness", value: "100"},
				{key: "vm.page-cluster", value: "0"},
			},
		},
		{
			name: "append a missing key",
			set:  []sysctlSetting{{name: sysctlWatermarkScaleFactor, value: 125}},
			want: []sysctlEntry{
				{key: "vm.vfs_cache_pressure", value: "50"},
				{key: "vm.swappiness", value: "60"},
				{key: "vm.page-cluster", value: "0"},
				{key: "vm.watermark_scale_factor", value: "125"},
			},
		},
		{
			name:   "remove keys a mode no longer manages",
			set:    []sysctlSetting{{name: sysctlSwappiness, value: 60}},
			remove: []string{sysctlPageCluster, sysctlWatermarkBoostFactor, sysctlWatermarkScaleFactor},
			want: []sysctlEntry{
				{key: "vm.vfs_cache_pressure", value: "50"},
				{key: "vm.swappiness", value: "60"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mergeSysctlDropIn(slices.Clone(existing), test.set, test.remove)
			if !slices.Equal(got, test.want) {
				t.Fatalf("entries = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestReadSysctlDropIn(t *testing.T) {
	f := newFixture(t)
	got, err := f.host().readSysctlDropIn()
	if err != nil || len(got) != 0 {
		t.Fatalf("absent drop-in = %+v, %v; want no entries", got, err)
	}
	f.write(sysctlDropInPath, "vm.swappiness = 30\n")
	got, err = f.host().readSysctlDropIn()
	if err != nil || !slices.Equal(got, []sysctlEntry{{key: "vm.swappiness", value: "30"}}) {
		t.Fatalf("drop-in = %+v, %v", got, err)
	}
}
