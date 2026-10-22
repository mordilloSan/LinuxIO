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

var profileFlags = map[string]uint64{
	"single": bgSingle, "dup": bgDup, "raid0": bgRaid0, "raid1": bgRaid1,
	"raid1c3": bgRaid1c3, "raid1c4": bgRaid1c4, "raid10": bgRaid10, "raid5": bgRaid5, "raid6": bgRaid6,
}

// profileName names the profile bits of block-group flags; no bit is single.
func profileName(flags uint64) string {
	for name, bit := range profileFlags {
		if bit != bgSingle && flags&bit != 0 {
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

	balanceArgsUsage   = 1 << 1
	balanceArgsConvert = 1 << 8

	balanceStateRunning  = 1 << 0
	balanceStatePauseReq = 1 << 1

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

	replaceStateStarted  = 1
	replaceStateFinished = 2

	// Offsets inside Params.
	replaceSrcDevID      = 0
	replaceContReading   = 8
	replaceTgtDevName    = 16 + 1025
	replaceStatusState   = 0
	replaceStatusPerMill = 8
)

// replaceResults are the Result codes of a start command.
var replaceResults = map[uint64]string{
	1: "replace not started",
	2: "a replace is already running",
	3: "a scrub is running",
}

// cstring returns the NUL-terminated string at the start of b.
func cstring(b []byte) string {
	s, _, _ := strings.Cut(string(b), "\x00")
	return s
}

// fsIoctl issues one ioctl on an open descriptor of the mounted filesystem;
// a var so tests can fake the kernel.
var fsIoctl = func(mountpoint string, req uintptr, arg unsafe.Pointer) error {
	fd, err := unix.Open(mountpoint, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", mountpoint, err)
	}
	defer unix.Close(fd)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
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
