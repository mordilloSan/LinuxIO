package memory

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFSKindOfMagic(t *testing.T) {
	for magic, want := range map[int64]string{
		unix.EXT4_SUPER_MAGIC:  "ext4",
		unix.XFS_SUPER_MAGIC:   "xfs",
		unix.BTRFS_SUPER_MAGIC: "btrfs",
		unix.TMPFS_MAGIC:       "",
		unix.NFS_SUPER_MAGIC:   "",
		0:                      "",
	} {
		if got := fsKindOfMagic(magic); got != want {
			t.Errorf("fsKindOfMagic(%#x) = %q, want %q", magic, got, want)
		}
	}
}

func TestReadFolderFS(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	var sfs unix.Statfs_t
	if err := unix.Statfs(b.root, &sfs); err != nil {
		t.Fatal(err)
	}
	got, err := readFolderFS(b.root)
	if err != nil {
		t.Fatal(err)
	}
	bsize := uint64(sfs.Bsize)
	if got.kind != "ext4" || got.readOnly || got.total != sfs.Blocks*bsize || got.free == 0 || got.free > got.total || got.btrfsLayout || got.subvolumeRoot {
		t.Fatalf("readFolderFS = %+v, want ext4 with the statfs sizes (%d blocks of %d)", got, sfs.Blocks, bsize)
	}

	b.kind, b.readOnly = "xfs", true
	if got, err = readFolderFS(b.root); err != nil || got.kind != "xfs" || !got.readOnly {
		t.Fatalf("read-only xfs: %+v, %v", got, err)
	}
	b.kind, b.readOnly = "tmpfs", false
	if got, err = readFolderFS(b.root); err != nil || got.kind != "" {
		t.Fatalf("tmpfs: %+v, %v; want no kind", got, err)
	}
}

func TestReadFolderFSOnBtrfs(t *testing.T) {
	b := newBlocksHost(t, "btrfs")
	b.subvolumes[b.path("/var/lib/linuxio")] = true
	got, err := readFolderFS(b.path("/var/lib/linuxio"))
	if err != nil || got.kind != "btrfs" || !got.btrfsLayout || !got.subvolumeRoot {
		t.Fatalf("subvolume on a btrfs of one device: %+v, %v", got, err)
	}
	b.layout = false
	if got, err = readFolderFS(b.path("/var/lib")); err != nil || got.btrfsLayout || got.subvolumeRoot {
		t.Fatalf("plain folder on a btrfs of two devices: %+v, %v", got, err)
	}
	b.layoutErr = unix.EPERM
	if _, err = readFolderFS(b.path("/var/lib")); !errors.Is(err, unix.EPERM) {
		t.Fatalf("err = %v, want the layout read's EPERM", err)
	}
}

func TestReadFolderFSReportsAMissingFolder(t *testing.T) {
	if _, err := readFolderFS(t.TempDir() + "/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// Without the fakes, on whatever filesystem holds the temporary folder: a
// btrfs one answers the layout ioctls for the user running the test.
func TestReadFolderFSOnTheRealKernel(t *testing.T) {
	dir := t.TempDir()
	got, err := readFolderFS(dir)
	if err != nil {
		t.Fatalf("readFolderFS(%s) as uid %d: %v", dir, os.Getuid(), err)
	}
	if got.total == 0 {
		t.Fatalf("readFolderFS(%s) = %+v, want its size", dir, got)
	}
	t.Logf("%s: %+v", dir, got)
}
