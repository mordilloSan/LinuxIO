package memory

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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

// The reader keeps only a swap line whose first field, unescaped, is an
// absolute path outside /dev, with the options of its first line and the
// number of lines that name it, in the order of the first one.
func TestFstabSwapsReadsPlainPaths(t *testing.T) {
	f := newFixture(t)
	f.write("/etc/fstab", `# comment
UUID=0a1b2c3d / btrfs defaults 0 1
/swap.img none swap sw 0 0
UUID=7e8f-aaaa none swap sw 0 0
LABEL=swap none swap sw 0 0
/dev/sda3 none swap sw 0 0
/dev/mapper/vg-swap none swap sw 0 0
/mnt/my\040disk/swapfile none swap sw,pri=5 0 0
none none swap sw 0 0
relative none swap sw 0 0
/swap.img none swap defaults,discard 0 0
/srv/swapfile none swap
/home /home ext4 defaults 0 2
#/old.img none swap sw 0 0
`)
	got, err := f.host().fstabSwaps()
	if err != nil {
		t.Fatal(err)
	}
	want := []fstabSwap{
		{path: "/swap.img", options: "sw"},
		{path: "/mnt/my disk/swapfile", options: "sw,pri=5"},
		{path: "/srv/swapfile"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("fstab swaps = %+v, want %+v", got, want)
	}
}

func TestFstabSwapsWithoutFstab(t *testing.T) {
	got, err := newFixture(t).host().fstabSwaps()
	if err != nil || len(got) != 0 {
		t.Fatalf("fstab swaps = %+v, %v, want none", got, err)
	}
}
