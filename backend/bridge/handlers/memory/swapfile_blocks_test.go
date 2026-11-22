package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// The request numbers and flags are linux/fs.h's, as strace prints them on a
// 64-bit kernel: the request encodes sizeof(long), though the kernel reads
// and writes an int.
func TestInodeFlagsMatchKernel(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the request numbers below are a 64-bit kernel's")
	}
	for name, c := range map[string]struct{ got, want uint64 }{
		"FS_IOC_GETFLAGS": {unix.FS_IOC_GETFLAGS, 0x80086601},
		"FS_IOC_SETFLAGS": {unix.FS_IOC_SETFLAGS, 0x40086602},
		"FS_COMPR_FL":     {fsComprFl, 0x4},
		"FS_NOCOMP_FL":    {fsNoCompFl, 0x400},
		"FS_NOCOW_FL":     {fsNoCowFl, 0x00800000},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", name, c.got, c.want)
		}
	}
}

func TestFSMagicNumbersMatchKernel(t *testing.T) {
	for name, c := range map[string]struct{ got, want int64 }{
		"EXT4_SUPER_MAGIC":  {unix.EXT4_SUPER_MAGIC, 0xef53},
		"XFS_SUPER_MAGIC":   {unix.XFS_SUPER_MAGIC, 0x58465342},
		"BTRFS_SUPER_MAGIC": {unix.BTRFS_SUPER_MAGIC, 0x9123683e},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", name, c.got, c.want)
		}
	}
}

// blocksHost is a fixture host root for the swap file blocks, owned by the
// user running the test as / is by root: /var/lib/linuxio, /etc/fstab,
// /proc/swaps with no device, and a mountinfo with / on kind. The kernel
// calls that need a real filesystem of that kind, and mkswap and swapon, are
// stood in for; tools logs the commands run.
type blocksHost struct {
	t    *testing.T
	root string
	h    hostRoot

	mu         sync.Mutex
	kind       string // what statfs and mountinfo say the filesystem is
	readOnly   bool
	layout     bool
	layoutErr  error
	subvolumes map[string]bool   // folders, by real path, that are subvolume roots
	flags      map[uint64]uint32 // inode flags by inode number
	dropNoCow  bool              // FS_IOC_SETFLAGS takes every flag but NOCOW
	noSpace    bool              // fallocate fails with ENOSPC
	tools      string            // the folder of the fake commands
	labelled   []string          // folders given their parent's SELinux context, by real path
	inherit    uint32            // the flags a file has before any are set, as from its folder
}

func newBlocksHost(t *testing.T, kind string) *blocksHost {
	t.Helper()
	b := &blocksHost{
		t: t, root: t.TempDir(), kind: kind, layout: true,
		subvolumes: map[string]bool{}, flags: map[uint64]uint32{},
	}
	b.h = hostRoot{root: b.root, owner: uint32(os.Getuid())}
	for _, dir := range []string{"/", "/var", "/var/lib", "/var/lib/linuxio", "/etc", "/proc", "/proc/self"} {
		b.mkdir(dir, 0o755)
	}
	b.write(procSwaps, swapsHeader)
	b.write(etcFstab, "")
	b.setMountType(kind)
	b.fakeKernel()
	b.fakeTools()
	return b
}

// fakeKernel stands in for the kernel calls that need a filesystem of b's
// kind, and keeps the fstab lock in a folder of the test's own.
func (b *blocksHost) fakeKernel() {
	t := b.t
	oldStatfs, oldLayout, oldSubvol, oldCreate, oldFlags, oldFallocate, oldLock, oldLabel := statfsFD, swapfileLayout, isSubvolumeRoot, createSubvolume, inodeFlagsIoctl, fallocateFile, fstabEditLock, labelLikeParent
	t.Cleanup(func() {
		statfsFD, swapfileLayout, isSubvolumeRoot, createSubvolume, inodeFlagsIoctl, fallocateFile, fstabEditLock, labelLikeParent = oldStatfs, oldLayout, oldSubvol, oldCreate, oldFlags, oldFallocate, oldLock, oldLabel
	})
	labelLikeParent = func(parent, dir *os.File) error {
		if want := filepath.Dir(fdPath(t, int(dir.Fd()))); fdPath(t, int(parent.Fd())) != want {
			t.Errorf("labelled %s like %s, not its own folder", fdPath(t, int(dir.Fd())), fdPath(t, int(parent.Fd())))
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		b.labelled = append(b.labelled, fdPath(t, int(dir.Fd())))
		return nil
	}
	fstabEditLock = filepath.Join(t.TempDir(), "fstab.lock")
	statfsFD = func(fd int, sfs *unix.Statfs_t) error {
		if err := unix.Fstatfs(fd, sfs); err != nil {
			return err
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		sfs.Type = map[string]int64{"ext4": unix.EXT4_SUPER_MAGIC, "xfs": unix.XFS_SUPER_MAGIC, "btrfs": unix.BTRFS_SUPER_MAGIC}[b.kind]
		if sfs.Type == 0 {
			sfs.Type = unix.TMPFS_MAGIC
		}
		if b.readOnly {
			sfs.Flags |= unix.ST_RDONLY
		}
		return nil
	}
	swapfileLayout = func(*os.File) (bool, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.layout, b.layoutErr
	}
	isSubvolumeRoot = func(dir *os.File) (bool, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.subvolumes[fdPath(t, int(dir.Fd()))], nil
	}
	createSubvolume = func(dir *os.File, name string) error {
		if err := unix.Mkdirat(int(dir.Fd()), name, 0o755); err != nil {
			return err
		}
		path := filepath.Join(fdPath(t, int(dir.Fd())), name)
		b.mu.Lock()
		b.subvolumes[path] = true
		b.mu.Unlock()
		return os.Chmod(path, 0o755)
	}
	inodeFlagsIoctl = b.inodeFlags
	fallocateFile = func(fd int, size int64) error {
		b.mu.Lock()
		noSpace := b.noSpace
		b.mu.Unlock()
		if noSpace {
			// A filesystem that runs out part way has grown the file by what
			// it found.
			var st unix.Stat_t
			if err := unix.Fstat(fd, &st); err == nil && st.Size < size {
				_ = unix.Ftruncate(fd, st.Size+(size-st.Size)/2)
			}
			return unix.ENOSPC
		}
		return unix.Fallocate(fd, 0, 0, size)
	}
}

// inodeFlags answers FS_IOC_GETFLAGS and FS_IOC_SETFLAGS as btrfs does.
func (b *blocksHost) inodeFlags(fd int, req uintptr, flags *uint32) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	old, ok := b.flags[st.Ino]
	if !ok {
		old = b.inherit
	}
	switch req {
	case unix.FS_IOC_GETFLAGS:
		*flags = old
	case unix.FS_IOC_SETFLAGS:
		// btrfs refuses a change of NOCOW while the old flags hold COMPR
		// or NOCOMP, and on a file with data keeps NOCOW as it was.
		if (old^*flags)&fsNoCowFl != 0 && old&(fsComprFl|fsNoCompFl) != 0 {
			return unix.EINVAL
		}
		b.flags[st.Ino] = *flags
		if b.dropNoCow || st.Size > 0 {
			b.flags[st.Ino] = b.flags[st.Ino]&^fsNoCowFl | old&fsNoCowFl
		}
	default:
		return fmt.Errorf("unexpected request %#x", req)
	}
	return nil
}

func fdPath(t *testing.T, fd int) string {
	t.Helper()
	path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeTools puts mkswap and swapon scripts alone on the command search path.
// Each logs its arguments, and the length of the file it is given after
// "--" in the tools folder's lengths, and fails as often as the file
// <name>.fails in the tools folder says, printing why.
func (b *blocksHost) fakeTools() {
	b.tools = b.t.TempDir()
	for _, name := range []string{mkswapCommand, swaponCommand} {
		script := fmt.Sprintf(`#!/bin/sh
PATH=/usr/bin:/bin
tools=%q
printf '%%s\n' "%s $*" >> "$tools/log"
if [ "$1" = "--" ] && [ -f "$2" ]; then stat -c %%s "$2" >> "$tools/lengths"; fi
n=$(cat "$tools/%s.fails" 2>/dev/null || echo 0)
if [ "$n" -gt 0 ]; then
	echo $((n - 1)) > "$tools/%s.fails"
	echo "%s: something went wrong" >&2
	exit 1
fi
`, b.tools, name, name, name, name)
		if err := os.WriteFile(filepath.Join(b.tools, name), []byte(script), 0o700); err != nil {
			b.t.Fatal(err)
		}
	}
	b.t.Setenv("PATH", b.tools)
	saved := hostcmd.SearchDirs
	hostcmd.SearchDirs = nil
	b.t.Cleanup(func() { hostcmd.SearchDirs = saved })
}

// fail makes the next n runs of the command name fail.
func (b *blocksHost) fail(name string, n int) {
	b.t.Helper()
	if err := os.WriteFile(filepath.Join(b.tools, name+".fails"), fmt.Appendf(nil, "%d\n", n), 0o600); err != nil {
		b.t.Fatal(err)
	}
}

// ran returns the commands run so far, one line each.
func (b *blocksHost) ran() []string {
	data, err := os.ReadFile(filepath.Join(b.tools, "log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		b.t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func (b *blocksHost) path(name string) string { return filepath.Join(b.root, name) }

func (b *blocksHost) mkdir(name string, mode os.FileMode) {
	b.t.Helper()
	if err := os.MkdirAll(b.path(name), mode); err != nil {
		b.t.Fatal(err)
	}
	if err := os.Chmod(b.path(name), mode); err != nil {
		b.t.Fatal(err)
	}
}

func (b *blocksHost) write(name, content string) {
	b.t.Helper()
	if err := os.WriteFile(b.path(name), []byte(content), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *blocksHost) read(name string) string {
	b.t.Helper()
	data, err := os.ReadFile(b.path(name))
	if err != nil {
		b.t.Fatal(err)
	}
	return string(data)
}

// setMountType has mountinfo say that / is a filesystem of type fsType.
func (b *blocksHost) setMountType(fsType string) {
	b.write(procMountinfo, "22 1 0:21 / / rw,relatime shared:1 - "+fsType+" /dev/vda1 rw\n")
}

// swapping lists path in /proc/swaps.
func (b *blocksHost) swapping(path string) {
	b.write(procSwaps, swapsHeader+path+"\tfile\t\t1048572\t\t0\t\t-2\n")
}

func (b *blocksHost) stat(name string) unix.Stat_t {
	b.t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(b.path(name), &st); err != nil {
		b.t.Fatal(err)
	}
	return st
}

func (b *blocksHost) exists(name string) bool {
	_, err := os.Lstat(b.path(name))
	return err == nil
}

// live is the host system the blocks are called through.
func (b *blocksHost) live() liveSystem { return liveSystem{root: b.h} }

// isRefusal checks that err is a 409 for reason.
func isRefusal(t *testing.T, err, reason error) {
	t.Helper()
	if !errors.Is(err, bridgeipc.ErrConflict) || !errors.Is(err, reason) {
		t.Fatalf("err = %v, want a conflict for %v", err, reason)
	}
}

func TestCheckSwapFilePathAndSize(t *testing.T) {
	for _, path := range []string{"", "swapfile", "/", "/a/../b", "/a/", "/a//b", "/a\x00b"} {
		if err := checkSwapFilePath(path); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("checkSwapFilePath(%q) = %v, want ErrInvalidArgs", path, err)
		}
	}
	if err := checkSwapFilePath("/var/lib/linuxio/swap/swapfile"); err != nil {
		t.Errorf("own path: %v", err)
	}
	for _, size := range []uint64{0, 1 << 63} {
		if err := checkSwapFileSize(size); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("checkSwapFileSize(%d) = %v, want ErrInvalidArgs", size, err)
		}
	}
}

func TestSetNoCowClearsCompressionAndReadsBack(t *testing.T) {
	b := newBlocksHost(t, "btrfs")
	f, err := os.Create(b.path("/file"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ino := b.stat("/file").Ino
	for _, inherited := range []uint32{0, fsComprFl, fsNoCompFl, fsComprFl | fsNoCompFl} {
		// 0x20, FS_APPEND_FL, stands for the flags that are left as they are.
		b.flags[ino] = inherited | 0x20
		if err := setNoCow(int(f.Fd())); err != nil {
			t.Fatalf("inherited %#x: %v", inherited, err)
		}
		if got, want := b.flags[ino], uint32(fsNoCowFl|fsNoCompFl|0x20); got != want {
			t.Fatalf("inherited %#x: flags = %#x, want %#x", inherited, got, want)
		}
	}
	b.flags[ino] = 0
	b.dropNoCow = true
	if err := setNoCow(int(f.Fd())); !errors.Is(err, errNoCow) || !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("NOCOW not kept: err = %v, want errNoCow", err)
	}
	// A file with data takes NOCOW without an error and does not keep it.
	b.flags[ino], b.dropNoCow = 0, false
	if _, err := f.WriteString("data"); err != nil {
		t.Fatal(err)
	}
	if err := setNoCow(int(f.Fd())); !errors.Is(err, errNoCow) {
		t.Fatalf("file with data: err = %v, want errNoCow", err)
	}
}

// The kernel copies a 4-byte int through the flags pointer.
func TestInodeFlagsAreFourBytes(t *testing.T) {
	var flags uint32
	if unsafe.Sizeof(flags) != 4 {
		t.Fatal("the inode flags are not 4 bytes")
	}
	var seen uintptr
	old := inodeFlagsIoctl
	t.Cleanup(func() { inodeFlagsIoctl = old })
	inodeFlagsIoctl = func(_ int, _ uintptr, p *uint32) error {
		seen = unsafe.Sizeof(*p)
		return nil
	}
	_ = checkNoCow(0)
	if seen != 4 {
		t.Fatalf("FS_IOC_GETFLAGS was given %d bytes, want 4", seen)
	}
}
