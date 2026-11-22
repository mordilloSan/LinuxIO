package memory

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// rowsHost is a host with nothing swapping, roomy ext4 and the given fstab.
func rowsHost(t *testing.T, fstab string) fixture {
	t.Helper()
	f := newFixture(t)
	f.write(procSwaps, swapsHeader)
	f.write(procMeminfo, meminfoText)
	f.write(etcFstab, fstab)
	return f
}

// readRows reads the status of the fixture and returns its rows by path.
func readRows(t *testing.T, f fixture) (apischema.SwapStatus, map[string]apischema.SwapFileRow) {
	t.Helper()
	status, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]apischema.SwapFileRow{}
	for _, row := range status.SwapFiles {
		rows[row.Path] = row
	}
	return status, rows
}

// The strict check: an exact plain path in fstab, folders only root can
// change and that are not links, a regular file root owns at the path, and
// not the hibernation file. A tag and a /dev source are no row at all.
func TestSwapFileRowsStrictCheck(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f fixture)
		path  string
		want  apischema.SwapFileLockedReason
	}{
		{name: "plain path", path: "/swap.img", setup: func(f fixture) {
			f.write(etcFstab, "/swap.img none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", gib)
		}},
		{name: "escaped path", path: "/srv/my swap", setup: func(f fixture) {
			f.write(etcFstab, `/srv/my\040swap none swap sw 0 0`+"\n")
			f.writeSwapFile("/srv/my swap", gib)
			f.write(procSwaps, swapsHeader+`/srv/my\040swap                          file		1048572		0		-2`+"\n")
		}},
		{name: "two lines for one path", path: "/swap.img", setup: func(f fixture) {
			f.write(etcFstab, "/swap.img none swap sw 0 0\n/swap.img none swap pri=3 0 0\n")
			f.writeSwapFile("/swap.img", gib)
		}},
		{name: "active and not in fstab", path: "/swap.img", want: apischema.SwapFileNotInFstab, setup: func(f fixture) {
			f.write(etcFstab, "UUID=0a1b2c3d none swap sw 0 0\n/dev/sda3 none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", gib)
			f.write(procSwaps, swapsHeader+swapsLine("/swap.img", gib, 0))
		}},
		{name: "a link on the path", path: "/srv/link/swapfile", want: apischema.SwapFileNotRootOnly, setup: func(f fixture) {
			f.write(etcFstab, "/srv/link/swapfile none swap sw 0 0\n")
			f.writeSwapFile("/srv/real/swapfile", gib)
			f.symlink("real", "/srv/link")
		}},
		{name: "a folder others can change", path: "/home/open/swapfile", want: apischema.SwapFileNotRootOnly, setup: func(f fixture) {
			f.write(etcFstab, "/home/open/swapfile none swap sw 0 0\n")
			f.writeSwapFile("/home/open/swapfile", gib)
			if err := os.Chmod(filepath.Join(f.root, "/home/open"), 0o775); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "the file is a link", path: "/swap.img", want: apischema.SwapFileNotRootOnly, setup: func(f fixture) {
			f.write(etcFstab, "/swap.img none swap sw 0 0\n")
			f.writeSwapFile("/real.img", gib)
			f.symlink("real.img", "/swap.img")
		}},
		{name: "a folder at the path", path: "/swap.img", want: apischema.SwapFileNotRootOnly, setup: func(f fixture) {
			f.write(etcFstab, "/swap.img none swap sw 0 0\n")
			f.mkdir("/swap.img")
		}},
		{name: "a path that is not clean", path: "/srv/../swap.img", want: apischema.SwapFileNotRootOnly, setup: func(f fixture) {
			f.write(etcFstab, "/srv/../swap.img none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", gib)
		}},
		{name: "the hibernation file", path: "/swap.img", want: apischema.SwapFileHibernation, setup: func(f fixture) {
			f.write(etcFstab, "/swap.img none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", gib)
			f.write(procMountinfo, "22 1 253:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw\n")
			f.write("/dev/vda1", "")
			f.write("/sys/class/block/vda1/dev", "253:1\n")
			f.write(sysPowerResume, "253:1\n")
			f.write(sysPowerResumeOffset, "34816\n")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := rowsHost(t, "")
			test.setup(f)
			status, rows := readRows(t, f)
			row, ok := rows[test.path]
			if !ok {
				t.Fatalf("rows = %+v, want one for %s", status.SwapFiles, test.path)
			}
			if row.LockedReason != test.want {
				t.Fatalf("locked reason = %q, want %q", row.LockedReason, test.want)
			}
			if len(status.SwapFiles) != 1 {
				t.Errorf("rows = %+v, want one", status.SwapFiles)
			}
		})
	}
}

// A row that fails the root-only rule is read from /proc/swaps, /etc/fstab
// and mountinfo alone: nothing is stat'ed or statfs'ed through its path,
// where a link could lead anywhere, and an off one has no size.
func TestSwapFileRowsNotRootOnlyReadNothingThroughThePath(t *testing.T) {
	f := rowsHost(t, "/srv/link/swapfile none swap sw 0 0\n/home/open/swapfile none swap sw 0 0\n")
	f.writeSwapFile("/srv/real/swapfile", gib)
	f.symlink("real", "/srv/link")
	f.writeSwapFile("/home/open/swapfile", gib)
	if err := os.Chmod(filepath.Join(f.root, "/home/open"), 0o775); err != nil {
		t.Fatal(err)
	}
	f.write(procSwaps, swapsHeader+swapsLine("/home/open/swapfile", gib, 0))
	f.write(procMountinfo, "22 1 253:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw\n")
	var read []string
	previous := readFolderFS
	readFolderFS = func(dir string) (folderFS, error) {
		read = append(read, dir)
		return roomyExt4, nil
	}
	t.Cleanup(func() { readFolderFS = previous })

	_, rows := readRows(t, f)
	off, on := rows["/srv/link/swapfile"], rows["/home/open/swapfile"]
	if off.LockedReason != apischema.SwapFileNotRootOnly || off.Size != 0 || off.FreeSpace != 0 || off.MaxSize != 0 || off.ResizeBlockedReason != "" || off.FSType != "ext4" {
		t.Errorf("off row = %+v, want not_root_only with no size, free space or resize reason and ext4 from mountinfo", off)
	}
	if on.LockedReason != apischema.SwapFileNotRootOnly || !on.Active || on.Size != gib-pageSize() || on.FreeSpace != 0 || on.MaxSize != 0 || on.FSType != "ext4" {
		t.Errorf("active row = %+v, want not_root_only with the size /proc/swaps gives and ext4 from mountinfo", on)
	}
	for _, dir := range read {
		if dir == filepath.Join(f.root, "/srv/link") || dir == filepath.Join(f.root, "/home/open") {
			t.Errorf("readFolderFS read %s, a folder on a path others can change", dir)
		}
	}
}

// The host resumes from another device, from a partition (offset 0) or from
// nothing: a swap file is not its hibernation file.
func TestSwapFileRowsHibernationOnlyOnItsDevice(t *testing.T) {
	for name, resume := range map[string][2]string{
		"another device": {"253:2", "34816"},
		"a partition":    {"253:1", "0"},
		"none":           {"0:0", "0"},
	} {
		t.Run(name, func(t *testing.T) {
			f := rowsHost(t, "/swap.img none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", gib)
			f.write(procMountinfo, "22 1 253:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw\n")
			f.write("/dev/vda1", "")
			f.write("/sys/class/block/vda1/dev", "253:1\n")
			f.write(sysPowerResume, resume[0]+"\n")
			f.write(sysPowerResumeOffset, resume[1]+"\n")
			if _, rows := readRows(t, f); rows["/swap.img"].LockedReason != "" {
				t.Fatalf("row = %+v, want it unlocked", rows["/swap.img"])
			}
		})
	}
}

// A row that passes says what it reads: missing, not a swap file, or off.
func TestSwapFileRowsOffRows(t *testing.T) {
	f := rowsHost(t, "/gone.img none swap sw 0 0\n/blank.img none swap sw 0 0\n/off.img none swap sw 0 0\n")
	f.write("/blank.img", "not a swap area")
	f.writeSwapFile("/off.img", 512*mib)
	_, rows := readRows(t, f)
	for path, want := range map[string]apischema.SwapFileResizeReason{
		"/gone.img":  apischema.SwapFileResizeMissing,
		"/blank.img": apischema.SwapFileResizeNoHeader,
		"/off.img":   apischema.SwapFileResizeOff,
	} {
		if row := rows[path]; row.LockedReason != "" || row.ResizeBlockedReason != want || row.Active || row.MaxSize != 0 {
			t.Errorf("%s = %+v, want unlocked, off and %q", path, row, want)
		}
	}
	if rows["/off.img"].Size != 512*mib {
		t.Errorf("off file size = %d, want its length", rows["/off.img"].Size)
	}
}

// A resize is refused on a filesystem LinuxIO does not make swap files on.
func TestSwapFileRowsResizeFilesystem(t *testing.T) {
	tests := map[string]struct {
		info  folderFS
		mount string
		want  apischema.SwapFileResizeReason
	}{
		"ext4":           {info: roomyExt4},
		"ext3 by mount":  {info: roomyExt4, mount: "ext3", want: apischema.SwapFileResizeUnsupportedFilesystem},
		"another magic":  {info: folderFS{total: 100 * gib, free: 50 * gib}, want: apischema.SwapFileResizeUnsupportedFilesystem},
		"read-only":      {info: folderFS{kind: "xfs", readOnly: true, total: 100 * gib, free: 50 * gib}, want: apischema.SwapFileResizeReadOnly},
		"btrfs raid1":    {info: folderFS{kind: "btrfs", total: 100 * gib, free: 50 * gib}, want: apischema.SwapFileResizeBtrfsLayout},
		"btrfs single":   {info: folderFS{kind: "btrfs", btrfsLayout: true, total: 100 * gib, free: 50 * gib}},
		"btrfs by mount": {info: folderFS{kind: "btrfs", btrfsLayout: true, total: 100 * gib, free: 50 * gib}, mount: "btrfs"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f := rowsHost(t, "/swap.img none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", gib)
			f.write(procSwaps, swapsHeader+swapsLine("/swap.img", gib, 0)+zramSwapsLine)
			if test.mount != "" {
				f.write(procMountinfo, "22 1 8:1 / / rw - "+test.mount+" /dev/sda1 rw\n")
			}
			f.useFolderFS(test.info)
			if _, rows := readRows(t, f); rows["/swap.img"].ResizeBlockedReason != test.want {
				t.Fatalf("row = %+v, want %q", rows["/swap.img"], test.want)
			}
		})
	}
}

// The largest size of a resize leaves the reserve, 1 GiB or 5% of the
// filesystem, and room for the temporary file when the resize needs one: it
// does when the file is the only swap device or its data does not pass the
// room check without help.
func TestSwapFileRowsMaxSize(t *testing.T) {
	zram := "/dev/zram0                              partition\t8388604\t\t0\t\t100\n"
	tests := map[string]struct {
		swaps        string
		info         folderFS
		memAvailable uint64
		want         uint64
	}{
		// 1 GiB of data: a temporary file of 1.25 GiB, at most the file's 2 GiB.
		"the only swap device": {swaps: swapsLine("/swap.img", 2*gib, gib), info: folderFS{kind: "ext4", total: 10 * gib, free: 5 * gib},
			memAvailable: 8 * gib, want: 2*gib + 5*gib - gib - gib - 256*mib},
		// zram beside it and room in memory: no temporary file.
		"beside zram": {swaps: swapsLine("/swap.img", 2*gib, gib) + zram, info: folderFS{kind: "ext4", total: 10 * gib, free: 5 * gib},
			memAvailable: 8 * gib, want: 2*gib + 5*gib - gib},
		// Without room in memory a temporary file is made beside zram too.
		"no room in memory": {swaps: swapsLine("/swap.img", 2*gib, gib) + zram, info: folderFS{kind: "ext4", total: 10 * gib, free: 5 * gib},
			memAvailable: gib, want: 2*gib + 5*gib - gib - gib - 256*mib},
		// 5% of 100 GiB is more than 1 GiB.
		"a large filesystem": {swaps: swapsLine("/swap.img", 2*gib, 0) + zram, info: folderFS{kind: "ext4", total: 100 * gib, free: 20 * gib},
			memAvailable: 8 * gib, want: 2*gib + 20*gib - 5*gib},
		// Fuller than the reserve: a shrink, which needs no room, only.
		"a full disk": {swaps: swapsLine("/swap.img", 2*gib, 0) + zram, info: folderFS{kind: "ext4", total: 10 * gib, free: 512 * mib},
			memAvailable: 8 * gib, want: 2 * gib},
		// Fuller than the reserve with a temporary file needed: no resize.
		"a full disk and no other swap": {swaps: swapsLine("/swap.img", 2*gib, 0), info: folderFS{kind: "ext4", total: 10 * gib, free: 512 * mib},
			memAvailable: 8 * gib},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f := rowsHost(t, "/swap.img none swap sw 0 0\n")
			f.writeSwapFile("/swap.img", 2*gib)
			f.write(procSwaps, swapsHeader+test.swaps)
			f.setMemAvailable(test.memAvailable)
			f.useFolderFS(test.info)
			if _, rows := readRows(t, f); rows["/swap.img"].MaxSize != test.want {
				t.Fatalf("max size = %d MiB, want %d MiB", rows["/swap.img"].MaxSize>>20, test.want>>20)
			}
		})
	}
}

func TestResizeTemporarySize(t *testing.T) {
	for _, test := range []struct{ used, length, want uint64 }{
		{used: 0, length: 4 * gib, want: 256 * mib},
		{used: gib, length: 4 * gib, want: gib + 256*mib},
		{used: 4 * gib, length: 4 * gib, want: 4 * gib},
		{used: 100 * mib, length: 4 * gib, want: 256 * mib},
		{used: 300 * mib, length: 4 * gib, want: 375 * mib},
	} {
		if got := resizeTemporarySize(test.used, test.length); got != test.want {
			t.Errorf("resizeTemporarySize(%d MiB, %d MiB) = %d MiB, want %d MiB", test.used>>20, test.length>>20, got>>20, test.want>>20)
		}
	}
}

func TestDefaultSwapFileSize(t *testing.T) {
	for _, test := range []struct{ memTotal, want uint64 }{
		{memTotal: gib, want: gib},
		{memTotal: 4 * gib, want: 2 * gib},
		{memTotal: 16308408 * kib, want: 7963 * mib},
		{memTotal: 64 * gib, want: 8 * gib},
	} {
		if got := defaultSwapFileSize(test.memTotal); got != test.want {
			t.Errorf("defaultSwapFileSize(%d MiB) = %d MiB, want %d MiB", test.memTotal>>20, got>>20, test.want>>20)
		}
	}
}

// A temporary swap file a resize left swapping beside a file fstab names is
// a row of its own with Remove only; one beside a file fstab does not name
// is just a swap file fstab does not name.
func TestSwapFileRowsTemporary(t *testing.T) {
	f := rowsHost(t, "/swap.img none swap sw 0 0\n")
	f.writeSwapFile("/swap.img", 2*gib)
	f.writeSwapFile("/swap.img.linuxio-resize", 512*mib)
	f.writeSwapFile("/other.img.linuxio-resize", 512*mib)
	f.write(procSwaps, swapsHeader+swapsLine("/swap.img", 2*gib, 0)+swapsLine("/swap.img.linuxio-resize", 512*mib, 0)+
		swapsLine("/other.img.linuxio-resize", 512*mib, 0))
	status, rows := readRows(t, f)
	if row := rows["/swap.img.linuxio-resize"]; !row.Temporary || row.LockedReason != "" || row.InFstab || row.MaxSize != 0 || row.CanMove {
		t.Errorf("temporary row = %+v, want temporary and removable", row)
	}
	if row := rows["/other.img.linuxio-resize"]; row.Temporary || row.LockedReason != apischema.SwapFileNotInFstab {
		t.Errorf("other row = %+v, want a file fstab does not name", row)
	}
	if removable := removableSwapFiles(status.SwapFiles); !slices.Equal(removable, []string{"/swap.img", "/swap.img.linuxio-resize"}) {
		t.Errorf("removable = %v", removable)
	}
}

// Create says why it is not offered, first reason first.
func TestSwapFileCreateReasons(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f fixture)
		want  apischema.SwapFileCreateReason
	}{
		{name: "offered"},
		{name: "offered beside a folder LinuxIO left empty", setup: func(f fixture) { f.mkdir(ownSwapDir) }},
		{name: "exists", want: apischema.SwapFileCreateExists, setup: func(f fixture) {
			f.write(etcFstab, ownSwapFilePath+" none swap sw,nofail 0 0\n")
			f.writeSwapFile(ownSwapFilePath, gib)
		}},
		{name: "a leftover of a cut-short create", setup: func(f fixture) { f.writeSwapFile(ownSwapFilePath, gib) }},
		{name: "a folder others can change", want: apischema.SwapFileCreateFolderNotRootOnly, setup: func(f fixture) {
			f.othersCanWrite("/var/lib/linuxio")
		}},
		{name: "a link at the folder", want: apischema.SwapFileCreateFolderNotRootOnly, setup: func(f fixture) {
			f.mkdir("/srv/swap")
			f.mkdir("/var/lib/linuxio")
			f.symlink("../../../srv/swap", ownSwapDir)
		}},
		{name: "a file that is not a swap file", want: apischema.SwapFileCreatePathTaken, setup: func(f fixture) {
			f.write(ownSwapFilePath, "data")
		}},
		{name: "another name in the folder", want: apischema.SwapFileCreatePathTaken, setup: func(f fixture) {
			f.write(ownSwapDir+"/notes", "")
		}},
		{name: "a plain folder with a leftover on btrfs", want: apischema.SwapFileCreatePathTaken, setup: func(f fixture) {
			f.writeSwapFile(ownSwapFilePath, gib)
			f.useFolderFS(folderFS{kind: "btrfs", btrfsLayout: true, total: 100 * gib, free: 50 * gib})
		}},
		{name: "unsupported filesystem", want: apischema.SwapFileCreateUnsupportedFilesystem, setup: func(f fixture) {
			f.useFolderFS(folderFS{total: 100 * gib, free: 50 * gib})
		}},
		{name: "read-only", want: apischema.SwapFileCreateReadOnly, setup: func(f fixture) {
			f.useFolderFS(folderFS{kind: "ext4", readOnly: true, total: 100 * gib, free: 50 * gib})
		}},
		{name: "btrfs layout", want: apischema.SwapFileCreateBtrfsLayout, setup: func(f fixture) {
			f.useFolderFS(folderFS{kind: "btrfs", total: 100 * gib, free: 50 * gib})
		}},
		{name: "not enough space", want: apischema.SwapFileCreateNotEnoughSpace, setup: func(f fixture) {
			f.useFolderFS(folderFS{kind: "ext4", total: 10 * gib, free: gib + 255*mib})
		}},
		{name: "just enough space", setup: func(f fixture) {
			f.useFolderFS(folderFS{kind: "ext4", total: 10 * gib, free: gib + 256*mib})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := rowsHost(t, "")
			if test.setup != nil {
				test.setup(f)
			}
			status, _ := readRows(t, f)
			if status.Create.BlockedReason != test.want {
				t.Fatalf("create = %+v, want %q", status.Create, test.want)
			}
			if status.Create.Path != ownSwapFilePath || status.Create.MinSize != swapFileMinSize {
				t.Errorf("create = %+v, want the path and the smallest size always", status.Create)
			}
		})
	}
}

// Create's default is a size a request may carry, never above the largest
// one, and the space of a swap file a cut-short Create left, which the next
// one deletes, counts as free.
func TestSwapFileCreateSizes(t *testing.T) {
	tests := map[string]struct {
		free             uint64
		leftover         bool
		max, defaultSize uint64
		want             apischema.SwapFileCreateReason
	}{
		"roomy":                 {free: 50 * gib, max: 49 * gib, defaultSize: defaultSwapFileSize(16308408 << 10)},
		"the default above max": {free: gib + 300*mib, max: 300 * mib, defaultSize: 300 * mib},
		"no room":               {free: gib + 255*mib, max: 255 * mib, defaultSize: defaultSwapFileSize(16308408 << 10), want: apischema.SwapFileCreateNotEnoughSpace},
		"room with a leftover":  {free: gib + 255*mib, leftover: true, max: 511 * mib, defaultSize: 511 * mib},
		"roomy with a leftover": {free: 50 * gib, leftover: true, max: 49*gib + 256*mib, defaultSize: defaultSwapFileSize(16308408 << 10)},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f := rowsHost(t, "")
			f.useFolderFS(folderFS{kind: "ext4", total: 10 * gib, free: test.free})
			if test.leftover {
				f.writeSwapFile(ownSwapFilePath, 256*mib)
			}
			status, _ := readRows(t, f)
			got := status.Create
			if got.BlockedReason != test.want || got.MaxSize != test.max || got.DefaultSize != test.defaultSize {
				t.Fatalf("create = %+v, want %q, max %d MiB, default %d MiB", got, test.want, test.max>>20, test.defaultSize>>20)
			}
		})
	}
}

// Move is offered for an active file on btrfs outside LinuxIO's folder, on
// the filesystem the folder is on, while Create has room for it.
func TestSwapFileRowsCanMove(t *testing.T) {
	btrfs := folderFS{kind: "btrfs", btrfsLayout: true, total: 100 * gib, free: 50 * gib}
	mounts := "22 1 0:21 /@ / rw shared:1 - btrfs /dev/vda2 rw\n" +
		"23 22 0:22 /@home /home rw shared:2 - btrfs /dev/vda2 rw\n" +
		"24 22 8:17 / /mnt/data rw shared:3 - btrfs /dev/vdb1 rw\n"
	tests := map[string]struct {
		path  string
		setup func(f fixture)
		want  bool
	}{
		"in the subvolume of /":    {path: "/swap.img", want: true},
		"in another subvolume":     {path: "/home/swapfile", want: true},
		"on another filesystem":    {path: "/mnt/data/swapfile"},
		"already in its folder":    {path: ownSwapFilePath},
		"while Create is not open": {path: "/swap.img", setup: func(f fixture) { f.write(ownSwapDir+"/notes", "") }},
		"larger than the room": {path: "/swap.img", setup: func(f fixture) {
			f.useFolderFS(folderFS{kind: "btrfs", btrfsLayout: true, total: 10 * gib, free: gib + 512*mib})
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f := rowsHost(t, test.path+" none swap sw 0 0\n")
			f.writeSwapFile(test.path, gib)
			f.write(procSwaps, swapsHeader+swapsLine(test.path, gib, 0))
			f.write(procMountinfo, mounts)
			f.useFolderFS(btrfs)
			if test.setup != nil {
				test.setup(f)
			}
			_, rows := readRows(t, f)
			row := rows[test.path]
			if row.CanMove != test.want {
				t.Fatalf("row = %+v, want can move %v", row, test.want)
			}
			if blocks := test.path != ownSwapFilePath; row.BlocksSnapshots != blocks {
				t.Errorf("blocks snapshots = %v, want %v", row.BlocksSnapshots, blocks)
			}
		})
	}
}
