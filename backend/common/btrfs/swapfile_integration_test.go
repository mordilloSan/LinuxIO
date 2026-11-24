package btrfs

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// swapfile reads the swap file layout of the raid1 pool, which cannot hold
// one, and of a pool of one disk with single data, which can, and creates a
// subvolume through a folder's handle with CreateSubvolumeAt: a subvolume
// root of mode 0755 under the bridge's umask, and a second create at the
// name refused.
func (p *itPool) swapfile() {
	t := p.t
	top, err := os.Open(p.mnt)
	p.must("open top level", err)
	defer top.Close()
	ok, err := SwapfileLayout(top)
	p.must("read the layout of the raid1 pool", err)
	if ok {
		t.Fatal("a raid1 pool of two disks reads as able to hold a swap file")
	}

	dir := t.TempDir()
	disk := loopDevice(t, filepath.Join(dir, "single.img"), 1<<30)
	uuid, err := CreatePool(p.ctx, "linuxio-swapfile-it", "single", "dup", []string{disk}, true, nil, nil)
	p.must("create a pool of one disk", err)
	mnt := filepath.Join(dir, "mnt")
	p.must("mount the pool of one disk", MountPool(p.ctx, disk, mnt, nil, false))
	t.Cleanup(func() { _ = unix.Unmount(mnt, 0) })
	single, err := os.Open(mnt)
	p.must("open the pool of one disk", err)
	ok, err = SwapfileLayout(single)
	p.must("read the layout of the pool of one disk", err)
	if !ok {
		t.Fatalf("pool %s of one disk with single data reads as unable to hold a swap file", uuid)
	}

	oldUmask := unix.Umask(0o077)
	defer unix.Umask(oldUmask)
	p.must("create a subvolume through a handle", CreateSubvolumeAt(single, "swap"))
	var st unix.Stat_t
	p.must("stat the new subvolume", unix.Fstatat(int(single.Fd()), "swap", &st, unix.AT_SYMLINK_NOFOLLOW))
	if st.Ino != SubvolumeRootInode || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0o7777 != 0o755 {
		t.Fatalf("new subvolume: inode %d, mode %#o; want a subvolume root of mode 0755", st.Ino, st.Mode)
	}
	if err = CreateSubvolumeAt(single, "swap"); err == nil {
		t.Fatal("a second subvolume at a taken name was created")
	}
	p.must("close the pool of one disk", single.Close())
	p.must("unmount the pool of one disk", unix.Unmount(mnt, 0))
}
