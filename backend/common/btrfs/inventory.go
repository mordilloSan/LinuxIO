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
	ID        int               `json:"id"`
	Path      string            `json:"path"`
	SizeBytes uint64            `json:"sizeBytes"`
	UsedBytes uint64            `json:"usedBytes"`
	Missing   bool              `json:"missing"`
	Errors    BtrfsDeviceErrors `json:"errors"`
	// PhysicalBytes is the block device's size; Growable is set when the
	// filesystem has not grown into it, as after replacing with a bigger disk.
	PhysicalBytes uint64 `json:"physicalBytes"`
	Growable      bool   `json:"growable"`
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
	s.BytesScrubbed += p.DataBytesScrubbed + p.TreeBytesScrubbed
	s.ErrorCount += p.ReadErrors + p.CsumErrors + p.VerifyErrors + p.SuperErrors
	s.UncorrectableErrors += p.UncorrectableErrors
	s.CorrectedErrors += p.CorrectedErrors
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
		fs.LowMetadataSpace = lowMetadataSpace(fs.Allocation, fs.UnallocatedBytes)
	}

	fs.Scrub = scrubStatus(mnt, fs.UUID, fs.Devices, fs.UsedBytes, warn)
}

// profileRatio is how many raw bytes one logical byte of a profile takes.
func profileRatio(profile string, devices int) float64 {
	switch profile {
	case "dup", "raid1", "raid10":
		return 2
	case "raid1c3":
		return 3
	case "raid1c4":
		return 4
	case "raid5":
		if devices > 1 {
			return float64(devices) / float64(devices-1)
		}
	case "raid6":
		if devices > 2 {
			return float64(devices) / float64(devices-2)
		}
	}
	return 1
}

// listDevices reads every device slot of the filesystem with its error
// counters; a device the kernel knows but cannot find is Missing. The bool
// is false when any read failed, so health stays unknown rather than clean.
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
		d := BtrfsDevice{ID: int(id), Path: cstring(info.Path[:]), SizeBytes: info.TotalBytes, UsedBytes: info.BytesUsed}
		d.Missing = d.Path == ""
		if !d.Missing {
			if size, err := blockDeviceSize(d.Path); err != nil {
				warn("device size", err)
			} else {
				d.PhysicalBytes = size
				d.Growable = size > d.SizeBytes && size-d.SizeBytes >= growSlack
			}
		}
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
// progress, else the last scrub on record.
func scrubStatus(mnt, uuid string, devices []BtrfsDevice, usedBytes uint64, warn func(string, error)) BtrfsScrubStatus {
	live := BtrfsScrubStatus{Status: "running"}
	running := false
	for _, d := range devices {
		if d.Missing {
			continue
		}
		args := scrubArgs{DevID: uint64(d.ID)}
		err := fsIoctl(mnt, iocScrubProgress, unsafe.Pointer(&args))
		switch {
		case err == nil:
			running = true
			live.add(args.Progress)
		case errors.Is(err, unix.ENOTCONN): // no scrub on this device
		default:
			warn("scrub progress", err)
		}
	}
	if running {
		if usedBytes > 0 {
			live.Percent = min(100, float64(live.BytesScrubbed)*100/float64(usedBytes))
		}
		if rec, ok := loadScrubRecord(uuid); ok && rec.Status == "running" {
			live.StartedAt = rec.StartedAt
		}
		return live
	}
	if rec, ok := lastScrub(uuid); ok {
		return rec
	}
	return BtrfsScrubStatus{Status: "none"}
}
