package storage

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func TestSetCompressionKeepsOtherOptions(t *testing.T) {
	const header = "# test fstab\n"
	const other = "UUID=y /data ext4 defaults 0 2\n"
	three := 3
	for _, tc := range []struct {
		name, alg, force string
		option           string
		want             string
	}{
		{name: "replace level", alg: "zstd", option: "compress=zstd:3", want: "subvol=@,noatime,compress=zstd:3"},
		{name: "force replaces plain", alg: "zstd", force: "y", option: "compress-force=zstd:3", want: "subvol=@,noatime,compress-force=zstd:3"},
		{name: "none removes", alg: "none", option: "compress=no", want: "subvol=@,noatime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubCompressionMounts(t, nil)
			orig := header + "UUID=x / btrfs subvol=@,noatime,compress=zstd:1 0 0\n" + other
			if err := os.WriteFile(fstabPath, []byte(orig), 0o644); err != nil {
				t.Fatal(err)
			}
			btrfsSetCompression = func(context.Context, string, string, *int, bool) (string, error) {
				return tc.option, nil
			}
			err := setBtrfsCompression(context.Background(), apischema.BtrfsCompressionRequest{
				UUID: "x", Algorithm: tc.alg, Level: &three, Force: tc.force != "",
			})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(fstabPath)
			want := header + "UUID=x / btrfs " + tc.want + " 0 0\n" + other
			if string(got) != want {
				t.Fatalf("fstab = %q, want %q", got, want)
			}
		})
	}
}

// Ubuntu's installer leaves a comment naming the root mountpoint; it must stay
// a comment, and the 0 1 pass field and trailing comment must survive.
func TestSetCompressionPreservesCommentsAndPassField(t *testing.T) {
	stubCompressionMounts(t, nil)
	const pre = "# / was on /dev/nvme0n1p2 during installation\n"
	orig := pre + "UUID=x  /  btrfs  subvol=@,compress,noatime  0  1  # root\n"
	if err := os.WriteFile(fstabPath, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	btrfsSetCompression = func(context.Context, string, string, *int, bool) (string, error) {
		return "compress=no", nil
	}
	if err := setBtrfsCompression(context.Background(), apischema.BtrfsCompressionRequest{UUID: "x", Algorithm: "none"}); err != nil {
		t.Fatal(err)
	}
	want := pre + "UUID=x  /  btrfs  subvol=@,noatime  0  1  # root\n"
	if got, _ := os.ReadFile(fstabPath); string(got) != want {
		t.Fatalf("fstab = %q, want %q", got, want)
	}
	// Already without compression: nothing to write.
	if err := setBtrfsCompression(context.Background(), apischema.BtrfsCompressionRequest{UUID: "x", Algorithm: "none"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(fstabPath); string(got) != want {
		t.Fatalf("fstab rewritten: %q", got)
	}
}

// Compression is filesystem-wide: every fstab entry of the filesystem is
// rewritten, whether matched by mountpoint or by UUID= source, and entries of
// other filesystems are left alone.
func TestSetCompressionRewritesEveryFstabEntryOfFilesystem(t *testing.T) {
	stubCompressionMounts(t, []btrfs.Mount{
		{Mountpoint: "/", UUID: "x", Subvol: "@"},
		{Mountpoint: "/home", UUID: "x", Subvol: "@home"},
		{Mountpoint: "/data", UUID: "y"},
	})
	orig := "UUID=x / btrfs subvol=@,compress=zstd:1 0 0\n" +
		"/dev/sda2 /home btrfs subvol=@home 0 0\n" +
		"UUID=X /srv btrfs subvol=@srv,noauto 0 0\n" +
		"UUID=y /data btrfs compress=zstd:1 0 0\n"
	if err := os.WriteFile(fstabPath, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	btrfsSetCompression = func(context.Context, string, string, *int, bool) (string, error) {
		return "compress=lzo", nil
	}
	if err := setBtrfsCompression(context.Background(), apischema.BtrfsCompressionRequest{UUID: "x", Algorithm: "lzo"}); err != nil {
		t.Fatal(err)
	}
	want := "UUID=x / btrfs subvol=@,compress=lzo 0 0\n" +
		"/dev/sda2 /home btrfs subvol=@home,compress=lzo 0 0\n" +
		"UUID=X /srv btrfs subvol=@srv,noauto,compress=lzo 0 0\n" +
		"UUID=y /data btrfs compress=zstd:1 0 0\n"
	if got, _ := os.ReadFile(fstabPath); string(got) != want {
		t.Fatalf("fstab = %q, want %q", got, want)
	}
}

func TestCompressionPersisted(t *testing.T) {
	lzo := btrfs.BtrfsCompression{Algorithm: "lzo"}
	both := map[string]fstabEntry{"/": {options: "compress=lzo"}, "/home": {options: "subvol=@home,compress=lzo"}}
	if !compressionPersisted(both, lzo) {
		t.Error("all entries carry lzo: want persisted")
	}
	both["/home"] = fstabEntry{options: "subvol=@home"}
	if compressionPersisted(both, lzo) {
		t.Error("one entry lacks lzo: want not persisted")
	}
	if compressionPersisted(map[string]fstabEntry{}, btrfs.BtrfsCompression{Algorithm: "none"}) {
		t.Error("no fstab entries: want not persisted")
	}
}

// stubCompressionMounts points fstab at a temp file and stubs mount discovery
// and the remount.
func stubCompressionMounts(t *testing.T, mounts []btrfs.Mount) {
	t.Helper()
	oldPath, oldSet, oldList := fstabPath, btrfsSetCompression, btrfsListMounts
	t.Cleanup(func() { fstabPath, btrfsSetCompression, btrfsListMounts = oldPath, oldSet, oldList })
	fstabPath = filepath.Join(t.TempDir(), "fstab")
	btrfsListMounts = func(context.Context) ([]btrfs.Mount, error) { return mounts, nil }
}

func TestReplaceFstabOption(t *testing.T) {
	opts := []string{"subvol=@", "compress=zstd:1", "noatime"}
	if got := replaceFstabOption(opts, "compress=", "compress=lzo"); !reflect.DeepEqual(got, []string{"subvol=@", "noatime", "compress=lzo"}) {
		t.Fatalf("got %v", got)
	}
	if got := replaceFstabOption(opts, "compress-force=", ""); !reflect.DeepEqual(got, opts) {
		t.Fatalf("got %v", got)
	}
	if opts[1] != "compress=zstd:1" {
		t.Fatal("input mutated")
	}
}

func TestFstabHasCompression(t *testing.T) {
	five := 5
	for _, tc := range []struct {
		options string
		mount   btrfs.BtrfsCompression
		want    bool
	}{
		{"noatime,compress=zstd", btrfs.BtrfsCompression{Algorithm: "zstd", Level: &five}, true},
		{"compress=zstd:3", btrfs.BtrfsCompression{Algorithm: "zstd", Level: &five}, false},
		{"compress-force=zstd:5", btrfs.BtrfsCompression{Algorithm: "zstd", Level: &five}, false},
		{"compress-force=zstd:5", btrfs.BtrfsCompression{Algorithm: "zstd", Level: &five, Force: true}, true},
		{"noatime", btrfs.BtrfsCompression{Algorithm: "none"}, true},
		{"compress=no", btrfs.BtrfsCompression{Algorithm: "none"}, true},
		{"compress", btrfs.BtrfsCompression{Algorithm: "zlib"}, true},
		{"compress-force", btrfs.BtrfsCompression{Algorithm: "zlib", Force: true}, true},
		{"compress", btrfs.BtrfsCompression{Algorithm: "none"}, false},
		{"compress=lzo", btrfs.BtrfsCompression{Algorithm: "none"}, false},
		{"noatime", btrfs.BtrfsCompression{Algorithm: "lzo"}, false},
	} {
		if got := fstabHasCompression(fstabEntry{options: tc.options}, tc.mount); got != tc.want {
			t.Errorf("fstabHasCompression(%q, %+v) = %v, want %v", tc.options, tc.mount, got, tc.want)
		}
	}
}
