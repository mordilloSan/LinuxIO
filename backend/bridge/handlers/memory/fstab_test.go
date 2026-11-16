package memory

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// symlink makes name a symbolic link to target inside the fixture.
func (f fixture) symlink(target, name string) {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		f.t.Fatal(err)
	}
}

// Each fstab source names its unit the way systemd-fstab-generator does: a
// path as written, a tag through its /dev/disk/by-* link, each escaped.
func TestFstabSwapsNameTheirUnits(t *testing.T) {
	f := newFixture(t)
	f.write("/etc/fstab", `# comment
UUID=0a1b2c3d / btrfs defaults 0 1
/swap.img none swap sw 0 0
UUID=7e8f-aaaa none swap sw 0 0
LABEL=my\040swap none swap sw 0 0
PARTUUID=1234-56 none swap sw 0 0
PARTLABEL=swap-part none swap sw 0 0
/dev/sda3 none swap sw 0 0
/mnt/my\040disk/swapfile none swap sw 0 0
none none swap sw 0 0
ID=ata-disk-part3 none swap sw 0 0
/a/../b none swap sw 0 0
#/old.img none swap sw 0 0
`)
	got, err := f.host().fstabSwaps()
	if err != nil {
		t.Fatal(err)
	}
	want := []fstabSwap{
		{path: "/swap.img", unit: "swap.img.swap"},
		{path: "/dev/disk/by-uuid/7e8f-aaaa", unit: `dev-disk-by\x2duuid-7e8f\x2daaaa.swap`},
		{path: `/dev/disk/by-label/my\x20swap`, unit: `dev-disk-by\x2dlabel-my\x5cx20swap.swap`},
		{path: "/dev/disk/by-partuuid/1234-56", unit: `dev-disk-by\x2dpartuuid-1234\x2d56.swap`},
		{path: "/dev/disk/by-partlabel/swap-part", unit: `dev-disk-by\x2dpartlabel-swap\x2dpart.swap`},
		{path: "/dev/sda3", unit: "dev-sda3.swap"},
		{path: "/mnt/my disk/swapfile", unit: `mnt-my\x20disk-swapfile.swap`},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("fstab swaps =\n%+v\nwant\n%+v", got, want)
	}
}

func TestFstabSwapsWithoutAnFstab(t *testing.T) {
	got, err := newFixture(t).host().fstabSwaps()
	if err != nil || len(got) != 0 {
		t.Fatalf("fstab swaps = %+v, %v; want none", got, err)
	}
}

// An fstab entry is the /proc/swaps row it resolves to inside the root: by
// its own path, through a /dev/disk/by-uuid link, or through a link to the
// swap file. A row nothing in fstab resolves to stays unmatched.
func TestMatchFstabSwapsResolvesLinksUnderTheRoot(t *testing.T) {
	f := newFixture(t)
	f.write("/swap.img", "")
	f.write("/data/swapfile", "")
	f.write("/dev/sda3", "")
	f.write("/dev/sdb2", "")
	f.write("/home/extra", "")
	f.symlink("../../sda3", "/dev/disk/by-uuid/7e8f")
	f.symlink("/data/swapfile", "/swaplink")
	entries := []fstabSwap{
		{path: "/swap.img", unit: "swap.img.swap"},
		{path: "/dev/disk/by-uuid/7e8f", unit: `dev-disk-by\x2duuid-7e8f.swap`},
		{path: "/swaplink", unit: "swaplink.swap"},
		{path: "/dev/disk/by-uuid/gone", unit: `dev-disk-by\x2duuid-gone.swap`},
	}
	devices := []apischema.SwapDevice{
		{Path: "/swap.img", Kind: apischema.SwapDeviceFile},
		{Path: "/dev/sda3", Kind: apischema.SwapDevicePartition},
		{Path: "/data/swapfile", Kind: apischema.SwapDeviceFile},
		{Path: "/dev/sdb2", Kind: apischema.SwapDevicePartition},
		{Path: "/home/extra", Kind: apischema.SwapDeviceFile},
	}
	got := f.host().matchFstabSwaps(entries, devices)
	wantDevices := []string{"/swap.img", "/dev/sda3", "/data/swapfile", ""}
	for i, entry := range got {
		if entry.device != wantDevices[i] {
			t.Errorf("%s matched %q, want %q", entry.path, entry.device, wantDevices[i])
		}
	}
}

// A deleted swap file shows in /proc/swaps with " (deleted)" and cannot be
// resolved; only the literal name matches it.
func TestMatchFstabSwapsNeedsTheFileOrItsLiteralName(t *testing.T) {
	f := newFixture(t)
	entries := []fstabSwap{{path: "/swap.img", unit: "swap.img.swap"}, {path: "/other.img", unit: "other.img.swap"}}
	devices := []apischema.SwapDevice{{Path: "/swap.img", Kind: apischema.SwapDeviceFile}, {Path: "/other.img (deleted)", Kind: apischema.SwapDeviceFile}}
	got := f.host().matchFstabSwaps(entries, devices)
	if got[0].device != "/swap.img" || got[1].device != "" {
		t.Fatalf("matches = %+v, want only the literal name to match", got)
	}
}

func TestResolveStaysUnderTheRoot(t *testing.T) {
	f := newFixture(t)
	f.write("/real/file", "")
	f.symlink("/real", "/abs")
	f.symlink("../../../../real/file", "/a/b/up")
	f.symlink("loop2", "/loop1")
	f.symlink("loop1", "/loop2")
	tests := map[string]string{
		"/abs/file":     "/real/file",
		"/a/b/up":       "/real/file",
		"/real/file":    "/real/file",
		"//real/./file": "/real/file",
	}
	for name, want := range tests {
		if got, err := f.host().resolve(name); err != nil || got != want {
			t.Errorf("resolve(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"/loop1", "/missing", "/real/file/below"} {
		if got, err := f.host().resolve(name); err == nil {
			t.Errorf("resolve(%q) = %q, want an error", name, got)
		}
	}
}

func TestDrainBlockedReasonOnlyOnUnmanagedDevices(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+"/home/extra                             file\t\t1048572\t\t524288\t\t-2\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/home/extra", "")
	f.write("/etc/fstab", "/swap.img none swap sw 0 0\n")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanDrain || got.DrainBlockedReason != apischema.SwapDrainUnmanagedDevice {
		t.Fatalf("can drain = %v, reason = %q; want false, unmanaged_device", got.CanDrain, got.DrainBlockedReason)
	}
}

// The fstab swap file counts as active when /proc/swaps holds the file it
// resolves to, not only its literal name.
func TestSwapFileReachedThroughALinkIsActive(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+"/data/swapfile                          file\t\t1048572\t\t0\t\t-2\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/data/swapfile", "")
	f.symlink("data/swapfile", "/swapfile")
	f.write("/etc/fstab", "/swapfile none swap sw 0 0\n")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if got.SwapFile == nil || got.SwapFile.Path != "/swapfile" || !got.SwapFile.Active {
		t.Fatalf("swap file = %+v, want /swapfile active", got.SwapFile)
	}
}
