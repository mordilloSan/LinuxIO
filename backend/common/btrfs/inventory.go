package btrfs

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BtrfsFilesystem is one btrfs filesystem with its allocation, devices,
// error counters and scrub state. Generation is not reported: no cheap
// btrfs-progs command exposes it.
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
	// HealthKnown is true only when both the device list and the error
	// counters were read, so callers can tell "healthy" from "unknown".
	HealthKnown bool `json:"-"`
}

// BtrfsAllocation is one block-group type (data, metadata, system).
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
	Status        string     `json:"status"`
	StartedAt     *time.Time `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	ErrorCount    uint64     `json:"errorCount"`
	BytesScrubbed uint64     `json:"bytesScrubbed"`
	Percent       float64    `json:"percent"`
}

// runBtrfs is a variable so tests can stub the btrfs command.
var runBtrfs = run

// ListFilesystems returns every mounted btrfs filesystem. A command that
// fails for one filesystem is logged and leaves that part empty.
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
		fillFilesystem(ctx, fs)
		filesystems = append(filesystems, *fs)
	}
	return filesystems, nil
}

func fillFilesystem(ctx context.Context, fs *BtrfsFilesystem) {
	mnt := fs.Mountpoints[0].Mountpoint
	warn := func(what string, err error) {
		slog.Warn("btrfs inventory", "uuid", fs.UUID, "command", what, "error", err)
	}
	fs.Scrub.Status = "none"

	if out, err := runBtrfs(ctx, "filesystem", "usage", "-b", mnt); err != nil {
		warn("filesystem usage", err)
	} else if fs.TotalBytes, fs.UnallocatedBytes, fs.UsedBytes, fs.Allocation, err = parseFilesystemUsage(out); err != nil {
		warn("filesystem usage", err)
	}

	showOK, statsOK := false, false
	if out, err := runBtrfs(ctx, "filesystem", "show", "--raw", mnt); err != nil {
		warn("filesystem show", err)
	} else if label, devices, missing, err := parseFilesystemShow(out); err != nil {
		warn("filesystem show", err)
	} else {
		fs.Label, fs.Devices, fs.Degraded, showOK = label, devices, missing, true
	}

	if out, err := runBtrfs(ctx, "device", "stats", mnt); err != nil {
		warn("device stats", err)
	} else if stats, err := parseDeviceStats(out); err != nil {
		warn("device stats", err)
	} else {
		fs.Devices = attachDeviceStats(fs.Devices, stats)
		statsOK = true
	}
	fs.HealthKnown = showOK && statsOK

	if out, err := runBtrfs(ctx, "scrub", "status", "--raw", mnt); err != nil {
		warn("scrub status", err)
	} else if scrub, err := parseScrubStatus(out); err != nil {
		warn("scrub status", err)
	} else {
		fs.Scrub = scrub
	}
}

// evalSymlinks is a variable so tests can stub path canonicalisation.
var evalSymlinks = filepath.EvalSymlinks

func canonicalPath(p string) string {
	if resolved, err := evalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// attachDeviceStats copies error counters onto devices. `device stats` may
// name a device by its kernel path (/dev/dm-0) while `filesystem show` uses
// the mapper path, so keys match by devid or by canonical path. A devid:N key
// with no device row is a missing device the mounted `filesystem show` left
// out; it is appended as Missing so it can be removed or replaced.
func attachDeviceStats(devices []BtrfsDevice, stats map[string]BtrfsDeviceErrors) []BtrfsDevice {
	byPath := make(map[string]BtrfsDeviceErrors, len(stats))
	for key, e := range stats {
		if !strings.HasPrefix(key, "devid:") {
			byPath[canonicalPath(key)] = e
		}
	}
	known := make(map[int]bool, len(devices))
	for i, d := range devices {
		known[d.ID] = true
		if e, ok := stats["devid:"+strconv.Itoa(d.ID)]; ok {
			devices[i].Errors = e
		} else if e, ok := byPath[canonicalPath(d.Path)]; ok && d.Path != "" {
			devices[i].Errors = e
		}
	}
	var missing []BtrfsDevice
	for key, e := range stats {
		idText, ok := strings.CutPrefix(key, "devid:")
		if !ok {
			continue
		}
		if id, err := strconv.Atoi(idText); err == nil && !known[id] {
			missing = append(missing, BtrfsDevice{ID: id, Missing: true, Errors: e})
		}
	}
	slices.SortFunc(missing, func(a, b BtrfsDevice) int { return a.ID - b.ID })
	return append(devices, missing...)
}

var usageSection = regexp.MustCompile(`^([A-Za-z+]+)(?:,\s*(\w+))?: Size:(\d+), Used:(\d+)`)

// parseFilesystemUsage parses `btrfs filesystem usage -b`. Per-device lines
// are ignored.
func parseFilesystemUsage(out string) (total, unallocated, used uint64, alloc []BtrfsAllocation, err error) {
	foundTotal := false
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if m := usageSection.FindStringSubmatch(line); m != nil {
			size, _ := strconv.ParseUint(m[3], 10, 64)
			usedBytes, _ := strconv.ParseUint(m[4], 10, 64)
			alloc = append(alloc, BtrfsAllocation{
				Kind: strings.ToLower(m[1]), Profile: strings.ToLower(m[2]),
				TotalBytes: size, UsedBytes: usedBytes,
			})
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		field, _, _ := strings.Cut(strings.TrimSpace(val), " ")
		switch key {
		case "Device size", "Device unallocated", "Used":
			n, perr := strconv.ParseUint(field, 10, 64)
			if perr != nil {
				return 0, 0, 0, nil, fmt.Errorf("parse %q: %w", line, perr)
			}
			switch key {
			case "Device size":
				total, foundTotal = n, true
			case "Device unallocated":
				unallocated = n
			default:
				used = n
			}
		}
	}
	if !foundTotal {
		return 0, 0, 0, nil, fmt.Errorf("no device size in filesystem usage output")
	}
	return total, unallocated, used, alloc, nil
}

var (
	showLabel  = regexp.MustCompile(`^Label: (?:'(.*)'|(\S+))\s+uuid: \S+`)
	showDevice = regexp.MustCompile(`^devid\s+(\d+) size (\d+) used (\d+) path\s*(.*)$`)
)

// parseFilesystemShow parses `btrfs filesystem show --raw`.
func parseFilesystemShow(out string) (label string, devices []BtrfsDevice, missing bool, err error) {
	found := false
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if m := showLabel.FindStringSubmatch(line); m != nil {
			found = true
			if label = m[1]; m[2] != "" && m[2] != "none" {
				label = m[2]
			}
		} else if m := showDevice.FindStringSubmatch(line); m != nil {
			id, _ := strconv.Atoi(m[1])
			size, _ := strconv.ParseUint(m[2], 10, 64)
			usedBytes, _ := strconv.ParseUint(m[3], 10, 64)
			// btrfs-progs prints "path <path> MISSING"; the path may be empty.
			path := m[4]
			isMissing := path == "MISSING" || strings.HasSuffix(path, " MISSING")
			if isMissing {
				path = strings.TrimSuffix(path, "MISSING")
			}
			d := BtrfsDevice{ID: id, Path: strings.TrimSpace(path), SizeBytes: size, UsedBytes: usedBytes, Missing: isMissing}
			missing = missing || d.Missing
			devices = append(devices, d)
		} else if strings.Contains(line, "Some devices missing") {
			missing = true
		}
	}
	if !found {
		return "", nil, false, fmt.Errorf("no filesystem header in filesystem show output")
	}
	return label, devices, missing, nil
}

var statsLine = regexp.MustCompile(`^\[(.+)\]\.(\w+)\s+(\d+)$`)

// parseDeviceStats parses `btrfs device stats`, keyed by device path (or
// devid:N for a missing device).
func parseDeviceStats(out string) (map[string]BtrfsDeviceErrors, error) {
	stats := map[string]BtrfsDeviceErrors{}
	for line := range strings.Lines(out) {
		m := statsLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		n, err := strconv.ParseUint(m[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", strings.TrimSpace(line), err)
		}
		e := stats[m[1]]
		switch m[2] {
		case "write_io_errs":
			e.Write = n
		case "read_io_errs":
			e.Read = n
		case "flush_io_errs":
			e.Flush = n
		case "corruption_errs":
			e.Corruption = n
		case "generation_errs":
			e.Generation = n
		}
		stats[m[1]] = e
	}
	return stats, nil
}

var (
	scrubPercent = regexp.MustCompile(`\(([\d.]+)%\)`)
	scrubErrors  = regexp.MustCompile(`\w+=(\d+)`)
)

// parseScrubStatus parses `btrfs scrub status --raw`.
func parseScrubStatus(out string) (BtrfsScrubStatus, error) {
	s := BtrfsScrubStatus{Status: "none"}
	if strings.Contains(strings.ToLower(out), "no stats available") {
		return s, nil
	}
	var duration time.Duration
	for line := range strings.Lines(out) {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		if err := s.setField(key, strings.TrimSpace(val), &duration); err != nil {
			return BtrfsScrubStatus{}, err
		}
	}
	if s.Status == "none" {
		return BtrfsScrubStatus{}, fmt.Errorf("no status in scrub status output")
	}
	if s.Status == "finished" {
		s.Percent = 100
		if s.StartedAt != nil {
			t := s.StartedAt.Add(duration)
			s.FinishedAt = &t
		}
	}
	return s, nil
}

func (s *BtrfsScrubStatus) setField(key, val string, duration *time.Duration) error {
	var err error
	switch key {
	case "Scrub started":
		var t time.Time
		if t, err = time.ParseInLocation(time.ANSIC, val, time.Local); err != nil {
			return fmt.Errorf("parse scrub start time: %w", err)
		}
		s.StartedAt = &t
	case "Status":
		s.Status = strings.ToLower(val)
	case "Duration":
		*duration, err = parseClock(val)
	case "Bytes scrubbed":
		err = s.setScrubbed(val)
	case "Error summary":
		for _, m := range scrubErrors.FindAllStringSubmatch(val, -1) {
			n, _ := strconv.ParseUint(m[1], 10, 64)
			s.ErrorCount += n
		}
	}
	return err
}

// setScrubbed reads a "Bytes scrubbed" value such as "7503290368  (13.97%)".
func (s *BtrfsScrubStatus) setScrubbed(val string) error {
	n, _, _ := strings.Cut(val, " ")
	b, err := strconv.ParseUint(n, 10, 64)
	if err != nil {
		return fmt.Errorf("parse bytes scrubbed %q: %w", val, err)
	}
	s.BytesScrubbed = b
	if m := scrubPercent.FindStringSubmatch(val); m != nil {
		s.Percent, _ = strconv.ParseFloat(m[1], 64)
	}
	return nil
}

// parseClock parses btrfs durations such as 0:42:13.
func parseClock(v string) (time.Duration, error) {
	parts := strings.Split(v, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("parse scrub duration %q", v)
	}
	var total time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, fmt.Errorf("parse scrub duration %q: %w", v, err)
		}
		total += time.Duration(n) * unit
	}
	return total, nil
}
