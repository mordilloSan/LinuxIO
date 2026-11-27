package btrfs

import (
	"context"
	"crypto/rand"
	"encoding/json/jsontext"
	"encoding/json/v2"
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
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// A pool that loses a disk keeps running, but after a reboot systemd never
// mounts it: udev holds its devices until all are present (64-btrfs.rules),
// and upstream leaves the "n of m disks" policy to storage userspace. This
// file is that policy, after mdadm's last-resort timer: wait a while for the
// disks, then mount degraded, but only when the disks present hold the
// pool's newest state. Once every disk is back, Resync repairs what the
// returned disk missed, which btrfs leaves to a scrub.
//
// udev also holds back a complete pool cut by a power loss mid-replace: the
// replace target is scanned as devid 0 and BTRFS_IOC_DEVICES_READY counts it
// against the superblock's num_devices (super.c btrfs_control_ioctl,
// volumes.c device_list_add). The kernel resumes the replace at mount
// (dev-replace.c btrfs_resume_dev_replace_async), so the boot check mounts
// that pool as fstab says.

// PoolStatePath records, per filesystem UUID, the newest generation LinuxIO
// saw the pool mounted at and whether it ran with a disk missing since its
// last resync.
var PoolStatePath = version.DataDir + "/btrfs-pools.json"

// FstabPath is the system mount table; a var so tests can use a temp file.
var FstabPath = "/etc/fstab"

// DegradedGrace is how long the boot check waits for missing disks, as
// mdadm's last-resort timer does before it starts an incomplete array.
const DegradedGrace = 30 * time.Second

// degradedPoll is how often the boot check looks for the missing disks.
var degradedPoll = 2 * time.Second

type poolState struct {
	Generation uint64 `json:"generation"`
	Degraded   bool   `json:"degraded"`
	// Present holds the devids that took the writes while the pool last ran
	// degraded; every other disk missed them.
	Present []uint64 `json:"present,omitempty"`
	// Missing holds the disks missing then, which missed the writes: the
	// resync resets their error counters once it made them whole.
	Missing []missingDisk `json:"missing,omitempty"`
	// Counters are each device's error counters as the health run last saw
	// the pool whole outside an episode: the baseline of a disk that goes
	// missing, read before its dropout raised them.
	Counters []deviceCounters `json:"counters,omitempty"`
	// Since is when the episode began, in seconds since the epoch: a
	// replace that finished after it may have put a new disk under a
	// missing disk's devid and UUID.
	Since int64 `json:"since,omitzero"`
	// Scrubbed is set once Resync's scrub finished, so a retry after a
	// failed convert does not scrub the whole pool again.
	Scrubbed bool `json:"scrubbed"`
	// ResyncRun is the run of this episode's resync scrub, "resync-" and 128
	// random bits: the scrub record carries it, so the next resync continues
	// that scrub after a cut. markDegraded and clearDegraded clear it, which
	// makes the next resync start over.
	ResyncRun string `json:"resyncRun,omitempty"`
	// ConvertData and ConvertMetadata are the targets of the soft convert
	// the resync started in this episode ("" for a kind it did not convert).
	ConvertData     string `json:"convertData,omitempty"`
	ConvertMetadata string `json:"convertMetadata,omitempty"`
	// ResyncFailures counts failed resyncs since the pool was last mounted
	// or degraded; at maxResyncAttempts the health run stops starting them.
	ResyncFailures int    `json:"resyncFailures,omitempty"`
	ResyncError    string `json:"resyncError,omitempty"`

	// The automatic rebuild (rebuild.go). Identities are each member's
	// hardware identity as the health run last saw the pool whole outside
	// an episode, and after a verified replace for the new disk.
	Identities []memberIdentity `json:"identities,omitempty"`
	// Returned is the disk the last health run saw back for a missing
	// member, with how many runs in a row saw it.
	Returned *returnedSighting `json:"returned,omitzero"`
	// Rebuild is a rebuild that started and is not verified yet, or failed.
	Rebuild *rebuildRecord `json:"rebuild,omitzero"`
	// RebuildRefusal is why the last health run started no rebuild for a
	// missing member, when it refused one, and RebuildAdvice what to do
	// instead of a replace, when the refusal says. RebuildWait is what the
	// rebuild on record waits for, when it waits.
	RebuildRefusal string `json:"rebuildRefusal,omitzero"`
	RebuildAdvice  string `json:"rebuildAdvice,omitzero"`
	RebuildWait    string `json:"rebuildWait,omitzero"`
	// Refused are the disks the episode refused or failed to rebuild onto.
	Refused []refusedDisk `json:"refused,omitempty"`
	// Rebuilt are the disks rebuilt onto in the last day.
	Rebuilt []rebuiltDisk `json:"rebuilt,omitempty"`
	// NoAutoRebuild turns the rebuild onto a disk that returns off.
	NoAutoRebuild bool `json:"noAutoRebuild,omitzero"`
}

// maxResyncAttempts bounds the resyncs the health run starts for one
// episode, so a cause that needs a person does not scrub every ten minutes.
const maxResyncAttempts = 3

// markDegraded notes that only the present devices take the writes, and
// which miss them: as the episode opens, each with its counters as the pool
// was last seen whole; later, a disk newly missing with what it reads now,
// beside those the episode already holds. Each call starts the resync
// over: writes made now can land below the offsets a resync scrub saved, so
// the next resync scrubs everything again.
func (s *poolState) markDegraded(devices []BtrfsDevice) {
	opening := !s.Degraded
	if opening {
		s.Missing, s.Since, s.Refused = nil, time.Now().Unix(), nil
	}
	s.Degraded, s.Scrubbed, s.Present, s.ResyncRun = true, false, nil, ""
	s.ConvertData, s.ConvertMetadata = "", ""
	s.ResyncFailures, s.ResyncError = 0, ""
	for _, d := range devices {
		switch {
		case !d.Missing:
			s.Present = append(s.Present, uint64(d.ID))
		case !slices.ContainsFunc(s.Missing, func(m missingDisk) bool { return m.ID == uint64(d.ID) && m.UUID == d.UUID }):
			s.Missing = append(s.Missing, s.newMissingDisk(d, opening))
		}
	}
}

// ResyncUnitName is the systemd unit that runs Resync for pool uuid.
func ResyncUnitName(uuid string) string {
	return "linuxio-btrfs-resync@" + uuid + ".service"
}

func loadPoolStates() (map[string]poolState, error) {
	states := map[string]poolState{}
	data, err := os.ReadFile(PoolStatePath)
	if errors.Is(err, os.ErrNotExist) {
		return states, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("parse %s: %w", PoolStatePath, err)
	}
	return states, nil
}

// updatePoolStates applies fn to the pool states under the state file's lock.
func updatePoolStates(ctx context.Context, fn func(map[string]poolState)) error {
	return filelock.RunExclusive(ctx, PoolStatePath+".lock", func() error {
		states, err := loadPoolStates()
		if err != nil {
			return err
		}
		fn(states)
		data, err := json.Marshal(states, jsontext.WithIndent("  "), json.Deterministic(true))
		if err != nil {
			return fmt.Errorf("marshal %s: %w", PoolStatePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(PoolStatePath), 0o755); err != nil {
			return err
		}
		return atomicfile.Write(PoolStatePath, append(data, '\n'), 0o644)
	}, filelock.WithTimeout(rulesLockWait))
}

// ForgetPool drops the record of pool uuid. Destroy calls it once the pool's
// disks are wiped: a pool that is on no disk is never looked up again, and
// nothing else removes a record. A health run that listed the pool before
// the destroy can write the record once more; it is read by nothing either.
func ForgetPool(ctx context.Context, uuid string) error {
	return updatePoolStates(ctx, func(s map[string]poolState) { delete(s, uuid) })
}

// mountedInfo returns the FS_INFO of the filesystem mounted at mnt, with its
// current generation.
func mountedInfo(mnt string) (fsInfoArgs, error) {
	info := fsInfoArgs{Flags: fsInfoFlagGeneration}
	if err := fsIoctl(mnt, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		return info, fmt.Errorf("fs info of %s: %w", mnt, err)
	}
	if info.Flags&fsInfoFlagGeneration == 0 {
		return info, errors.New("the kernel does not report the filesystem generation")
	}
	return info, nil
}

// recordMount notes a pool LinuxIO just mounted: its generation, and when a
// degraded mount really lacks a disk, the disks that now take its writes. A
// mount also re-arms a resync that gave up, as the alert asks. A failure is
// logged, since the mount itself succeeded.
func recordMount(ctx context.Context, mnt string, degraded bool) {
	info, err := mountedInfo(mnt)
	if err == nil {
		uuid := formatUUID(info.FSID[:])
		var devices []BtrfsDevice
		if degraded {
			devices, _ = listDevices(mnt, info.MaxID, func(what string, err error) {
				slog.Warn("btrfs inventory", "uuid", uuid, "ioctl", what, "error", err)
			})
		}
		err = updatePoolStates(ctx, func(states map[string]poolState) {
			s := states[uuid]
			s.Generation = max(s.Generation, info.Generation)
			s.ResyncFailures, s.ResyncError = 0, ""
			if slices.ContainsFunc(devices, func(d BtrfsDevice) bool { return d.Missing }) {
				s.markDegraded(devices)
			}
			states[uuid] = s
		})
	}
	if err != nil {
		slog.Warn("failed to record btrfs pool state", "mountpoint", mnt, "error", err)
	}
}

// recordFilesystems notes the generation of every mounted filesystem, marks
// the degraded ones, saves the error counters and the members' hardware
// identities of each it sees whole outside an episode, and returns the
// states. Identities that cannot be read keep the ones saved before.
func recordFilesystems(ctx context.Context, filesystems []BtrfsFilesystem) (map[string]poolState, error) {
	disks, disksErr := wholeDisks()
	if disksErr != nil {
		slog.Warn("failed to read the identities of the disks", "error", disksErr)
	}
	var states map[string]poolState
	err := updatePoolStates(ctx, func(s map[string]poolState) {
		states = s
		for _, fs := range filesystems {
			info, err := mountedInfo(fs.Mountpoints[0].Mountpoint)
			if err != nil {
				slog.Warn("btrfs generation", "uuid", fs.UUID, "error", err)
				continue
			}
			state := s[fs.UUID]
			state.Generation = max(state.Generation, info.Generation)
			switch {
			case fs.Degraded:
				state.markDegraded(fs.Devices)
			case !state.Degraded && fs.HealthKnown:
				state.Counters = deviceCountersOf(fs.Devices)
				if disksErr == nil && state.Rebuild == nil {
					state.Identities = memberIdentities(fs.Devices, disks)
				}
			}
			s[fs.UUID] = state
		}
	})
	return states, err
}

type fstabMount struct {
	mountpoint string
	options    []string
}

// bootMounts returns the /etc/fstab entries that mount filesystem uuid at
// boot, in mountpoint order so a parent mounts before what sits inside it.
func bootMounts(uuid string) []fstabMount {
	data, err := os.ReadFile(FstabPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to read fstab", "path", FstabPath, "error", err)
		}
		return nil
	}
	var mounts []fstabMount
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 4 || strings.HasPrefix(fields[0], "#") || !strings.EqualFold(fields[0], "UUID="+uuid) {
			continue
		}
		// noauto is not mounted at boot; _netdev waits for the network,
		// which this check runs before.
		if options := strings.Split(fields[3], ","); !slices.Contains(options, "noauto") && !slices.Contains(options, "_netdev") {
			mounts = append(mounts, fstabMount{fields[1], options})
		}
	}
	slices.SortFunc(mounts, func(a, b fstabMount) int { return strings.Compare(a.mountpoint, b.mountpoint) })
	return mounts
}

// degradedMountBlocker says why pool must not be mounted degraded without a
// person deciding, or returns "" when its present disks hold its newest
// state. A disk that missed writes carries an older generation than the
// disks that got them. And the newest data is on a missing disk when the
// present disks are older than the newest generation LinuxIO saw the pool
// at, or when a disk that took the writes of its last degraded run is
// missing. Mounting such a set would write a second history that the other
// disks contradict.
func degradedMountBlocker(pool BtrfsOfflinePool, state poolState) string {
	if stale := staleDisks(pool); stale != "" {
		return stale
	}
	tookWrites := func(id uint64) bool { return !slices.Contains(pool.devids, id) }
	if slices.Max(pool.generations) < state.Generation || state.Degraded && slices.ContainsFunc(state.Present, tookWrites) {
		return "the present disks missed writes, so the newest data is on a missing disk"
	}
	return ""
}

// staleDisks names the present disks that missed writes, or returns "": a
// disk that missed writes carries an older generation than the disks that
// got them.
func staleDisks(pool BtrfsOfflinePool) string {
	newest := slices.Max(pool.generations)
	var stale []string
	for i, g := range pool.generations {
		if g < newest {
			name := pool.Devices[i]
			if pool.devids[i] == replaceTargetDevID {
				name = "the replace target " + name
			}
			stale = append(stale, name)
		}
	}
	if len(stale) == 0 {
		return ""
	}
	return strings.Join(stale, ", ") + " missed writes the other present disks have"
}

// replaceTargetDevID is the devid a device replace gives its target until it
// finishes (BTRFS_DEV_REPLACE_DEVID); the target is no member.
const replaceTargetDevID = 0

// missingDisks counts the members pool lacks, by devid: two devices that
// carry one devid are one disk, and a replace target is none.
func missingDisks(pool BtrfsOfflinePool) int {
	present := map[uint64]bool{}
	for _, id := range pool.devids {
		if id != replaceTargetDevID {
			present[id] = true
		}
	}
	return pool.NumDevices - len(present)
}

// lackedOnLeg says why pool, missing disks, is not mounted without one that
// shows on a leg (pool.legs), or returns "": the disk is there, under an md
// array or a multipath disk that has not started, as when mdadm's own
// last-resort timer has yet to start an array with a leg missing. Only a
// leg that shows the superblock is seen: one of an md 0.90 or 1.0 array or
// a multipath path, not a leg of an md 1.2 array or an LVM volume, whose
// data starts further in. A pool with no disk present is never mounted
// either.
func lackedOnLeg(pool BtrfsOfflinePool) string {
	for _, l := range pool.legs {
		if l.devid != replaceTargetDevID && !slices.Contains(pool.devids, l.devid) {
			return fmt.Sprintf("disk %d of the pool shows only on %s, which %s", l.devid, l.device, l.why)
		}
	}
	if len(pool.Devices) == 0 {
		return "no disk of the pool is present"
	}
	return ""
}

// refusal says why the boot check leaves pool, held back by udev, unmounted,
// or returns "": devices it cannot tell are the pool's disks (unclearDisks);
// with an unverified rebuild on record, why its disks are not erased
// (planUnverifiedErase), the pool being judged without those it erases;
// for a pool missing disks, one of them showing on a leg, or
// degradedMountBlocker's objection; for one held back only by a replace
// target, a disk that missed writes. The health run asks too, so the
// offline alert says the same either way. wayOut, for a refusal of the
// rebuild's erase, says what a person can do about it. The signature read here claims
// nothing; MountOfflinePool reads again, exclusively, before it registers.
func refusal(pool BtrfsOfflinePool, state poolState) (reason, wayOut string) {
	reason, err := unclearDisks(pool, peekSignature)
	if err != nil {
		return fmt.Sprintf("the signatures of its disks could not be read (%v)", err), ""
	}
	if reason != "" {
		return reason, ""
	}
	if state.Rebuild != nil {
		plan, err := planUnverifiedErase(pool, state)
		if err != nil {
			return fmt.Sprintf("the disks of its unverified rebuild could not be checked (%v)", err), ""
		}
		if plan.reason != "" {
			return plan.reason, plan.wayOut
		}
		pool = plan.remaining
	}
	switch {
	case missingDisks(pool) > 0:
		if reason := lackedOnLeg(pool); reason != "" {
			return reason, ""
		}
		return degradedMountBlocker(pool, state), ""
	case slices.Contains(pool.devids, replaceTargetDevID):
		return staleDisks(pool), ""
	}
	return "", ""
}

type fstabPool struct {
	pool   BtrfsOfflinePool
	mounts []fstabMount
}

// heldFstabPools returns the unmounted pools that /etc/fstab mounts at boot
// and that udev holds back, with their fstab entries: those missing disks,
// and those with a replace target.
func heldFstabPools(ctx context.Context) ([]fstabPool, error) {
	pools, err := FindOfflinePools(ctx, BlockDevices())
	if err != nil {
		return nil, err
	}
	var held []fstabPool
	for _, p := range pools {
		if mounts := bootMounts(p.UUID); len(mounts) > 0 && (missingDisks(p) > 0 || slices.Contains(p.devids, replaceTargetDevID)) {
			held = append(held, fstabPool{p, mounts})
		}
	}
	return held, nil
}

// MountDegradedPools is the boot-time last resort for the pools /etc/fstab
// mounts that udev holds back. It waits up to grace for the disks, then
// mounts each such pool at each of its fstab mountpoints with that entry's
// options: degraded when it is missing disks, unless degradedMountBlocker
// objects, and as it is when only a replace target holds it back, unless a
// disk missed writes. A pool it objects to stays unmounted and its offline
// alert says why. The kernel itself refuses a pool missing more disks than
// its profiles survive. First, and again for disks that came while it
// waited, a complete pool whose device systemd did not find gets its members
// evaluated again by udev (see bringUpUnready).
func MountDegradedPools(ctx context.Context, grace time.Duration) error {
	recordDegradedMounts(ctx)
	deadline := time.Now().Add(grace)
	settle := min(grace, udevSettle)
	err := bringUpUnready(ctx, settle)
	if ctx.Err() != nil {
		// Stopped while it waited: LinuxIO mounts nothing degraded after
		// that, though systemd may still finish a mount the bring-up
		// started.
		if err == nil {
			err = ctx.Err()
		}
		return err
	}
	waited, heldErr := mountHeldAfter(ctx, deadline)
	err = errors.Join(err, heldErr)
	if waited && ctx.Err() == nil {
		err = errors.Join(err, bringUpUnready(ctx, settle))
	}
	return err
}

// mountHeldAfter waits until deadline for the disks of the pools udev holds
// back, then mounts those still held. It reports whether it found any.
func mountHeldAfter(ctx context.Context, deadline time.Time) (bool, error) {
	waited := false
	for {
		pools, err := heldFstabPools(ctx)
		if err != nil {
			return waited, err
		}
		if len(pools) == 0 {
			return waited, nil
		}
		if !time.Now().Before(deadline) {
			return true, mountHeld(ctx, pools)
		}
		waited = true
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(degradedPoll):
		}
	}
}

// recordDegradedMounts marks the pools already mounted with a disk missing,
// such as a root the initramfs mounted with rootflags=degraded, at boot
// rather than at the first health run two minutes later: a power cut in
// between would leave a resync run whose offsets writes made since may lie
// below. A failure is logged; the health run records them too.
func recordDegradedMounts(ctx context.Context) {
	filesystems, err := ListFilesystems(ctx)
	if err != nil {
		slog.Warn("failed to list mounted btrfs filesystems", "error", err)
		return
	}
	degraded := slices.DeleteFunc(filesystems, func(fs BtrfsFilesystem) bool { return !fs.Degraded })
	if len(degraded) == 0 {
		return
	}
	if _, err := recordFilesystems(ctx, degraded); err != nil {
		slog.Warn("failed to record btrfs pool state", "path", PoolStatePath, "error", err)
	}
}

func mountHeld(ctx context.Context, pools []fstabPool) error {
	states, err := loadPoolStates()
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range pools {
		pool, ok, err := heldPoolToMount(ctx, p.pool, states[p.pool.UUID])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok {
			continue
		}
		p.pool = pool
		// A pool missing a disk mounts degraded even mid-replace: without
		// degraded the kernel refuses a replace whose source is missing
		// (dev-replace.c btrfs_init_dev_replace).
		missing := missingDisks(p.pool)
		var failed []string
		open := false
		for _, m := range p.mounts {
			// Mounting inside a mountpoint that failed would land on the
			// filesystem underneath and be hidden later.
			if slices.ContainsFunc(failed, func(f string) bool { return strings.HasPrefix(m.mountpoint, f+"/") }) {
				continue
			}
			if err := mountLine(ctx, p.pool, m, missing > 0, open); err != nil {
				failed = append(failed, m.mountpoint)
				errs = append(errs, fmt.Errorf("mount btrfs pool %s at %s (degraded: %t): %w", p.pool.UUID, m.mountpoint, missing > 0, err))
				continue
			}
			open = true
			if missing > 0 {
				slog.Info("btrfs pool mounted degraded", "uuid", p.pool.UUID, "mountpoint", m.mountpoint, "missing", missing)
			} else {
				slog.Info("btrfs pool mounted to continue a device replace", "uuid", p.pool.UUID, "mountpoint", m.mountpoint)
			}
		}
		// Say so now, not at the first health run: delivery and the bell
		// pick it up at once.
		if fs, err := FindFilesystem(ctx, p.pool.UUID); err == nil {
			RaiseAlerts(ctx, fs)
		}
	}
	return errors.Join(errs...)
}

// heldPoolToMount returns pool as the boot check mounts it, or false when it
// leaves the pool unmounted and raises its offline alert with the reason
// (refusal). Once the pool is judged mountable, the disks of an unverified
// automatic rebuild go first, as planUnverifiedErase decides: mounted with
// its target, the kernel would continue that replace past what a failed
// target missed.
func heldPoolToMount(ctx context.Context, pool BtrfsOfflinePool, state poolState) (BtrfsOfflinePool, bool, error) {
	// Judged before anything is erased: refusal plans the erase and judges
	// the pool without the disks it would erase.
	reason, wayOut := refusal(pool, state)
	if reason == "" {
		erased, plan, err := eraseUnverifiedTarget(ctx, pool)
		if err != nil {
			return pool, false, err
		}
		if reason, wayOut = plan.reason, plan.wayOut; reason == "" {
			return erased, true, nil
		}
	}
	slog.Warn("btrfs pool left unmounted", "uuid", pool.UUID, "reason", reason)
	raiseOfflineAlert(ctx, pool, reason, wayOut)
	return pool, false, nil
}

// mountLine mounts pool as fstab line m says. The first line registers the
// pool's disks (MountOfflinePool); once one is mounted, open, the pool holds
// its disks, and a further line mounts what is open.
func mountLine(ctx context.Context, pool BtrfsOfflinePool, m fstabMount, degraded, open bool) error {
	if open {
		return MountPool(ctx, pool.Devices[0], m.mountpoint, m.options, degraded)
	}
	return MountOfflinePool(ctx, pool, m.mountpoint, m.options, degraded)
}

// markUnopened marks Missing a device the kernel found again after the pool
// was mounted without it: the kernel lists its path but never opens it, so
// it gets no writes until the pool is mounted again. Its writeable flag is
// clear while the other devices' are set; a read-only pool has none set and
// is left alone.
func markUnopened(uuid string, devices []BtrfsDevice) {
	// Since 7.1 the kernel also marks a member missing when its block device
	// goes away (btrfs_remove_bdev); DEV_INFO then names it "<missing disk>".
	for i, d := range devices {
		if b, err := os.ReadFile(filepath.Join(sysFSBtrfs, uuid, "devinfo", strconv.Itoa(d.ID), "missing")); err == nil && strings.TrimSpace(string(b)) == "1" {
			devices[i].Missing = true
		}
	}
	writeable := map[int]bool{}
	anyWriteable := false
	for _, d := range devices {
		if d.Missing {
			continue
		}
		// An older kernel has no devinfo: that device stays as listed.
		if b, err := os.ReadFile(filepath.Join(sysFSBtrfs, uuid, "devinfo", strconv.Itoa(d.ID), "writeable")); err == nil {
			writeable[d.ID] = strings.TrimSpace(string(b)) == "1"
			anyWriteable = anyWriteable || writeable[d.ID]
		}
	}
	if !anyWriteable {
		return
	}
	for i, d := range devices {
		if w, known := writeable[d.ID]; known && !w && !d.Missing {
			devices[i].Missing = true
		}
	}
}

// convertTargets returns, for data and for metadata (system chunks follow
// metadata), the most redundant profile present when chunks of another
// profile sit beside it, as chunks allocated while too few disks were
// present do; nil when a kind has one profile.
func convertTargets(allocation []BtrfsAllocation) (data, meta *string) {
	present := map[string][]string{}
	for _, a := range allocation {
		kind := a.Kind
		if kind == "system" {
			kind = "metadata"
		}
		if !slices.Contains(present[kind], a.Profile) {
			present[kind] = append(present[kind], a.Profile)
		}
	}
	target := func(kind string) *string {
		if len(present[kind]) < 2 {
			return nil
		}
		best := slices.MaxFunc(present[kind], func(a, b string) int { return profiles[a].rank - profiles[b].rank })
		return &best
	}
	return target("data"), target("metadata")
}

// Resync makes a pool that ran with a disk missing whole again once all its
// disks are back. The returned disk missed every write made without it, and
// btrfs neither copies them over nor stops reading it: stale copies are
// only fixed when a read happens to hit them. A scrub checks every copy and
// rewrites the stale ones from good ones; a soft convert then moves chunks
// written without redundancy to the pool's most redundant profile. The
// degraded mark is cleared once both are done, and the error counters the
// scrub raised on the returned disks are reset (resetReturnedCounters); a
// scrub that found uncorrectable errors is reported by the scrub alert and
// not run again, and one cut short is continued by the episode's next
// resync. A failure is recorded so the health run gives up after
// maxResyncAttempts. report, when not nil, is given the scrub's percentage
// as it advances. A failure the pool's alerts report, refreshed here, is
// marked scheduledrun.Reported; one that left them as they were is not.
func Resync(ctx context.Context, uuid string, report func(percent float64)) error {
	out, err := resync(ctx, uuid, report)
	// A disk that drops out starts a new episode, and an unverified rebuild
	// waits for its own end, neither a failed resync.
	if err != nil && ctx.Err() == nil && !errors.Is(err, errDiskMissing) && !errors.Is(err, errRebuildPending) {
		if recErr := updatePoolStates(ctx, func(s map[string]poolState) {
			state := s[uuid]
			state.ResyncFailures++
			state.ResyncError = err.Error()
			s[uuid] = state
		}); recErr != nil {
			return errors.Join(err, recErr)
		}
	}
	if refreshResyncAlerts(ctx, uuid, err == nil, out) {
		return scheduledrun.Reported(err)
	}
	return err
}

// refreshResyncAlerts updates the pool's alerts when a resync ends rather
// than at the next health run, and on success says the pool is whole again,
// with what became of the returned disks' error counters, unless the
// notification settings report resync failures only. It reports whether the
// pool's health was known and its alerts were brought up to date.
func refreshResyncAlerts(ctx context.Context, uuid string, resynced bool, out resyncOutcome) bool {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		slog.Warn("failed to refresh btrfs alerts after resync", "uuid", uuid, "error", err)
		return false
	}
	states, err := loadPoolStates()
	if err != nil {
		slog.Warn("failed to refresh btrfs alerts after resync", "uuid", uuid, "error", err)
		return false
	}
	RaiseAlerts(ctx, fs)
	raiseResyncAlert(ctx, fs, states[uuid])
	if resynced && alerts.NotificationsOrDefaults().ReportFor(alerts.KindResync) == alerts.ReportChanges {
		raise(ctx, resyncedObservation(fs, out))
	}
	if resynced && fs.HealthKnown && !fs.Degraded {
		// The counters as the episode ends, kept or reset, are the baseline
		// of a disk that drops out before the next health run.
		if err := updatePoolStates(ctx, func(s map[string]poolState) {
			if state := s[uuid]; !state.Degraded {
				state.Counters = deviceCountersOf(fs.Devices)
				s[uuid] = state
			}
		}); err != nil {
			slog.Warn("failed to record btrfs pool state", "uuid", uuid, "error", err)
		}
	}
	return fs.HealthKnown
}

// resyncConvert gives the chunks written without redundancy their second
// copy with a soft convert to data and meta. A power cut that cut this
// convert leaves the balance item, which the kernel continues at the next
// read-write mount while it refuses a new balance (EINPROGRESS): the resync
// recognises its own convert by the arguments the kernel reports and
// follows it to the end. It takes over no other balance, an admin's or a
// job's, and resumes no paused one: an admin paused it, or mounted with
// skip_balance, the recovery option for a balance that fails at mount.
//
// The kernel relocates chunks from the highest address down, whatever their
// kind (volumes.c __btrfs_balance), so a convert of data and metadata cut
// midway can leave one kind done while the kernel continues both: the
// resync records the convert it starts in the pool state and compares the
// kernel's balance with that record, or, without one (a new episode cleared
// it while the kernel's balance survived into the next mount), with what is
// left to convert now.
func resyncConvert(ctx context.Context, uuid string, data, meta *string) error {
	opts := BalanceOptions{DataProfile: data, MetadataProfile: meta, Soft: true}
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	states, err := loadPoolStates()
	if err != nil {
		return err
	}
	want := opts
	if state := states[uuid]; state.ConvertData != "" || state.ConvertMetadata != "" {
		want = BalanceOptions{DataProfile: profileOrNil(state.ConvertData), MetadataProfile: profileOrNil(state.ConvertMetadata), Soft: true}
	}
	running, err := balanceStatusOf(mnt)
	switch {
	case errors.Is(err, unix.ENOTCONN):
		if recErr := recordConvert(ctx, uuid, data, meta); recErr != nil {
			return recErr
		}
		return Balance(ctx, uuid, opts, func(float64) {})
	case err != nil:
		return err
	case running.State&balanceStateRunning == 0:
		return fmt.Errorf("a paused balance waits on pool %s; resume it (btrfs balance resume) or cancel it, and the next resync converts", uuid)
	case !sameBalance(running, balanceArgsOf(want)):
		return fmt.Errorf("a balance is already running on pool %s, and it is not the resync's convert; the next resync converts once it ends", uuid)
	}
	slog.Debug("btrfs resync follows its convert the kernel continued", "uuid", uuid)
	return followBalance(ctx, mnt)
}

// recordConvert notes the convert the resync is about to start, before the
// kernel takes it, so a later attempt knows it as its own.
func recordConvert(ctx context.Context, uuid string, data, meta *string) error {
	return updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		state.ConvertData, state.ConvertMetadata = "", ""
		if data != nil {
			state.ConvertData = *data
		}
		if meta != nil {
			state.ConvertMetadata = *meta
		}
		s[uuid] = state
	})
}

// profileOrNil is profile, or nil for "".
func profileOrNil(profile string) *string {
	if profile == "" {
		return nil
	}
	return &profile
}

// sameBalance reports whether the kernel's balance got was started with
// want's kinds and filters. The kernel stores them in the balance item as
// given and adds BTRFS_BALANCE_RESUME when it continues the balance
// (volumes.c btrfs_recover_balance).
func sameBalance(got, want balanceArgs) bool {
	const kinds = balanceData | balanceMetadata | balanceSystem
	if got.Flags&kinds != want.Flags&kinds {
		return false
	}
	for _, f := range []struct {
		kind      uint64
		got, want balanceFilter
	}{{balanceData, got.Data, want.Data}, {balanceMetadata, got.Meta, want.Meta}, {balanceSystem, got.Sys, want.Sys}} {
		if want.Flags&f.kind != 0 && f.got != f.want {
			return false
		}
	}
	return true
}

// followBalance waits for the balance the kernel runs on mnt to end. When
// ctx ends, at a shutdown or a stop of the resync's unit, it stops watching
// and leaves the balance running: the kernel pauses it at unmount and
// continues it at the next mount, where a cancel would delete it.
func followBalance(ctx context.Context, mnt string) error {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, _, err := balanceProgressOf(mnt)
			switch {
			case errors.Is(err, unix.ENOTCONN):
				return nil
			case err != nil:
				return err
			case state&balanceStateRunning == 0:
				return fmt.Errorf("the convert the resync followed was paused: %w", errBalancePaused)
			}
		}
	}
}

// checkConverted fails while the pool still has chunks to convert by the
// rule that started the convert (convertTargets), which counts system
// chunks with metadata.
func checkConverted(ctx context.Context, uuid string) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if !fs.HealthKnown || len(fs.Allocation) == 0 {
		return fmt.Errorf("could not read the allocation of pool %s", uuid)
	}
	data, meta := convertTargets(fs.Allocation)
	var left []string
	if data != nil {
		left = append(left, "data to "+*data)
	}
	if meta != nil {
		left = append(left, "metadata and system to "+*meta)
	}
	if len(left) > 0 {
		return fmt.Errorf("pool %s still has chunks to convert (%s)", uuid, strings.Join(left, ", "))
	}
	return nil
}

// errDiskMissing stops a resync while the pool lacks a disk.
var errDiskMissing = errors.New("a disk is missing")

// errRebuildPending stops a resync while an automatic rebuild of the pool is
// on record and not verified: the disk it wrote may hold stale copies.
var errRebuildPending = errors.New("an automatic rebuild is not verified")

func resync(ctx context.Context, uuid string, report func(percent float64)) (resyncOutcome, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return resyncOutcome{}, err
	}
	if fs.Degraded {
		return resyncOutcome{}, fmt.Errorf("pool %s: %w; mount it again once every disk is back", uuid, errDiskMissing)
	}
	// Without the layout the chunks left without redundancy go unseen.
	if !fs.HealthKnown || len(fs.Allocation) == 0 {
		return resyncOutcome{}, fmt.Errorf("could not read the devices and allocation of pool %s", uuid)
	}
	states, err := loadPoolStates()
	if err != nil {
		return resyncOutcome{}, err
	}
	if rec := states[uuid].Rebuild; rec != nil {
		return resyncOutcome{}, fmt.Errorf("pool %s: the automatic rebuild of device %d is not verified: %w", uuid, rec.ID, errRebuildPending)
	}
	// A scrub counts only with the run that proves it: one noted with no run
	// has no record behind it, so the resync scrubs.
	scrubbed := states[uuid].Scrubbed && states[uuid].ResyncRun != ""
	run, err := resyncRunOf(ctx, uuid)
	if err != nil {
		return resyncOutcome{}, err
	}
	if !scrubbed {
		if err := resyncScrub(ctx, uuid, run, report); err != nil {
			return resyncOutcome{}, err
		}
	}
	data, meta := convertTargets(fs.Allocation)
	if data != nil || meta != nil {
		if err := resyncConvert(ctx, uuid, data, meta); err != nil {
			return resyncOutcome{}, err
		}
	}
	// It refuses a pool that went degraded meanwhile as a new episode, before
	// new single chunks could read as chunks left to convert.
	if err := confirmScrubbed(ctx, uuid, run); err != nil {
		return resyncOutcome{}, err
	}
	// By the rule that started the convert, system chunks with metadata.
	if err := checkConverted(ctx, uuid); err != nil {
		return resyncOutcome{}, err
	}
	out := resetReturnedCounters(ctx, uuid, run)
	if err := clearDegraded(ctx, uuid); err != nil {
		return resyncOutcome{}, err
	}
	slog.Info("btrfs pool resynced", "uuid", uuid, "converted", data != nil || meta != nil)
	return out, nil
}

// resyncScrub scrubs the whole pool as the episode's resync run and notes
// that it did, so a retry after a failed convert skips it. The scrub record
// carries the run, so after a power cut, a crash, a kill, a shutdown or Stop
// check the next resync continues that scrub instead of starting over, and
// counts it when it had finished; see continueScrub. The scrub counts only
// while the run is still the episode's: a health run that saw the pool
// degraded meanwhile began a new episode, which it does not cover.
func resyncScrub(ctx context.Context, uuid, run string, report func(percent float64)) error {
	status, err := continueScrub(ctx, uuid, run, func(s BtrfsScrubStatus) {
		if report != nil {
			report(s.Percent)
		}
	})
	if err != nil {
		return err
	}
	// A scrub canceled from the pool page checked only part of the
	// pool; the next resync continues it.
	if status.Status != "finished" {
		return fmt.Errorf("the scrub of pool %s was %s before it checked every copy", uuid, status.Status)
	}
	slog.Debug("btrfs resync scrub finished", "uuid", uuid, "run", run, "corrected", status.CorrectedErrors, "uncorrectable", status.UncorrectableErrors)
	if err := checkScrubbedWrites(ctx, uuid, run); err != nil {
		return err
	}
	current := false
	if err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		if current = state.ResyncRun == run; current {
			state.Scrubbed = true
			s[uuid] = state
		}
	}); err != nil {
		return err
	}
	if !current {
		return fmt.Errorf("pool %s lost a disk again during its resync scrub: %w", uuid, errDiskMissing)
	}
	return nil
}

// checkScrubbedWrites fails when a present device failed writes or flushes
// since its scan in run began. While a disk stays attached the kernel does
// not mark it missing (it does so at mount, and since 7.1 when its block
// device goes away, btrfs_remove_bdev, which LinuxIO reads on an older
// kernel too, see markDroppedOut): a disk whose writes
// fail only raises its error counters, writes made meanwhile miss it, and a
// repair whose write fails
// still counts as corrected (scrub.c at 6.8 and 7.3: scrub_write_endio only
// raises BTRFS_DEV_STAT_WRITE_ERRS, and scrub_stripe_report_errors counts a
// sector repaired once a read cleared it). The attempt fails, so the pool is not
// declared whole, and the next resync scrubs again, since a finished scrub
// does not count once a device's counters changed.
//
// It refuses what it cannot read: counters it could not read would read as
// 0, and a device it could not list would not be compared. A disk gone
// missing, one whose block device went away included, starts a new
// episode, as errDiskMissing. A scrub that no longer covers
// the pool, or a record no longer this run's, fails with errScrubStale.
func checkScrubbedWrites(ctx context.Context, uuid, run string) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if !fs.HealthKnown {
		return fmt.Errorf("could not read the devices of pool %s to check their write errors", uuid)
	}
	if fs.Degraded {
		return fmt.Errorf("pool %s: %w", uuid, errDiskMissing)
	}
	rec, ok := loadScrubRecord(uuid)
	if !ok || rec.Run != run {
		return fmt.Errorf("the scrub record of pool %s is no longer this resync's: %w", uuid, errScrubStale)
	}
	if stale := rec.staleFor(fs.Devices); stale != "" {
		return fmt.Errorf("pool %s: %s; the next resync scrubs it again: %w", uuid, stale, errScrubStale)
	}
	return nil
}

// confirmScrubbed checks the scrub of run again right before the pool is
// declared whole: the convert wrote second copies onto the returned disk, a
// balance can take long, and an attempt that skipped the scrub has not
// looked since. A scrub that no longer covers the pool is withdrawn, so the
// next resync scrubs again.
func confirmScrubbed(ctx context.Context, uuid, run string) error {
	err := checkScrubbedWrites(ctx, uuid, run)
	if errors.Is(err, errScrubStale) {
		err = errors.Join(err, unscrub(ctx, uuid, run))
	}
	return err
}

// errScrubStale ends a resync whose scrub no longer counts.
var errScrubStale = errors.New("the resync scrub no longer covers the pool")

// unscrub withdraws the pool's scrub of run, so the next resync scrubs again.
func unscrub(ctx context.Context, uuid, run string) error {
	return updatePoolStates(ctx, func(s map[string]poolState) {
		if state := s[uuid]; state.ResyncRun == run && state.Scrubbed {
			state.Scrubbed = false
			s[uuid] = state
		}
	})
}

// resyncRunOf returns the run of the pool's resync scrub in this episode,
// set by the first resync that scrubs in it.
func resyncRunOf(ctx context.Context, uuid string) (string, error) {
	var run string
	err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		if state.ResyncRun == "" {
			state.ResyncRun = "resync-" + rand.Text() // 128 random bits or more
			s[uuid] = state
		}
		run = state.ResyncRun
	})
	return run, err
}

// clearDegraded ends the pool's degraded episode, unless a health run saw a
// disk drop out during the resync and reset Scrubbed: that new episode
// needs its own resync.
func clearDegraded(ctx context.Context, uuid string) error {
	cleared := false
	if err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		if cleared = state.Scrubbed; cleared {
			state.Degraded, state.Scrubbed, state.Present, state.Missing, state.ResyncRun = false, false, nil, nil, ""
			state.Since, state.ConvertData, state.ConvertMetadata = 0, "", ""
			state.ResyncFailures, state.ResyncError = 0, ""
			state.Refused, state.RebuildRefusal, state.RebuildAdvice, state.Returned = nil, "", "", nil
			s[uuid] = state
		}
	}); err != nil {
		return err
	}
	if !cleared {
		return fmt.Errorf("pool %s lost a disk again during its resync: %w", uuid, errDiskMissing)
	}
	return nil
}
