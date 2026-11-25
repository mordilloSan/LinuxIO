package btrfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// fakeDevice is one member of a fakeKernel filesystem.
type fakeDevice struct {
	path        string
	uuid        string // the device's own UUID; zero when empty
	total, used uint64
	physical    uint64
	stats       [5]uint64
	running     *scrubProgress // SCRUB_PROGRESS while a scrub runs; set with setRunning while one does
	scrubResult scrubProgress  // what a SCRUB call returns
}

// fakeKernel answers the btrfs ioctls for one filesystem and records the
// mutating ones. Blocking operations call the hooks, which may wait on
// cancelled to order events deterministically.
type fakeKernel struct {
	mu         sync.Mutex
	label      string
	generation uint64
	maxID      uint64
	devices    map[uint64]*fakeDevice
	spaces     []spaceInfo
	fail       map[uintptr]error
	calls      []string
	trees      fakeTrees // what tree searches find, such as the replace item

	balance         func(*balanceArgs) error
	balanceProgress func() (balanceArgs, error)
	scrub           func(id, start uint64) (scrubProgress, error)
	scrubCancel     func() error // nil: every SCRUB_CANCEL finds a scrub
	replaceStart    func(*devReplaceArgs) error
	replaceStatus   func() (state, perMill uint64)
	replaceStarted  uint64                 // STATUS time_started, seconds since the epoch
	replaceStopped  uint64                 // STATUS time_stopped, seconds since the epoch
	onCall          func(call string)      // sees RESET_DEV_STATS and SET_FSLABEL as they happen
	replaceCancel   func() (result uint64) // nil: every cancel is taken
	remove          func(id uint64) error
	removing        atomic.Bool // the remove hook has started
	statsFail       atomic.Bool // GET_DEV_STATS fails, as when the kernel cannot be asked
	statsResetFail  error       // what a GET_DEV_STATS that resets fails with; nil resets
	readOnly        bool        // statfs says the pool is read-only, as after a transaction abort
	freeBytes       uint64      // statfs's room for more data (f_bavail times f_bsize)
	freeErr         error       // what statfs's room fails with; nil answers freeBytes

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

// setRunning sets what SCRUB_PROGRESS reports for device id; nil is idle.
func (k *fakeKernel) setRunning(id uint64, p *scrubProgress) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.devices[id].running = p
}

// setErrors sets device id's write and flush error counters, as the kernel
// raises them when the disk fails a write or a cache flush.
func (k *fakeKernel) setErrors(id uint64, write, flush uint64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.devices[id].stats[0], k.devices[id].stats[2] = write, flush
}

// setCounters sets every error counter of device id.
func (k *fakeKernel) setCounters(id uint64, e BtrfsDeviceErrors) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.devices[id].stats = [5]uint64{e.Write, e.Read, e.Flush, e.Corruption, e.Generation}
}

// counters returns the error counters of device id.
func (k *fakeKernel) counters(id uint64) BtrfsDeviceErrors {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := k.devices[id].stats
	return BtrfsDeviceErrors{Write: s[0], Read: s[1], Flush: s[2], Corruption: s[3], Generation: s[4]}
}

// devStats is one GET_DEV_STATS call: the values up to nr_items, and with
// BTRFS_DEV_STATS_RESET all five zeroed (volumes.c btrfs_get_dev_stats).
func (k *fakeKernel) devStats(a *devStatsArgs) error {
	if k.statsFail.Load() {
		return unix.EIO
	}
	d, ok := k.devices[a.DevID]
	if !ok {
		return unix.ENODEV
	}
	k.mu.Lock() // setErrors changes them while a scrub runs
	defer k.mu.Unlock()
	if a.Flags&devStatsReset != 0 && k.statsResetFail != nil {
		return k.statsResetFail
	}
	copy(a.Values[:min(a.NrItems, 5)], d.stats[:])
	if a.Flags&devStatsReset != 0 {
		d.stats = [5]uint64{}
		k.calls = append(k.calls, fmt.Sprintf("RESET_DEV_STATS %d", a.DevID))
		if k.onCall != nil {
			k.onCall(fmt.Sprintf("RESET_DEV_STATS %d", a.DevID))
		}
	}
	return nil
}

func (k *fakeKernel) ioctl(path string, req uintptr, arg unsafe.Pointer) error {
	if err, ok := k.fail[req]; ok {
		return err
	}
	switch req {
	case iocTreeSearchV2:
		return k.trees.ioctl(path, req, arg)
	case iocFSInfo:
		a := (*fsInfoArgs)(arg)
		requested := a.Flags
		*a = fsInfoArgs{MaxID: k.maxID, NumDevices: uint64(len(k.devices)), FSID: uuidBytes(dataUUID)}
		if requested&fsInfoFlagGeneration != 0 {
			a.Flags, a.Generation = fsInfoFlagGeneration, k.generation
		}
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
		if d.uuid != "" {
			a.UUID = uuidBytes(d.uuid)
		}
		k.mu.Lock() // a remove hook moves data off a device while it is polled
		a.TotalBytes, a.BytesUsed = d.total, d.used
		k.mu.Unlock()
	case iocGetDevStats:
		return k.devStats((*devStatsArgs)(arg))
	case iocSpaceInfo:
		a := (*spaceArgs)(arg)
		a.TotalSpaces = uint64(len(k.spaces))
		copy(a.Spaces[:], k.spaces)
	case iocScrubProgress:
		a := (*scrubArgs)(arg)
		k.mu.Lock()
		defer k.mu.Unlock()
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
		if k.onCall != nil {
			k.onCall("SET_FSLABEL " + k.label)
		}
	case iocScrub:
		return k.scrubCall((*scrubArgs)(arg))
	case iocScrubCancel:
		k.log("SCRUB_CANCEL")
		if k.scrubCancel != nil {
			if err := k.scrubCancel(); err != nil {
				return err
			}
		}
		k.cancel()
	case iocAddDev:
		k.log("ADD_DEV %s", cstring((*volArgs)(arg).Name[:]))
	case iocResize:
		k.log("RESIZE %s", cstring((*volArgs)(arg).Name[:]))
	case iocRmDevV2:
		a := (*volArgsV2)(arg)
		if a.Flags == 0 && cstring(a.Name[:]) == "cancel" {
			k.log("RM_DEV cancel")
			if !k.removing.Load() {
				return unix.ENOTCONN // no removal holds the pool
			}
			k.cancel()
			return unix.ECANCELED
		}
		k.log("RM_DEV %d flags=%d", getU64(a.Name[:], 0), a.Flags)
		if k.remove != nil {
			k.removing.Store(true)
			return k.remove(getU64(a.Name[:], 0))
		}
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

// scrubCall is one BTRFS_IOC_SCRUB call.
func (k *fakeKernel) scrubCall(a *scrubArgs) error {
	if a.End != ^uint64(0) {
		// The real kernel scrubs nothing outside the range and still succeeds.
		return fmt.Errorf("scrub range %d-%d does not reach the end of device %d", a.Start, a.End, a.DevID)
	}
	if a.Start != 0 {
		k.log("SCRUB %d from %d", a.DevID, a.Start)
	} else {
		k.log("SCRUB %d", a.DevID)
	}
	if k.scrub != nil {
		p, err := k.scrub(a.DevID, a.Start)
		a.Progress = p
		return err
	}
	if d, ok := k.devices[a.DevID]; ok {
		a.Progress = d.scrubResult
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
			return nil // never started: every field zero, as the kernel reports it
		}
		state, perMill := k.replaceStatus()
		putU64(a.Params[:], replaceStatusState, state)
		putU64(a.Params[:], replaceStatusPerMill, perMill)
		k.mu.Lock()
		putU64(a.Params[:], replaceStatusTimeStarted, k.replaceStarted)
		putU64(a.Params[:], replaceStatusTimeStopped, k.replaceStopped)
		k.mu.Unlock()
	case replaceCmdCancel:
		k.log("REPLACE_CANCEL")
		if k.replaceCancel != nil {
			if a.Result = k.replaceCancel(); a.Result != 0 {
				return nil
			}
		}
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
// devices from it, lists its present devices in a sysfs of its own, polls
// fast, keeps scrub records in temp files and flushes no real device.
func stubKernel(t *testing.T, k *fakeKernel) {
	t.Helper()
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	stubStatePaths(t, t.TempDir())
	sys := t.TempDir()
	for _, d := range k.devices {
		if d.path != "" {
			if err := os.MkdirAll(filepath.Join(sys, dataUUID, "devices", filepath.Base(d.path)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	oldIoctl, oldCtl, oldSize, oldInterval, oldProgs, oldFlush, oldSys, oldNotBusy := fsIoctl, balanceCtl, blockDeviceSize, progressInterval, progsScrubDir, flushDevice, sysFSBtrfs, deviceNotBusy
	flushDevice, sysFSBtrfs = func(string) error { return nil }, sys
	// The pool holds every member it lists.
	deviceNotBusy = func(device string) error { return fmt.Errorf("%s is in use: %w", device, unix.EBUSY) }
	fsIoctl = k.ioctl
	oldReadOnly, oldUnitState, oldFree := poolReadOnly, unitActiveState, poolFreeBytes
	poolReadOnly = func(string) (bool, error) { return k.readOnly, nil }
	poolFreeBytes = func(string) (uint64, error) { return k.freeBytes, k.freeErr }
	unitActiveState = func(context.Context, string) (string, error) { return "inactive", nil }
	t.Cleanup(func() { poolReadOnly, unitActiveState, poolFreeBytes = oldReadOnly, oldUnitState, oldFree })
	// A descriptor of the mount is one of a folder of its own, which the
	// kernel answers; any other descriptor goes on as before.
	mountDir, oldOpen, oldFD := t.TempDir(), openMount, ioctlFD
	openMount = func(string) (int, error) {
		return unix.Open(mountDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}
	ioctlFD = func(fd int, req uintptr, arg unsafe.Pointer) error {
		if path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd)); err == nil && path == mountDir {
			return k.ioctl(mountDir, req, arg)
		}
		return oldFD(fd, req, arg)
	}
	t.Cleanup(func() { openMount, ioctlFD = oldOpen, oldFD })
	balanceCtl = func(mnt string, cmd uintptr) error {
		arg := int32(cmd)
		return k.ioctl(mnt, iocBalanceCtl, unsafe.Pointer(&arg))
	}
	blockDeviceSize = func(device string) (uint64, error) {
		for _, d := range k.devices {
			if d.path == device {
				return d.physical, nil
			}
		}
		return 0, fmt.Errorf("no such device %s", device)
	}
	progressInterval = time.Millisecond
	progsScrubDir = t.TempDir()
	t.Cleanup(func() {
		fsIoctl, balanceCtl, blockDeviceSize, progressInterval, progsScrubDir, flushDevice, sysFSBtrfs, deviceNotBusy = oldIoctl, oldCtl, oldSize, oldInterval, oldProgs, oldFlush, oldSys, oldNotBusy
	})
}
