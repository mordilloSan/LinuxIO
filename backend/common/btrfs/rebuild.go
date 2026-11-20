package btrfs

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// The automatic rebuild: when a member of a mounted pool is missing, LinuxIO
// starts a device replace of its devid by itself, without unmounting, onto
// the member's own disk when it came back (pulled and plugged in again), or
// onto the pool's hot spare when it did not. The health run decides
// (checkRebuilds) and starts linuxio-btrfs-rebuild@<uuid>.service, which runs
// Rebuild; the pool's resync then scrubs and converts as after any episode.
//
// The returned disk carries the pool's superblock, so the mounted pool
// refuses it (volumes.c device_list_add: EEXIST while the member holds its
// dead block device), and every other operation refuses it too (deviceInUse
// in the bridge). Rebuild alone takes it: it erases the disk's superblocks
// (WipePoolSuperblocks) and replaces the member's devid onto it, after
// checking that the disk is that member and nothing else: its own
// superblock names the pool, a missing member's devid and that member's
// device UUID at a generation no newer than the pool's, and its hardware
// identity is the one LinuxIO recorded for that devid while the pool was
// whole. A clone of the member's disk is identical to it in every
// superblock field, and a disk replaced earlier whose superblock was never
// erased claims the same devid and device UUID; only the hardware identity
// tells them apart. Two health runs in a row must see the same disk back
// before it is erased, and a disk rebuilt onto that drops out again within a
// day is not fed back in: on kernels before 7.1 a target that dies during the
// replace forces a two-disk pool read-only.
//
// The kernel's success is not trusted (verifyReplaced): a target that dies
// mid-replace is installed dead. So a record of the rebuild is written
// before the erase (poolState.Rebuild) and cleared only once the new disk is
// verified. A mount LinuxIO makes while it is set erases the recorded
// target first, since the kernel would continue the replace from a cursor a
// failed target left past every extent, and cancels what the kernel then
// suspends (eraseUnverifiedTarget, settleUnverifiedRebuild); it erases only
// when the pool keeps another disk with every write the target has, and
// otherwise mounts nothing (planUnverifiedErase).

const (
	// rebuildSightings is how many health runs in a row must see a returned
	// disk, with one identity, before it is rebuilt onto: about ten minutes
	// with the health timer.
	rebuildSightings = 2
	// rebuildAgainAfter: a disk rebuilt onto that drops out again sooner is
	// not rebuilt onto automatically.
	rebuildAgainAfter = 24 * time.Hour
)

// RebuildUnitName is the systemd unit that runs Rebuild for pool uuid.
func RebuildUnitName(uuid string) string {
	return "linuxio-btrfs-rebuild@" + uuid + ".service"
}

// BtrfsRebuildState is where a pool's automatic rebuild stands.
type BtrfsRebuildState string

const (
	// RebuildWaiting: a returned disk was seen once and waits for the next
	// health run.
	RebuildWaiting BtrfsRebuildState = "waiting"
	// RebuildRunning: a rebuild started and is not verified yet.
	RebuildRunning BtrfsRebuildState = "running"
	// RebuildFailed: a rebuild failed after it wrote to its disk; it waits
	// for a person.
	RebuildFailed BtrfsRebuildState = "failed"
	// RebuildRefused: the last health run started none, for Reason.
	RebuildRefused BtrfsRebuildState = "refused"
)

// BtrfsRebuild is a pool's automatic rebuild as the page shows it.
type BtrfsRebuild struct {
	State    BtrfsRebuildState `json:"state"`
	DeviceID int               `json:"deviceId,omitzero"`
	// Device is the disk rebuilt onto, or seen back.
	Device string `json:"device,omitzero"`
	Spare  bool   `json:"spare,omitzero"`
	Reason string `json:"reason,omitzero"`
	// Advice, for a refusal, is what to do in place of replacing the missing
	// disk, which would give up the copy the refusal keeps.
	Advice string `json:"advice,omitzero"`
}

// memberIdentity is the hardware identity of the disk of device ID (with
// device UUID UUID) as the health run last saw the pool whole; Disk is zero
// for a member that is no whole disk or has no WWN or serial.
type memberIdentity struct {
	ID   uint64         `json:"id"`
	UUID string         `json:"uuid"`
	Disk BtrfsSpareDisk `json:"disk,omitzero"`
}

// returnedSighting is the returned disk the last health run saw for device
// ID, and how many runs in a row saw it. Boot and Seq (the disk's diskseq,
// which the kernel raises at every attach) count only sightings of one
// attachment of the disk in one boot.
type returnedSighting struct {
	ID     uint64         `json:"id"`
	Disk   BtrfsSpareDisk `json:"disk"`
	Device string         `json:"device"`
	Seen   int            `json:"seen"`
	Boot   string         `json:"boot,omitzero"`
	Seq    uint64         `json:"seq,omitzero"`
}

// rebuildRecord is a rebuild of device ID onto the disk Disk, written before
// anything is erased and cleared once the new disk is verified.
type rebuildRecord struct {
	ID   uint64         `json:"id"`
	UUID string         `json:"uuid"` // the member's device UUID, which the target takes
	Disk BtrfsSpareDisk `json:"disk"`
	// Device is the target's node when the rebuild started.
	Device  string `json:"device"`
	Spare   bool   `json:"spare,omitzero"`
	Started int64  `json:"started"`
	// Boot is the boot the rebuild started in: a replace the kernel continued
	// at a mount in a later boot was not watched to its end.
	Boot string `json:"boot"`
	// Failed says why the rebuild failed after it wrote to its disk; the
	// pool then waits for a person, and only its hot spare may still replace
	// the member.
	Failed string `json:"failed,omitzero"`
	// Restart is set once the replace was canceled with its target blank
	// again, by a stop of the rebuild or by a mount LinuxIO made: the next
	// rebuild starts it again onto the same disk.
	Restart bool `json:"restart,omitzero"`
	// Prior is the start time the kernel reported for the pool's last
	// replace when the record was written, 0 for none: a replace whose start
	// time differs started after the record (ours), whatever the clock did.
	Prior int64 `json:"prior,omitzero"`
	// Untrusted is the disk a failed rebuild left in the member's slot, which
	// this rebuild, onto the hot spare, replaces: LinuxIO's mount erases it
	// too while the record is set.
	Untrusted BtrfsSpareDisk `json:"untrusted,omitzero"`
}

// ours reports whether the pool's replace, which the kernel says started at
// started, is one started after the record rec was written.
func (rec rebuildRecord) ours(started time.Time) bool {
	return started.Unix() != rec.Prior
}

// sameRecord reports whether a and b are one rebuild on record.
func sameRecord(a, b *rebuildRecord) bool {
	return a != nil && b != nil && a.ID == b.ID && a.Started == b.Started
}

// rebuiltDisk is a disk an automatic rebuild was verified onto, and when.
type rebuiltDisk struct {
	Disk BtrfsSpareDisk `json:"disk"`
	At   int64          `json:"at"`
}

// refusedDisk is a disk LinuxIO refused, or failed, to rebuild onto in the
// episode: it is not tried again without a person.
type refusedDisk struct {
	Disk   BtrfsSpareDisk `json:"disk"`
	Reason string         `json:"reason"`
}

// Seams for tests: the boot, the exclusive open of a target, and the
// manager's view of the resync unit.
var (
	bootID            = jobs.BootID
	openRebuildTarget = func(device string) (*os.File, error) { return openExclusive(device, os.O_RDWR) }
	unitActiveState   = systemd.GetActiveState
)

// SetAutoRebuild turns the rebuild onto a disk that returns on or off for
// pool uuid; it is on unless turned off.
func SetAutoRebuild(ctx context.Context, uuid string, enabled bool) error {
	return updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		state.NoAutoRebuild = !enabled
		if !enabled {
			state.Returned = nil
		}
		s[uuid] = state
	})
}

// attachRebuilds gives each of filesystems its rebuild switch and state. A
// pool record that cannot be read leaves the switch on, its default.
func attachRebuilds(filesystems []BtrfsFilesystem) {
	states, err := loadPoolStates()
	if err != nil {
		slog.Warn("failed to read btrfs pool state", "path", PoolStatePath, "error", err)
	}
	for i := range filesystems {
		st := states[filesystems[i].UUID]
		filesystems[i].AutoRebuild = !st.NoAutoRebuild
		filesystems[i].Rebuild = rebuildOf(st)
	}
}

// rebuildOf is the rebuild of a pool as its state records it, nil when none:
// a rebuild on record runs, failed, or waits for what RebuildWait says.
func rebuildOf(st poolState) *BtrfsRebuild {
	switch {
	case st.Rebuild != nil:
		r := &BtrfsRebuild{State: RebuildRunning, DeviceID: int(st.Rebuild.ID), Device: st.Rebuild.Device, Spare: st.Rebuild.Spare}
		switch {
		case st.Rebuild.Failed != "":
			r.State, r.Reason = RebuildFailed, st.Rebuild.Failed
		case st.RebuildWait != "":
			r.State, r.Reason = RebuildWaiting, st.RebuildWait
		}
		return r
	case st.RebuildRefusal != "":
		return &BtrfsRebuild{State: RebuildRefused, Reason: st.RebuildRefusal, Advice: st.RebuildAdvice}
	case st.Returned != nil:
		return &BtrfsRebuild{State: RebuildWaiting, DeviceID: int(st.Returned.ID), Device: st.Returned.Device}
	}
	return nil
}

// memberIdentities returns the identity of each of devices' disks, by its
// node among disks (wholeDisks).
func memberIdentities(devices []BtrfsDevice, disks []spareCandidate) []memberIdentity {
	ids := make([]memberIdentity, 0, len(devices))
	for _, d := range devices {
		disk, _ := diskOf(d.Path, disks)
		ids = append(ids, memberIdentity{ID: uint64(d.ID), UUID: d.UUID, Disk: disk})
	}
	return ids
}

// diskOf returns the identity of the whole disk at path among disks, and
// whether path is one.
func diskOf(path string, disks []spareCandidate) (BtrfsSpareDisk, bool) {
	if path == "" {
		return BtrfsSpareDisk{}, false
	}
	canonical := canonicalPath(path)
	for _, c := range disks {
		if c.device == path || c.device == canonical {
			return c.disk, true
		}
	}
	return BtrfsSpareDisk{}, false
}

// identityOf returns the identity recorded for device id with device UUID
// uuid, when there is one.
func (s poolState) identityOf(id uint64, uuid string) (BtrfsSpareDisk, bool) {
	for _, m := range s.Identities {
		if m.ID == id && m.UUID == uuid {
			return m.Disk, true
		}
	}
	return BtrfsSpareDisk{}, false
}

// refusalOf returns why disk was refused in the episode, if it was.
func (s poolState) refusalOf(disk BtrfsSpareDisk) (string, bool) {
	for _, r := range s.Refused {
		if r.Disk.matches(disk) {
			return r.Reason, true
		}
	}
	return "", false
}

// noteReplaced records, once a replace of device id of pool uuid is
// verified, the hardware identity of the disk now at path for that devid,
// and drops an automatic rebuild's record of the devid and any returned
// disk seen for it: the disk there works. A failure is logged.
func noteReplaced(ctx context.Context, uuid string, id int, path string) {
	replaced := replacedMember(ctx, uuid, id, path)
	err := updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		state.noteReplaced(replaced)
		s[uuid] = state
	})
	if err != nil {
		slog.Warn("failed to record a btrfs replace in the pool state", "uuid", uuid, "devid", id, "error", err)
	}
}

// replacedMember reads the identity of the disk at path, now device id of
// pool uuid, and the device UUID the kernel gives it; UUID is empty when it
// could not be read. A failure is logged.
func replacedMember(ctx context.Context, uuid string, id int, path string) memberIdentity {
	disks, err := wholeDisks()
	if err != nil {
		slog.Warn("failed to read the identity of a replaced btrfs device", "uuid", uuid, "devid", id, "error", err)
	}
	m := memberIdentity{ID: uint64(id)}
	m.Disk, _ = diskOf(path, disks)
	if mnt, mntErr := mountpointFor(ctx, uuid); mntErr == nil {
		info := devInfoArgs{DevID: uint64(id)}
		if fsIoctl(mnt, iocDevInfo, unsafe.Pointer(&info)) == nil {
			m.UUID = formatUUID(info.UUID[:])
		}
	}
	return m
}

// noteReplaced records m as the disk of its devid after a verified replace,
// and drops an automatic rebuild's record of the devid and any returned disk
// seen for it.
func (s *poolState) noteReplaced(m memberIdentity) {
	s.Identities = slices.DeleteFunc(s.Identities, func(o memberIdentity) bool { return o.ID == m.ID })
	if m.UUID != "" {
		s.Identities = append(s.Identities, m)
		slices.SortFunc(s.Identities, func(a, b memberIdentity) int { return cmp.Compare(a.ID, b.ID) })
	}
	if s.Rebuild != nil && s.Rebuild.ID == m.ID {
		s.Rebuild = nil
	}
	if s.Returned != nil && s.Returned.ID == m.ID {
		s.Returned = nil
	}
}

// poolDevice is a device whose own primary superblock names a pool.
type poolDevice struct {
	device string
	sb     Superblock
	// disk is the device's identity when it is a whole disk (whole).
	disk  BtrfsSpareDisk
	whole bool
	// inUse says, from sysfs alone, why something holds it, or "".
	inUse string
	// probed is set once udev's database has an entry for it.
	probed bool
	// seq is the disk's diskseq, 0 where the kernel has none.
	seq uint64
}

// replaceNow is the pool's replace as the kernel reports it.
type replaceNow struct {
	state   uint64
	started time.Time
}

// rebuildInput is what decideRebuild decides on.
type rebuildInput struct {
	fs    BtrfsFilesystem
	state poolState
	// devices are the devices whose superblock names the pool, read only
	// when a member is missing.
	devices []poolDevice
	// generation is the pool's, from FS_INFO.
	generation uint64
	readOnly   bool
	// busy says what runs on the pool that a replace may not run beside.
	busy string
	now  time.Time
	boot string
	// disks are the connected whole disks, and replace the pool's replace,
	// read when a member is missing or a rebuild is on record.
	disks   []spareCandidate
	replace replaceNow
	// oneCopy says, by devid, what chunks that keep one copy have a stripe
	// on the device (oneCopyChunks).
	oneCopy map[uint64]string
	// recordDisk is the node of the disk of the rebuild on record when one
	// connected disk has its identity, and recordSB that disk's superblock
	// when it carries one.
	recordDisk string
	recordSB   *Superblock
	// runner is set when Rebuild decides: it counts no new sighting.
	runner bool
}

type rebuildKind int

const (
	rebuildNone rebuildKind = iota
	rebuildWait
	rebuildRefuse
	rebuildReturned
	rebuildSpare
	rebuildResume
)

// rebuildPlan is what decideRebuild decided.
type rebuildPlan struct {
	kind   rebuildKind
	member BtrfsDevice
	// target is the disk to rebuild onto, and disk its identity.
	target string
	disk   BtrfsSpareDisk
	// reason says why it waits or is refused, and advice, when set, what to
	// do in place of replacing the missing disk. key, when set, names the
	// refusal's alert occurrence in place of the reason, which may carry
	// counts that change from run to run.
	reason string
	advice string
	key    string
	// refused is the disk the refusal is final for, in the episode.
	refused *BtrfsSpareDisk
	// untrusted is the disk a failed rebuild left in the member's slot,
	// which the hot spare replaces.
	untrusted BtrfsSpareDisk
	// sighting is the returned disk this run saw, which the health run saves.
	sighting *returnedSighting
	// record is the rebuild on record the plan was decided on. clearRecord
	// drops it: its devid left the pool, or it is set aside with nothing
	// erased. fail fails it with reason.
	record      *rebuildRecord
	clearRecord bool
	fail        bool
}

// starts reports whether the plan starts the rebuild unit.
func (p rebuildPlan) starts() bool {
	return p.kind == rebuildReturned || p.kind == rebuildSpare || p.kind == rebuildResume
}

// decideRebuild decides what a pool's automatic rebuild does now: follow or
// finish a rebuild on record; rebuild a missing member onto its own disk
// back (returnedPlan) when the switch is on; or else onto the hot spare
// (sparePlan) once the member has been missing for its delay. A returned
// disk wins over the spare while it waits for its second sighting. Nothing
// starts on a pool that is read-only, keeps one copy of something on the
// member, or runs a replace, balance, device add, removal, resize, scrub or
// resync (poolAllows); a pool with nothing to rebuild onto is not judged.
func decideRebuild(in rebuildInput) rebuildPlan {
	if rec := in.state.Rebuild; rec != nil {
		return decideOnRecord(in, *rec)
	}
	var missing []BtrfsDevice
	for _, d := range in.fs.Devices {
		if d.Missing {
			missing = append(missing, d)
		}
	}
	switch {
	case len(missing) == 0:
		return rebuildPlan{}
	case !in.fs.HealthKnown:
		return rebuildPlan{kind: rebuildWait, reason: "the pool's devices could not all be read"}
	}
	var refusal rebuildPlan
	if !in.state.NoAutoRebuild {
		p, decided := returnedPlans(in, missing)
		if decided {
			return poolAllows(in, p, true)
		}
		refusal = p
	}
	for _, m := range missing {
		if p, ok := sparePlan(in, m, false); ok {
			return poolAllows(in, p, false)
		}
	}
	return refusal
}

// poolAllows returns p, a rebuild of p.member to start or wait for, unless
// the pool cannot take it now: refused while the pool is read-only, keeps
// one copy of a kind at best, or keeps one copy of chunks on the member;
// waiting while its profiles cannot be read or another operation runs. A
// returned disk, which the rebuild erases, is also refused while a present
// member counts read, corruption or generation errors: the returned disk's
// old copy may be the only good one of a block, and taken back by the pool
// at a mount it repairs both ways. Each is evaluated again at every run.
func poolAllows(in rebuildInput, p rebuildPlan, returned bool) rebuildPlan {
	refuse := func(reason, advice string) rebuildPlan {
		return rebuildPlan{kind: rebuildRefuse, member: p.member, target: p.target, reason: reason, advice: advice}
	}
	if reason, final := rebuildBlocked(in); reason != "" && final {
		return refuse(reason, "")
	} else if reason != "" {
		return rebuildPlan{kind: rebuildWait, member: p.member, reason: reason}
	}
	if reason, advice := beyondTolerance(in); reason != "" {
		return refuse(reason, advice)
	}
	if what := in.oneCopy[uint64(p.member.ID)]; what != "" {
		return refuse(fmt.Sprintf("device %d holds %s chunks, which keep one copy, so their only copy is on its disk", p.member.ID, what),
			"Reconnect that disk and mount the pool again so btrfs takes it back, or copy the data off the pool.")
	}
	if in.busy != "" {
		return rebuildPlan{kind: rebuildWait, member: p.member, reason: in.busy}
	}
	if !returned {
		return p
	}
	for _, d := range in.fs.Devices {
		if e := d.Errors; !d.Missing && e.Read+e.Corruption+e.Generation > 0 {
			refused := refuse(fmt.Sprintf("device %d counts %d read, %d corruption and %d generation errors, so the copy on the disk that came back may be the only good one of some blocks, and LinuxIO does not erase it", d.ID, e.Read, e.Corruption, e.Generation),
				fmt.Sprintf("Unmount the pool and mount it again from the Storage overview so btrfs takes the disk back; the resync then repairs both disks. Then reset the error counters, which LinuxIO does not do for you: btrfs device stats --reset %s.", in.fs.Mountpoints[0].Mountpoint))
			refused.key = fmt.Sprintf("errors/%d", d.ID)
			return refused
		}
	}
	return p
}

// beyondTolerance says why no rebuild may run while the pool misses more
// members than its profiles lose, with what to do instead, or "". raid1 and
// raid10 keep two copies of a chunk, on two of the pool's disks: with two
// members missing, the chunks those two held together have no copy on the
// disks that stayed. Erasing the disk of one of them that came back would
// erase their only copy; a replace onto it, or onto the hot spare, cannot
// copy them, and still ends well for the kernel, which loses one device
// within what raid1 tolerates. So the missing members, kernel-missing and
// dropped out alike, must number no more than the copies its weakest
// redundant profile keeps, less one. Profiles that keep one copy are not
// counted here: oneCopyChunks judges those by member.
func beyondTolerance(in rebuildInput) (reason, advice string) {
	var missing []string
	for _, d := range in.fs.Devices {
		if d.Missing {
			missing = append(missing, strconv.Itoa(d.ID))
		}
	}
	weakest := ""
	for _, a := range in.fs.Allocation {
		if slices.Contains(spareProfiles, a.Profile) && (weakest == "" || profiles[a.Profile].copies < profiles[weakest].copies) {
			weakest = a.Profile
		}
	}
	if weakest == "" || len(missing) < profiles[weakest].copies {
		return "", ""
	}
	return fmt.Sprintf("devices %s are missing, and %s keeps %d copies of a chunk: what those disks held together has no copy on the disks that stayed, so LinuxIO erases none of them and replaces none", andList(missing), weakest, profiles[weakest].copies),
		"Reconnect the other missing disks too, then unmount the pool and mount it again from the Storage overview so btrfs takes them back; the resync then repairs them."
}

// decideOnRecord decides on the pool's rebuild on record, rec: a rebuild in
// flight is followed or settled by the runner (resumePlan), and one whose
// devid left the pool is dropped. A failed rebuild waits for a person; only
// the hot spare may still replace the member, when it is missing or, at
// once, when its slot holds the disk the failed rebuild left untrusted.
func decideOnRecord(in rebuildInput, rec rebuildRecord) rebuildPlan {
	i := slices.IndexFunc(in.fs.Devices, func(d BtrfsDevice) bool { return d.ID == int(rec.ID) })
	switch {
	case !in.fs.HealthKnown:
		return rebuildPlan{kind: rebuildWait, reason: "the pool's devices could not all be read"}
	case i < 0:
		return rebuildPlan{record: &rec, clearRecord: true}
	case rec.Failed == "":
		return resumePlan(in, rec, in.fs.Devices[i])
	}
	m := in.fs.Devices[i]
	refused := rebuildPlan{kind: rebuildRefuse, member: m, reason: rec.Failed, record: &rec}
	untrusted := !m.Missing
	if untrusted {
		// A disk other than the one the rebuild wrote took the slot since,
		// such as by a replace made outside LinuxIO: nothing to take over.
		if now, _ := diskOf(m.Path, in.disks); !rec.Disk.matches(now) {
			return refused
		}
	}
	p, ok := sparePlan(in, m, untrusted)
	if !ok || p.kind != rebuildSpare {
		return refused
	}
	if p = poolAllows(in, p, false); p.kind != rebuildSpare {
		return refused
	}
	if untrusted {
		p.untrusted = rec.Disk
	}
	return p
}

// resumePlan decides on the rebuild rec in flight, of member m: the runner
// follows, verifies or cancels a replace of its own, settles a record whose
// erase never happened, and starts the rebuild again onto its disk
// (restartPlan). It is not started for a replace the rebuild did not start,
// which runs on.
func resumePlan(in rebuildInput, rec rebuildRecord, m BtrfsDevice) rebuildPlan {
	r := in.replace
	running := r.state == replaceStateStarted || r.state == replaceStateSuspended
	switch {
	case running && !rec.ours(r.started):
		return waitOnRecord(rec, m, "a replace the rebuild did not start runs on the pool")
	case running, r.state == replaceStateFinished && rec.ours(r.started), !m.Missing && !rec.Spare:
		return rebuildPlan{kind: rebuildResume, member: m, record: &rec}
	}
	return restartPlan(in, rec, m)
}

// restartWaitsFor is how long a rebuild to start again waits for its disk to
// be connected: two health runs with the timer. Then it fails, which lets
// the hot spare in.
const restartWaitsFor = 20 * time.Minute

// restartPlan decides on the rebuild rec of member m to start again onto its
// disk. A disk that is not connected is waited for, and the rebuild fails
// once it has been away for restartWaitsFor since the record. A disk that
// still carries m's own superblock, never erased, is taken by the pool back
// at a mount, so the record is set aside, not failed, when the switch is
// off or the pool's checks refuse that disk (poolAllows), and LinuxIO's
// next mount does not erase it. A rebuild onto an erased disk, or onto the
// hot spare, waits while the pool cannot take a replace, and with the
// switch off one onto the returned disk fails.
func restartPlan(in rebuildInput, rec rebuildRecord, m BtrfsDevice) rebuildPlan {
	resume := rebuildPlan{kind: rebuildResume, member: m, record: &rec}
	_, n := findDisk(rec.Disk, in.disks)
	switch {
	case n == 0 && in.now.Sub(time.Unix(rec.Started, 0)) >= restartWaitsFor:
		return failOnRecord(rec, m, fmt.Sprintf("%s, the disk LinuxIO was rebuilding device %d onto, has not been connected since the rebuild was cut; connecting it again does not resume the rebuild: replace device %d from the pool details", rec.Device, rec.ID, rec.ID))
	case n == 0:
		return waitOnRecord(rec, m, fmt.Sprintf("the rebuild of device %d was cut and starts again once %s, its disk, is connected", rec.ID, rec.Device))
	case n > 1:
		return resume // the runner fails it
	}
	sb := in.recordSB
	untouched := !rec.Spare && sb != nil && sb.UUID == in.fs.UUID && sb.DevID == rec.ID && sb.DevUUID == rec.UUID
	switch {
	case untouched && in.state.NoAutoRebuild:
		return rebuildPlan{record: &rec, clearRecord: true}
	case untouched:
		p := poolAllows(in, rebuildPlan{kind: rebuildReturned, member: m, target: in.recordDisk, disk: rec.Disk}, true)
		switch p.kind {
		case rebuildRefuse:
			p.record, p.clearRecord = &rec, true
			return p
		case rebuildWait:
			return waitOnRecord(rec, m, p.reason)
		case rebuildNone, rebuildReturned, rebuildSpare, rebuildResume:
		}
	case in.state.NoAutoRebuild && !rec.Spare:
		return failOnRecord(rec, m, fmt.Sprintf("the automatic rebuild is off for the pool, so LinuxIO does not start the rebuild of device %d onto %s again; replace device %d from the pool details", rec.ID, rec.Device, rec.ID))
	default:
		// Nothing of the member is left on an erased disk or the spare to
		// keep, so only what keeps a replace from working holds it, or from
		// copying everything.
		if reason, _ := rebuildBlocked(in); reason != "" {
			return waitOnRecord(rec, m, reason)
		}
		if reason, _ := beyondTolerance(in); reason != "" {
			return waitOnRecord(rec, m, reason)
		}
		if in.busy != "" {
			return waitOnRecord(rec, m, in.busy)
		}
	}
	return resume
}

// waitOnRecord is a plan that keeps the rebuild rec of member m on record,
// waiting for what reason says, which the page and the alert show.
func waitOnRecord(rec rebuildRecord, m BtrfsDevice, reason string) rebuildPlan {
	return rebuildPlan{kind: rebuildWait, member: m, record: &rec, reason: reason}
}

// failOnRecord is a plan that fails the rebuild rec of member m for reason,
// as the runner fails one after it wrote to its disk.
func failOnRecord(rec rebuildRecord, m BtrfsDevice, reason string) rebuildPlan {
	return rebuildPlan{kind: rebuildRefuse, member: m, record: &rec, reason: reason, fail: true}
}

// returnedPlans decides on the returned disks of the missing members, in
// devid order: the first rebuild to start or wait for decides, and is
// returned with true; otherwise the first refusal is, with false.
func returnedPlans(in rebuildInput, missing []BtrfsDevice) (rebuildPlan, bool) {
	var refusal rebuildPlan
	for _, m := range missing {
		p := returnedPlan(in, m)
		switch p.kind {
		case rebuildReturned, rebuildWait:
			return p, true
		case rebuildRefuse:
			if refusal.kind == rebuildNone {
				refusal = p
			}
		case rebuildNone, rebuildSpare, rebuildResume:
		}
	}
	return refusal, false
}

// rebuildBlocked says why no replace can work on the pool, or "", and
// whether that is final for the run: it is read-only, or a kind of its
// chunks keeps a single copy at best, so the pool cannot run with a member
// missing; profiles that could not be read are waited for. Chunks a
// degraded mount wrote single sit beside the pool's own profile and do not
// count here; oneCopyChunks finds those on the member.
func rebuildBlocked(in rebuildInput) (string, bool) {
	if in.readOnly {
		return "the pool is mounted read-only", true
	}
	best := map[string]string{}
	for _, a := range in.fs.Allocation {
		kind := a.Kind
		if p, ok := best[kind]; !ok || profiles[a.Profile].rank > profiles[p].rank {
			best[kind] = a.Profile
		}
	}
	if len(best) == 0 {
		return "the pool's profiles could not be read", false
	}
	for _, kind := range slices.Sorted(maps.Keys(best)) {
		if !slices.Contains(spareProfiles, best[kind]) {
			return fmt.Sprintf("its %s is %s, which keeps one copy", kind, best[kind]), true
		}
	}
	return "", false
}

// oneCopyChunks reads the chunk tree of the pool mounted at mnt and returns,
// by devid, the kind and profile of a chunk that keeps one copy (single,
// dup, raid0) with a stripe on that device: its only copy is there. A pool
// that ran degraded holds such chunks beside its own profile, often empty,
// on the disks that were present, which a rebuild of another member leaves
// alone; one on the member to rebuild, left by an earlier episode whose
// convert gave up or by an interrupted convert, is lost with that disk.
func oneCopyChunks(mnt string) (map[uint64]string, error) {
	found := map[uint64]string{}
	key := searchKey{
		TreeID: chunkTreeObjectID, MinObjectID: firstChunkTreeObjectID, MaxObjectID: firstChunkTreeObjectID,
		MinType: chunkItemKey, MaxType: chunkItemKey, MaxOffset: ^uint64(0), MaxTransid: ^uint64(0),
	}
	err := treeSearch(mnt, key, func(h searchHeader, item []byte) {
		if h.Type != chunkItemKey || len(item) < chunkStripes {
			return
		}
		flags := binary.LittleEndian.Uint64(item[chunkType:])
		profile := profileName(flags)
		if profile != "single" && profile != "dup" && profile != "raid0" {
			return
		}
		n := int(binary.LittleEndian.Uint16(item[chunkNumStripes:]))
		for i := range n {
			off := chunkStripes + i*chunkStripeSize
			if off+8 > len(item) {
				return
			}
			devid := binary.LittleEndian.Uint64(item[off:])
			if _, seen := found[devid]; !seen {
				found[devid] = kindName(flags) + " " + profile
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("read the chunk tree: %w", err)
	}
	return found, nil
}

// returnedPlan decides on the disk that came back for missing member m: the
// one device, apart from the pool's present members, whose own superblock
// carries m's devid (a replace target's devid 0 is never one). Two such
// devices are none: LinuxIO cannot tell the disk from a copy of it.
func returnedPlan(in rebuildInput, m BtrfsDevice) rebuildPlan {
	var found []poolDevice
	for _, d := range in.devices {
		if d.sb.DevID == uint64(m.ID) && !presentMember(in.fs, d.device) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return rebuildPlan{}
	case 1:
	default:
		var names []string
		for _, d := range found {
			names = append(names, d.device)
		}
		return rebuildPlan{kind: rebuildRefuse, member: m, reason: fmt.Sprintf("%s all carry the superblock of device %d, as copies of one disk would, so LinuxIO cannot tell which is that disk", andList(names), m.ID)}
	}
	c := found[0]
	if !c.probed {
		// Its identity is udev's to report; a refusal now would be final.
		return rebuildPlan{kind: rebuildWait, member: m, target: c.device, reason: c.device + " is back, and udev has not reported it yet"}
	}
	if reason := returnedRefusal(in, m, c); reason != "" {
		p := rebuildPlan{kind: rebuildRefuse, member: m, target: c.device, reason: reason}
		if c.disk.named() {
			p.refused = &c.disk
		}
		return p
	}
	// The sightings must be of one attachment of the disk in one boot: one
	// pulled and plugged in again between two runs starts over.
	seen := 0
	if prev := in.state.Returned; prev != nil && prev.ID == uint64(m.ID) && prev.Disk == c.disk && prev.Boot == in.boot && prev.Seq == c.seq {
		seen = prev.Seen
	}
	if !in.runner {
		seen++
	}
	sighting := &returnedSighting{ID: uint64(m.ID), Disk: c.disk, Device: c.device, Seen: seen, Boot: in.boot, Seq: c.seq}
	if seen < rebuildSightings {
		return rebuildPlan{kind: rebuildWait, member: m, target: c.device, disk: c.disk, sighting: sighting,
			reason: fmt.Sprintf("%s is back and is rebuilt onto once %d health checks in a row have seen it", c.device, rebuildSightings)}
	}
	return rebuildPlan{kind: rebuildReturned, member: m, target: c.device, disk: c.disk, sighting: sighting}
}

// presentMember reports whether device is a member of fs that is not
// missing: the pool holds it.
func presentMember(fs BtrfsFilesystem, device string) bool {
	return slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool {
		return !d.Missing && (d.Path == device || canonicalPath(d.Path) == canonicalPath(device))
	})
}

// returnedRefusal says why c, a device that came back carrying missing
// member m's superblock, is not rebuilt onto, or "". Its superblock must name
// m's device UUID at a generation no newer than the pool's, and its
// hardware identity must be the one recorded for m while the pool was whole,
// and no other member's or other connected disk's; it must be a whole disk
// nothing holds and no smaller than m; it is not one refused before in the
// episode; and it was not rebuilt onto within a day. The reason names
// nothing that changes from run to run, such as the pool's generation: a
// refusal of a disk without an identity is not stored, and its alert is one
// occurrence while the reason is the same.
func returnedRefusal(in rebuildInput, m BtrfsDevice, c poolDevice) string {
	st := in.state
	if c.disk.named() {
		if reason, ok := st.refusalOf(c.disk); ok {
			return reason
		}
	}
	recorded, known := st.identityOf(uint64(m.ID), m.UUID)
	switch {
	case !c.whole:
		return fmt.Sprintf("%s, which carries device %d's superblock, is not a whole disk; LinuxIO rebuilds only onto a whole disk", c.device, m.ID)
	case c.inUse != "":
		return fmt.Sprintf("%s, which carries device %d's superblock, %s", c.device, m.ID, c.inUse)
	case c.sb.DevUUID != m.UUID:
		return fmt.Sprintf("%s carries device %d's ID with another device UUID, as a disk replaced earlier would", c.device, m.ID)
	case c.sb.Generation > in.generation:
		return newerGeneration(c.device, c.sb.Generation)
	case !known:
		return fmt.Sprintf("LinuxIO has no record of which disk was device %d: it records it while the pool is whole", m.ID)
	case !recorded.named():
		return fmt.Sprintf("device %d's disk had no WWN or serial number to recognise it by when LinuxIO last saw the pool whole", m.ID)
	case !c.disk.named():
		return fmt.Sprintf("%s has no WWN or serial number to recognise it by", c.device)
	case !recorded.matches(c.disk):
		return fmt.Sprintf("%s is not the disk that was device %d: its WWN, serial number or size differ", c.device, m.ID)
	case c.disk.SizeBytes < m.SizeBytes:
		return fmt.Sprintf("%s is smaller than device %d", c.device, m.ID)
	}
	if reason := sharedIdentity(in, m, recorded); reason != "" {
		return reason
	}
	for _, r := range st.Rebuilt {
		if r.Disk.matches(c.disk) && in.now.Sub(time.Unix(r.At, 0)) < rebuildAgainAfter {
			return fmt.Sprintf("%s was rebuilt onto %s ago and dropped out again: the disk keeps dropping out; replace it, or check its cable and connection", c.device, in.now.Sub(time.Unix(r.At, 0)).Round(time.Minute))
		}
	}
	return ""
}

// sharedIdentity says why recorded, the identity of member m's disk, does not
// tell that disk apart, or "": another member's disk was recorded with it,
// or more than one connected disk has it, as behind USB bridges that report
// one serial number for every disk.
func sharedIdentity(in rebuildInput, m BtrfsDevice, recorded BtrfsSpareDisk) string {
	for _, other := range in.state.Identities {
		if other.ID != uint64(m.ID) && (recorded.matches(other.Disk) || other.Disk.matches(recorded)) {
			return fmt.Sprintf("device %d's disk has the WWN, serial number and size of device %d's, so LinuxIO cannot recognise it", m.ID, other.ID)
		}
	}
	if _, n := findDisk(recorded, in.disks); n > 1 {
		return fmt.Sprintf("%d connected disks have the WWN, serial number and size of device %d's disk, so LinuxIO cannot recognise it", n, m.ID)
	}
	return ""
}

// newerGeneration is the refusal of device, a disk that came back carrying a
// generation (gen) the pool has not reached.
func newerGeneration(device string, gen uint64) string {
	return fmt.Sprintf("%s carries a newer generation (%d) than the pool", device, gen)
}

// sparePlan decides on the hot spare for member m: ready and not refused
// before in the episode, once m has been missing for the spare's delay, or
// at once for a member whose disk a failed rebuild left untrusted.
func sparePlan(in rebuildInput, m BtrfsDevice, untrusted bool) (rebuildPlan, bool) {
	s := in.fs.Spare
	if s == nil || s.State != SpareReady || s.Disk.SizeBytes < m.SizeBytes {
		return rebuildPlan{}, false
	}
	if _, refused := in.state.refusalOf(s.Disk); refused {
		return rebuildPlan{}, false
	}
	if !untrusted {
		since := in.state.missingSince(m)
		if wait := time.Duration(s.DelayMinutes)*time.Minute - in.now.Sub(since); wait > 0 {
			return rebuildPlan{kind: rebuildWait, member: m, reason: fmt.Sprintf("the hot spare replaces device %d once it has been missing for %d minutes", m.ID, s.DelayMinutes)}, true
		}
	}
	return rebuildPlan{kind: rebuildSpare, member: m, target: s.Device, disk: s.Disk}, true
}

// missingSince is when the episode recorded member m missing: when the disk
// went missing, or the episode's start for a record without it.
func (s poolState) missingSince(m BtrfsDevice) time.Time {
	for _, d := range s.Missing {
		if d.ID == uint64(m.ID) && d.UUID == m.UUID && d.At != 0 {
			return time.Unix(d.At, 0)
		}
	}
	if s.Since != 0 {
		return time.Unix(s.Since, 0)
	}
	return time.Now()
}

// rebuildInputOf gathers what decideRebuild decides on for the mounted pool
// fs with state st. It reads the superblocks of every block device only
// when a member is missing.
func rebuildInputOf(ctx context.Context, fs BtrfsFilesystem, st poolState, runner bool) (rebuildInput, error) {
	in := rebuildInput{fs: fs, state: st, now: time.Now(), runner: runner}
	missing := slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool { return d.Missing })
	if !missing && st.Rebuild == nil {
		return in, nil
	}
	mnt := fs.Mountpoints[0].Mountpoint
	info, err := mountedInfo(mnt)
	if err != nil {
		return in, err
	}
	in.generation = info.Generation
	if in.readOnly, err = poolReadOnly(mnt); err != nil {
		return in, err
	}
	if in.boot, err = bootID(); err != nil {
		return in, err
	}
	if in.disks, err = wholeDisks(); err != nil {
		return in, err
	}
	if in.replace.state, _, in.replace.started, err = replaceStatusOf(mnt); err != nil {
		return in, err
	}
	if in.oneCopy, err = oneCopyChunks(mnt); err != nil {
		return in, err
	}
	in.busy = rebuildBusy(ctx, fs)
	if rec := st.Rebuild; rec != nil {
		if device, n := findDisk(rec.Disk, in.disks); n == 1 {
			in.recordDisk = device
			if sb, sbErr := readSuperblock(device); sbErr == nil {
				in.recordSB = &sb
			}
		}
	}
	if missing && !st.NoAutoRebuild {
		if in.devices, err = devicesOfPool(fs.UUID, in.disks); err != nil {
			return in, err
		}
	}
	return in, nil
}

// devicesOfPool lists the devices whose own primary superblock names pool
// uuid, with the identity of the whole disks among them (of disks) and why
// sysfs says something holds one.
func devicesOfPool(uuid string, disks []spareCandidate) ([]poolDevice, error) {
	var found []poolDevice
	for _, device := range BlockDevices() {
		sb, err := readSuperblock(device)
		if err != nil || sb.UUID != uuid || sb.DevID == replaceTargetDevID {
			continue
		}
		d := poolDevice{device: device, sb: sb}
		d.disk, d.whole = diskOf(device, disks)
		if d.whole {
			// Only a whole disk is rebuilt onto.
			if d.inUse, err = heldBy(device); err != nil {
				return nil, err
			}
		}
		props, _ := udevProperties(filepath.Base(device))
		d.probed = props != nil
		d.seq, _ = sysNumber(filepath.Join(sysBlockDir, filepath.Base(device), "diskseq"))
		found = append(found, d)
	}
	return found, nil
}

// heldBy says, from sysfs alone, why something holds device: partitions or a
// holder. The exclusive open right before the erase is the final word. A
// read that fails, the device gone included, is an error: what it could not
// read may be what holds the device.
func heldBy(device string) (string, error) {
	parts, err := kernelPartitions(device)
	if err != nil {
		return "", fmt.Errorf("read the partitions of %s: %w", device, err)
	}
	if len(parts) > 0 {
		return "has partitions", nil
	}
	holders, err := os.ReadDir(filepath.Join(sysBlockDir, filepath.Base(device), "holders"))
	if err != nil {
		return "", fmt.Errorf("read the holders of %s: %w", device, err)
	}
	if len(holders) > 0 {
		return "is used by /dev/" + holders[0].Name(), nil
	}
	return "", nil
}

// rebuildBusy says what runs on the pool that a replace may not run beside,
// or "": an exclusive operation of the kernel (balance, device add, removal,
// replace, resize), a replace running or suspended, a scrub, or the pool's
// resync. The kernel refuses a second exclusive operation itself; this
// keeps the rebuild from erasing a disk it then cannot replace onto.
func rebuildBusy(ctx context.Context, fs BtrfsFilesystem) string {
	if b, err := os.ReadFile(filepath.Join(sysFSBtrfs, fs.UUID, "exclusive_operation")); err == nil {
		if op := strings.TrimSpace(string(b)); op != "" && op != "none" {
			return "a " + op + " runs on the pool"
		}
	}
	mnt := fs.Mountpoints[0].Mountpoint
	if state, _, _, err := replaceStatusOf(mnt); err != nil {
		return "the pool's replace status could not be read"
	} else if state == replaceStateStarted || state == replaceStateSuspended {
		return "a device replace is " + replaceStateName(state) + " on the pool"
	}
	if _, err := balanceStatusOf(mnt); !errors.Is(err, unix.ENOTCONN) {
		return "a balance runs on the pool"
	}
	if fs.Scrub.Status == "running" {
		return "a scrub runs on the pool"
	}
	switch state, err := unitActiveState(ctx, ResyncUnitName(fs.UUID)); {
	case err != nil:
		return "the state of the pool's resync could not be read"
	case state == "active", state == "activating", state == "deactivating", state == "reloading":
		return "the pool's resync runs"
	}
	return ""
}

// checkRebuilds decides each mounted pool's automatic rebuild after the
// health run recorded states, saves what the next decision needs, raises or
// resolves the pool's rebuild alert, and returns the pools whose rebuild
// unit starts. A pool whose state could not be read starts nothing.
func checkRebuilds(ctx context.Context, filesystems []BtrfsFilesystem, states map[string]poolState) []string {
	var start []string
	for _, fs := range filesystems {
		st := states[fs.UUID]
		in, err := rebuildInputOf(ctx, fs, st, false)
		if err != nil {
			slog.Warn("failed to check btrfs automatic rebuild", "uuid", fs.UUID, "error", err)
			continue
		}
		plan := decideRebuild(in)
		// A whole pool with nothing on record is not written again. The alert
		// follows the state as it is now: the rebuild unit may have written
		// its record, and raised its alert, since this run read st.
		quiet := plan.kind == rebuildNone && !plan.clearRecord && st.Returned == nil && st.RebuildRefusal == ""
		if !quiet {
			if saveErr := savePlan(ctx, fs.UUID, plan); saveErr != nil {
				slog.Warn("failed to record btrfs pool state", "uuid", fs.UUID, "error", saveErr)
				continue
			}
		}
		now, err := loadPoolStates()
		if err != nil {
			slog.Warn("failed to read btrfs pool state", "path", PoolStatePath, "error", err)
			continue
		}
		raiseRebuildAlert(ctx, fs, now[fs.UUID], plan)
		if plan.starts() {
			start = append(start, fs.UUID)
		}
	}
	return start
}

// savePlan records what plan saw: the returned disk and how many runs in a
// row saw it, the reason no rebuild starts, a disk refused for the episode,
// and a record whose devid left the pool, which it drops.
func savePlan(ctx context.Context, uuid string, plan rebuildPlan) error {
	return updatePoolStates(ctx, func(s map[string]poolState) {
		state := s[uuid]
		state.Returned = plan.sighting
		// A record cleared or replaced since the plan read it stays as it is.
		current := sameRecord(state.Rebuild, plan.record)
		switch {
		case plan.clearRecord && current:
			state.Rebuild = nil
		case plan.fail && current:
			rec := *state.Rebuild
			rec.Failed, rec.Restart = plan.reason, false
			state.Rebuild = &rec
			if _, ok := state.refusalOf(rec.Disk); !ok {
				state.Refused = append(state.Refused, refusedDisk{Disk: rec.Disk, Reason: plan.reason})
			}
		}
		state.RebuildRefusal, state.RebuildAdvice, state.RebuildWait = "", "", ""
		switch {
		case plan.kind == rebuildRefuse && state.Rebuild == nil:
			state.RebuildRefusal, state.RebuildAdvice = plan.reason, plan.advice
		case plan.kind == rebuildWait && current && state.Rebuild.Failed == "":
			state.RebuildWait = plan.reason
		}
		if plan.refused != nil {
			if _, ok := state.refusalOf(*plan.refused); !ok {
				state.Refused = append(state.Refused, refusedDisk{Disk: *plan.refused, Reason: plan.reason})
			}
		}
		s[uuid] = state
	})
}

// raiseRebuildAlert shows a rebuild that runs or failed, or one refused, as
// the pool's rebuild alert, and resolves it once nothing waits on a person.
func raiseRebuildAlert(ctx context.Context, fs BtrfsFilesystem, st poolState, plan rebuildPlan) {
	key := "rebuild-" + fs.UUID
	name := poolName(fs.Label, fs.UUID)
	switch {
	case st.Rebuild != nil && st.Rebuild.Failed != "":
		raise(ctx, rebuildFailedObservation(fs, *st.Rebuild, st.Refused))
	case st.Rebuild != nil && st.RebuildWait != "":
		raise(ctx, alerts.Observation{
			Key: key, Severity: alerts.SeverityWarning,
			Title:   fmt.Sprintf("Rebuild of device %d of pool %s waits", st.Rebuild.ID, name),
			Message: sentenceOf(st.RebuildWait) + " Nothing is copied meanwhile.",
			Link:    "/storage?pool=" + fs.UUID, OccurrenceID: "waiting/" + strconv.FormatInt(st.Rebuild.Started, 10) + "/" + shortHash(st.RebuildWait),
		})
	case st.Rebuild != nil:
		raise(ctx, rebuildRunningObservation(fs, *st.Rebuild))
	case plan.kind == rebuildRefuse:
		advice := cmp.Or(plan.advice, "Replace the missing disk from the pool details.")
		raise(ctx, alerts.Observation{
			Key: key, Severity: alerts.SeverityError,
			Title:   "Pool " + name + " was not rebuilt automatically",
			Message: sentenceOf(plan.reason) + " " + advice,
			Link:    "/storage?pool=" + fs.UUID, OccurrenceID: "refused/" + shortHash(cmp.Or(plan.key, plan.reason)),
		})
	case st.Rebuild == nil:
		// Nothing on record and no refusal: a run that waits, as for a
		// returned disk's second sighting or the spare's delay, or is about to
		// start the rebuild, has nothing for a person either.
		resolve(ctx, key)
	}
}

// rebuildRunningObservation is the alert of a rebuild that started.
func rebuildRunningObservation(fs BtrfsFilesystem, rec rebuildRecord) alerts.Observation {
	name := poolName(fs.Label, fs.UUID)
	onto := "the disk that came back, " + rec.Device + ", which LinuxIO erased first"
	if rec.Spare {
		onto = "the hot spare " + rec.Device
	}
	return alerts.Observation{
		Key: "rebuild-" + fs.UUID, Severity: alerts.SeverityWarning,
		Title:   fmt.Sprintf("Pool %s is rebuilding device %d", name, rec.ID),
		Message: fmt.Sprintf("Device %d is missing, so LinuxIO copies its data onto %s from the pool's other disks. The pool stays mounted; once the copy is checked, LinuxIO resyncs the pool.", rec.ID, onto),
		Link:    "/storage?pool=" + fs.UUID, OccurrenceID: "running/" + strconv.FormatInt(rec.Started, 10),
	}
}

// rebuildFailedObservation is the alert of a rebuild that failed, with the
// other disks the episode refused, such as the hot spare tried after it.
func rebuildFailedObservation(fs BtrfsFilesystem, rec rebuildRecord, refused []refusedDisk) alerts.Observation {
	name := poolName(fs.Label, fs.UUID)
	var message strings.Builder
	message.WriteString(sentenceOf(rec.Failed) + " LinuxIO does not rebuild onto " + rec.Device + " again by itself.")
	for _, r := range refused {
		if !r.Disk.matches(rec.Disk) {
			message.WriteString(" " + sentenceOf(r.Reason))
		}
	}
	return alerts.Observation{
		Key: "rebuild-" + fs.UUID, Severity: alerts.SeverityError,
		Title:   fmt.Sprintf("Rebuild of device %d of pool %s failed", rec.ID, name),
		Message: message.String(),
		Link:    "/storage?pool=" + fs.UUID, OccurrenceID: "failed/" + strconv.FormatInt(rec.Started, 10) + "/" + shortHash(message.String()),
	}
}

// sentenceOf makes a clause a sentence.
func sentenceOf(clause string) string {
	if clause == "" {
		return ""
	}
	s := strings.ToUpper(clause[:1]) + clause[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// shortHash is a short, stable name for text in an occurrence ID.
func shortHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:4])
}
