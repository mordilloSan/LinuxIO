package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

const testSwapSize = 4 * mib

func TestMakeSwapFileInLinuxIOsFolder(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	oldUmask := unix.Umask(0o077)
	t.Cleanup(func() { unix.Umask(oldUmask) })

	id, err := b.live().makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
	if err != nil {
		t.Fatal(err)
	}
	if dir := b.stat(ownSwapDir); dir.Mode&0o7777 != 0o755 || dir.Mode&unix.S_IFMT != unix.S_IFDIR {
		t.Errorf("%s: mode %#o, want a folder of mode 0755", ownSwapDir, dir.Mode)
	}
	st := b.stat(ownSwapFilePath)
	if st.Mode&0o7777 != 0o600 || st.Size != testSwapSize || st.Blocks*512 < testSwapSize {
		t.Errorf("swap file: mode %#o, size %d, %d bytes allocated; want 0600 and %d allocated", st.Mode&0o7777, st.Size, st.Blocks*512, testSwapSize)
	}
	if id != fileIDOf(&st) {
		t.Errorf("id = %+v, want the file's %+v", id, fileIDOf(&st))
	}
	if want := []string{"mkswap -- " + b.path(ownSwapFilePath)}; !slices.Equal(b.ran(), want) {
		t.Errorf("ran %q, want %q", b.ran(), want)
	}
	if len(b.subvolumes) != 0 || len(b.flags) != 0 {
		t.Errorf("subvolumes %v, flags %v: want none on ext4", b.subvolumes, b.flags)
	}
}

// The folder exists, as LinuxIO's from an earlier file, with something in
// it: on ext4 and xfs that is no matter.
func TestMakeSwapFileUsesTheFolderThatIsThere(t *testing.T) {
	b := newBlocksHost(t, "xfs")
	b.setMountType("xfs")
	b.mkdir(ownSwapDir, 0o700)
	b.write(ownSwapDir+"/other", "kept")
	if _, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize); err != nil {
		t.Fatal(err)
	}
	if b.stat(ownSwapDir).Mode&0o7777 != 0o755 || b.read(ownSwapDir+"/other") != "kept" {
		t.Fatal("the folder was not set 0755, or lost what it held")
	}
}

func TestMakeSwapFileOnBtrfs(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(b *blocksHost)
		made  bool
	}{
		"a new subvolume": {func(*blocksHost) {}, true},
		"the subvolume there": {func(b *blocksHost) {
			b.mkdir(ownSwapDir, 0o755)
			b.subvolumes[b.path(ownSwapDir)] = true
		}, false},
		"in place of an empty plain folder": {func(b *blocksHost) { b.mkdir(ownSwapDir, 0o755) }, true},
		// A new file inherits compression from a folder that has it.
		"in a compressed folder": {func(b *blocksHost) { b.inherit = fsComprFl }, true},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "btrfs")
			c.setup(b)
			if _, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize); err != nil {
				t.Fatal(err)
			}
			if !b.subvolumes[b.path(ownSwapDir)] {
				t.Fatalf("%s is not a subvolume", ownSwapDir)
			}
			flags := b.flags[b.stat(ownSwapFilePath).Ino]
			if flags&fsNoCowFl == 0 || flags&fsNoCompFl == 0 || flags&fsComprFl != 0 {
				t.Fatalf("flags = %#x, want NOCOW and NOCOMP without COMPR", flags)
			}
			// Only a subvolume made here is given its folder's SELinux context.
			if want := []string{b.path(ownSwapDir)}; c.made != slices.Equal(b.labelled, want) || (!c.made && len(b.labelled) != 0) {
				t.Fatalf("labelled %q, want the new subvolume labelled: %v", b.labelled, c.made)
			}
		})
	}
}

func TestMakeSwapFileRefusesAPlainFolderWithFilesOnBtrfs(t *testing.T) {
	b := newBlocksHost(t, "btrfs")
	b.mkdir(ownSwapDir, 0o755)
	b.write(ownSwapDir+"/notes", "kept")
	_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
	isRefusal(t, err, errSwapNameTaken)
	if b.read(ownSwapDir+"/notes") != "kept" || b.exists(ownSwapFilePath) || len(b.ran()) != 0 {
		t.Fatal("the folder was changed")
	}
}

func TestMakeSwapFileRefusesWhatIsAtTheFoldersName(t *testing.T) {
	for name, setup := range map[string]func(b *blocksHost){
		"a file": func(b *blocksHost) { b.write(ownSwapDir, "") },
		"a link to a folder": func(b *blocksHost) {
			b.mkdir("/elsewhere", 0o755)
			if err := os.Symlink(b.path("/elsewhere"), b.path(ownSwapDir)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "ext4")
			setup(b)
			_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
			isRefusal(t, err, errSwapNameTaken)
			if b.exists("/elsewhere/swapfile") {
				t.Fatal("a file was made where the link points")
			}
		})
	}
	t.Run("a folder others can write", func(t *testing.T) {
		b := newBlocksHost(t, "ext4")
		b.mkdir(ownSwapDir, 0o777)
		_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
		isRefusal(t, err, errOthersCanRedirect)
		if b.exists(ownSwapFilePath) {
			t.Fatal("a file was made")
		}
	})
}

func TestMakeSwapFileRefusesTheFilesystem(t *testing.T) {
	for name, c := range map[string]struct {
		setup  func(b *blocksHost)
		reason error
	}{
		"tmpfs":     {func(b *blocksHost) { b.kind = "tmpfs"; b.setMountType("tmpfs") }, errUnsupportedFilesystem},
		"ext3":      {func(b *blocksHost) { b.setMountType("ext3") }, errUnsupportedFilesystem},
		"no mount":  {func(b *blocksHost) { b.write(procMountinfo, "") }, errUnsupportedFilesystem},
		"read-only": {func(b *blocksHost) { b.readOnly = true }, errReadOnlyFilesystem},
		"btrfs of two devices": {func(b *blocksHost) {
			b.kind, b.layout = "btrfs", false
			b.setMountType("btrfs")
		}, errBtrfsLayout},
		// mountinfo names a path, statfs the folder the handle is on.
		"mountinfo and statfs disagree": {func(b *blocksHost) { b.setMountType("xfs") }, errUnsupportedFilesystem},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "ext4")
			c.setup(b)
			_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
			isRefusal(t, err, c.reason)
			if b.exists(ownSwapDir) || len(b.ran()) != 0 {
				t.Fatal("the folder was made, or a command run, before the filesystem was checked")
			}
		})
	}
}

func TestMakeSwapFileRefusesATakenName(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.mkdir(ownSwapDir, 0o755)
	b.write(ownSwapFilePath, "kept")
	_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
	isRefusal(t, err, errSwapNameTaken)
	if b.read(ownSwapFilePath) != "kept" {
		t.Fatal("the file at the name was changed")
	}
	if err = os.Symlink("/etc/shadow", b.path("/var/lib/linuxio/link")); err != nil {
		t.Fatal(err)
	}
	_, err = b.h.makeSwapFile(t.Context(), "/var/lib/linuxio/link", testSwapSize)
	isRefusal(t, err, errSwapNameTaken)
}

func TestMakeSwapFileBesideAnotherFile(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.write("/swap.img", "")
	id, err := b.h.makeSwapFile(t.Context(), "/swap2.img", testSwapSize)
	if err != nil {
		t.Fatal(err)
	}
	if st := b.stat("/swap2.img"); fileIDOf(&st) != id || st.Size != testSwapSize {
		t.Fatalf("file %+v, id %+v", st, id)
	}
	if b.exists(ownSwapDir) {
		t.Fatal("LinuxIO's folder was made for a file elsewhere")
	}
	b.mkdir("/shared", 0o777)
	_, err = b.h.makeSwapFile(t.Context(), "/shared/swapfile", testSwapSize)
	isRefusal(t, err, errOthersCanRedirect)
	if b.exists("/shared/swapfile") {
		t.Fatal("a file was made in a folder others can write")
	}
}

// Every failure after the create removes the file, and the folder stays.
func TestMakeSwapFileRemovesTheFileWhenAStepFails(t *testing.T) {
	for name, c := range map[string]struct {
		kind  string
		setup func(b *blocksHost)
		check func(t *testing.T, err error)
	}{
		"no room": {"ext4", func(b *blocksHost) { b.noSpace = true }, func(t *testing.T, err error) {
			isRefusal(t, err, errNoSpaceForSwap)
			if !errors.Is(err, unix.ENOSPC) {
				t.Fatalf("err = %v, want ENOSPC in it", err)
			}
		}},
		"mkswap fails": {"ext4", func(b *blocksHost) { b.fail(mkswapCommand, 1) }, func(t *testing.T, err error) {
			if err == nil || !strings.Contains(err.Error(), "mkswap: something went wrong") {
				t.Fatalf("err = %v, want what mkswap printed", err)
			}
		}},
		"btrfs keeps copy-on-write": {"btrfs", func(b *blocksHost) { b.dropNoCow = true }, func(t *testing.T, err error) {
			isRefusal(t, err, errNoCow)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, c.kind)
			c.setup(b)
			_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
			c.check(t, err)
			if b.exists(ownSwapFilePath) {
				t.Fatal("the file was left")
			}
			if !b.exists(ownSwapDir) {
				t.Fatal("the folder was removed")
			}
		})
	}
}

// A file at the name that is not the one made is not removed, and the error
// says the name is left: mkswap here puts a new file at the path, as only
// root could in the folder, and then fails.
func TestMakeSwapFileSaysWhenTheFileIsLeft(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	script := "#!/bin/sh\nPATH=/usr/bin:/bin\nfor last; do :; done\nrm -f \"$last\" && : > \"$last\"\necho 'mkswap: failed' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(b.tools, mkswapCommand), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, testSwapSize)
	if err == nil || !strings.Contains(err.Error(), "mkswap: failed") || !strings.Contains(err.Error(), "is left") || !errors.Is(err, errSwapFileChanged) {
		t.Fatalf("err = %v, want mkswap's failure and that the file is left", err)
	}
	if !b.exists(ownSwapFilePath) {
		t.Fatal("the file another put at the name was removed")
	}
}

// The temporary file of a resize is made beside the file it stands in for,
// in the same root-only folder.
func TestMakeSwapFileForAResize(t *testing.T) {
	b := newBlocksHost(t, "btrfs")
	b.madeSwapFile(ownSwapFilePath)
	temporary := ownSwapFilePath + resizeSuffix
	if _, err := b.h.makeSwapFile(t.Context(), temporary, testSwapSize); err != nil {
		t.Fatal(err)
	}
	if st := b.stat(temporary); st.Size != testSwapSize || b.flags[st.Ino]&fsNoCowFl == 0 {
		t.Fatalf("%s: size %d, flags %#x", temporary, st.Size, b.flags[st.Ino])
	}
}

func TestMakeSwapFileRefusesBadInput(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	if _, err := b.h.makeSwapFile(t.Context(), "swapfile", testSwapSize); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("relative path: %v", err)
	}
	if _, err := b.h.makeSwapFile(t.Context(), ownSwapFilePath, 0); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("size 0: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.h.makeSwapFile(ctx, ownSwapFilePath, testSwapSize); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if b.exists(ownSwapDir) {
		t.Fatal("the folder was made")
	}
}

// Without SELinux, as on the machine running the unit tests, a folder has no
// context to copy and nothing is set.
func TestLabelLikeParentWithoutAContext(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "swap"), 0o755); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	dir, err := os.Open(filepath.Join(root, "swap"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	var label [256]byte
	if _, err := unix.Fgetxattr(int(parent.Fd()), selinuxXattr, label[:]); err == nil {
		t.Skip("this machine labels its files for SELinux")
	}
	if err := labelLikeParent(parent, dir); err != nil {
		t.Fatal(err)
	}
}
