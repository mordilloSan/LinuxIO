package btrfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The kernel ABI from linux/btrfs.h and linux/btrfs_tree.h. Every struct
// here mirrors a kernel struct byte for byte; ioctl_test.go pins the sizes
// through the request numbers, which encode them.

const (
	iocMagic = 0x94
	ioNone   = 0
	ioWrite  = 1 << 30
	ioRead   = 2 << 30
	ioRW     = 3 << 30
)

// Request numbers: direction | size << 16 | magic << 8 | number.
const (
	iocResize          = ioWrite | unsafe.Sizeof(volArgs{})<<16 | iocMagic<<8 | 3
	iocScanDev         = ioWrite | unsafe.Sizeof(volArgs{})<<16 | iocMagic<<8 | 4
	iocForgetDev       = ioWrite | unsafe.Sizeof(volArgs{})<<16 | iocMagic<<8 | 5
	iocAddDev          = ioWrite | unsafe.Sizeof(volArgs{})<<16 | iocMagic<<8 | 10
	iocSubvolCreate    = ioWrite | unsafe.Sizeof(volArgs{})<<16 | iocMagic<<8 | 14
	iocSnapDestroy     = ioWrite | unsafe.Sizeof(volArgs{})<<16 | iocMagic<<8 | 15
	iocTreeSearchV2    = ioRW | (unsafe.Sizeof(searchKey{})+8)<<16 | iocMagic<<8 | 17 // sized by key and buf_size
	iocInoLookup       = ioRW | unsafe.Sizeof(inoLookupArgs{})<<16 | iocMagic<<8 | 18
	iocDefaultSubvol   = ioWrite | 8<<16 | iocMagic<<8 | 19
	iocSpaceInfo       = ioRW | 16<<16 | iocMagic<<8 | 20 // sized by the two-word header
	iocSnapCreateV2    = ioWrite | unsafe.Sizeof(volArgsV2{})<<16 | iocMagic<<8 | 23
	iocSubvolGetFlags  = ioRead | 8<<16 | iocMagic<<8 | 25
	iocSubvolSetFlags  = ioWrite | 8<<16 | iocMagic<<8 | 26
	iocScrub           = ioRW | unsafe.Sizeof(scrubArgs{})<<16 | iocMagic<<8 | 27
	iocScrubCancel     = ioNone | iocMagic<<8 | 28
	iocScrubProgress   = ioRW | unsafe.Sizeof(scrubArgs{})<<16 | iocMagic<<8 | 29
	iocDevInfo         = ioRW | unsafe.Sizeof(devInfoArgs{})<<16 | iocMagic<<8 | 30
	iocSend            = ioWrite | unsafe.Sizeof(sendArgs{})<<16 | iocMagic<<8 | 38
	iocQuotaCtl        = ioRW | unsafe.Sizeof(quotaCtlArgs{})<<16 | iocMagic<<8 | 40
	iocQgroupLimit     = ioRead | unsafe.Sizeof(qgroupLimitArgs{})<<16 | iocMagic<<8 | 43 // _IOR in the header
	iocFSInfo          = ioRead | unsafe.Sizeof(fsInfoArgs{})<<16 | iocMagic<<8 | 31
	iocBalanceV2       = ioRW | unsafe.Sizeof(balanceArgs{})<<16 | iocMagic<<8 | 32
	iocBalanceCtl      = ioWrite | 4<<16 | iocMagic<<8 | 33
	iocBalanceProgress = ioRead | unsafe.Sizeof(balanceArgs{})<<16 | iocMagic<<8 | 34
	iocGetFSLabel      = ioRead | labelSize<<16 | iocMagic<<8 | 49
	iocSetFSLabel      = ioWrite | labelSize<<16 | iocMagic<<8 | 50
	iocGetDevStats     = ioRW | unsafe.Sizeof(devStatsArgs{})<<16 | iocMagic<<8 | 52
	iocDevReplace      = ioRW | unsafe.Sizeof(devReplaceArgs{})<<16 | iocMagic<<8 | 53
	iocRmDevV2         = ioWrite | unsafe.Sizeof(volArgsV2{})<<16 | iocMagic<<8 | 58
)

// volArgs mirrors struct btrfs_ioctl_vol_args.
type volArgs struct {
	Fd   int64
	Name [4088]byte
}

// volArgsV2 mirrors struct btrfs_ioctl_vol_args_v2; Name is the union
// that also carries a devid.
type volArgsV2 struct {
	Fd      int64
	Transid uint64
	Flags   uint64
	Unused  [4]uint64
	Name    [4040]byte
}

const devSpecByID = 1 << 3 // BTRFS_DEVICE_SPEC_BY_ID

// fsInfoFlagGeneration asks BTRFS_IOC_FS_INFO for the current generation.
const fsInfoFlagGeneration = 1 << 1

// fsInfoArgs mirrors struct btrfs_ioctl_fs_info_args.
type fsInfoArgs struct {
	MaxID          uint64
	NumDevices     uint64
	FSID           [16]byte
	Nodesize       uint32
	Sectorsize     uint32
	CloneAlignment uint32
	CsumType       uint16
	CsumSize       uint16
	Flags          uint64
	Generation     uint64
	MetadataUUID   [16]byte
	Reserved       [944]byte
}

// devInfoArgs mirrors struct btrfs_ioctl_dev_info_args.
type devInfoArgs struct {
	DevID      uint64
	UUID       [16]byte
	BytesUsed  uint64
	TotalBytes uint64
	FSID       [16]byte
	Unused     [377]uint64
	Path       [1024]byte
}

// spaceInfo mirrors struct btrfs_ioctl_space_info.
type spaceInfo struct {
	Flags      uint64
	TotalBytes uint64
	UsedBytes  uint64
}

// spaceArgs mirrors struct btrfs_ioctl_space_args with room for every
// block-group type and profile combination the kernel can report.
type spaceArgs struct {
	SpaceSlots  uint64
	TotalSpaces uint64
	Spaces      [64]spaceInfo
}

// Block-group flags from btrfs_tree.h.
const (
	bgData      = 1 << 0
	bgSystem    = 1 << 1
	bgMetadata  = 1 << 2
	bgRaid0     = 1 << 3
	bgRaid1     = 1 << 4
	bgDup       = 1 << 5
	bgRaid10    = 1 << 6
	bgRaid5     = 1 << 7
	bgRaid6     = 1 << 8
	bgRaid1c3   = 1 << 9
	bgRaid1c4   = 1 << 10
	bgSingle    = 1 << 48 // BTRFS_AVAIL_ALLOC_BIT_SINGLE, the extended "single"
	bgGlobalRsv = 1 << 49 // BTRFS_SPACE_INFO_GLOBAL_RSV
)

// profileName names the profile bits of block-group flags; no bit is single.
func profileName(flags uint64) string {
	for name, p := range profiles {
		if p.flag != bgSingle && flags&p.flag != 0 {
			return name
		}
	}
	return "single"
}

// kindName names the block-group type bits the way `filesystem usage` does.
func kindName(flags uint64) string {
	var kinds []string
	for _, k := range []struct {
		bit  uint64
		name string
	}{{bgData, "data"}, {bgMetadata, "metadata"}, {bgSystem, "system"}} {
		if flags&k.bit != 0 {
			kinds = append(kinds, k.name)
		}
	}
	return strings.Join(kinds, "+")
}

// devStatsArgs mirrors struct btrfs_ioctl_get_dev_stats; its size is 1032,
// a kernel quirk.
type devStatsArgs struct {
	DevID   uint64
	NrItems uint64
	Flags   uint64
	Values  [5]uint64
	Unused  [121]uint64
}

// balanceFilter mirrors struct btrfs_balance_args.
type balanceFilter struct {
	Profiles   uint64
	Usage      uint64
	DevID      uint64
	PStart     uint64
	PEnd       uint64
	VStart     uint64
	VEnd       uint64
	Target     uint64
	Flags      uint64
	Limit      uint64
	StripesMin uint32
	StripesMax uint32
	Unused     [6]uint64
}

type balanceProgress struct {
	Expected   uint64
	Considered uint64
	Completed  uint64
}

// balanceArgs mirrors struct btrfs_ioctl_balance_args.
type balanceArgs struct {
	Flags  uint64
	State  uint64
	Data   balanceFilter
	Meta   balanceFilter
	Sys    balanceFilter
	Stat   balanceProgress
	Unused [72]uint64
}

const (
	balanceData     = 1 << 0
	balanceSystem   = 1 << 1
	balanceMetadata = 1 << 2
	balanceResume   = 1 << 4 // continue the paused balance

	balanceArgsUsage   = 1 << 1
	balanceArgsConvert = 1 << 8
	balanceArgsSoft    = 1 << 9 // convert only chunks not already in the target profile

	balanceStateRunning   = 1 << 0
	balanceStatePauseReq  = 1 << 1
	balanceStateCancelReq = 1 << 2

	balanceCtlPause  = 1
	balanceCtlCancel = 2
)

// scrubProgress mirrors struct btrfs_scrub_progress.
type scrubProgress struct {
	DataExtentsScrubbed uint64
	TreeExtentsScrubbed uint64
	DataBytesScrubbed   uint64
	TreeBytesScrubbed   uint64
	ReadErrors          uint64
	CsumErrors          uint64
	VerifyErrors        uint64
	NoCsum              uint64
	CsumDiscards        uint64
	SuperErrors         uint64
	MallocErrors        uint64
	UncorrectableErrors uint64
	CorrectedErrors     uint64
	LastPhysical        uint64
	UnverifiedErrors    uint64
}

// scrubArgs mirrors struct btrfs_ioctl_scrub_args.
type scrubArgs struct {
	DevID    uint64
	Start    uint64
	End      uint64
	Flags    uint64
	Progress scrubProgress
	Unused   [109]uint64
}

// devReplaceArgs mirrors struct btrfs_ioctl_dev_replace_args. Params is the
// union of the start and status parameter structs, 2072 bytes with padding.
type devReplaceArgs struct {
	Cmd    uint64
	Result uint64
	Params [2072]byte
	Spare  [64]uint64
}

const (
	replaceCmdStart  = 0
	replaceCmdStatus = 1
	replaceCmdCancel = 2

	replaceStateStarted   = 1
	replaceStateFinished  = 2
	replaceStateCanceled  = 3
	replaceStateSuspended = 4 // an unmount stopped it; the next rw mount resumes it

	// Offsets inside Params.
	replaceSrcDevID          = 0
	replaceContReading       = 8
	replaceTgtDevName        = 16 + 1025
	replaceStatusState       = 0
	replaceStatusPerMill     = 8
	replaceStatusTimeStarted = 16 // seconds since the epoch, kept across a power loss
)

// replaceResultNotStarted is also what a cancel reports when it canceled
// nothing.
const replaceResultNotStarted = 1

// replaceResults are the Result codes of a start command.
var replaceResults = map[uint64]string{
	replaceResultNotStarted: "replace not started",
	2:                       "a replace is already running",
	3:                       "a scrub is running",
}

// cstring returns the NUL-terminated string at the start of b.
func cstring(b []byte) string {
	s, _, _ := strings.Cut(string(b), "\x00")
	return s
}

// kernelCode is a refusal a btrfs ioctl returns as a positive value, an
// enum btrfs_err_code such as 8 while another exclusive operation runs.
type kernelCode uint64

// kernelCodeTexts are btrfs_err_str's texts (btrfs-progs libbtrfs/ioctl.h).
var kernelCodeTexts = map[kernelCode]string{
	1:  "unable to go below two devices on raid1",
	2:  "unable to go below four/two devices on raid10",
	3:  "unable to go below two devices on raid5",
	4:  "unable to go below three devices on raid6",
	5:  "unable to remove the dev_replace target dev",
	6:  "no missing devices found to remove",
	7:  "unable to remove the only writeable device",
	8:  "add/delete/balance/replace/resize operation in progress",
	9:  "unable to go below three devices on raid1c3",
	10: "unable to go below four devices on raid1c4",
}

func (c kernelCode) Error() string {
	if text, ok := kernelCodeTexts[c]; ok {
		return text
	}
	return fmt.Sprintf("kernel refused with code %d", uint64(c))
}

// fsIoctl issues one ioctl on an open descriptor of the mounted filesystem;
// a var so tests can fake the kernel.
var fsIoctl = func(mountpoint string, req uintptr, arg unsafe.Pointer) error {
	fd, err := unix.Open(mountpoint, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", mountpoint, err)
	}
	defer unix.Close(fd)
	r1, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	if r1 != 0 {
		return kernelCode(r1)
	}
	return nil
}

// balanceCtl sends cmd, pause or cancel, to BALANCE_CTL. The kernel reads
// the command from the argument itself, not through a pointer; a var so tests
// can fake the kernel.
var balanceCtl = func(mountpoint string, cmd uintptr) error {
	fd, err := unix.Open(mountpoint, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", mountpoint, err)
	}
	defer unix.Close(fd)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), iocBalanceCtl, cmd); errno != 0 {
		return errno
	}
	return nil
}

// syncSignals are the faults a thread raises itself. Blocked, the kernel
// would kill the process on one instead of letting Go panic.
var syncSignals = []unix.Signal{unix.SIGSEGV, unix.SIGBUS, unix.SIGFPE, unix.SIGILL, unix.SIGTRAP, unix.SIGSYS}

// blockingIoctl runs a long ioctl (scrub, replace, balance, device removal)
// on a thread that blocks every other signal. Since 6.19 any signal pending
// on the calling thread aborts a scrub or a replace, and Go takes all of them
// (SIGURG, SIGCHLD, ...) on whichever thread it likes.
func blockingIoctl(mountpoint string, req uintptr, arg unsafe.Pointer) error {
	done := make(chan error, 1)
	go func() {
		// Never unlocked: the runtime ends the thread, and its mask, with
		// this goroutine.
		runtime.LockOSThread()
		// The mask also blocks signal 33, Go's per-thread syscall signal, so
		// a syscall.Setuid-family call elsewhere in the process would stall
		// until the ioctl returns; none exists today.
		var set unix.Sigset_t
		set.Val[0] = ^uint64(0) // the kernel's 64 signals; amd64 and arm64 only
		for _, sig := range syncSignals {
			set.Val[0] &^= 1 << (sig - 1)
		}
		if err := unix.PthreadSigmask(unix.SIG_BLOCK, &set, nil); err != nil {
			done <- fmt.Errorf("block signals: %w", err)
			return
		}
		done <- fsIoctl(mountpoint, req, arg)
	}()
	return <-done
}

// rmDevCancel cancels the running device removal (kernel 5.14). The kernel
// answers ECANCELED when a removal holds the pool and ENOTCONN otherwise.
func rmDevCancel(mountpoint string) error {
	var args volArgsV2 // Flags 0: BTRFS_DEVICE_SPEC_BY_ID turns the cancel off
	copy(args.Name[:], "cancel")
	if err := fsIoctl(mountpoint, iocRmDevV2, unsafe.Pointer(&args)); !errors.Is(err, unix.ECANCELED) {
		return err
	}
	return nil
}

// controlIoctl sends one device path to /dev/btrfs-control; a var so tests
// can record calls.
var controlIoctl = func(req uintptr, device string) error {
	var args volArgs
	if len(device) >= len(args.Name) {
		return fmt.Errorf("device path too long: %s", device)
	}
	copy(args.Name[:], device)
	f, err := os.OpenFile(controlDevice, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s is missing; is the btrfs kernel module loaded? (%w)", controlDevice, err)
		}
		return err
	}
	defer f.Close()
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&args)))
	runtime.KeepAlive(&args)
	if errno != 0 {
		return errno
	}
	return nil
}

// blockDeviceSize reads the size of a block device; a var so tests can stub it.
var blockDeviceSize = func(device string) (uint64, error) {
	f, err := os.Open(device)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var size uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKGETSIZE64, uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(&size)
	if errno != 0 {
		return 0, fmt.Errorf("size of %s: %w", device, errno)
	}
	return size, nil
}

// putU64 writes v little-endian at off.
func putU64(b []byte, off int, v uint64) {
	binary.LittleEndian.PutUint64(b[off:], v)
}

func getU64(b []byte, off int) uint64 {
	return binary.LittleEndian.Uint64(b[off:])
}
