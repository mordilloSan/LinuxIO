package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

// The resync's scrub counts the stale copies it finds on the returned disk
// as errors of that disk, repaired or not: a data copy whose checksum fails
// as a corruption error, a tree block of an older generation as a
// generation error (7.0 scrub.c scrub_stripe_report_errors; 6.8's scrub
// counts none), and so does a read that hit a stale data copy before
// (inode.c btrfs_data_csum_ok). Nothing lowers a counter but a reset, so
// they would keep the pool's health alert up beside its whole again notice.
// Once a resync has found the pool whole it resets the counters of a disk
// the episode recorded missing only when they hold nothing but corruption
// and generation errors: no read, write or flush error, and none at all
// when LinuxIO last saw the pool whole, or, with no such record, when the
// disk went missing. Anything else is the disk's own to look into, and is
// kept. The reset zeroes all five counters of a device at once (volumes.c
// btrfs_get_dev_stats).
//
// The kernel writes the reset to the device tree at its next commit
// (btrfs_run_dev_stats in transaction.c commit_cowonly_roots), which the
// reset does not start, and 7.0 does not commit at unmount without a
// running transaction (disk-io.c btrfs_commit_super), so a pool left idle
// would read the counters back at its next mount. The resync starts that
// commit by setting the label to itself: BTRFS_IOC_SET_FSLABEL starts a
// transaction and commits it whatever the label (ioctl.c
// btrfs_ioctl_set_fslabel, 6.8 and 7.0).

// deviceCounters is one device's error counters, by devid and device UUID.
type deviceCounters struct {
	ID     uint64            `json:"id"`
	UUID   string            `json:"uuid"`
	Errors BtrfsDeviceErrors `json:"errors,omitzero"`
}

// deviceCountersOf returns the error counters of devices.
func deviceCountersOf(devices []BtrfsDevice) []deviceCounters {
	counters := make([]deviceCounters, 0, len(devices))
	for _, d := range devices {
		counters = append(counters, deviceCounters{ID: uint64(d.ID), UUID: d.UUID, Errors: d.Errors})
	}
	return counters
}

// missingDisk is a disk a degraded pool ran without. Errors are its
// counters as the health run last saw the pool whole (Whole), or else as
// they read once it was missing.
type missingDisk struct {
	ID     uint64            `json:"id"`
	UUID   string            `json:"uuid"`
	Errors BtrfsDeviceErrors `json:"errors,omitzero"`
	Whole  bool              `json:"whole,omitzero"`
	// At is when the episode recorded the disk missing, in seconds since the
	// epoch: the hot spare's delay counts from it.
	At int64 `json:"at,omitzero"`
}

// newMissingDisk records d, which just went missing. Only a disk missing as
// the episode opens takes the counters saved when the pool was last seen
// whole: one that goes missing later may have counted errors of its own in
// between, so it keeps what it reads now and takes the stricter rule.
func (s *poolState) newMissingDisk(d BtrfsDevice, opening bool) missingDisk {
	now := time.Now().Unix()
	if opening {
		for _, c := range s.Counters {
			if c.ID == uint64(d.ID) && c.UUID == d.UUID {
				return missingDisk{ID: c.ID, UUID: c.UUID, Errors: c.Errors, Whole: true, At: now}
			}
		}
	}
	return missingDisk{ID: uint64(d.ID), UUID: d.UUID, Errors: d.Errors, At: now}
}

// forgetReplacedDisk drops device id from the open episode of pool uuid
// once a replace put a new disk under its devid and UUID, which holds no
// stale copy: what that disk counts is its own. A failure is logged, and
// the resync's own check of the last replace keeps the counters then.
func forgetReplacedDisk(ctx context.Context, uuid string, id int) {
	states, err := loadPoolStates()
	if err == nil && !slices.ContainsFunc(states[uuid].Missing, func(m missingDisk) bool { return m.ID == uint64(id) }) {
		return
	}
	if err == nil {
		err = updatePoolStates(ctx, func(s map[string]poolState) {
			state := s[uuid]
			state.Missing = slices.DeleteFunc(state.Missing, func(m missingDisk) bool { return m.ID == uint64(id) })
			s[uuid] = state
		})
	}
	if err != nil {
		slog.Warn("failed to record a btrfs replace in the pool state", "uuid", uuid, "devid", id, "error", err)
	}
}

// Why a returned disk kept its error counters.
const (
	keptIOErrors       = "it counts a failed read, write or flush"
	keptWholeErrors    = "it counted errors when LinuxIO last saw the pool whole"
	keptMissingErrors  = "it counted errors when it went missing"
	keptUnread         = "they could not be read"
	keptResetFailed    = "they could not be reset"
	keptReasonOfOthers = "LinuxIO resets only the counters of a disk recorded missing in the episode, and only corruption and generation errors"
)

// returnedDisk is what a resync that ended whole did with the error
// counters of one disk that returned: Errors are what the kernel returned
// as it reset them, or what it read when they were kept (Counted), and
// Kept says why they were ("" when they were reset). Late is set when a
// read, write or flush failed between the check and the reset, which zeroed
// it too.
type returnedDisk struct {
	ID      uint64
	Path    string
	Errors  BtrfsDeviceErrors
	Counted bool
	Kept    string
	Late    bool
}

// resyncOutcome is what a resync that found the pool whole did with the
// error counters of the disks that returned: its scrub run, each disk, and
// why none was looked at, when that is so.
type resyncOutcome struct {
	run   string
	kept  string
	disks []returnedDisk
}

// errorsOf reads GET_DEV_STATS values, in enum btrfs_dev_stat_values order.
func errorsOf(v [5]uint64) BtrfsDeviceErrors {
	return BtrfsDeviceErrors{Write: v[0], Read: v[1], Flush: v[2], Corruption: v[3], Generation: v[4]}
}

// ioErrors reports whether e counts a failed read, write or flush.
func (e BtrfsDeviceErrors) ioErrors() bool {
	return e.Read != 0 || e.Write != 0 || e.Flush != 0
}

// resetBound bounds the mount lookup and the wait for the label lock of a
// reset, which runs on past a stop of the resync once begun. The ioctls
// themselves block as long as the kernel does: setting the label of a
// frozen filesystem waits for it to thaw.
const resetBound = 2 * time.Minute

// resetReturnedCounters resets the error counters of the disks of pool uuid
// that its episode recorded missing and commits the reset, once its resync
// of run found the pool whole. It runs outside the pool state's lock,
// before the state ends the episode, so a crash at any point leaves an
// episode the next resync ends the same way: counters already zero are
// left as they are, and the commit is made again. It resets nothing unless
// the record of run is a finished scrub with no uncorrectable error and no
// replace finished during the episode. What it cannot do it logs, and
// leaves the counters and their health alert to a person: the pool is
// whole all the same.
func resetReturnedCounters(ctx context.Context, uuid, run string) resyncOutcome {
	out := resyncOutcome{run: run}
	states, err := loadPoolStates()
	if err != nil {
		slog.Warn("failed to reset btrfs device error counters", "uuid", uuid, "error", err)
		out.kept = "the pool record could not be read"
		return out
	}
	state := states[uuid]
	if !state.Scrubbed || state.ResyncRun != run || len(state.Missing) == 0 {
		return out // a new episode began, which needs its own resync, or no disk was recorded
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resetBound)
	defer cancel()
	fd, err := openPool(ctx, uuid)
	if err != nil {
		slog.Warn("failed to reset btrfs device error counters", "uuid", uuid, "error", err)
		out.kept = "the pool could not be opened"
		return out
	}
	defer unix.Close(fd)
	if out.kept = resetRefusal(fd, uuid, run, state.Since); out.kept != "" {
		return out
	}
	present := false
	for _, m := range state.Missing {
		disk, found, err := resetDeviceCounters(fd, uuid, m)
		present = present || found
		attrs := []any{"uuid", uuid, "devid", m.ID, "device", disk.Path,
			"write", disk.Errors.Write, "read", disk.Errors.Read, "flush", disk.Errors.Flush,
			"corruption", disk.Errors.Corruption, "generation", disk.Errors.Generation}
		switch {
		case err != nil:
			slog.Warn("failed to reset btrfs device error counters", append(attrs, "error", err)...)
		case !found || disk.Errors == (BtrfsDeviceErrors{}):
			continue // not the disk that went missing, or nothing to reset
		case disk.Kept == keptIOErrors:
			slog.Warn("btrfs device error counters kept after resync for its i/o errors", attrs...)
		case disk.Kept == keptWholeErrors:
			slog.Warn("btrfs device error counters kept after resync for errors counted before the episode", attrs...)
		case disk.Kept == keptMissingErrors:
			slog.Warn("btrfs device error counters kept after resync for errors counted when it went missing", attrs...)
		case disk.Late:
			slog.Warn("btrfs device error counters reset after resync with i/o errors", attrs...)
		default:
			slog.Info("btrfs device error counters reset after resync", attrs...)
		}
		out.disks = append(out.disks, disk)
	}
	if present {
		// Again on every attempt that ends whole, so a reset an earlier
		// attempt made before a crash or a failed commit reaches the disk.
		if err := commitLabel(ctx, fd); err != nil {
			slog.Warn("failed to commit the reset of btrfs device error counters", "uuid", uuid, "error", err)
		}
	}
	return out
}

// openPool opens the mounted filesystem uuid once, for every ioctl of a
// reset to act on the same filesystem, and checks that it is that one.
func openPool(ctx context.Context, uuid string) (int, error) {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return -1, err
	}
	fd, err := openMount(mnt)
	if err != nil {
		return -1, err
	}
	var info fsInfoArgs
	if err := ioctlFD(fd, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("fs info of %s: %w", mnt, err)
	}
	if got := formatUUID(info.FSID[:]); got != uuid {
		unix.Close(fd)
		return -1, fmt.Errorf("%s holds filesystem %s, not %s", mnt, got, uuid)
	}
	return fd, nil
}

// resetRefusal says why the resync of run resets no counter of pool uuid,
// open at fd, or "" when it may: its scrub record is gone or found errors it
// could not correct, or a replace finished since the episode began
// (BTRFS_IOC_DEV_REPLACE keeps when the last one stopped).
func resetRefusal(fd int, uuid, run string, since int64) string {
	rec, ok := loadScrubRecord(uuid)
	if !ok || rec.Run != run || rec.Status != "finished" {
		// A scrub started since the resync checked its own.
		slog.Warn("btrfs device error counters kept after resync since its scrub is no longer on record", "uuid", uuid, "run", run)
		return "another scrub replaced the resync's record"
	}
	if n := rec.total().UncorrectableErrors; n != 0 {
		slog.Warn("btrfs device error counters kept after resync since its scrub found uncorrectable errors", "uuid", uuid, "uncorrectable", n)
		return "its scrub found errors it could not correct"
	}
	status := devReplaceArgs{Cmd: replaceCmdStatus}
	if err := ioctlFD(fd, iocDevReplace, unsafe.Pointer(&status)); err != nil {
		slog.Warn("failed to reset btrfs device error counters", "uuid", uuid, "error", fmt.Errorf("replace status: %w", err))
		return "the pool's last replace could not be read"
	}
	switch getU64(status.Params[:], replaceStatusState) {
	case replaceStateStarted, replaceStateSuspended:
		slog.Warn("btrfs device error counters kept after resync since a replace runs", "uuid", uuid)
		return "a replace runs on the pool"
	case replaceStateFinished:
		if stopped := int64(getU64(status.Params[:], replaceStatusTimeStopped)); stopped >= since {
			slog.Warn("btrfs device error counters kept after resync since a replace finished during the episode", "uuid", uuid, "stopped", stopped, "since", since)
			return "a disk was replaced during the episode"
		}
	}
	return ""
}

// resetDeviceCounters resets the error counters of disk m of pool uuid,
// open at fd, when they count only corruption and generation errors and
// counted none when LinuxIO last saw the pool whole (m.Whole), or else when
// the disk went missing; it keeps them, saying why, otherwise. It reports
// whether the pool still has the disk, under its devid and UUID and in use,
// and returns it with no errors when there was nothing to reset.
func resetDeviceCounters(fd int, uuid string, m missingDisk) (returnedDisk, bool, error) {
	disk := returnedDisk{ID: m.ID, Kept: keptUnread}
	info := devInfoArgs{DevID: m.ID}
	if err := ioctlFD(fd, iocDevInfo, unsafe.Pointer(&info)); err != nil {
		if errors.Is(err, unix.ENODEV) {
			return returnedDisk{ID: m.ID}, false, nil // removed since the episode began
		}
		return disk, true, fmt.Errorf("device info: %w", err)
	}
	path := cstring(info.Path[:])
	if path == "" || m.UUID == "" || formatUUID(info.UUID[:]) != m.UUID || !inUse(uuid, m.ID) {
		return returnedDisk{ID: m.ID}, false, nil // missing again, or another disk under the devid
	}
	disk.Path = path
	stats := devStatsArgs{DevID: m.ID, NrItems: 5}
	if err := ioctlFD(fd, iocGetDevStats, unsafe.Pointer(&stats)); err != nil {
		return disk, true, fmt.Errorf("device stats: %w", err)
	}
	checked := errorsOf(stats.Values)
	disk.Errors, disk.Counted, disk.Kept = checked, true, ""
	switch {
	case checked == (BtrfsDeviceErrors{}):
		return disk, true, nil
	case checked.ioErrors():
		disk.Kept = keptIOErrors
	case m.Errors != (BtrfsDeviceErrors{}) && m.Whole:
		disk.Kept = keptWholeErrors
	case m.Errors != (BtrfsDeviceErrors{}):
		disk.Kept = keptMissingErrors
	}
	if disk.Kept != "" {
		return disk, true, nil
	}
	stats = devStatsArgs{DevID: m.ID, NrItems: 5, Flags: devStatsReset}
	if err := ioctlFD(fd, iocGetDevStats, unsafe.Pointer(&stats)); err != nil {
		disk.Kept = keptResetFailed
		return disk, true, fmt.Errorf("reset device stats: %w", err)
	}
	disk.Errors = errorsOf(stats.Values)
	disk.Late = disk.Errors.ioErrors() // a read, write or flush failed between the check and the reset
	return disk, true, nil
}

// inUse reports whether the kernel uses device id of pool uuid, as the
// inventory reads it (markUnopened): not marked missing and writeable. A
// kernel without devinfo leaves the device as listed.
func inUse(uuid string, id uint64) bool {
	dir := filepath.Join(sysFSBtrfs, uuid, "devinfo", strconv.FormatUint(id, 10))
	if b, err := os.ReadFile(filepath.Join(dir, "missing")); err == nil && strings.TrimSpace(string(b)) == "1" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(dir, "writeable"))
	return err != nil || strings.TrimSpace(string(b)) != "0"
}

// commitLabel commits a transaction of the filesystem open at fd, so the
// kernel writes a counter reset to disk, by setting its label to itself
// under the label lock.
func commitLabel(ctx context.Context, fd int) error {
	return withLabelLock(ctx, func() error {
		var label [labelSize]byte
		if err := ioctlFD(fd, iocGetFSLabel, unsafe.Pointer(&label)); err != nil {
			return fmt.Errorf("read label: %w", err)
		}
		if err := ioctlFD(fd, iocSetFSLabel, unsafe.Pointer(&label)); err != nil {
			return fmt.Errorf("set label: %w", err)
		}
		return nil
	})
}

// withLabelLock runs fn under the lock of every label change, so the
// resync's commit, which sets a label to itself, never puts back a label a
// rename replaced meanwhile.
func withLabelLock(ctx context.Context, fn func() error) error {
	return filelock.RunExclusive(ctx, filepath.Join(filepath.Dir(PoolStatePath), "btrfs-label.lock"), fn, filelock.WithTimeout(rulesLockWait))
}

// resyncedObservation is the whole again notice of a resync that ended with
// fs whole: what its scrub corrected, by its own record, what it could not,
// which leaves data damaged and keeps the scrub alert up, and what became of
// the returned disks' error counters. While a device of the pool still
// counts errors the health alert stays, and the notice is a warning that
// says why each does.
func resyncedObservation(fs BtrfsFilesystem, out resyncOutcome) alerts.Observation {
	name := poolName(fs.Label, fs.UUID)
	scrub := fs.Scrub
	if rec, ok := loadScrubRecord(fs.UUID); ok && rec.Run == out.run {
		scrub = rec.total()
	}
	obs := alerts.Observation{
		Key: "resynced-" + fs.UUID, Severity: alerts.SeverityInfo,
		Title:   "Pool " + name + " is whole again",
		Message: fmt.Sprintf("The returned disk was resynced: the scrub corrected %d stale or damaged copies, and data written while it was missing has its redundancy back.", scrub.CorrectedErrors),
		Link:    "/storage?pool=" + fs.UUID, OccurrenceID: strconv.FormatInt(time.Now().Unix(), 10),
	}
	note, explained, late := returnedNote(out.disks)
	obs.Message += note
	if late {
		obs.Severity = alerts.SeverityWarning
	}
	if still := stillCounting(fs, out.kept, explained); still != "" {
		obs.Severity = alerts.SeverityWarning
		obs.Title = "Pool " + name + " is whole again, but its disks still count errors"
		obs.Message += still
	}
	if n := scrub.UncorrectableErrors; n > 0 {
		obs.Severity = alerts.SeverityWarning
		obs.Title = fmt.Sprintf("Pool %s is whole again, but %d errors could not be corrected", name, n)
		obs.Message += fmt.Sprintf(" The scrub found %d errors it could not correct, where no good copy was left: that data is damaged. Restore the affected files from a backup or snapshot, then scrub the pool; the scrub alert stays until a scrub finds none.", n)
	}
	return obs
}

// returnedNote says what the resync did with each returned disk's error
// counters, naming a disk by its devid when the kernel gave no path and
// giving only counts it read. It returns the devids whose counters it
// explained as kept, and whether a read, write or flush was reset with the
// rest.
func returnedNote(disks []returnedDisk) (string, map[uint64]bool, bool) {
	var note strings.Builder
	explained := map[uint64]bool{}
	var reset, late []string
	for _, d := range disks {
		disk, counts := d.Path, ""
		if disk == "" {
			disk = "device " + strconv.FormatUint(d.ID, 10)
		}
		if d.Counted {
			counts = " (" + countsOf(d.Errors) + ")"
		}
		switch {
		case d.Kept != "":
			note.WriteString(" " + disk + " kept its error counters" + counts + ": " + d.Kept + ".")
			explained[d.ID] = true
		case d.Late:
			late = append(late, disk+counts)
		default:
			reset = append(reset, disk+counts)
		}
	}
	if len(reset) > 0 {
		note.WriteString(" The stale copies' corruption and generation errors on " + andList(reset) + " were reset; the system journal keeps the counts.")
	}
	if len(late) > 0 {
		note.WriteString(" A read, write or flush failed on " + andList(late) + " in the instant its counters were reset, and was reset with them; the system journal keeps the counts.")
	}
	return note.String(), explained, len(late) > 0
}

// stillCounting says why each device of fs that still counts errors does,
// beyond those explained already, or "" when none does: the health alert
// stays beside the notice then.
func stillCounting(fs BtrfsFilesystem, kept string, explained map[uint64]bool) string {
	var note string
	counting := false
	for _, d := range fs.Devices {
		if d.Errors == (BtrfsDeviceErrors{}) {
			continue
		}
		counting = true
		if kept == "" && !explained[uint64(d.ID)] {
			disk := d.Path
			if disk == "" || d.Missing {
				disk = "device " + strconv.Itoa(d.ID)
			}
			note += " " + disk + " still counts errors (" + countsOf(d.Errors) + "): " + keptReasonOfOthers + "."
		}
	}
	if !counting {
		return ""
	}
	if kept != "" {
		note = " The returned disks' error counters were kept: " + kept + "." + note
	}
	return note + " The health alert stays; once you have looked at a disk, btrfs device stats -z clears its counters."
}

// countsOf names the non-zero counters of e, as "3 corruption and 1
// generation errors".
func countsOf(e BtrfsDeviceErrors) string {
	var counts []string
	for _, c := range []struct {
		name string
		n    uint64
	}{{"read", e.Read}, {"write", e.Write}, {"flush", e.Flush}, {"corruption", e.Corruption}, {"generation", e.Generation}} {
		if c.n > 0 {
			counts = append(counts, strconv.FormatUint(c.n, 10)+" "+c.name)
		}
	}
	if len(counts) == 0 {
		return "no errors"
	}
	return andList(counts) + " errors"
}

// andList joins items as "a, b and c".
func andList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
