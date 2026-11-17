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

// A swap file is one LinuxIO manages only when nobody but the root's owner
// can change a folder on the path fstab names or on the path it swaps at.
func TestMatchFstabSwapsMarksASwapFileOthersCanRedirect(t *testing.T) {
	f := newFixture(t)
	f.write("/swap.img", "")
	f.write("/var/swap/file", "")
	f.othersCanWrite("/srv/open")
	f.write("/srv/open/swapfile", "")
	f.write("/srv/open/below/swapfile", "")
	f.write("/srv/open/target", "")
	f.symlink("/srv/open/target", "/swaplink")
	f.othersCanWrite("/dev/shm")
	f.write("/dev/shm/part", "")
	if err := os.Chmod(filepath.Join(f.root, "/var/swap"), 0o775); err != nil {
		t.Fatal(err)
	}
	entries := []fstabSwap{
		{path: "/swap.img"},
		{path: "/var/swap/file"},
		{path: "/srv/open/swapfile"},
		{path: "/srv/open/below/swapfile"},
		{path: "/swaplink"},
		{path: "/dev/shm/part"},
		{path: "/missing/swapfile"},
	}
	devices := []apischema.SwapDevice{{Path: "/srv/open/target", Kind: apischema.SwapDeviceFile}}

	got := f.host().matchFstabSwaps(entries, devices)

	want := map[string]bool{
		"/swap.img":                false,
		"/var/swap/file":           true, // its folder is group-writable
		"/srv/open/swapfile":       true,
		"/srv/open/below/swapfile": true,
		"/swaplink":                true,  // swaps at a path in an open folder
		"/dev/shm/part":            true,  // the rule holds for a device node too
		"/missing/swapfile":        false, // nothing there to redirect
	}
	for _, entry := range got {
		if entry.othersCanWrite != want[entry.path] {
			t.Errorf("%s: others can write = %v, want %v", entry.path, entry.othersCanWrite, want[entry.path])
		}
	}
}

// A swap file that is off is judged by where its path leads as well: a
// start would have systemd swap on the file the link names.
func TestMatchFstabSwapsFollowsALinkOfAFileThatIsOff(t *testing.T) {
	f := newFixture(t)
	f.othersCanWrite("/srv/open")
	f.write("/srv/open/target", "")
	f.write("/var/swap/target", "")
	f.symlink("/srv/open/target", "/openlink")
	f.symlink("/srv/open/gone", "/danglinglink")
	f.symlink("/var/swap/target", "/safelink")

	got := f.host().matchFstabSwaps([]fstabSwap{{path: "/openlink"}, {path: "/danglinglink"}, {path: "/safelink"}}, nil)

	if !got[0].othersCanWrite || !got[1].othersCanWrite || got[2].othersCanWrite {
		t.Fatalf("entries = %+v, want only the link into a folder root alone writes managed", got)
	}
}

// Every link on the way is judged where it sits: one in a folder others can
// write could be pointed elsewhere after the check, whatever it names now.
func TestMatchFstabSwapsJudgesEveryLinkOnTheWay(t *testing.T) {
	f := newFixture(t)
	f.othersCanWrite("/srv/open")
	f.write("/var/swap/target", "")
	f.symlink("/var/swap/target", "/srv/open/hop")
	f.symlink("/srv/open/hop", "/swaplink")
	devices := []apischema.SwapDevice{{Path: "/var/swap/target", Kind: apischema.SwapDeviceFile}}

	off := f.host().matchFstabSwaps([]fstabSwap{{path: "/swaplink"}}, nil)
	on := f.host().matchFstabSwaps([]fstabSwap{{path: "/swaplink"}}, devices)

	if !off[0].othersCanWrite || !on[0].othersCanWrite || on[0].device != "/var/swap/target" {
		t.Fatalf("off = %+v, on = %+v; want both judged by the link in /srv/open", off[0], on[0])
	}
}

// A link target that passes through a folder and leaves it by ".." is
// judged at that folder too: a link put there would lead the rest of the
// way elsewhere.
func TestMatchFstabSwapsJudgesAFolderALinkLeavesByDotDot(t *testing.T) {
	f := newFixture(t)
	f.othersCanWrite("/srv/open")
	f.mkdir("/srv/open/through")
	f.mkdir("/var/lib/through")
	f.write("/var/swap/target", "")
	f.symlink("/srv/open/through/../../../var/swap/target", "/openlink")
	f.symlink("/var/lib/through/../../swap/target", "/safelink")

	got := f.host().matchFstabSwaps([]fstabSwap{{path: "/openlink"}, {path: "/safelink"}}, nil)

	if !got[0].othersCanWrite || got[1].othersCanWrite {
		t.Fatalf("entries = %+v, want only the link through /srv/open judged as one others can redirect", got)
	}
}

// The status is read without privilege: a folder the reader may not look
// into is not judged, since root, which makes the changes, can look into
// every folder.
func TestMatchFstabSwapsDoesNotJudgeAFolderItMayNotRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can look into every folder")
	}
	f := newFixture(t)
	f.write("/root/swap/file", "")
	if err := os.Chmod(filepath.Join(f.root, "/root"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(f.root, "/root"), 0o755) })

	got := f.host().matchFstabSwaps([]fstabSwap{{path: "/root/swap/file"}}, nil)

	if got[0].othersCanWrite {
		t.Fatalf("entry = %+v, want a folder that cannot be read left unjudged", got[0])
	}
}
