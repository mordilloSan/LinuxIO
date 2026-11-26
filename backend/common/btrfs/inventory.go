package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// BtrfsFilesystem is one btrfs filesystem with its allocation, devices,
// error counters and scrub state, read through the kernel's ioctls.
type BtrfsFilesystem struct {
	UUID             string            `json:"uuid"`
	Label            string            `json:"label"`
	Mountpoints      []Mount           `json:"mountpoints"`
	TotalBytes       uint64            `json:"totalBytes"`
	UnallocatedBytes uint64            `json:"unallocatedBytes"`
	UsedBytes        uint64            `json:"usedBytes"`
	StoredBytes      uint64            `json:"storedBytes"`
	FreeBytes        uint64            `json:"freeBytes"`
	Allocation       []BtrfsAllocation `json:"allocation"`
	Devices          []BtrfsDevice     `json:"devices"`
	Scrub            BtrfsScrubStatus  `json:"scrub"`
	Degraded         bool              `json:"degraded"`
	// LowMetadataSpace is set when metadata is nearly full and no
	// unallocated space is left to grow it, the btrfs way to ENOSPC.
	LowMetadataSpace bool `json:"lowMetadataSpace"`
	// HealthKnown is true only when both the device list and the error
	// counters were read, so callers can tell "healthy" from "unknown".
	HealthKnown bool `json:"-"`
	// Spare is the pool's hot spare as it is now, nil when it has none (see
	// spare.go). spareKnown is false when the designations could not be
	// read, so a missing Spare does not resolve the spare alert.
	Spare      *BtrfsSpare `json:"spare,omitzero"`
	spareKnown bool
	// AutoRebuild is set while LinuxIO rebuilds the pool onto a missing
	// member's own disk when it comes back, and Rebuild is that rebuild, or
	// the hot spare's, as it stands (see rebuild.go).
	AutoRebuild bool          `json:"autoRebuild"`
	Rebuild     *BtrfsRebuild `json:"rebuild,omitzero"`
	// Balance is the kernel's balance of the pool, whoever started it (a
	// job, the command line, btrfsmaintenance's timer); nil when there is
	// none or the bridge is not root, since reading it needs CAP_SYS_ADMIN.
	Balance *BtrfsBalanceStatus `json:"balance,omitzero"`
}

// BtrfsBalanceStatus is a balance the kernel holds: State is running or
// paused (a paused one continues when the pool is mounted again), Percent
// the share of the chunks it expects to move that it has moved.
type BtrfsBalanceStatus struct {
	State   string  `json:"state"`
	Percent float64 `json:"percent"`
}

// BtrfsAllocation is one block-group type (data, metadata, system) with
// logical sizes.
type BtrfsAllocation struct {
	Kind       string `json:"kind"`
	Profile    string `json:"profile"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

type BtrfsDevice struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	SizeBytes uint64 `json:"sizeBytes"`
	UsedBytes uint64 `json:"usedBytes"`
	Missing   bool   `json:"missing"`
	// DroppedOut is set, with Missing, when the device's block device went
	// away while the pool is mounted (see markDroppedOut): the kernel keeps
	// the device, with its path, until the pool is mounted again.
	DroppedOut bool              `json:"droppedOut,omitzero"`
	Errors     BtrfsDeviceErrors `json:"errors"`
	// PhysicalBytes is the block device's size; Growable is set when the
	// filesystem has not grown into it, as after replacing with a bigger disk.
	PhysicalBytes uint64 `json:"physicalBytes"`
	Growable      bool   `json:"growable"`
	// UUID is the device's own UUID, which a scrub's saved progress is
	// matched against.
	UUID string `json:"-"`
}

type BtrfsDeviceErrors struct {
	Write      uint64 `json:"write"`
	Read       uint64 `json:"read"`
	Flush      uint64 `json:"flush"`
	Corruption uint64 `json:"corruption"`
	Generation uint64 `json:"generation"`
}

// BtrfsScrubStatus Status is one of none, running, finished, aborted, canceled.
type BtrfsScrubStatus struct {
	Status              string     `json:"status"`
	StartedAt           *time.Time `json:"startedAt"`
	FinishedAt          *time.Time `json:"finishedAt"`
	ErrorCount          uint64     `json:"errorCount"`
	UncorrectableErrors uint64     `json:"uncorrectableErrors"`
	CorrectedErrors     uint64     `json:"correctedErrors"`
	BytesScrubbed       uint64     `json:"bytesScrubbed"`
	Percent             float64    `json:"percent"`
}

// add accumulates one device's kernel counters.
func (s *BtrfsScrubStatus) add(p scrubProgress) {
	s.addCounters(countersOf(p))
}

// addCounters accumulates counters a scrub record kept.
func (s *BtrfsScrubStatus) addCounters(c scrubCounters) {
	s.BytesScrubbed += c.BytesScrubbed
	s.ErrorCount += c.ErrorCount
	s.UncorrectableErrors += c.UncorrectableErrors
	s.CorrectedErrors += c.CorrectedErrors
}

// growSlack is how much larger than the filesystem's view a block device
// must be before growing is offered; the kernel rounds sizes to sectors.
const growSlack = 64 << 20

// ListFilesystems returns every mounted btrfs filesystem. A read that fails
// for one filesystem is logged and leaves that part empty.
func ListFilesystems(ctx context.Context) ([]BtrfsFilesystem, error) {
	mounts, err := listMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list btrfs mounts: %w", err)
	}
	byUUID := map[string]*BtrfsFilesystem{}
	filesystems := make([]BtrfsFilesystem, 0)
	var order []string
	for _, m := range mounts {
		fs := byUUID[m.UUID]
		if fs == nil {
			fs = &BtrfsFilesystem{UUID: m.UUID}
			byUUID[m.UUID] = fs
			order = append(order, m.UUID)
		}
		fs.Mountpoints = append(fs.Mountpoints, m)
	}
	for _, uuid := range order {
		fs := byUUID[uuid]
		fillFilesystem(fs)
		filesystems = append(filesystems, *fs)
	}
	if err := attachSpares(filesystems); err != nil {
		slog.Warn("failed to read btrfs hot spares", "path", SparePath, "error", err)
	}
	attachRebuilds(filesystems)
	return filesystems, nil
}

func fillFilesystem(fs *BtrfsFilesystem) {
	mnt := fs.Mountpoints[0].Mountpoint
	warn := func(what string, err error) {
		slog.Warn("btrfs inventory", "uuid", fs.UUID, "ioctl", what, "error", err)
	}
	fs.Scrub.Status = "none"

	var info fsInfoArgs
	if err := fsIoctl(mnt, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		warn("fs info", err)
		return
	}
	var label [labelSize]byte
	if err := fsIoctl(mnt, iocGetFSLabel, unsafe.Pointer(&label)); err != nil {
		warn("label", err)
	} else {
		fs.Label = cstring(label[:])
	}

	fs.Devices, fs.HealthKnown = listDevices(mnt, info.MaxID, warn)
	markUnopened(fs.UUID, fs.Devices)
	fs.HealthKnown = markDroppedOut(fs.UUID, fs.Devices, warn) && fs.HealthKnown
	readSizes(fs.Devices, warn)
	var allocated uint64
	for _, d := range fs.Devices {
		fs.Degraded = fs.Degraded || d.Missing
		fs.TotalBytes += d.SizeBytes
		allocated += d.UsedBytes
	}
	if fs.TotalBytes > allocated {
		fs.UnallocatedBytes = fs.TotalBytes - allocated
	}

	spaces, err := spaceInfos(mnt)
	if err != nil {
		warn("space info", err)
	} else {
		present := 0
		for _, d := range fs.Devices {
			if !d.Missing {
				present++
			}
		}
		for _, s := range spaces {
			if s.Flags&bgGlobalRsv != 0 {
				continue
			}
			profile := profileName(s.Flags)
			fs.Allocation = append(fs.Allocation, BtrfsAllocation{Kind: kindName(s.Flags), Profile: profile, TotalBytes: s.TotalBytes, UsedBytes: s.UsedBytes})
			// Used is raw bytes on disk, so every copy counts.
			fs.UsedBytes += uint64(float64(s.UsedBytes) * profileRatio(profile, present))
		}
		fs.StoredBytes = storedBytes(fs.Allocation)
		fs.FreeBytes = freeOrZero(mnt, warn)
		fs.LowMetadataSpace = lowMetadataSpace(fs.Allocation, fs.UnallocatedBytes)
	}

	fs.Scrub = scrubStatus(mnt, fs.UUID, fs.Devices, fs.UsedBytes, warn)
	fs.Balance = balanceStatus(mnt, warn)
}

// balanceStatus reads the kernel's balance on mnt, nil for none. A bridge
// that is not root may not read it (EPERM) and shows none.
func balanceStatus(mnt string, warn func(string, error)) *BtrfsBalanceStatus {
	state, pct, err := balanceProgressOf(mnt)
	switch {
	case errors.Is(err, unix.ENOTCONN):
		return nil
	case errors.Is(err, unix.EPERM):
		slog.Debug("btrfs balance progress not readable", "mountpoint", mnt, "error", err)
		return nil
	case err != nil:
		warn("balance progress", err)
		return nil
	case state&balanceStateRunning == 0:
		return &BtrfsBalanceStatus{State: "paused", Percent: pct}
	}
	return &BtrfsBalanceStatus{State: "running", Percent: pct}
}

// storedBytes is what the pool holds, each byte counted once: the used
// bytes of every data, metadata and system row, which SPACE_INFO gives as
// logical sizes.
func storedBytes(alloc []BtrfsAllocation) uint64 {
	var stored uint64
	for _, a := range alloc {
		stored += a.UsedBytes
	}
	return stored
}

// freeOrZero is poolFreeBytes, or 0 after warning when statfs fails.
func freeOrZero(mnt string, warn func(string, error)) uint64 {
	free, err := poolFreeBytes(mnt)
	if err != nil {
		warn("statfs", err)
		return 0
	}
	return free
}

// poolFreeBytes is the kernel's room for more data in the filesystem at mnt,
// as df shows it: statfs's f_bavail, which btrfs counts disk by disk for the
// profile new data gets (btrfs_calc_avail_data_space), so a mirror of uneven
// disks, a degraded pool and a convert are counted as btrfs will allocate.
// A variable so tests can stand in for the kernel.
var poolFreeBytes = func(mnt string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(mnt, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", mnt, err)
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// profileRatio is how many raw bytes one logical byte of a profile takes.
func profileRatio(profile string, devices int) float64 {
	switch {
	case profile == "raid5" && devices > 1:
		return float64(devices) / float64(devices-1)
	case profile == "raid6" && devices > 2:
		return float64(devices) / float64(devices-2)
	}
	return float64(max(profiles[profile].copies, 1))
}

// missingDiskName is the path DEV_INFO gives a device the kernel marked
// missing (volumes.h btrfs_dev_name).
const missingDiskName = "<missing disk>"

// listDevices reads every device slot of the filesystem with its error
// counters; a device the kernel knows but cannot find is Missing, with no
// path. The bool is false when any read failed, so health stays unknown
// rather than clean.
func listDevices(mnt string, maxID uint64, warn func(string, error)) ([]BtrfsDevice, bool) {
	var devices []BtrfsDevice
	known := true
	for id := uint64(1); id <= maxID; id++ {
		info := devInfoArgs{DevID: id}
		if err := fsIoctl(mnt, iocDevInfo, unsafe.Pointer(&info)); err != nil {
			if errors.Is(err, unix.ENODEV) {
				continue
			}
			warn("device info", err)
			known = false
			continue
		}
		d := BtrfsDevice{ID: int(id), Path: cstring(info.Path[:]), SizeBytes: info.TotalBytes, UsedBytes: info.BytesUsed, UUID: formatUUID(info.UUID[:])}
		if d.Path == missingDiskName {
			d.Path = ""
		}
		d.Missing = d.Path == ""
		stats := devStatsArgs{DevID: id, NrItems: 5}
		if err := fsIoctl(mnt, iocGetDevStats, unsafe.Pointer(&stats)); err != nil {
			warn("device stats", err)
			known = false
		} else {
			d.Errors = BtrfsDeviceErrors{Write: stats.Values[0], Read: stats.Values[1], Flush: stats.Values[2], Corruption: stats.Values[3], Generation: stats.Values[4]}
		}
		devices = append(devices, d)
	}
	return devices, known
}

// readSizes reads the block device size of each present device.
func readSizes(devices []BtrfsDevice, warn func(string, error)) {
	for i, d := range devices {
		if d.Missing {
			continue
		}
		if size, err := blockDeviceSize(d.Path); err != nil {
			warn("device size", err)
		} else {
			devices[i].PhysicalBytes = size
			devices[i].Growable = size > d.SizeBytes && size-d.SizeBytes >= growSlack
		}
	}
}

// spaceInfos returns the kernel's space accounting per block-group type.
func spaceInfos(mnt string) ([]spaceInfo, error) {
	args := spaceArgs{SpaceSlots: uint64(len(spaceArgs{}.Spaces))}
	if err := fsIoctl(mnt, iocSpaceInfo, unsafe.Pointer(&args)); err != nil {
		return nil, err
	}
	n := min(args.TotalSpaces, args.SpaceSlots)
	return args.Spaces[:n], nil
}

// lowMetadataSpace is the ENOSPC precursor: metadata over 90% full with
// less than 1 GiB unallocated, so no new metadata chunk can be made.
func lowMetadataSpace(allocation []BtrfsAllocation, unallocated uint64) bool {
	if unallocated >= 1<<30 {
		return false
	}
	for _, a := range allocation {
		if a.TotalBytes > 0 && a.UsedBytes*10 >= a.TotalBytes*9 {
			switch a.Kind {
			case "metadata", "data+metadata":
				return true
			}
		}
	}
	return false
}

// scrubStatus reports a running scrub from the kernel's per-device
// progress, else the last scrub on record. A scrub on record still running
// whose process lives is running too, between kernel calls or waiting for a
// thaw, though the kernel reports nothing.
func scrubStatus(mnt, uuid string, devices []BtrfsDevice, usedBytes uint64, warn func(string, error)) BtrfsScrubStatus {
	progress := scrubProgressOf(mnt, devices, warn)
	rec, have := loadScrubRecord(uuid)
	ours := have && rec.live()
	if len(progress) > 0 || ours {
		return liveScrub(rec, ours, progress, usedBytes)
	}
	if last, ok := lastScrub(uuid, rec, have); ok {
		// No device is scrubbing, so a record still saying "running" was cut
		// short by a crash or power loss.
		if last.Status == "running" {
			last.Status = "aborted"
		}
		return last
	}
	return BtrfsScrubStatus{Status: "none"}
}

// scrubProgressOf reads the kernel's progress of each present device being
// scrubbed.
func scrubProgressOf(mnt string, devices []BtrfsDevice, warn func(string, error)) map[int]scrubProgress {
	progress := map[int]scrubProgress{}
	for _, d := range devices {
		if d.Missing {
			continue
		}
		p, running, err := deviceScrubProgress(mnt, d.ID)
		switch {
		case err != nil:
			warn("scrub progress", err)
		case running:
			progress[d.ID] = p
		}
	}
	return progress
}

// liveScrub is a running scrub from the kernel's progress. When it is ours,
// the running scrub on record of a live process, it adds what that counted
// before: earlier kernel calls, the part before a resume, and the saved
// counters of a device whose call the kernel does not report. A record
// still running without a live process, one a crash left or one whose
// writer could not be identified, gives only the start, which the banner
// keys the scrub on.
func liveScrub(rec scrubRecord, ours bool, progress map[int]scrubProgress, usedBytes uint64) BtrfsScrubStatus {
	live := BtrfsScrubStatus{Status: "running"}
	if rec.Status == "running" {
		live.StartedAt = rec.StartedAt
	}
	if ours {
		live = rec.BtrfsScrubStatus
		for id, d := range rec.Devices {
			if _, reported := progress[id]; !reported {
				live.addCounters(d.scrubCounters)
			}
		}
	}
	for _, p := range progress {
		live.add(p)
	}
	if usedBytes > 0 {
		live.Percent = min(100, float64(live.BytesScrubbed)*100/float64(usedBytes))
	}
	return live
}
