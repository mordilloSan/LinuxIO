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
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// A pool that loses a disk keeps running, but after a reboot systemd never
// mounts it: udev holds its devices until all are present (64-btrfs.rules),
// and upstream leaves the "n of m disks" policy to storage userspace. This
// file is that policy, after mdadm's last-resort timer: wait a while for the
// disks, then mount degraded, but only when the disks present hold the
// pool's newest state. Once every disk is back, Resync repairs what the
// returned disk missed, which btrfs leaves to a scrub.

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
	// Scrubbed is set once Resync's scrub finished, so a retry after a
	// failed convert does not scrub the whole pool again.
	Scrubbed bool `json:"scrubbed"`
	// ResyncFailures counts failed resyncs since the pool was last mounted
	// or degraded; at maxResyncAttempts the health run stops starting them.
	ResyncFailures int    `json:"resyncFailures,omitempty"`
	ResyncError    string `json:"resyncError,omitempty"`
}

// maxResyncAttempts bounds the resyncs the health run starts for one
// episode, so a cause that needs a person does not scrub every ten minutes.
const maxResyncAttempts = 3

// markDegraded notes that only the present devices take the writes.
func (s *poolState) markDegraded(devices []BtrfsDevice) {
	s.Degraded, s.Scrubbed, s.Present = true, false, nil
	s.ResyncFailures, s.ResyncError = 0, ""
	for _, d := range devices {
		if !d.Missing {
			s.Present = append(s.Present, uint64(d.ID))
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

// recordFilesystems notes the generation of every mounted filesystem and
// marks the degraded ones, and returns the states.
func recordFilesystems(ctx context.Context, filesystems []BtrfsFilesystem) (map[string]poolState, error) {
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
			if fs.Degraded {
				state.markDegraded(fs.Devices)
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
	newest := slices.Max(pool.generations)
	var stale []string
	for i, g := range pool.generations {
		if g < newest {
			stale = append(stale, pool.Devices[i])
		}
	}
	if len(stale) > 0 {
		return strings.Join(stale, ", ") + " missed writes the other present disks have"
	}
	tookWrites := func(id uint64) bool { return !slices.Contains(pool.devids, id) }
	if newest < state.Generation || state.Degraded && slices.ContainsFunc(state.Present, tookWrites) {
		return "the present disks missed writes, so the newest data is on a missing disk"
	}
	return ""
}

type fstabPool struct {
	pool   BtrfsOfflinePool
	mounts []fstabMount
}

// incompleteFstabPools returns the unmounted pools that /etc/fstab mounts at
// boot and that are missing disks, with their fstab entries.
func incompleteFstabPools(ctx context.Context) ([]fstabPool, error) {
	pools, err := FindOfflinePools(ctx, BlockDevices())
	if err != nil {
		return nil, err
	}
	var incomplete []fstabPool
	for _, p := range pools {
		if mounts := bootMounts(p.UUID); len(mounts) > 0 && len(p.Devices) < p.NumDevices {
			incomplete = append(incomplete, fstabPool{p, mounts})
		}
	}
	return incomplete, nil
}

// MountDegradedPools is the boot-time last resort for the pools /etc/fstab
// mounts that are missing disks. It waits up to grace for the disks, then
// mounts each such pool degraded at each of its fstab mountpoints with that
// entry's options, unless degradedMountBlocker objects; that pool stays
// unmounted and its offline alert says why. The kernel itself refuses a pool
// missing more disks than its profiles survive.
func MountDegradedPools(ctx context.Context, grace time.Duration) error {
	deadline := time.Now().Add(grace)
	for {
		pools, err := incompleteFstabPools(ctx)
		if err != nil {
			return err
		}
		if len(pools) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return mountDegraded(ctx, pools)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(degradedPoll):
		}
	}
}

func mountDegraded(ctx context.Context, pools []fstabPool) error {
	states, err := loadPoolStates()
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range pools {
		if reason := degradedMountBlocker(p.pool, states[p.pool.UUID]); reason != "" {
			slog.Warn("btrfs pool left unmounted", "uuid", p.pool.UUID, "reason", reason)
			RaiseOfflineAlert(ctx, p.pool, reason)
			continue
		}
		var failed []string
		for _, m := range p.mounts {
			// Mounting inside a mountpoint that failed would land on the
			// filesystem underneath and be hidden later.
			if slices.ContainsFunc(failed, func(f string) bool { return strings.HasPrefix(m.mountpoint, f+"/") }) {
				continue
			}
			if err := MountOfflinePool(ctx, p.pool, m.mountpoint, m.options, true); err != nil {
				failed = append(failed, m.mountpoint)
				errs = append(errs, fmt.Errorf("mount btrfs pool %s degraded at %s: %w", p.pool.UUID, m.mountpoint, err))
				continue
			}
			slog.Info("btrfs pool mounted degraded", "uuid", p.pool.UUID, "mountpoint", m.mountpoint, "missing", p.pool.NumDevices-len(p.pool.Devices))
		}
		// Say so now, not at the first health run: delivery and the bell
		// pick it up at once.
		if fs, err := FindFilesystem(ctx, p.pool.UUID); err == nil {
			RaiseAlerts(ctx, fs)
		}
	}
	return errors.Join(errs...)
}

// markUnopened marks Missing a device the kernel found again after the pool
// was mounted without it: the kernel lists its path but never opens it, so
// it gets no writes until the pool is mounted again. Its writeable flag is
// clear while the other devices' are set; a read-only pool has none set and
// is left alone.
func markUnopened(uuid string, devices []BtrfsDevice) {
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

// profileRank orders profiles by the disk failures they survive, then by
// copies kept.
var profileRank = map[string]int{
	"single": 0, "raid0": 0, "dup": 1, "raid1": 2, "raid10": 2, "raid5": 2,
	"raid1c3": 3, "raid6": 3, "raid1c4": 4,
}

// convertTargets returns, for data and for metadata (system chunks follow
// metadata), the most redundant profile present when chunks of another
// profile sit beside it, as chunks allocated while too few disks were
// present do; nil when a kind has one profile.
func convertTargets(allocation []BtrfsAllocation) (data, meta *string) {
	profiles := map[string][]string{}
	for _, a := range allocation {
		kind := a.Kind
		if kind == "system" {
			kind = "metadata"
		}
		if !slices.Contains(profiles[kind], a.Profile) {
			profiles[kind] = append(profiles[kind], a.Profile)
		}
	}
	target := func(kind string) *string {
		if len(profiles[kind]) < 2 {
			return nil
		}
		best := slices.MaxFunc(profiles[kind], func(a, b string) int { return profileRank[a] - profileRank[b] })
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
// degraded mark is cleared once both are done; a scrub that found
// uncorrectable errors is reported by the scrub alert and not run again. A
// failure is recorded so the health run gives up after maxResyncAttempts.
func Resync(ctx context.Context, uuid string) error {
	err := resync(ctx, uuid)
	// A disk that drops out starts a new episode, not a failed resync.
	if err != nil && ctx.Err() == nil && !errors.Is(err, errDiskMissing) {
		if recErr := updatePoolStates(ctx, func(s map[string]poolState) {
			state := s[uuid]
			state.ResyncFailures++
			state.ResyncError = err.Error()
			s[uuid] = state
		}); recErr != nil {
			return errors.Join(err, recErr)
		}
	}
	refreshResyncAlerts(ctx, uuid, err == nil)
	return err
}

// refreshResyncAlerts updates the pool's alerts when a resync ends rather
// than at the next health run, and on success says the pool is whole again.
func refreshResyncAlerts(ctx context.Context, uuid string, resynced bool) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		slog.Warn("failed to refresh btrfs alerts after resync", "uuid", uuid, "error", err)
		return
	}
	states, err := loadPoolStates()
	if err != nil {
		slog.Warn("failed to refresh btrfs alerts after resync", "uuid", uuid, "error", err)
		return
	}
	RaiseAlerts(ctx, fs)
	raiseResyncAlert(ctx, fs, states[uuid])
	if resynced {
		raise(ctx, alerts.Observation{
			Key: "resynced-" + uuid, Severity: alerts.SeverityInfo,
			Title:   "Pool " + poolName(fs.Label, uuid) + " is whole again",
			Message: fmt.Sprintf("The returned disk was resynced: the scrub corrected %d stale or damaged copies, and data written while it was missing has its redundancy back.", fs.Scrub.CorrectedErrors),
			Link:    "/storage?pool=" + uuid, OccurrenceID: strconv.FormatInt(time.Now().Unix(), 10),
		})
	}
}

// errDiskMissing stops a resync while the pool lacks a disk.
var errDiskMissing = errors.New("a disk is missing")

func resync(ctx context.Context, uuid string) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if fs.Degraded {
		return fmt.Errorf("pool %s: %w; mount it again once every disk is back", uuid, errDiskMissing)
	}
	// Without the layout the chunks left without redundancy go unseen.
	if !fs.HealthKnown || len(fs.Allocation) == 0 {
		return fmt.Errorf("could not read the devices and allocation of pool %s", uuid)
	}
	states, err := loadPoolStates()
	if err != nil {
		return err
	}
	if !states[uuid].Scrubbed {
		if err := resyncScrub(ctx, uuid); err != nil {
			return err
		}
	}
	data, meta := convertTargets(fs.Allocation)
	if data != nil || meta != nil {
		if err := Balance(ctx, uuid, BalanceOptions{DataProfile: data, MetadataProfile: meta, Soft: true}, func(float64) {}); err != nil {
			return err
		}
		if _, err := BalanceResult(ctx, uuid, data, meta); err != nil {
			return err
		}
	}
	if err := clearDegraded(ctx, uuid); err != nil {
		return err
	}
	slog.Info("btrfs pool resynced", "uuid", uuid, "converted", data != nil || meta != nil)
	return nil
}

// resyncScrub scrubs the whole pool and notes that it did, so a retry after
// a failed convert skips it.
func resyncScrub(ctx context.Context, uuid string) error {
	status, err := Scrub(ctx, uuid, func(BtrfsScrubStatus) {})
	if err != nil {
		return err
	}
	// A scrub canceled from the Storage overview checked only part of the
	// pool.
	if status.Status != "finished" {
		return fmt.Errorf("the scrub of pool %s was %s before it checked every copy", uuid, status.Status)
	}
	slog.Debug("btrfs resync scrub finished", "uuid", uuid, "corrected", status.CorrectedErrors, "uncorrectable", status.UncorrectableErrors)
	return updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		state.Scrubbed = true
		s[uuid] = state
	})
}

// clearDegraded ends the pool's degraded episode, unless a health run saw a
// disk drop out during the resync and reset Scrubbed: that new episode
// needs its own resync.
func clearDegraded(ctx context.Context, uuid string) error {
	cleared := false
	if err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		if cleared = state.Scrubbed; cleared {
			state.Degraded, state.Scrubbed, state.Present = false, false, nil
			state.ResyncFailures, state.ResyncError = 0, ""
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
