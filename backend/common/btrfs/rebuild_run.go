package btrfs

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// The rebuild unit's run (Rebuild), and LinuxIO's own mounts of a pool with
// a rebuild on record (eraseUnverifiedTarget, settleUnverifiedRebuild); what
// the health run decides is in rebuild.go.

// ErrNothingToRebuild is a rebuild run that found nothing to do, such as
// one started while another operation runs on the pool.
var ErrNothingToRebuild = errors.New("nothing to rebuild")

// Rebuild runs the automatic rebuild of pool uuid the health run decided
// on: it follows or finishes a rebuild on record, or rebuilds a missing
// member onto its returned disk or the hot spare, deciding again on what it
// reads now. report is given the replace's percentage. A refusal or failure
// it raised the pool's rebuild alert for is marked scheduledrun.Reported;
// ErrNothingToRebuild says there was nothing to do.
func Rebuild(ctx context.Context, uuid string, report func(percent float64)) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	states, err := loadPoolStates()
	if err != nil {
		return err
	}
	st := states[uuid]
	in, err := rebuildInputOf(ctx, fs, st, true)
	if err != nil {
		return err
	}
	plan := decideRebuild(in)
	switch plan.kind {
	case rebuildResume:
		return resumeRebuild(ctx, fs, *st.Rebuild, plan.member, report)
	case rebuildReturned, rebuildSpare:
		return rebuildOnto(ctx, fs, plan, nil, report)
	case rebuildRefuse:
		if saveErr := savePlan(ctx, uuid, plan); saveErr != nil {
			return saveErr
		}
		if states, err = loadPoolStates(); err != nil {
			return err
		}
		raiseRebuildAlert(ctx, fs, states[uuid], plan)
		return scheduledrun.Reported(fmt.Errorf("rebuild of pool %s refused: %s", uuid, plan.reason))
	case rebuildNone, rebuildWait:
	}
	if plan.reason != "" {
		return fmt.Errorf("%w: %s", ErrNothingToRebuild, plan.reason)
	}
	return ErrNothingToRebuild
}

// Seams for tests: the disk lock with the device number of the disk it
// holds, and the device number of an open block device.
var (
	lockTargetDisk = lockNumberedDisk
	deviceNumberOf = blockDeviceNumber
)

// lockNumberedDisk takes device's disk lock (LockDisk) and returns, with the
// unlock, the device number of the disk the lock's descriptor holds.
func lockNumberedDisk(ctx context.Context, device string) (func(), uint64, error) {
	f, err := lockDiskFile(ctx, device)
	if err != nil {
		return nil, 0, err
	}
	rdev, err := deviceNumberOf(f)
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("device number of %s: %w", device, err)
	}
	return func() { f.Close() }, rdev, nil
}

// blockDeviceNumber returns the device number of the block device open at f.
func blockDeviceNumber(f *os.File) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return 0, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return 0, fmt.Errorf("%s is not a block device", f.Name())
	}
	return st.Rdev, nil
}

// rebuildOnto rebuilds plan's member onto plan's target: under the target's
// disk lock it checks the target again through an exclusive open (the
// returned disk's superblock and identity, or the spare as SpareOf reads it
// now), records the rebuild, erases the returned disk's superblocks through
// that same open, replaces the member onto it reading the other mirrors,
// and verifies the result. restart, when set, is the record of a rebuild
// started again onto its disk after a cut, which the erase or a cancel left
// blank: a refusal then fails it, rather than leaving it to start again at
// every health run.
func rebuildOnto(ctx context.Context, fs BtrfsFilesystem, plan rebuildPlan, restart *rebuildRecord, report func(float64)) error {
	uuid, m, target := fs.UUID, plan.member, plan.target
	// The lock's descriptor, held to the end, keeps target's name from
	// passing to another disk, even after the disk goes away, until the
	// kernel's own open for the replace: sd and virtio_blk give a name back
	// only once the disk's last open is closed (scsi_disk_free_disk,
	// virtblk_free_disk). So the disk checked here, the one the lock holds,
	// is the one the replace opens by name.
	unlock, locked, err := lockTargetDisk(ctx, target)
	if err != nil {
		return fmt.Errorf("lock %s: %w", target, err)
	}
	defer unlock()
	// A check that could not be made is an error, and the next run tries
	// again; a reason is what the check found, final for that disk.
	f, reason, err := openCheckedTarget(ctx, fs, plan, locked, restart)
	switch {
	case err != nil:
		return err
	case reason != "" && restart != nil:
		return failRebuild(ctx, fs, *restart, reason)
	case reason != "":
		return refuseTarget(ctx, fs, plan, reason)
	}
	closeTarget := func() {
		if f != nil {
			f.Close()
		}
	}
	boot, err := bootID()
	if err != nil {
		closeTarget()
		return err
	}
	_, _, prior, err := replaceStatusOf(fs.Mountpoints[0].Mountpoint)
	if err != nil {
		closeTarget()
		return err
	}
	rec := rebuildRecord{
		ID: uint64(m.ID), UUID: m.UUID, Disk: plan.disk, Device: target, Spare: plan.kind == rebuildSpare,
		Started: time.Now().Unix(), Boot: boot, Prior: prior.Unix(), Untrusted: plan.untrusted,
	}
	if err := writeRebuildRecord(ctx, uuid, &rec, restart); err != nil {
		closeTarget()
		return err
	}
	raise(ctx, rebuildRunningObservation(fs, rec))
	slog.Debug("btrfs automatic rebuild started", "uuid", uuid, "devid", m.ID, "device", target, "spare", rec.Spare, "restart", restart != nil)
	if f != nil {
		if ctx.Err() != nil {
			// Stopped before anything was written: the disk is as it was.
			closeTarget()
			markRestart(ctx, uuid, rec)
			return ctx.Err()
		}
		if failure := eraseTarget(f, target, uuid, m); failure != "" {
			return failRebuild(ctx, fs, rec, failure)
		}
		slog.Info("erased the returned disk of a missing btrfs device to rebuild onto it", "uuid", uuid, "devid", m.ID, "device", target)
	}
	if err := replaceDevice(ctx, fs, m.ID, target, true, report); err != nil {
		if ctx.Err() != nil {
			// The cancel destroyed the target: the next rebuild starts again.
			markRestart(ctx, uuid, rec)
			return err
		}
		return failRebuild(ctx, fs, rec, fmt.Sprintf("the replace of device %d onto %s failed: %v", m.ID, target, err))
	}
	return finishRebuild(ctx, fs, rec)
}

// refuseTarget refuses plan's target for the episode, for reason, what its
// final check found, and raises the pool's rebuild alert.
func refuseTarget(ctx context.Context, fs BtrfsFilesystem, plan rebuildPlan, reason string) error {
	plan.kind, plan.reason, plan.refused, plan.sighting = rebuildRefuse, reason, &plan.disk, nil
	refused := fmt.Errorf("rebuild of device %d of pool %s onto %s refused: %s", plan.member.ID, fs.UUID, plan.target, reason)
	if err := savePlan(ctx, fs.UUID, plan); err != nil {
		return errors.Join(refused, err)
	}
	states, err := loadPoolStates()
	if err != nil {
		return errors.Join(refused, err)
	}
	raiseRebuildAlert(ctx, fs, states[fs.UUID], plan)
	return scheduledrun.Reported(refused)
}

// openCheckedTarget checks plan's target right before the rebuild writes
// it, locked being the device number of the disk its lock holds: the spare
// as SpareOf reads it now, or the returned disk through an exclusive open
// for writing, which it returns open for the erase. A reason is what the
// check found, a refusal final for that disk in the episode; an error is a
// check that could not be made, as when something holds the disk for a
// moment, which the next run makes again.
func openCheckedTarget(ctx context.Context, fs BtrfsFilesystem, plan rebuildPlan, locked uint64, restart *rebuildRecord) (*os.File, string, error) {
	target := plan.target
	if plan.kind == rebuildSpare {
		s, err := SpareOf(ctx, fs.UUID)
		switch {
		case errors.Is(err, ErrNoSpare):
			return nil, "the pool has no hot spare any more", nil
		case err != nil:
			return nil, "", err
		case s.State == SpareUnknown:
			return nil, "", fmt.Errorf("check the hot spare %s: %s", target, s.Reason)
		case s.State != SpareReady:
			return nil, "the hot spare " + target + " is not ready: " + s.Reason, nil
		case s.Device != target || !s.Disk.matches(plan.disk):
			return nil, "the hot spare changed since the health check chose it", nil
		}
		// SpareOf read the disk by its name, which must still be the disk
		// the lock holds.
		disks, err := wholeDisks()
		if err != nil {
			return nil, "", err
		}
		if held, ok := diskNumbered(locked, disks); !ok || !plan.disk.matches(held.disk) {
			return nil, "", fmt.Errorf("%s no longer names the disk its lock holds", target)
		}
		return nil, "", nil
	}
	f, err := openRebuildTarget(target)
	if err != nil {
		return nil, "", fmt.Errorf("open %s for itself alone: %w", target, err)
	}
	reason, err := checkTargetOpen(f, fs, plan, locked, restart)
	if err != nil || reason != "" {
		f.Close()
		return nil, reason, err
	}
	return f, "", nil
}

// eraseTarget erases the pool's superblocks on target, open at f, then
// closes f and has the kernel forget target, and returns why it failed, or
// "". The forget fails with EBUSY once the erase is done when the kernel
// marked member m missing: the returned disk's scan gave that member this
// disk's device number, which a forget refuses to drop (seen on 7.0), so it
// is accepted there, and only there.
func eraseTarget(f *os.File, target, uuid string, m BtrfsDevice) string {
	wiped, err := wipePoolSuperblocksOf(f, target, uuid)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil && (!wiped || !errors.Is(err, unix.EBUSY) || m.DroppedOut) {
		return fmt.Sprintf("%s could not be erased: %v", target, err)
	}
	return ""
}

// heldDisk returns the whole disk open at f, by its device number, which
// must be that of the disk target's lock holds (locked): the name may have
// passed to another disk between the two opens. An error is a check that
// could not be made, such as on a disk udev has no entry for yet.
func heldDisk(f *os.File, target string, locked uint64) (spareCandidate, error) {
	rdev, err := deviceNumberOf(f)
	if err != nil {
		return spareCandidate{}, fmt.Errorf("device number of %s: %w", target, err)
	}
	if rdev != locked {
		return spareCandidate{}, fmt.Errorf("%s names another disk than the one its lock holds", target)
	}
	disks, err := wholeDisks()
	if err != nil {
		return spareCandidate{}, err
	}
	held, ok := diskNumbered(rdev, disks)
	if !ok || unidentified(held.device) {
		return spareCandidate{}, fmt.Errorf("the identity of %s could not be read: it is no whole disk udev reported", target)
	}
	return held, nil
}

// diskNumbered returns the disk of disks whose device number is rdev, and
// whether there is one.
func diskNumbered(rdev uint64, disks []spareCandidate) (spareCandidate, bool) {
	want := fmt.Sprintf("%d:%d", unix.Major(rdev), unix.Minor(rdev))
	for _, c := range disks {
		b, err := os.ReadFile(filepath.Join(sysBlockDir, filepath.Base(c.device), "dev"))
		if err == nil && strings.TrimSpace(string(b)) == want {
			return c, true
		}
	}
	return spareCandidate{}, false
}

// checkTargetOpen says why the target open at f is not to be erased, or "":
// it must be the disk the plan chose, by the hardware identity udev has for
// the disk the open reached, part of nothing else, and carry the pool's
// superblock for member m at a generation the pool has reached. A restart,
// the record's, also takes the disk blank as LinuxIO's erase and the
// kernel's cancel leave it: the pool's superblock with only its magic
// zeroed (both zero only the magic, volumes.c btrfs_scratch_superblock), or
// a replace target's superblock of the pool (devid 0); and no other
// filesystem udev reports on it. An error is a check that could not be made.
func checkTargetOpen(f *os.File, fs BtrfsFilesystem, plan rebuildPlan, locked uint64, restart *rebuildRecord) (string, error) {
	target, m := plan.target, plan.member
	held, err := heldDisk(f, target, locked)
	if err != nil {
		return "", err
	}
	switch {
	case !plan.disk.matches(held.disk):
		return target + " is no longer the disk the health check saw", nil
	case held.disk.SizeBytes < m.SizeBytes:
		return fmt.Sprintf("%s is smaller than device %d", target, m.ID), nil
	}
	if why, partErr := partOfSomethingElse(f, target); why != "" || partErr != nil {
		return why, partErr
	}
	raw, err := primarySuperblock(f)
	if err != nil {
		return "", fmt.Errorf("read the superblock of %s: %w", target, err)
	}
	sb, sbErr := parseSuperblock(raw)
	if restart != nil {
		// A stop right after the record leaves the disk as it was before the
		// erase, which the checks below take as before.
		if why, blank, blankErr := blankAsCut(raw, sb, sbErr, held, fs.UUID, m, target); why != "" || blank || blankErr != nil {
			return why, blankErr
		}
	}
	if sbErr != nil {
		if errors.Is(sbErr, errNotBtrfs) {
			return fmt.Sprintf("%s no longer carries device %d's superblock", target, m.ID), nil
		}
		return "", fmt.Errorf("read the superblock of %s: %w", target, sbErr)
	}
	info, err := mountedInfo(fs.Mountpoints[0].Mountpoint)
	if err != nil {
		return "", err
	}
	switch {
	case sb.UUID != fs.UUID || sb.DevID != uint64(m.ID):
		return fmt.Sprintf("%s no longer carries device %d's superblock", target, m.ID), nil
	case sb.DevUUID != m.UUID:
		return fmt.Sprintf("%s carries device %d's ID with another device UUID, as a disk replaced earlier would", target, m.ID), nil
	case sb.Generation > info.Generation:
		return newerGeneration(target, sb.Generation), nil
	}
	return "", nil
}

// blankAsCut judges, for a rebuild started again, the disk held carrying
// raw, its primary superblock (sb, or sbErr when it is not one): blank as a
// cut leaves it (the pool's superblock with only its magic zeroed, or a
// replace target's of the pool), why it is no longer blank, or, for member
// m's own superblock from before the erase, neither, for the caller's
// checks. No filesystem udev reports on it may be another than the pool.
func blankAsCut(raw []byte, sb Superblock, sbErr error, held spareCandidate, uuid string, m BtrfsDevice, target string) (string, bool, error) {
	props, err := udevProperties(filepath.Base(held.device))
	if err != nil {
		return "", false, err
	}
	if fsType := props["ID_FS_TYPE"]; fsType != "" && (fsType != "btrfs" || props["ID_FS_UUID"] != uuid) {
		return fmt.Sprintf("%s is no longer blank: it carries a %s filesystem", target, fsType), false, nil
	}
	switch {
	case sbErr == nil && sb.UUID == uuid && sb.DevID == replaceTargetDevID:
		return "", true, nil
	case errors.Is(sbErr, errNotBtrfs) && binary.LittleEndian.Uint64(raw[superMagicAt:]) == 0 && formatUUID(raw[superFSID:superFSID+16]) == uuid:
		return "", true, nil
	case sbErr != nil || sb.UUID != uuid || sb.DevID != uint64(m.ID):
		return target + " is no longer blank: it holds something other than the pool's erased superblock", false, nil
	}
	return "", false, nil
}

// partOfSomethingElse says why target, open at f, is part of something else,
// or "": partitions or a holder by sysfs, or an md or LVM2 signature only the
// disk itself shows, read through f. An error is a read that failed.
func partOfSomethingElse(f *os.File, target string) (string, error) {
	why, err := heldBy(target)
	if err != nil {
		return "", err
	}
	if why != "" {
		return target + " " + why, nil
	}
	sig, err := fileSignature(f, target, nil)
	switch {
	case err != nil:
		return "", fmt.Errorf("read the signatures of %s: %w", target, err)
	case sig.Kind != "":
		return fmt.Sprintf("%s carries %s signature, so it is part of something else", target, cmp.Or(signatureNames[sig.Kind], sig.Kind)), nil
	}
	return "", nil
}

// primarySuperblock reads the bytes of the primary superblock of the device
// open at f; a device too small to have one is not btrfs.
func primarySuperblock(f *os.File) ([]byte, error) {
	buf := make([]byte, superSize)
	if _, err := f.ReadAt(buf, superOffset); errors.Is(err, io.EOF) {
		return nil, errNotBtrfs
	} else if err != nil {
		return nil, err
	}
	return buf, nil
}

// superblockOf reads the primary superblock of the device open at f.
func superblockOf(f *os.File) (Superblock, error) {
	raw, err := primarySuperblock(f)
	if err != nil {
		return Superblock{}, err
	}
	return parseSuperblock(raw)
}

// writeRebuildRecord records rec as the pool's rebuild, before anything is
// erased: the state file is written whole and synced (atomicfile). A rebuild
// started again replaces its own record, restart, only while it is still
// on file: one cleared or failed since, as by a person's replace, is
// errRecordChanged and nothing is written.
func writeRebuildRecord(ctx context.Context, uuid string, rec, restart *rebuildRecord) error {
	changed := false
	err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		if restart != nil && (!sameRecord(state.Rebuild, restart) || state.Rebuild.Failed != "") {
			changed = true
			return
		}
		state.Rebuild, state.Returned, state.RebuildRefusal, state.RebuildAdvice, state.RebuildWait = rec, nil, "", "", ""
		s[uuid] = state
	})
	if err == nil && changed {
		return errRecordChanged
	}
	return err
}

// errRecordChanged is a rebuild started again whose record was cleared or
// failed since the run read it.
var errRecordChanged = fmt.Errorf("%w: the record of the rebuild changed meanwhile", ErrNothingToRebuild)

// failRebuild records why the rebuild rec failed after it wrote to its disk,
// refuses that disk for the episode and raises the pool's rebuild alert. A
// record cleared or replaced since the run read it, as by a person's
// replace, is left as it is, and nothing is raised: the failure is returned
// alone.
func failRebuild(ctx context.Context, fs BtrfsFilesystem, rec rebuildRecord, reason string) error {
	ctx, cancel := afterStop(ctx)
	defer cancel()
	// The worker prints the failure, which goes to the run's output.
	failure := fmt.Errorf("rebuild of device %d of pool %s onto %s failed: %s", rec.ID, fs.UUID, rec.Device, reason)
	rec.Failed, rec.Restart = reason, false
	current := false
	err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[fs.UUID]
		if current = state.Rebuild != nil && state.Rebuild.ID == rec.ID && state.Rebuild.Started == rec.Started; !current {
			return
		}
		state.Rebuild = &rec
		if _, ok := state.refusalOf(rec.Disk); !ok {
			state.Refused = append(state.Refused, refusedDisk{Disk: rec.Disk, Reason: reason})
		}
		s[fs.UUID] = state
	})
	switch {
	case err != nil:
		return errors.Join(failure, err)
	case !current:
		return fmt.Errorf("%w; its record was cleared meanwhile", failure)
	}
	states, err := loadPoolStates()
	if err != nil {
		return errors.Join(failure, err)
	}
	raise(ctx, rebuildFailedObservation(fs, rec, states[fs.UUID].Refused))
	return scheduledrun.Reported(failure)
}

// markRestart notes that the rebuild rec was canceled with its target
// blank again, so the next rebuild starts it again onto the same disk.
func markRestart(ctx context.Context, uuid string, rec rebuildRecord) {
	ctx, cancel := afterStop(ctx)
	defer cancel()
	err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		if state.Rebuild != nil && state.Rebuild.Started == rec.Started && state.Rebuild.Failed == "" {
			state.Rebuild.Restart = true
		}
		s[uuid] = state
	})
	if err != nil {
		slog.Warn("failed to record btrfs pool state", "uuid", uuid, "error", err)
	}
}

// stopBound bounds what runs on after a stop of the rebuild unit: below
// systemd's default TimeoutStopSec (90 seconds), which the unit keeps, so the
// writes end before systemd kills the run.
const stopBound = time.Minute

// afterStop is a context for what must run to its end once the rebuild is
// stopped, such as the record and alert writes: ctx's values, not its end,
// bounded by stopBound.
func afterStop(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), stopBound)
}

// finishRebuild verifies the rebuild rec once its replace ended, the disk
// in the slot being rec's by its hardware identity too, and on success
// notes the disk as rebuilt onto, drops a spare it took, grows into a
// bigger disk and says so; on failure the record stays, as failed. A stop
// of the run does not cut the check or the clearing of the record: a
// record left in flight would have the next boot take this good disk for
// an unverified one.
func finishRebuild(ctx context.Context, fs BtrfsFilesystem, rec rebuildRecord) error {
	ctx, cancel := afterStop(ctx)
	defer cancel()
	got, err := verifyReplaced(ctx, fs.UUID, int(rec.ID), "")
	if err != nil {
		if !errors.Is(err, ErrReplaceUnverified) {
			return err
		}
		return failRebuild(ctx, fs, rec, err.Error())
	}
	i := slices.IndexFunc(got.Devices, func(d BtrfsDevice) bool { return d.ID == int(rec.ID) })
	installed := got.Devices[i]
	disks, err := wholeDisks()
	if err != nil {
		return err
	}
	if now, _ := diskOf(installed.Path, disks); !rec.Disk.matches(now) {
		return failRebuild(ctx, fs, rec, fmt.Sprintf("device %d is %s, which is not the disk the rebuild wrote", rec.ID, installed.Path))
	}
	replaced := replacedMember(ctx, fs.UUID, int(rec.ID), installed.Path)
	// One write records the new disk, clears the record and notes the disk
	// rebuilt onto. A record left behind would have LinuxIO's next mount
	// erase this good disk: a write that fails fails the run, and the next
	// run in this boot checks the replace again and clears it.
	if err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[fs.UUID]
		state.noteReplaced(replaced)
		cutoff := time.Now().Add(-rebuildAgainAfter).Unix()
		state.Rebuilt = slices.DeleteFunc(state.Rebuilt, func(r rebuiltDisk) bool { return r.At < cutoff || r.Disk.matches(rec.Disk) })
		state.Rebuilt = append(state.Rebuilt, rebuiltDisk{Disk: rec.Disk, At: time.Now().Unix()})
		s[fs.UUID] = state
	}); err != nil {
		return fmt.Errorf("record the verified rebuild of device %d of pool %s: %w", rec.ID, fs.UUID, err)
	}
	if rec.Spare {
		if _, err := ClearSpare(ctx, fs.UUID); err != nil {
			slog.Warn("failed to remove the hot spare a rebuild installed", "uuid", fs.UUID, "device", rec.Device, "error", err)
		}
	}
	if installed.Growable {
		if err := GrowDevice(ctx, fs.UUID, int(rec.ID)); err != nil {
			slog.Warn("btrfs grow after rebuild failed", "uuid", fs.UUID, "devid", rec.ID, "error", err)
		}
	}
	resolve(ctx, "rebuild-"+fs.UUID)
	name := poolName(fs.Label, fs.UUID)
	onto := "the disk that came back, " + installed.Path
	if rec.Spare {
		onto = "the hot spare " + installed.Path
	}
	raise(ctx, alerts.Observation{
		Key: "rebuilt-" + fs.UUID, Severity: alerts.SeverityInfo,
		Title:   fmt.Sprintf("Pool %s rebuilt device %d", name, rec.ID),
		Message: fmt.Sprintf("LinuxIO copied device %d onto %s and checked that the disk works in the pool. It now resyncs the pool, which gives a second copy to what was written while the disk was missing.", rec.ID, onto),
		Link:    "/storage?pool=" + fs.UUID, OccurrenceID: strconv.FormatInt(rec.Started, 10),
	})
	slog.Info("btrfs automatic rebuild finished", "uuid", fs.UUID, "devid", rec.ID, "device", installed.Path, "spare", rec.Spare)
	return nil
}

// resumeRebuild settles the rebuild rec found on record when the run starts:
// a replace the kernel still runs in the boot it started in is followed to
// its end, and one that finished there is verified; one the kernel
// continued after a reboot, which nothing watched, is canceled (or, once
// finished, cannot be trusted); one canceled with its target blank is
// started again onto the same disk, as restartPlan allowed. A replace is the
// rebuild's when the kernel's start time for it is not the one on record
// from before (rebuildRecord.Prior), whatever the clock did in between.
func resumeRebuild(ctx context.Context, fs BtrfsFilesystem, rec rebuildRecord, m BtrfsDevice, report func(float64)) error {
	mnt := fs.Mountpoints[0].Mountpoint
	boot, err := bootID()
	if err != nil {
		return err
	}
	state, _, started, err := replaceStatusOf(mnt)
	if err != nil {
		return err
	}
	watched := rec.Boot == boot && !rec.Restart
	ours := rec.ours(started)
	switch {
	case (state == replaceStateStarted || state == replaceStateSuspended) && !ours:
		// Not this rebuild's replace, such as one started from the pool
		// details: it is left to run.
		return fmt.Errorf("%w: pool %s runs a replace the rebuild of device %d did not start", ErrNothingToRebuild, fs.UUID, rec.ID)
	case state == replaceStateStarted && watched:
		return followRebuild(ctx, fs, rec, report)
	case state == replaceStateStarted, state == replaceStateSuspended:
		// Continued at a mount nobody watched, or held for its target: the
		// cancel leaves the target blank to start again.
		if err := cancelRecordedReplace(mnt, fs.UUID, rec); err != nil {
			return err
		}
	case state == replaceStateFinished && ours && watched:
		return finishRebuild(ctx, fs, rec)
	case state == replaceStateFinished && ours:
		return failRebuild(ctx, fs, rec, untrustedReason(rec))
	}
	if !m.Missing && !rec.Spare {
		return settleNeverErased(ctx, fs, rec, m, state == replaceStateFinished)
	}
	// A spare left blank by the cancel replaces the member again, whether
	// the member is missing or the untrusted disk a failed rebuild left.
	return restartRebuild(ctx, fs, rec, m, report)
}

// followRebuild follows the replace of the rebuild rec that the kernel runs
// in the boot it started in, and verifies it once it ends.
func followRebuild(ctx context.Context, fs BtrfsFilesystem, rec rebuildRecord, report func(float64)) error {
	if err := checkReplaceSource(fs.Mountpoints[0].Mountpoint, fs.UUID, int(rec.ID)); err != nil {
		return err
	}
	err := resumeReplace(ctx, fs.UUID, int(rec.ID), time.Unix(rec.Started, 0), report, false)
	switch {
	case err == nil:
		return finishRebuild(ctx, fs, rec)
	case ctx.Err() == nil && errors.Is(err, ErrReplaceUnverified):
		return failRebuild(ctx, fs, rec, err.Error())
	}
	return err
}

// cancelRecordedReplace cancels the running or suspended replace of the
// pool mounted at mnt when it is the rebuild rec's: it copies from rec's
// devid (or the kernel names no source) and its target, when the kernel
// still has one, is rec's disk. Any other replace is left alone.
func cancelRecordedReplace(mnt, uuid string, rec rebuildRecord) error {
	if src, known, err := replaceSource(mnt); err != nil {
		return err
	} else if known && src != rec.ID {
		return fmt.Errorf("%w: pool %s replaces device %d, not device %d", ErrNothingToRebuild, uuid, src, rec.ID)
	}
	if disk, ok, err := replaceTargetDisk(mnt); err != nil {
		return err
	} else if ok && !rec.Disk.matches(disk) {
		return fmt.Errorf("%w: pool %s replaces device %d onto a disk other than the rebuild's", ErrNothingToRebuild, uuid, rec.ID)
	}
	if _, err := cancelReplace(mnt); err != nil {
		return fmt.Errorf("cancel the replace of device %d: %w", rec.ID, err)
	}
	slog.Info("canceled the replace of an unverified btrfs rebuild", "uuid", uuid, "devid", rec.ID)
	return nil
}

// replaceTargetDisk returns the hardware identity of the target of the pool's
// replace (DEV_INFO of devid 0), and whether the kernel has one: a replace
// suspended for its missing target has none.
func replaceTargetDisk(mnt string) (BtrfsSpareDisk, bool, error) {
	info := devInfoArgs{DevID: replaceTargetDevID}
	if err := fsIoctl(mnt, iocDevInfo, unsafe.Pointer(&info)); errors.Is(err, unix.ENODEV) {
		return BtrfsSpareDisk{}, false, nil
	} else if err != nil {
		return BtrfsSpareDisk{}, false, fmt.Errorf("device info of the replace target: %w", err)
	}
	path := cstring(info.Path[:])
	if path == "" || path == missingDiskName {
		return BtrfsSpareDisk{}, false, nil
	}
	disks, err := wholeDisks()
	if err != nil {
		return BtrfsSpareDisk{}, false, err
	}
	disk, _ := diskOf(path, disks)
	return disk, true, nil
}

// settleNeverErased settles a rebuild on record whose replace never ran
// while its member m is in use again: when m is the recorded disk, the pool
// took it back at a mount, which a disk with its superblock erased cannot
// be, so the rebuild stopped before its erase and the record goes; any other
// disk in the slot is not one LinuxIO can account for, and the rebuild
// fails. When the pool's last replace finished (finished), the disk is
// checked as after a replace first (verifyReplaced), whatever its start
// time said: no record is cleared on that alone.
func settleNeverErased(ctx context.Context, fs BtrfsFilesystem, rec rebuildRecord, m BtrfsDevice, finished bool) error {
	disks, err := wholeDisks()
	if err != nil {
		return err
	}
	if now, _ := diskOf(m.Path, disks); !rec.Disk.matches(now) {
		return failRebuild(ctx, fs, rec, fmt.Sprintf("device %d is in use again as %s without the rebuild onto %s having finished", rec.ID, m.Path, rec.Device))
	}
	if finished {
		if _, err := verifyReplaced(ctx, fs.UUID, int(rec.ID), ""); errors.Is(err, ErrReplaceUnverified) {
			return failRebuild(ctx, fs, rec, err.Error())
		} else if err != nil {
			return err
		}
	}
	if err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[fs.UUID]
		if state.Rebuild != nil && state.Rebuild.Started == rec.Started {
			state.Rebuild = nil
		}
		s[fs.UUID] = state
	}); err != nil {
		return err
	}
	resolve(ctx, "rebuild-"+fs.UUID)
	slog.Info("dropped the record of a btrfs rebuild that stopped before its erase", "uuid", fs.UUID, "devid", rec.ID, "device", m.Path)
	return fmt.Errorf("%w: the pool took device %d back before its rebuild erased it", ErrNothingToRebuild, rec.ID)
}

// untrustedReason is why a rebuild the kernel finished at a mount LinuxIO
// did not make, after a reboot, is not trusted.
func untrustedReason(rec rebuildRecord) string {
	return fmt.Sprintf("the pool was mounted with %s, the disk LinuxIO was rebuilding device %d onto, before LinuxIO could check it, and the kernel finished that rebuild from where it was cut; if the disk failed during the rebuild, it holds stale copies the scrub cannot all find (files without checksums), so LinuxIO does not call the pool whole. Unmount the pool and mount it again from the Storage overview: LinuxIO then erases that disk and mounts the pool degraded; then replace device %d", rec.Device, rec.ID, rec.ID)
}

// restartRebuild starts the rebuild rec again onto the same disk, found by
// its hardware identity, once a cancel or LinuxIO's mount left it blank. A
// disk that is not connected is waited for; two with its identity fail it.
func restartRebuild(ctx context.Context, fs BtrfsFilesystem, rec rebuildRecord, m BtrfsDevice, report func(float64)) error {
	disks, err := wholeDisks()
	if err != nil {
		return err
	}
	device, n := findDisk(rec.Disk, disks)
	switch {
	case n == 0:
		return fmt.Errorf("%w: %s, the disk of the rebuild of device %d, is not connected", ErrNothingToRebuild, rec.Device, rec.ID)
	case n > 1:
		return failRebuild(ctx, fs, rec, fmt.Sprintf("%d connected disks have the identity of %s, the disk the rebuild was writing", n, rec.Device))
	}
	kind := rebuildReturned
	if rec.Spare {
		kind = rebuildSpare
	}
	return rebuildOnto(ctx, fs, rebuildPlan{kind: kind, member: m, target: device, disk: rec.Disk, untrusted: rec.Untrusted}, &rec, report)
}

// erasable reports whether disk, carrying devid (0, or rec.ID), is a disk of
// the unverified rebuild rec that LinuxIO's mount erases: its target, or
// the untrusted disk a failed rebuild left in the slot the spare replaces.
func (rec rebuildRecord) erasable(devid uint64, disk BtrfsSpareDisk) bool {
	return rec.Disk.matches(disk) || devid == rec.ID && rec.Untrusted.matches(disk)
}

// unverifiedErase is what LinuxIO's mount of a pool does with the disks of
// an unverified rebuild on record: the devices to erase, by index in the
// pool's Devices, and the pool without them; or, in reason, why it erases
// nothing and does not mount the pool, and in wayOut what a person can do
// about it from where they are.
type unverifiedErase struct {
	erase     []int
	remaining BtrfsOfflinePool
	reason    string
	wayOut    string
}

// planUnverifiedErase decides, before anything is written, which devices of
// pool LinuxIO's mount erases for the rebuild on record in state. It looks
// only at devices carrying the replace target's devid 0 or the member's
// devid whose hardware identity is the rebuild's disk (erasable):
//
//   - the replace target is erased unless it is newer than every device that
//     stays (a replace target is written with the pool's superblock at every
//     commit, dev-replace.c btrfs_init_dev_replace_tgtdev and disk-io.c
//     write_all_supers, so its generation is the last commit that reached
//     it, as a member's is); it holds no copy the pool needs;
//   - a disk carrying the member's devid, which a finish nobody checked left
//     there, is erased only when every other member of the pool is present
//     and none is older than it: it may hold the only copy of what a
//     missing member held, so otherwise it is left as it is, and the pool is
//     mounted with it as without a record.
//
// It refuses the mount only where it cannot tell: two connected disks with
// the rebuild's identity, a device that may be the target or a member's
// disk it would erase whose identity udev does not report, or a target
// that is the only device present. Each reason comes with a way out. What
// the devices left are judged by beyond that is the caller's: the boot
// check's refusal, or the person's choice of a degraded mount.
func planUnverifiedErase(pool BtrfsOfflinePool, state poolState) (unverifiedErase, error) {
	plan := unverifiedErase{remaining: pool}
	rec := state.Rebuild
	if rec == nil || !rec.Disk.named() {
		return plan, nil
	}
	disks, err := wholeDisks()
	if err != nil {
		return plan, err
	}
	record := fmt.Sprintf("the unverified rebuild of device %d onto %s", rec.ID, rec.Device)
	found := rebuildDevicesOf(pool, *rec, disks, record)
	if found.reason != "" {
		plan.reason, plan.wayOut = found.reason, found.wayOut
		return plan, nil
	}
	erase := map[int]bool{}
	for _, i := range found.members {
		erase[i] = othersCurrent(pool, rec.ID, pool.generations[i])
	}
	for _, i := range found.unknown {
		if othersCurrent(pool, rec.ID, pool.generations[i]) {
			plan.reason = fmt.Sprintf("udev reports no identity for %s, which carries device %d's superblock, so LinuxIO cannot tell whether it is the disk of %s, which it would erase first", pool.Devices[i], rec.ID, record)
			plan.wayOut = fmt.Sprintf("Wait until udev reports %s, or disconnect it: the pool then mounts without it.", pool.Devices[i])
			return plan, nil
		}
	}
	newest, stays := uint64(0), false
	for i := range pool.Devices {
		if !erase[i] && !slices.Contains(found.targets, i) {
			newest, stays = max(newest, pool.generations[i]), true
		}
	}
	if len(found.targets) > 0 && !stays {
		plan.reason = fmt.Sprintf("%s, the replace target of %s, is the only device of the pool present", pool.Devices[found.targets[0]], record)
		plan.wayOut = "Reconnect the pool's other disks, then mount it."
		return plan, nil
	}
	for _, i := range found.targets {
		erase[i] = pool.generations[i] <= newest
	}
	rest := pool
	rest.Devices, rest.devids, rest.generations = nil, nil, nil
	for i, device := range pool.Devices {
		if erase[i] {
			plan.erase = append(plan.erase, i)
			continue
		}
		rest.Devices = append(rest.Devices, device)
		rest.devids = append(rest.devids, pool.devids[i])
		rest.generations = append(rest.generations, pool.generations[i])
	}
	rest.Missing = missingDisks(rest)
	plan.remaining = rest
	return plan, nil
}

// othersCurrent reports whether every member of pool other than devid id is
// among its devices, none with a generation older than gen.
func othersCurrent(pool BtrfsOfflinePool, id, gen uint64) bool {
	present := map[uint64]bool{}
	for i, devid := range pool.devids {
		if devid == replaceTargetDevID || devid == id {
			continue
		}
		if pool.generations[i] < gen {
			return false
		}
		present[devid] = true
	}
	return len(present) == pool.NumDevices-1
}

// rebuildDevices are the devices of a pool, by index in its Devices, that
// are or may be disks of an unverified rebuild: its replace target
// (targets), the disk of its member's devid (members), and those carrying
// that devid whose identity udev does not report (unknown); or why they
// cannot be told, with a way out.
type rebuildDevices struct {
	targets, members, unknown []int
	reason, wayOut            string
}

// rebuildDevicesOf finds the devices of pool that are or may be disks of the
// unverified rebuild rec, by their devid (0, or rec.ID) and their identity
// among disks. Two connected disks with one of its identities cannot be
// told apart. A whole disk udev has no entry for, or reports without a WWN
// or serial number (as some enclosures do), may be any disk: as a replace
// target it is refused, since mounted with it the kernel would continue the
// replace. record names the rebuild in the reason.
func rebuildDevicesOf(pool BtrfsOfflinePool, rec rebuildRecord, disks []spareCandidate, record string) rebuildDevices {
	var found rebuildDevices
	for _, d := range []BtrfsSpareDisk{rec.Disk, rec.Untrusted} {
		if _, n := findDisk(d, disks); n > 1 {
			found.reason = fmt.Sprintf("%d connected disks have the WWN, serial number and size of a disk of %s, so LinuxIO cannot tell which one to erase first", n, record)
			found.wayOut = "Disconnect the disks with that serial number that are not the pool's, then mount the pool from the Storage overview."
			return found
		}
	}
	for i, device := range pool.Devices {
		devid := pool.devids[i]
		if devid != replaceTargetDevID && devid != rec.ID {
			continue
		}
		disk, whole := diskOf(device, disks)
		known := whole && disk.named()
		switch {
		case known && rec.erasable(devid, disk) && devid == replaceTargetDevID:
			found.targets = append(found.targets, i)
		case known && rec.erasable(devid, disk):
			found.members = append(found.members, i)
		case known, !whole && !unidentified(device):
			// Another disk, or no whole disk: none LinuxIO rebuilds onto.
		case devid == replaceTargetDevID:
			found.reason = fmt.Sprintf("udev reports no identity for %s, a replace target of the pool, so LinuxIO cannot tell whether it is the disk of %s, which it erases first: mounted with it, the kernel would continue that replace past what it never copied", device, record)
			found.wayOut = fmt.Sprintf("Disconnect %s: the pool then mounts without it. Or wait until udev reports the disk, if it has a WWN or serial number.", device)
			return found
		default:
			found.unknown = append(found.unknown, i)
		}
	}
	return found
}

// unidentified reports whether device is a disk udev has no entry for yet,
// whose identity cannot be read: it may be any disk. A partition, or a disk
// udev reported without a WWN or serial number, is no disk LinuxIO recorded.
func unidentified(device string) bool {
	name := filepath.Base(canonicalPath(device))
	if _, err := os.Stat(filepath.Join(sysBlockDir, name)); err != nil {
		return false
	}
	props, err := udevProperties(name)
	return err != nil || props == nil
}

// eraseUnverifiedTarget erases, before LinuxIO mounts pool, the disks of an
// automatic rebuild on record and not verified, as planUnverifiedErase
// decides: the replace target (devid 0), or the member it became after a
// finish nobody checked. Mounted with it, the kernel continues the replace
// from the cursor the last commit wrote, which after a target that failed
// is past every extent, so the replace "finishes" at once with the stale
// copies left (seen in the test VM, docs/btrfs.md); without it the kernel
// suspends the replace ("tgtdev is missing"), which settleUnverifiedRebuild
// cancels. It returns the pool without those disks, and the plan, whose
// reason, when set, is why it erased nothing and the pool is not to be
// mounted.
func eraseUnverifiedTarget(ctx context.Context, pool BtrfsOfflinePool) (BtrfsOfflinePool, unverifiedErase, error) {
	states, err := loadPoolStates()
	if err != nil {
		return pool, unverifiedErase{}, err
	}
	state := states[pool.UUID]
	plan, err := planUnverifiedErase(pool, state)
	if err != nil || plan.reason != "" {
		return pool, plan, err
	}
	for _, i := range plan.erase {
		if err := eraseDisk(ctx, pool, i, *state.Rebuild); err != nil {
			return pool, unverifiedErase{}, fmt.Errorf("erase %s, the disk of an unverified rebuild: %w", pool.Devices[i], err)
		}
		slog.Info("erased the disk of an unverified btrfs rebuild before mounting the pool", "uuid", pool.UUID, "device", pool.Devices[i], "devid", pool.devids[i])
	}
	return plan.remaining, plan, nil
}

// eraseDisk zeroes the magic of every superblock copy of pool on its device
// i, under its disk lock and through an exclusive open that reaches the
// disk the lock holds and finds on that disk what the plan was made on: the
// rebuild rec's disk by hardware identity, the devid and generation the scan
// read, and nothing else using it. It then has the kernel forget the device
// (WipePoolSuperblocks).
func eraseDisk(ctx context.Context, pool BtrfsOfflinePool, i int, rec rebuildRecord) error {
	device, devid := pool.Devices[i], pool.devids[i]
	unlock, locked, err := lockTargetDisk(ctx, device)
	if err != nil {
		return fmt.Errorf("lock %s: %w", device, err)
	}
	defer unlock()
	f, err := openRebuildTarget(device)
	if err != nil {
		return err
	}
	defer f.Close()
	held, err := heldDisk(f, device, locked)
	if err != nil {
		return err
	}
	if !rec.erasable(devid, held.disk) {
		return fmt.Errorf("%s is no longer the disk of the rebuild", device)
	}
	sb, err := superblockOf(f)
	if err != nil {
		return fmt.Errorf("read the superblock of %s: %w", device, err)
	}
	if sb.UUID != pool.UUID || sb.DevID != devid || sb.Generation != pool.generations[i] {
		return fmt.Errorf("%s no longer carries the superblock LinuxIO read from it", device)
	}
	why, err := partOfSomethingElse(f, device)
	switch {
	case err != nil:
		return err
	case why != "":
		return fmt.Errorf("%w: %s", ErrUnclearDisks, why)
	}
	_, err = wipePoolSuperblocksOf(f, device, pool.UUID)
	return err
}

// settleUnverifiedRebuild cancels, right after LinuxIO mounted pool uuid
// with a rebuild on record that is not verified, the replace the kernel
// suspended or continued, and marks the rebuild to start again unless it
// failed. A failure is logged: the pool is mounted.
func settleUnverifiedRebuild(ctx context.Context, uuid, mnt string) {
	states, err := loadPoolStates()
	if err != nil {
		slog.Warn("failed to read the btrfs pool state after mounting the pool", "uuid", uuid, "error", err)
		return
	}
	if states[uuid].Rebuild == nil {
		return
	}
	rec := *states[uuid].Rebuild
	state, _, started, err := replaceStatusOf(mnt)
	if err == nil && (state == replaceStateStarted || state == replaceStateSuspended) && rec.ours(started) {
		err = cancelRecordedReplace(mnt, uuid, rec)
		if errors.Is(err, ErrNothingToRebuild) {
			return // another replace, which stays
		}
	}
	if err != nil {
		slog.Warn("failed to cancel the replace of an unverified btrfs rebuild", "uuid", uuid, "devid", rec.ID, "error", err)
		return
	}
	if rec.Failed == "" {
		markRestart(ctx, uuid, rec)
	}
}
