package btrfs

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ScrubRecordPath stores the last scrub LinuxIO ran on each filesystem,
// keyed by UUID; the kernel keeps no history of its own.
var ScrubRecordPath = version.DataDir + "/btrfs-scrub.json"

// progsScrubDir is where btrfs-progs records the scrubs it ran, such as the
// btrfsmaintenance timer's, as scrub.status.<uuid> files.
var progsScrubDir = "/var/lib/btrfs"

// Scrub scrubs every present device of the filesystem to completion, reporting
// progress on the way, and records the outcome. Canceling ctx cancels the
// scrub; the kernel then returns each device's partial counters.
func Scrub(ctx context.Context, uuid string, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return BtrfsScrubStatus{}, err
	}
	mnt := fs.Mountpoints[0].Mountpoint
	start := time.Now()
	saveScrubRecord(uuid, BtrfsScrubStatus{Status: "running", StartedAt: &start})

	type result struct {
		progress scrubProgress
		err      error
	}
	results := make(chan result)
	pending := 0
	for _, d := range fs.Devices {
		if d.Missing {
			continue
		}
		pending++
		go func(id int) {
			// The kernel scrubs only [Start, End]; the whole device, as btrfs-progs asks.
			args := scrubArgs{DevID: uint64(id), End: ^uint64(0)}
			err := fsIoctl(mnt, iocScrub, unsafe.Pointer(&args))
			results <- result{args.Progress, err}
		}(d.ID)
	}

	final := BtrfsScrubStatus{Status: "finished", StartedAt: &start}
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	ctxDone := ctx.Done()
	var firstErr error
	for pending > 0 {
		select {
		case r := <-results:
			pending--
			final.add(r.progress)
			firstErr = final.absorb(r.err, firstErr)
		case <-ctxDone:
			ctxDone = nil
			_ = fsIoctl(mnt, iocScrubCancel, nil)
		case <-ticker.C:
			report(scrubStatus(mnt, uuid, fs.Devices, fs.UsedBytes, func(string, error) {}))
		}
	}
	end := time.Now()
	final.FinishedAt = &end
	if final.Status == "finished" {
		final.Percent = 100
	} else if fs.UsedBytes > 0 {
		final.Percent = min(100, float64(final.BytesScrubbed)*100/float64(fs.UsedBytes))
	}
	saveScrubRecord(uuid, final)
	if ctx.Err() != nil {
		return final, ctx.Err()
	}
	if firstErr != nil {
		return final, fmt.Errorf("scrub: %w", firstErr)
	}
	return final, nil
}

// absorb folds one device's outcome into the status: a cancel wins over an
// abort, and the first real error is kept.
func (s *BtrfsScrubStatus) absorb(err, firstErr error) error {
	switch {
	case err == nil:
	case errors.Is(err, unix.ECANCELED):
		s.Status = "canceled"
	default:
		if s.Status == "finished" {
			s.Status = "aborted"
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ScrubCancel cancels the running scrub.
func ScrubCancel(ctx context.Context, uuid string) error {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	if err := fsIoctl(mnt, iocScrubCancel, nil); err != nil {
		if errors.Is(err, unix.ENOTCONN) {
			return errors.New("no scrub is running")
		}
		return fmt.Errorf("cancel scrub: %w", err)
	}
	return nil
}

func loadScrubRecords() map[string]BtrfsScrubStatus {
	records := map[string]BtrfsScrubStatus{}
	data, err := os.ReadFile(ScrubRecordPath)
	if err != nil {
		return records
	}
	if err := json.Unmarshal(data, &records); err != nil {
		slog.Warn("btrfs scrub records", "path", ScrubRecordPath, "error", err)
	}
	return records
}

func loadScrubRecord(uuid string) (BtrfsScrubStatus, bool) {
	rec, ok := loadScrubRecords()[uuid]
	return rec, ok
}

// saveScrubRecord stores one filesystem's scrub; a failure is logged, since
// the scrub itself already happened.
func saveScrubRecord(uuid string, s BtrfsScrubStatus) {
	records := loadScrubRecords()
	records[uuid] = s
	data, err := json.Marshal(records, jsontext.WithIndent("  "), json.Deterministic(true))
	if err == nil {
		err = os.MkdirAll(filepath.Dir(ScrubRecordPath), 0o755)
	}
	if err == nil {
		err = atomicfile.Write(ScrubRecordPath, data, 0o644)
	}
	if err != nil {
		slog.Warn("btrfs scrub records", "path", ScrubRecordPath, "error", err)
	}
}

// lastScrub returns the newer of LinuxIO's own record and btrfs-progs'
// status file for the filesystem.
func lastScrub(uuid string) (BtrfsScrubStatus, bool) {
	ours, haveOurs := loadScrubRecord(uuid)
	theirs, haveTheirs := progsScrubStatus(uuid)
	switch {
	case haveOurs && haveTheirs:
		if theirs.StartedAt != nil && (ours.StartedAt == nil || theirs.StartedAt.After(*ours.StartedAt)) {
			return theirs, true
		}
		return ours, true
	case haveOurs:
		return ours, true
	case haveTheirs:
		return theirs, true
	}
	return BtrfsScrubStatus{}, false
}

// progsScrubStatus reads btrfs-progs' scrub.status.<uuid> file: a version
// line, then one "<uuid>:<devid>|key:value|..." line per device.
func progsScrubStatus(uuid string) (BtrfsScrubStatus, bool) {
	data, err := os.ReadFile(filepath.Join(progsScrubDir, "scrub.status."+uuid))
	if err != nil {
		return BtrfsScrubStatus{}, false
	}
	return parseProgsScrubStatus(string(data))
}

func parseProgsScrubStatus(text string) (BtrfsScrubStatus, bool) {
	s := BtrfsScrubStatus{Status: "finished"}
	var start, duration uint64
	devices := 0
	for line := range strings.Lines(text) {
		fields := strings.Split(strings.TrimSpace(line), "|")
		if len(fields) < 2 || !strings.Contains(fields[0], ":") || strings.HasPrefix(fields[0], "scrub status") {
			continue
		}
		devices++
		d := parseProgsScrubDevice(fields[1:])
		s.add(d.progress)
		if start == 0 || (d.start > 0 && d.start < start) {
			start = d.start
		}
		duration = max(duration, d.duration)
		switch {
		case d.canceled:
			s.Status = "canceled"
		case !d.finished && s.Status == "finished":
			s.Status = "aborted"
		}
	}
	if devices == 0 {
		return BtrfsScrubStatus{}, false
	}
	if start > 0 {
		t := time.Unix(int64(start), 0)
		s.StartedAt = &t
		end := t.Add(time.Duration(duration) * time.Second)
		s.FinishedAt = &end
	}
	if s.Status == "finished" {
		s.Percent = 100
	}
	return s, true
}

// progsScrubDevice is one device line of btrfs-progs' scrub status file.
type progsScrubDevice struct {
	progress           scrubProgress
	start, duration    uint64
	canceled, finished bool
}

func parseProgsScrubDevice(fields []string) progsScrubDevice {
	var d progsScrubDevice
	counters := map[string]*uint64{
		"data_bytes_scrubbed": &d.progress.DataBytesScrubbed, "tree_bytes_scrubbed": &d.progress.TreeBytesScrubbed,
		"read_errors": &d.progress.ReadErrors, "csum_errors": &d.progress.CsumErrors, "verify_errors": &d.progress.VerifyErrors,
		"super_errors": &d.progress.SuperErrors, "uncorrectable_errors": &d.progress.UncorrectableErrors,
		"corrected_errors": &d.progress.CorrectedErrors, "t_start": &d.start, "duration": &d.duration,
	}
	for _, f := range fields {
		key, val, _ := strings.Cut(f, ":")
		n, _ := strconv.ParseUint(val, 10, 64)
		switch key {
		case "canceled":
			d.canceled = n != 0
		case "finished":
			d.finished = n != 0
		default:
			if p := counters[key]; p != nil {
				*p = n
			}
		}
	}
	return d
}
