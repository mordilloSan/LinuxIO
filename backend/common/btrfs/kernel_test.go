package btrfs

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// fakeDevice is one member of a fakeKernel filesystem.
type fakeDevice struct {
	path        string
	total, used uint64
	physical    uint64
	stats       [5]uint64
	running     *scrubProgress // SCRUB_PROGRESS while a scrub runs
	scrubResult scrubProgress  // what a SCRUB call returns
}

// fakeKernel answers the btrfs ioctls for one filesystem and records the
// mutating ones. Blocking operations call the hooks, which may wait on
// cancelled to order events deterministically.
type fakeKernel struct {
	mu      sync.Mutex
	label   string
	maxID   uint64
	devices map[uint64]*fakeDevice
	spaces  []spaceInfo
	fail    map[uintptr]error
	calls   []string

	balance         func(*balanceArgs) error
	balanceProgress func() (balanceArgs, error)
	scrub           func(id uint64) (scrubProgress, error)
	replaceStart    func(*devReplaceArgs) error
	replaceStatus   func() (state, perMill uint64)

	cancelled  chan struct{} // closed on the first cancel of any operation
	cancelOnce sync.Once
}

func (k *fakeKernel) log(format string, a ...any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, fmt.Sprintf(format, a...))
}

func (k *fakeKernel) Calls() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.calls...)
}

func (k *fakeKernel) cancel() { k.cancelOnce.Do(func() { close(k.cancelled) }) }

func (k *fakeKernel) ioctl(_ string, req uintptr, arg unsafe.Pointer) error {
	if err, ok := k.fail[req]; ok {
		return err
	}
	switch req {
	case iocFSInfo:
		*(*fsInfoArgs)(arg) = fsInfoArgs{MaxID: k.maxID, NumDevices: uint64(len(k.devices))}
	case iocGetFSLabel:
		buf := (*[labelSize]byte)(arg)
		*buf = [labelSize]byte{}
		copy(buf[:], k.label)
	case iocDevInfo:
		a := (*devInfoArgs)(arg)
		d, ok := k.devices[a.DevID]
		if !ok {
			return unix.ENODEV
		}
		a.Path = [1024]byte{}
		copy(a.Path[:], d.path)
		a.TotalBytes, a.BytesUsed = d.total, d.used
	case iocGetDevStats:
		a := (*devStatsArgs)(arg)
		d, ok := k.devices[a.DevID]
		if !ok {
			return unix.ENODEV
		}
		a.Values = d.stats
	case iocSpaceInfo:
		a := (*spaceArgs)(arg)
		a.TotalSpaces = uint64(len(k.spaces))
		copy(a.Spaces[:], k.spaces)
	case iocScrubProgress:
		a := (*scrubArgs)(arg)
		d, ok := k.devices[a.DevID]
		if !ok || d.running == nil {
			return unix.ENOTCONN
		}
		a.Progress = *d.running
	case iocBalanceProgress:
		if k.balanceProgress == nil {
			return unix.ENOTCONN
		}
		p, err := k.balanceProgress()
		if err != nil {
			return err
		}
		*(*balanceArgs)(arg) = p
	default:
		return k.mutate(req, arg)
	}
	return nil
}

// mutate handles the ioctls that change the filesystem or run an operation.
func (k *fakeKernel) mutate(req uintptr, arg unsafe.Pointer) error {
	switch req {
	case iocSetFSLabel:
		k.label = cstring((*[labelSize]byte)(arg)[:])
		k.log("SET_FSLABEL %s", k.label)
	case iocScrub:
		a := (*scrubArgs)(arg)
		k.log("SCRUB %d", a.DevID)
		if k.scrub != nil {
			p, err := k.scrub(a.DevID)
			a.Progress = p
			return err
		}
		if d, ok := k.devices[a.DevID]; ok {
			a.Progress = d.scrubResult
		}
	case iocScrubCancel:
		k.log("SCRUB_CANCEL")
		k.cancel()
	case iocAddDev:
		k.log("ADD_DEV %s", cstring((*volArgs)(arg).Name[:]))
	case iocResize:
		k.log("RESIZE %s", cstring((*volArgs)(arg).Name[:]))
	case iocRmDevV2:
		a := (*volArgsV2)(arg)
		k.log("RM_DEV %d flags=%d", getU64(a.Name[:], 0), a.Flags)
	case iocBalanceV2:
		a := (*balanceArgs)(arg)
		k.log("BALANCE flags=%d", a.Flags)
		if k.balance != nil {
			return k.balance(a)
		}
	case iocBalanceCtl:
		k.log("BALANCE_CTL %d", *(*int32)(arg))
		k.cancel()
	case iocDevReplace:
		return k.replace((*devReplaceArgs)(arg))
	default:
		return fmt.Errorf("unexpected ioctl %#x", req)
	}
	return nil
}

func (k *fakeKernel) replace(a *devReplaceArgs) error {
	switch a.Cmd {
	case replaceCmdStart:
		k.log("REPLACE_START %d %s", getU64(a.Params[:], replaceSrcDevID), cstring(a.Params[replaceTgtDevName:]))
		if k.replaceStart != nil {
			return k.replaceStart(a)
		}
	case replaceCmdStatus:
		if k.replaceStatus == nil {
			return unix.ENOTCONN
		}
		state, perMill := k.replaceStatus()
		putU64(a.Params[:], replaceStatusState, state)
		putU64(a.Params[:], replaceStatusPerMill, perMill)
	case replaceCmdCancel:
		k.log("REPLACE_CANCEL")
		k.cancel()
	}
	return nil
}

// raid1Kernel is a healthy two-disk raid1 filesystem labelled data.
func raid1Kernel() *fakeKernel {
	return &fakeKernel{
		label: "data", maxID: 2,
		devices: map[uint64]*fakeDevice{
			1: {path: "/dev/sda1", total: 100 << 30, used: 51 << 30, physical: 100 << 30},
			2: {path: "/dev/sdb1", total: 100 << 30, used: 51 << 30, physical: 100 << 30},
		},
		spaces: []spaceInfo{
			{Flags: bgData | bgRaid1, TotalBytes: 50 << 30, UsedBytes: 40 << 30},
			{Flags: bgMetadata | bgRaid1, TotalBytes: 1 << 30, UsedBytes: 512 << 20},
			{Flags: bgSystem | bgRaid1, TotalBytes: 32 << 20, UsedBytes: 16 << 10},
			{Flags: bgGlobalRsv, TotalBytes: 512 << 20},
		},
		cancelled: make(chan struct{}),
	}
}

// stubKernel installs k as the kernel behind /data (dataUUID), sizes block
// devices from it, polls fast and keeps scrub records in temp files.
func stubKernel(t *testing.T, k *fakeKernel) {
	t.Helper()
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	oldIoctl, oldSize, oldInterval := fsIoctl, blockDeviceSize, progressInterval
	oldRecords, oldProgs := ScrubRecordPath, progsScrubDir
	fsIoctl = k.ioctl
	blockDeviceSize = func(device string) (uint64, error) {
		for _, d := range k.devices {
			if d.path == device {
				return d.physical, nil
			}
		}
		return 0, fmt.Errorf("no such device %s", device)
	}
	progressInterval = time.Millisecond
	ScrubRecordPath = filepath.Join(t.TempDir(), "scrub.json")
	progsScrubDir = t.TempDir()
	t.Cleanup(func() {
		fsIoctl, blockDeviceSize, progressInterval = oldIoctl, oldSize, oldInterval
		ScrubRecordPath, progsScrubDir = oldRecords, oldProgs
	})
}
