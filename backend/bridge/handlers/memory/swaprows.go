package memory

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// The sizes a swap file LinuxIO makes or resizes may have, in bytes. Every
// size is a whole number of MiB.
const (
	swapFileMinSize = 256 << 20
	// The default size of a new swap file is half of RAM within these, the
	// same as zram's planned size. It is a starting value, not a limit.
	swapFileDefaultMin = 1 << 30
	swapFileDefaultMax = 8 << 30
	// swapFileReserveMin is the least free space a new or larger swap file
	// leaves on its filesystem; 5% of the filesystem when that is more.
	swapFileReserveMin = 1 << 30
)

const mibMask = 1<<20 - 1

func floorMiB(n uint64) uint64 { return n &^ mibMask }

func ceilMiB(n uint64) uint64 { return floorMiB(n + mibMask) }

// swapFileReserve is the free space a new or larger swap file leaves on a
// filesystem of total bytes: 1 GiB, or 5% of the filesystem when that is
// more.
func swapFileReserve(total uint64) uint64 {
	return max(swapFileReserveMin, total/20)
}

// defaultSwapFileSize is the size the dialog starts a new swap file at: half
// of RAM, at least 1 GiB and at most 8 GiB, in whole MiB.
func defaultSwapFileSize(memTotal uint64) uint64 {
	return floorMiB(min(max(memTotal/2, swapFileDefaultMin), swapFileDefaultMax))
}

// The files that say whether the host hibernates to a swap file: the block
// device it resumes from, "major:minor", and the offset on it, in pages, of
// the swap file's header, 0 for a partition.
const (
	sysPowerResume       = "/sys/power/resume"
	sysPowerResumeOffset = "/sys/power/resume_offset"
)

// hibernationDevice is the block device, "major:minor", the host resumes
// from at a file offset, or empty when it resumes from none or from a
// partition. A swap file on that device may be the hibernation file.
func (h hostRoot) hibernationDevice() (string, error) {
	device, ok, err := h.readOptional(sysPowerResume)
	if err != nil || !ok {
		return "", err
	}
	device = strings.TrimSpace(device)
	if device == "" || device == "0:0" {
		return "", nil
	}
	offset, err := h.readOptionalUint(sysPowerResumeOffset)
	if err != nil || offset == 0 {
		return "", err
	}
	return device, nil
}

// mountDevice is the block device, "major:minor", that a mount's source
// names, read from /sys/class/block for the node the source leads to, or
// empty when it cannot be told. A file's own st_dev is no help: on btrfs it
// is an anonymous device of the subvolume.
func (h hostRoot) mountDevice(mount mountEntry) string {
	if !strings.HasPrefix(mount.source, "/dev/") {
		return ""
	}
	node, err := filepath.EvalSymlinks(h.path(mount.source))
	if err != nil {
		return ""
	}
	text, ok, err := h.readOptional(filepath.Join(sysClassBlockDir, filepath.Base(node), "dev"))
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// hibernates reports whether a swap file on mount may be the host's
// hibernation file: the host resumes at a file offset from the device the
// mount is on, or from a device, when the mount's device cannot be told.
func (h hostRoot) hibernates(resume string, mount mountEntry) bool {
	if resume == "" {
		return false
	}
	device := h.mountDevice(mount)
	return device == "" || device == resume
}

// rootOnlySwapFile reports whether only root can change what path names:
// the path is clean, every folder from / down is a folder, not a link, that
// root owns and no group or everyone may write (foldersOnlyOf), and what is
// at the path, looked at without following a link, is a regular file root
// owns, or nothing. A folder or file the reader may not look into is not
// judged: a session without sudo reads the status too, and a change, which
// runs as root, reads it again.
func (h hostRoot) rootOnlySwapFile(path string) bool {
	if filepath.Clean(path) != path {
		return false
	}
	if err := foldersOnlyOf(h.owner, h.root, h.path(path)); err != nil && !errors.Is(err, fs.ErrPermission) {
		return false
	}
	info, err := os.Lstat(h.path(path))
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		return true
	case err != nil:
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return info.Mode().IsRegular() && ok && st.Uid == h.owner
}

// swapFileFacts is one swap file row with what the status read to make it,
// which the sizes and the move offer are worked out from.
type swapFileFacts struct {
	row apischema.SwapFileRow
	// length is the file's length, or the swap size when it cannot be read.
	length uint64
	mount  mountEntry
	fs     folderFS
	fsOK   bool
}

// swapFileRows is a row for every swap file: the active ones in the order
// of devices, then the ones /etc/fstab names that are not active, in fstab
// order. resume is hibernationDevice.
func (h hostRoot) swapFileRows(devices []apischema.SwapDevice, fstab []fstabSwap, mounts []mountEntry, resume string) []swapFileFacts {
	folders := map[string]folderFSRead{}
	var paths []string
	for _, device := range devices {
		if device.Kind == apischema.SwapDeviceFile {
			paths = append(paths, device.Path)
		}
	}
	for _, entry := range fstab {
		if !slices.Contains(paths, entry.path) {
			paths = append(paths, entry.path)
		}
	}
	rows := make([]swapFileFacts, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, h.swapFileRow(path, devices, fstab, mounts, resume, folders))
	}
	return rows
}

// folderFSRead is readFolderFS's answer for one folder.
type folderFSRead struct {
	fs folderFS
	ok bool
}

// folderFS reads what the filesystem holding the host folder dir reports,
// once per status. One that cannot be read is not judged.
func (h hostRoot) folderFS(dir string, cache map[string]folderFSRead) (folderFS, bool) {
	if read, ok := cache[dir]; ok {
		return read.fs, read.ok
	}
	info, err := readFolderFS(h.path(dir))
	if err != nil {
		slog.Debug("read swap file folder filesystem failed", "path", dir, "error", err)
	}
	cache[dir] = folderFSRead{fs: info, ok: err == nil}
	return info, err == nil
}

func (h hostRoot) swapFileRow(path string, devices []apischema.SwapDevice, fstab []fstabSwap, mounts []mountEntry, resume string, folders map[string]folderFSRead) swapFileFacts {
	facts := swapFileFacts{row: apischema.SwapFileRow{Path: path}}
	row := &facts.row
	if i := slices.IndexFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind == apischema.SwapDeviceFile && d.Path == path }); i >= 0 {
		row.Active, row.Size, row.Used = true, devices[i].Size, devices[i].Used
	}
	row.InFstab = fstabIndex(fstab, path) >= 0
	facts.mount, _ = mountHolding(mounts, path)
	row.DiskHint = h.diskHint(facts.mount)
	// A temporary swap file a resize left swapping has no fstab line of its
	// own: the line of the file it stood in for counts.
	base, temporary := strings.CutSuffix(path, resizeSuffix)
	row.Temporary = temporary && row.Active && !row.InFstab && fstabIndex(fstab, base) >= 0
	rootOnly := h.rootOnlySwapFile(path)
	switch {
	case !row.InFstab && !row.Temporary:
		row.LockedReason = apischema.SwapFileNotInFstab
	case !rootOnly:
		row.LockedReason = apischema.SwapFileNotRootOnly
	case h.hibernates(resume, facts.mount):
		row.LockedReason = apischema.SwapFileHibernation
	}
	// Nothing is read through a path others can change, where a link could
	// send a stat, a statfs or a btrfs ioctl anywhere: such a row keeps what
	// /proc/swaps, /etc/fstab and mountinfo say, with no size when it is off.
	if !rootOnly {
		facts.length = row.Size
		row.FSType = facts.mount.fsType
		row.BlocksSnapshots = !row.Temporary && row.FSType == "btrfs" && filepath.Dir(path) != ownSwapDir
		return facts
	}
	info, statErr := os.Lstat(h.path(path))
	facts.length = row.Size
	if statErr == nil && info.Mode().IsRegular() {
		facts.length = uint64(info.Size())
	}
	if !row.Active {
		row.Size = facts.length
	}
	facts.fs, facts.fsOK = h.folderFS(filepath.Dir(path), folders)
	row.FSType = fsTypeOf(facts.mount, facts.fs, facts.fsOK)
	row.FreeSpace = facts.fs.free
	row.BlocksSnapshots = !row.Temporary && row.FSType == "btrfs" && filepath.Dir(path) != ownSwapDir
	if row.LockedReason == "" && !row.Temporary {
		row.ResizeBlockedReason = h.resizeBlockedReason(facts, statErr)
	}
	return facts
}

// fsTypeOf is the type of the filesystem a file is on: mountinfo's, or the
// one its folder's statfs names when mountinfo has no mount for it.
func fsTypeOf(mount mountEntry, info folderFS, ok bool) string {
	if mount.fsType != "" || !ok {
		return mount.fsType
	}
	return info.kind
}

// fsRefusal says why LinuxIO does not make or resize a swap file on a
// filesystem, or is empty when it does: ext4, xfs or btrfs by both its
// statfs magic and, when mountinfo has the mount, its type there, which
// tells ext2 and ext3 from ext4; writable; and on btrfs one device with
// single data.
func fsRefusal(mount mountEntry, info folderFS) string {
	switch {
	case info.kind == "" || mount.fsType != "" && mount.fsType != info.kind:
		return "unsupported_filesystem"
	case info.readOnly:
		return "read_only"
	case info.kind == "btrfs" && !info.btrfsLayout:
		return "btrfs_layout"
	default:
		return ""
	}
}

// resizeBlockedReason says why a swap file that passes the strict check
// cannot be resized, or is empty when it can: missing, off without a swap
// header, off, or on a filesystem LinuxIO does not make swap files on. A
// header the reader may not read, and a filesystem it cannot read, are not
// judged.
func (h hostRoot) resizeBlockedReason(facts swapFileFacts, statErr error) apischema.SwapFileResizeReason {
	row := facts.row
	if !row.Active {
		if errors.Is(statErr, fs.ErrNotExist) {
			return apischema.SwapFileResizeMissing
		}
		swap, err := h.holdsSwap(row.Path)
		if err != nil {
			slog.Debug("read swap file header failed", "path", row.Path, "error", err)
		}
		if err == nil && !swap {
			return apischema.SwapFileResizeNoHeader
		}
		return apischema.SwapFileResizeOff
	}
	if !facts.fsOK {
		return ""
	}
	return apischema.SwapFileResizeReason(fsRefusal(facts.mount, facts.fs))
}

// resizeTemporarySize is the size of the temporary swap file a resize makes
// for a file of length bytes holding used bytes: the data plus a quarter, at
// least 256 MiB and at most the file's own length, in whole MiB.
func resizeTemporarySize(used, length uint64) uint64 {
	return min(max(ceilMiB(used+used/4), swapFileMinSize), floorMiB(length))
}

// resizeNeedsTemporary reports whether a resize of the active swap file at
// path makes a temporary swap file first: the file is the only active swap
// device, or its data does not pass the room check without help.
func resizeNeedsTemporary(status apischema.SwapStatus, path string) bool {
	others := slices.ContainsFunc(status.Devices, func(d apischema.SwapDevice) bool { return d.Path != path })
	return !others || !roomToTurnOff(status, path)
}

// resizeTemporary is the size of the temporary swap file a resize of the
// row would make on status, or 0 when it makes none.
func resizeTemporary(status apischema.SwapStatus, facts swapFileFacts) uint64 {
	if !resizeNeedsTemporary(status, facts.row.Path) {
		return 0
	}
	return resizeTemporarySize(facts.row.Used, facts.length)
}

// resizeMaxSize is the largest size a resize of the row may ask for: the
// filesystem must hold the temporary file and the growth and still leave the
// reserve free. A shrink that needs no temporary file needs no room, so it
// is allowed on a disk that is fuller than the reserve allows.
func resizeMaxSize(facts swapFileFacts, temporary uint64) uint64 {
	free, reserve := facts.fs.free, swapFileReserve(facts.fs.total)
	switch {
	case free >= reserve+temporary:
		return floorMiB(facts.length + free - reserve - temporary)
	case temporary == 0:
		return floorMiB(facts.length)
	default:
		return 0
	}
}

// createFolder is the folder Create decides on: LinuxIO's swap folder, or
// the nearest folder above it that exists, which Create makes it in.
func (h hostRoot) createFolder() string {
	for dir := ownSwapDir; dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(h.path(dir)); !errors.Is(err, fs.ErrNotExist) {
			return dir
		}
	}
	return "/"
}

// swapFileCreate is what the dialog needs to offer Create, given the rows.
// resume is hibernationDevice: a swap file made on that device would be
// locked like every other one there, so none is made.
func (h hostRoot) swapFileCreate(rows []swapFileFacts, mounts []mountEntry, resume string, memTotal uint64) (apischema.SwapFileCreate, mountEntry) {
	create := apischema.SwapFileCreate{
		Path:        ownSwapFilePath,
		MinSize:     swapFileMinSize,
		DefaultSize: defaultSwapFileSize(memTotal),
	}
	folder := h.createFolder()
	mount, _ := mountHolding(mounts, folder)
	info, err := readFolderFS(h.path(folder))
	if err != nil {
		slog.Debug("read swap file folder filesystem failed", "path", folder, "error", err)
	}
	create.FSType = fsTypeOf(mount, info, err == nil)
	create.FreeSpace, create.Reserve = info.free, swapFileReserve(info.total)
	// A swap file a cut-short Create left at the path is deleted by the next
	// one, so its space counts as free.
	room := info.free
	if !slices.ContainsFunc(rows, func(f swapFileFacts) bool { return f.row.Path == ownSwapFilePath }) && h.leftoverOwnSwapFile() {
		if leftover, statErr := os.Lstat(h.path(ownSwapFilePath)); statErr == nil {
			room += uint64(leftover.Size())
		}
	}
	if room > create.Reserve {
		create.MaxSize = floorMiB(room - create.Reserve)
	}
	folderErr := foldersOnlyOf(h.owner, h.root, h.path(ownSwapFilePath))
	switch {
	case slices.ContainsFunc(rows, func(f swapFileFacts) bool { return f.row.Path == ownSwapFilePath }):
		create.BlockedReason = apischema.SwapFileCreateExists
	case folderErr != nil && !errors.Is(folderErr, fs.ErrPermission):
		create.BlockedReason = apischema.SwapFileCreateFolderNotRootOnly
	case h.ownPathTaken(folder == ownSwapDir && info.kind == "btrfs" && !info.subvolumeRoot):
		create.BlockedReason = apischema.SwapFileCreatePathTaken
	case err != nil:
		create.BlockedReason = apischema.SwapFileCreateUnsupportedFilesystem
	case fsRefusal(mount, info) != "":
		create.BlockedReason = apischema.SwapFileCreateReason(fsRefusal(mount, info))
	case h.hibernates(resume, mount):
		create.BlockedReason = apischema.SwapFileCreateHibernation
	case create.MaxSize < create.MinSize:
		create.BlockedReason = apischema.SwapFileCreateNotEnoughSpace
	}
	// The default is a size a request may carry: swap_file_size 0 asks for it.
	if create.BlockedReason == "" {
		create.DefaultSize = min(create.DefaultSize, create.MaxSize)
	}
	return create, mount
}

// ownPathTaken reports whether something that is not LinuxIO's is in its
// swap folder: anything but a folder at the folder's name, a name in it
// other than the swap file's, anything in it while it is a plain folder on
// btrfs (plainBtrfs), which Create replaces with a subvolume only while it
// is empty, or anything at the swap file's name that is not a swap file a
// cut-short Create left. What the reader may not look into is not judged.
func (h hostRoot) ownPathTaken(plainBtrfs bool) bool {
	info, err := os.Lstat(h.path(ownSwapDir))
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	entries, err := os.ReadDir(h.path(ownSwapDir))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if plainBtrfs || entry.Name() != filepath.Base(ownSwapFilePath) {
			return true
		}
	}
	if len(entries) == 0 {
		return false
	}
	return !h.leftoverOwnSwapFile()
}

// leftoverOwnSwapFile reports whether what is at LinuxIO's swap file path,
// which no row lists, is a swap file a Create that was cut short left: a
// regular file root owns that starts with a swap signature. Create deletes
// it and makes it again. A header the reader may not read is not judged.
func (h hostRoot) leftoverOwnSwapFile() bool {
	info, err := os.Lstat(h.path(ownSwapFilePath))
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || st.Uid != h.owner {
		return false
	}
	swap, err := h.holdsSwap(ownSwapFilePath)
	return swap || errors.Is(err, fs.ErrPermission)
}

// moveSize is the size Move makes LinuxIO's file at for a file of length
// bytes: the same length, in whole MiB, and at least the smallest size.
func moveSize(length uint64) uint64 {
	return max(ceilMiB(length), swapFileMinSize)
}

// canMove reports whether Move is offered for the row: an active swap file
// that passes the strict check and blocks snapshots, on the filesystem
// LinuxIO's folder is on, while Create is offered and has room for a file of
// its size beside it.
func canMove(facts swapFileFacts, create apischema.SwapFileCreate, createMount mountEntry) bool {
	row := facts.row
	return row.Active && row.LockedReason == "" && !row.Temporary && row.BlocksSnapshots &&
		create.BlockedReason == "" && facts.mount.source != "" &&
		facts.mount.source == createMount.source && facts.mount.fsType == createMount.fsType &&
		moveSize(facts.length) <= create.MaxSize
}

// fileIDOf is the identity of the file at the host path name, read without
// following a link.
func (h hostRoot) fileIDOf(name string) (fileID, error) {
	info, err := os.Lstat(h.path(name))
	if err != nil {
		return fileID{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, errors.New("no stat")
	}
	return fileID{dev: st.Dev, ino: st.Ino}, nil
}
