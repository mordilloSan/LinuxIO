package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// The sizes the root test makes, grows to and shrinks to.
const (
	blocksITSize   = 256 * mib
	blocksITGrown  = 384 * mib
	blocksITShrunk = 320 * mib
)

// TestSwapFileBlocksIntegration drives the swap file blocks on the real
// host: it makes a swap file, adds its fstab line, turns it on, turns it
// off, grows it, shrinks it, turning it on and off after each, removes its
// fstab line and deletes it, checking /proc/swaps and /etc/fstab at each
// step. It does that in LinuxIO's own folder (a subvolume on btrfs, labelled
// like /var/lib/linuxio where SELinux is on), beside the files in /, and on
// btrfs in a folder whose new files inherit compression (chattr +c), and
// stops a shrink between its two steps (blocksITShrinkHalves). It needs root and leaves the host as it found it, so it runs in the
// disposable test VM.
func TestSwapFileBlocksIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_SWAPFILE_BLOCKS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_SWAPFILE_BLOCKS_INTEGRATION=1 and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the swap file blocks test needs root")
	}
	stateDir := blocksITPrepare(t)

	t.Run("in LinuxIO's folder", func(t *testing.T) {
		blocksITRun(t, ownSwapFilePath, "")
		if root, err := readFolderFS(ownSwapDir); err != nil || (root.kind == "btrfs" && !root.subvolumeRoot) {
			t.Fatalf("%s: %+v, %v; want a subvolume on btrfs", ownSwapDir, root, err)
		}
		checkLabelledLike(t, ownSwapDir, stateDir)
	})
	t.Run("beside the files in /", func(t *testing.T) {
		blocksITRun(t, "/linuxio-blocks-it.swap", "sw,nofail,pri=7,discard=once")
	})
	t.Run("a shrink cut between its two steps", func(t *testing.T) {
		blocksITShrinkHalves(t, "/linuxio-blocks-it-shrink.swap")
	})
	t.Run("in a compressed folder on btrfs", func(t *testing.T) {
		if root, err := readFolderFS("/"); err != nil || root.kind != "btrfs" {
			t.Skipf("/ is not on btrfs: %+v, %v", root, err)
		}
		dir := "/linuxio-blocks-it-c"
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(dir) })
		f, err := os.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		flags := uint32(fsComprFl)
		err = inodeFlagsIoctl(int(f.Fd()), unix.FS_IOC_SETFLAGS, &flags)
		_ = f.Close()
		if err != nil {
			t.Fatalf("chattr +c %s: %v", dir, err)
		}
		blocksITRun(t, dir+"/swapfile", "")
	})
}

// blocksITShrinkHalves takes a shrink apart at the point a power cut could
// stop it: the smaller header written while the file still has its old
// length, which the installed mkswap must take as a size in KiB after the
// path, and the file then swaps at the new size; then the cut, after which
// it still does.
func blocksITShrinkHalves(t *testing.T, path string) {
	ctx := t.Context()
	t.Cleanup(func() {
		if listed, _ := swapListed(host, path); listed {
			_ = swapOff(context.Background(), path)
		}
		_ = os.Remove(path)
	})
	if _, err := host.makeSwapFile(ctx, path, blocksITGrown); err != nil {
		t.Fatal(err)
	}
	if err := host.writeSwapHeader(ctx, path, blocksITSize); err != nil {
		t.Fatal(err)
	}
	if got := fileLength(t, path); got != blocksITGrown {
		t.Fatalf("%s is %d bytes after the header, want its old %d", path, got, blocksITGrown)
	}
	blocksITTurnOn(t, path, "", blocksITSize)
	blocksITTurnOff(t, path)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	cutSwapFile(int(f.Fd()), path, blocksITSize)
	_ = f.Close()
	if got := fileLength(t, path); got != blocksITSize {
		t.Fatalf("%s is %d bytes after the cut, want %d", path, got, blocksITSize)
	}
	blocksITTurnOn(t, path, "", blocksITSize)
	blocksITTurnOff(t, path)
}

// blocksITPrepare refuses a host a cut run left files on, makes LinuxIO's
// state folder where LinuxIO is not installed, as tmpfiles.d makes it, and
// has cleanup remove what the test made and reload systemd. It returns the
// state folder.
func blocksITPrepare(t *testing.T) string {
	for _, path := range []string{ownSwapFilePath, "/linuxio-blocks-it.swap", "/linuxio-blocks-it-shrink.swap", "/linuxio-blocks-it-c"} {
		if _, err := os.Lstat(path); err == nil {
			t.Fatalf("%s exists: a cut run left it, run make vm-reset", path)
		}
	}
	stateDir := filepath.Dir(ownSwapDir)
	if _, err := os.Stat(stateDir); errors.Is(err, fs.ErrNotExist) {
		if err = os.Mkdir(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(stateDir) })
	}
	if _, err := os.Stat(ownSwapDir); errors.Is(err, fs.ErrNotExist) {
		// An empty subvolume is removed like a folder.
		t.Cleanup(func() {
			if rmErr := unix.Rmdir(ownSwapDir); rmErr != nil {
				t.Errorf("remove %s: %v", ownSwapDir, rmErr)
			}
		})
	}
	t.Cleanup(func() {
		if err := systemd.DaemonReload(context.Background()); err != nil {
			t.Errorf("reload systemd: %v", err)
		}
	})
	return stateDir
}

// blocksITRun takes one swap file at path through every block, its fstab
// line holding options when they are given.
func blocksITRun(t *testing.T, path, options string) {
	ctx := t.Context()
	h := host
	t.Cleanup(func() {
		bg := context.Background()
		if listed, _ := swapListed(h, path); listed {
			_ = swapOff(bg, path)
		}
		if err := h.removeFstabSwap(bg, path); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		_ = os.Remove(path)
	})

	id, kind := blocksITMake(t, path)
	lineOptions := blocksITFstab(t, path, options)

	blocksITTurnOn(t, path, lineOptions, blocksITSize)
	if err := h.deleteSwapFile(ctx, path, id); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("delete while it swaps: err = %v, want a conflict", err)
	}
	if err := h.resizeSwapFile(ctx, path, id, blocksITGrown); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("resize while it swaps: err = %v, want a conflict", err)
	}
	blocksITTurnOff(t, path)
	for _, size := range []int64{blocksITGrown, blocksITShrunk} {
		if err := h.resizeSwapFile(ctx, path, id, uint64(size)); err != nil {
			t.Fatalf("resize to %d: %v", size, err)
		}
		if kind == "btrfs" {
			checkNoCowOn(t, path)
		}
		blocksITTurnOn(t, path, lineOptions, size)
		blocksITTurnOff(t, path)
	}

	if err := h.removeFstabSwap(ctx, path); err != nil {
		t.Fatal(err)
	}
	if lines := fstabSwapLines(t, path); len(lines) != 0 {
		t.Fatalf("fstab still has %q", lines)
	}
	if err := h.deleteSwapFile(ctx, path, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is still there: %v", path, err)
	}
}

// blocksITMake makes the swap file at path and checks it: a regular file of
// root's, 0600, of the size asked, NOCOW on btrfs and labelled swapfile_t
// where SELinux is on. It returns its identity and its filesystem's kind.
func blocksITMake(t *testing.T, path string) (fileID, string) {
	t.Helper()
	id, err := host.makeSwapFile(t.Context(), path, blocksITSize)
	if err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if err = unix.Lstat(path, &st); err != nil {
		t.Fatal(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o7777 != 0o600 || st.Uid != 0 || st.Size != blocksITSize || fileIDOf(&st) != id {
		t.Fatalf("%s: mode %#o, uid %d, size %d; want a regular file of root's, 0600, of %d bytes", path, st.Mode, st.Uid, st.Size, blocksITSize)
	}
	folder, err := readFolderFS(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s on %+v", path, folder)
	if folder.kind == "btrfs" {
		checkNoCowOn(t, path)
	}
	if label := selinuxLabel(t, path); label != "" && !strings.Contains(label, ":swapfile_t:") {
		t.Errorf("%s is labelled %s, want swapfile_t as mkswap labels it", path, label)
	}
	return id, folder.kind
}

// blocksITFstab adds the fstab line of path, replaced by one with options,
// as an admin would have written it, when they are given, and returns the
// options of the one line that names path.
func blocksITFstab(t *testing.T, path, options string) string {
	t.Helper()
	if err := host.addFstabSwap(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if options != "" {
		if err := host.removeFstabSwap(t.Context(), path); err != nil {
			t.Fatal(err)
		}
		appendFstab(t, path+" none swap "+options+" 0 0\n")
	}
	lines := fstabSwapLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("fstab has %q for %s, want one line", lines, path)
	}
	return strings.Fields(lines[0])[3]
}

// blocksITTurnOn turns path on and checks that it swaps size bytes less the
// header's page, at the priority its options name.
func blocksITTurnOn(t *testing.T, path, options string, size int64) {
	t.Helper()
	if err := host.swapOn(t.Context(), path, options); err != nil {
		t.Fatal(err)
	}
	dev := waitSwapListed(t, host, path, true)
	if want := uint64(size - int64(os.Getpagesize())); dev.Size != want {
		t.Fatalf("%s swaps %d bytes, want %d", path, dev.Size, want)
	}
	if strings.Contains(options, "pri=7") && dev.Priority != 7 {
		t.Fatalf("%s swaps at priority %d, want 7", path, dev.Priority)
	}
}

func blocksITTurnOff(t *testing.T, path string) {
	t.Helper()
	if err := swapOff(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	waitSwapListed(t, host, path, false)
}

func swapListed(h hostRoot, path string) (bool, error) {
	devices, err := h.readSwaps()
	for _, d := range devices {
		if d.Path == path {
			return true, err
		}
	}
	return false, err
}

// waitSwapListed waits for /proc/swaps to list path, or not, and returns its
// row.
func waitSwapListed(t *testing.T, h hostRoot, path string, want bool) apischema.SwapDevice {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		devices, err := h.readSwaps()
		if err != nil {
			t.Fatal(err)
		}
		var row apischema.SwapDevice
		listed := false
		for _, d := range devices {
			if d.Path == path {
				row, listed = d, true
			}
		}
		if listed == want {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("/proc/swaps lists %s: %v, want %v", path, listed, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func checkNoCowOn(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var flags uint32
	if err = inodeFlagsIoctl(int(f.Fd()), unix.FS_IOC_GETFLAGS, &flags); err != nil {
		t.Fatal(err)
	}
	if flags&fsNoCowFl == 0 || flags&fsComprFl != 0 {
		t.Fatalf("%s has flags %#x, want NOCOW without COMPR", path, flags)
	}
}

// selinuxLabel is the SELinux context of path, or "" where it has none.
func selinuxLabel(t *testing.T, path string) string {
	t.Helper()
	label := make([]byte, 256)
	n, err := unix.Lgetxattr(path, selinuxXattr, label)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return ""
	}
	if err != nil {
		t.Fatalf("read the SELinux context of %s: %v", path, err)
	}
	return strings.TrimRight(string(label[:n]), "\x00")
}

// checkLabelledLike checks, where SELinux labels files, that dir has the
// context of parent and is not unlabeled_t, as a new subvolume would be.
func checkLabelledLike(t *testing.T, dir, parent string) {
	t.Helper()
	want := selinuxLabel(t, parent)
	if want == "" {
		return
	}
	got := selinuxLabel(t, dir)
	t.Logf("SELinux: %s is %s, %s is %s", dir, got, parent, want)
	if got != want || strings.Contains(got, ":unlabeled_t:") {
		t.Fatalf("%s is labelled %q, want %q like %s", dir, got, want, parent)
	}
}

func appendFstab(t *testing.T, line string) {
	t.Helper()
	f, err := os.OpenFile(etcFstab, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func fstabSwapLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(etcFstab)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.Lines(string(data)) {
		if fstabLineSwaps(line, path) {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestReadFolderFSWithoutRootIntegration reads folderFS for / and LinuxIO's
// state folder as a user without sudo, as the status of such a session
// does: on btrfs the layout ioctls must answer that user.
func TestReadFolderFSWithoutRootIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_FOLDER_FS_WITHOUT_ROOT") != "1" {
		t.Skip("set LINUXIO_RUN_FOLDER_FS_WITHOUT_ROOT=1 and run as a user without sudo in the test VM")
	}
	if os.Geteuid() == 0 {
		t.Skip("runs as a user without sudo")
	}
	for _, dir := range []string{"/", "/var/lib"} {
		got, err := readFolderFS(dir)
		if err != nil {
			t.Fatalf("%s as uid %d: %v", dir, os.Geteuid(), err)
		}
		t.Logf("%s as uid %d: %+v", dir, os.Geteuid(), got)
		if got.kind == "btrfs" && !got.btrfsLayout {
			t.Errorf("%s: a btrfs of one device reads as unable to hold a swap file", dir)
		}
	}
}
