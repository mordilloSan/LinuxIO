package btrfs

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// A pool can have one hot spare: an empty whole disk the administrator names
// for it, which LinuxIO keeps for that pool alone and replaces a missing
// member onto, the way an md array uses its spare. The designation lives in
// SparePath; nothing is written to the disk until a replace uses it.
//
// The disk is named by what udev reads from the disk itself, its WWN and its
// serial, with its size, never by its kernel name, which another disk can
// take after a reboot. When that identity names no connected disk, or more
// than one, the spare is absent and no other disk stands in for it. Whether
// it can serve is decided again each time the pool is read (spareNow):
// present, no smaller than any member, and empty by sysfs and udev's
// database, without opening the disk, so a read neither wakes nor claims it.
// Its own signatures, read through an exclusive open (readSpare), are
// checked when it is set and right before it is used (SpareOf).

// SparePath records the hot spare of each pool that has one, by pool UUID.
var SparePath = version.DataDir + "/btrfs-spares.json"

const (
	// DefaultSpareDelayMinutes is how long a member is missing before the
	// spare takes its place, unless the administrator says otherwise.
	DefaultSpareDelayMinutes = 10
	// MaxSpareDelayMinutes is the longest delay accepted: a week.
	MaxSpareDelayMinutes = 7 * 24 * 60
)

// BtrfsSpareState is what a pool's hot spare is now.
type BtrfsSpareState string

const (
	// SpareReady: present, empty, unclaimed and no smaller than any member.
	SpareReady BtrfsSpareState = "ready"
	// SpareAbsent: its identity names no connected disk of its size, or more
	// than one.
	SpareAbsent BtrfsSpareState = "absent"
	// SpareTooSmall: smaller than a member of the pool, which a replace
	// refuses (btrfs_init_dev_replace_tgtdev in fs/btrfs/dev-replace.c).
	SpareTooSmall BtrfsSpareState = "too-small"
	// SpareInUse: something other than LinuxIO wrote to it or claims it.
	SpareInUse BtrfsSpareState = "in-use"
	// SpareUnknown: it could not be checked, such as without the privilege
	// to open the disk, or with the pool's devices not all read.
	SpareUnknown BtrfsSpareState = "unknown"
)

var (
	// ErrSpareUnsuitable is SetSpare or SetSpareDelay refusing a device or a
	// value that cannot serve as this pool's spare.
	ErrSpareUnsuitable = errors.New("unsuitable hot spare")
	// ErrSpareInUse is SetSpare refusing a disk that is not empty or that
	// something claims.
	ErrSpareInUse = errors.New("hot spare in use")
	// ErrNoSpare is a pool without a hot spare.
	ErrNoSpare = errors.New("the pool has no hot spare")
)

// BtrfsSpareDisk names a whole disk across reboots and renames, by what udev
// reads from the disk: its WWN (ID_WWN_WITH_EXTENSION, else ID_WWN) and its
// serial (ID_SERIAL, which udev makes from the model and the serial number),
// and its size in bytes. A disk is the one named when its size and every
// identity field recorded here match.
type BtrfsSpareDisk struct {
	WWN       string `json:"wwn,omitzero"`
	Serial    string `json:"serial,omitzero"`
	SizeBytes uint64 `json:"sizeBytes"`
}

// spareRecord is one pool's designation as SparePath stores it.
type spareRecord struct {
	Disk         BtrfsSpareDisk `json:"disk"`
	DelayMinutes int            `json:"delayMinutes"`
}

// BtrfsSpare is a pool's hot spare as it is now.
type BtrfsSpare struct {
	Disk BtrfsSpareDisk `json:"disk"`
	// Device is the disk's node now, empty while the spare is absent.
	Device string `json:"device"`
	// DelayMinutes is how long a member may be missing before the spare
	// replaces it; 0 is at the next health run.
	DelayMinutes int             `json:"delayMinutes"`
	State        BtrfsSpareState `json:"state"`
	// Reason says why a spare is not ready, as a clause about the disk,
	// such as "it has partitions".
	Reason string `json:"reason,omitzero"`
}

// spareProfiles keep a second copy of everything, so a pool of them runs
// with a member missing, which is when a spare is used.
var spareProfiles = []string{"raid1", "raid1c3", "raid1c4", "raid10"}

// SpareOf returns the hot spare of the mounted pool uuid as it is right now:
// the disk its identity names, checked against the pool's members and, when
// that leaves it ready, read through an exclusive open for any md, LVM2 or
// btrfs signature (readSpare), so a Ready spare is present, no smaller than
// any member, unclaimed and empty at the time of the call. The open is
// closed before it returns: the caller starts its replace right after, and
// the kernel claims the target itself. ErrNoSpare when the pool has none.
func SpareOf(ctx context.Context, uuid string) (BtrfsSpare, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return BtrfsSpare{}, err
	}
	if !fs.spareKnown {
		// ListFilesystems only logged why; the caller gets it.
		one := []BtrfsFilesystem{fs}
		if err = attachSpares(one); err != nil {
			return BtrfsSpare{}, fmt.Errorf("read hot spare of %s: %w", uuid, err)
		}
		fs = one[0]
	}
	if fs.Spare == nil {
		return BtrfsSpare{}, ErrNoSpare
	}
	return readSpare(*fs.Spare), nil
}

// SetSpare makes device, a whole disk, the hot spare of the mounted pool fs,
// used once a member has been missing for delayMinutes. It refuses with
// ErrSpareUnsuitable a delay outside 0 to MaxSpareDelayMinutes, a pool whose
// profiles keep a single copy, a device that is no whole disk, one without a
// WWN or serial or whose identity another connected disk shares, and one
// smaller than a member; with ErrSpareInUse one that is not empty
// (spareNow, readSpare). Callers check first that nothing else uses the device,
// another existing pool's reservation included (deviceInUse in the bridge).
// A pool that has a spare gets this one in its place, and a disk is the
// spare of one pool at a time: a designation of the same disk for another
// pool, which can only be stale by then, such as of a pool destroyed outside
// LinuxIO, is dropped.
func SetSpare(ctx context.Context, fs BtrfsFilesystem, device string, delayMinutes int) error {
	if err := checkSpareDelay(delayMinutes); err != nil {
		return err
	}
	if err := checkSpareProfiles(fs); err != nil {
		return err
	}
	disks, err := wholeDisks()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(disks, func(c spareCandidate) bool { return c.device == device })
	if i < 0 {
		return fmt.Errorf("%w: %s is not a whole disk", ErrSpareUnsuitable, device)
	}
	rec := spareRecord{Disk: disks[i].disk, DelayMinutes: delayMinutes}
	if !rec.Disk.named() {
		return fmt.Errorf("%w: %s has no WWN or serial number to recognise it by", ErrSpareUnsuitable, device)
	}
	if _, n := findDisk(rec.Disk, disks); n != 1 {
		return fmt.Errorf("%w: %d connected disks have the WWN, serial number and size of %s", ErrSpareUnsuitable, n, device)
	}
	s := readSpare(spareNow(rec, disks, fs.Devices, fs.HealthKnown))
	switch s.State {
	case SpareReady:
	case SpareTooSmall:
		return fmt.Errorf("%w: %s: %s", ErrSpareUnsuitable, device, s.Reason)
	case SpareInUse:
		return fmt.Errorf("%w: %s: %s", ErrSpareInUse, device, s.Reason)
	case SpareAbsent, SpareUnknown: // absent cannot be: found above
		return fmt.Errorf("check %s as hot spare: %s", device, s.Reason)
	}
	return updateSpares(ctx, func(spares map[string]spareRecord) error {
		for uuid, other := range spares {
			if uuid != fs.UUID && other.Disk.matches(rec.Disk) {
				slog.Info("dropped the hot spare of a pool that is not connected", "uuid", uuid, "device", device)
				delete(spares, uuid)
			}
		}
		spares[fs.UUID] = rec
		return nil
	})
}

// SetSpareDelay changes how long a member of pool uuid may be missing before
// its spare replaces it; ErrNoSpare when it has none.
func SetSpareDelay(ctx context.Context, uuid string, delayMinutes int) error {
	if err := checkSpareDelay(delayMinutes); err != nil {
		return err
	}
	return updateSpares(ctx, func(spares map[string]spareRecord) error {
		rec, ok := spares[uuid]
		if !ok {
			return fmt.Errorf("%w: %s", ErrNoSpare, uuid)
		}
		rec.DelayMinutes = delayMinutes
		spares[uuid] = rec
		return nil
	})
}

// ClearSpare drops the hot spare of pool uuid, if it has one, and resolves
// its alert. Call it once a replace installed the spare as a member: while
// the replace runs the designation keeps the disk reserved, and a replace
// that fails leaves it the spare.
func ClearSpare(ctx context.Context, uuid string) (bool, error) {
	cleared := false
	err := updateSpares(ctx, func(spares map[string]spareRecord) error {
		_, cleared = spares[uuid]
		delete(spares, uuid)
		return nil
	})
	if err != nil {
		return false, err
	}
	resolve(ctx, "spare-"+uuid)
	return cleared, nil
}

// SpareDisks maps the node of each disk a designation's identity names now
// to its pool's UUID, every one of them when the identity names several (the
// spare is then absent, and none of them may be taken). A spare that is not
// connected maps nothing. It reads sysfs and udev's database only, so it
// needs no privilege.
func SpareDisks() (map[string]string, error) {
	records, err := loadSpares()
	if err != nil || len(records) == 0 {
		return nil, err
	}
	disks, err := wholeDisks()
	if err != nil {
		return nil, err
	}
	present := map[string]string{}
	for uuid, rec := range records {
		for _, c := range disks {
			if rec.Disk.matches(c.disk) {
				present[c.device] = uuid
			}
		}
	}
	return present, nil
}

// attachSpares gives each of filesystems its hot spare as it is now. A state
// file that cannot be read leaves every one unknown.
func attachSpares(filesystems []BtrfsFilesystem) error {
	records, err := loadSpares()
	if err != nil {
		return err
	}
	var disks []spareCandidate
	if len(records) > 0 {
		if disks, err = wholeDisks(); err != nil {
			return err
		}
	}
	for i := range filesystems {
		fs := &filesystems[i]
		fs.spareKnown = true
		if rec, ok := records[fs.UUID]; ok {
			s := spareNow(rec, disks, fs.Devices, fs.HealthKnown)
			fs.Spare = &s
		}
	}
	return nil
}

// raiseSpareAlert warns while the spare of fs cannot serve, one occurrence
// per state, and resolves the warning once it is ready or no longer
// designated. An unknown state, or designations that could not be read,
// neither raise nor resolve.
func raiseSpareAlert(ctx context.Context, fs BtrfsFilesystem) {
	key := "spare-" + fs.UUID
	switch {
	case !fs.spareKnown:
		return
	case fs.Spare == nil || fs.Spare.State == SpareReady:
		resolve(ctx, key)
		return
	case fs.Spare.State == SpareUnknown:
		return
	}
	name := poolName(fs.Label, fs.UUID)
	obs := alerts.Observation{
		Key: key, Severity: alerts.SeverityWarning,
		Link: "/storage?pool=" + fs.UUID, OccurrenceID: string(fs.Spare.State),
	}
	switch fs.Spare.State {
	case SpareAbsent:
		obs.Title = "Hot spare of pool " + name + " is missing"
		obs.Message = "LinuxIO cannot replace a missing disk with it: " + fs.Spare.Reason + ". Reconnect it, or set another hot spare in the pool details."
	case SpareTooSmall:
		obs.Title = "Hot spare of pool " + name + " is too small"
		obs.Message = "A replace needs a disk no smaller than the one it replaces, and " + fs.Spare.Reason + ". Set a larger disk as the hot spare."
	default:
		obs.Title = "Hot spare of pool " + name + " is no longer empty"
		obs.Message = "LinuxIO uses only an empty disk as a spare, and " + fs.Spare.Reason + ". Remove the hot spare in the pool details; to use the disk again, erase it with Initialize disk and set it again."
	}
	raise(ctx, obs)
}

// checkSpareDelay refuses a delay outside 0 to MaxSpareDelayMinutes.
func checkSpareDelay(minutes int) error {
	if minutes < 0 || minutes > MaxSpareDelayMinutes {
		return fmt.Errorf("%w: the delay must be 0 to %d minutes", ErrSpareUnsuitable, MaxSpareDelayMinutes)
	}
	return nil
}

// checkSpareProfiles refuses a pool with a profile that keeps one copy, or
// whose profiles were not read: such a pool cannot run with a member missing.
func checkSpareProfiles(fs BtrfsFilesystem) error {
	if len(fs.Allocation) == 0 {
		return fmt.Errorf("%w: the profiles of pool %s could not be read", ErrSpareUnsuitable, fs.UUID)
	}
	for _, a := range fs.Allocation {
		if !slices.Contains(spareProfiles, a.Profile) {
			return fmt.Errorf("%w: %s %s cannot run with a disk missing; a pool needs raid1, raid1c3, raid1c4 or raid10 to use a hot spare", ErrSpareUnsuitable, a.Kind, a.Profile)
		}
	}
	return nil
}

// spareNow is the designation rec as it is now: the one disk of disks its
// identity names, no smaller than any of members, the pool's devices (whose
// list is complete when known), and empty by sysfs and udev (spareInUse).
// It opens no disk; readSpare does.
func spareNow(rec spareRecord, disks []spareCandidate, members []BtrfsDevice, known bool) BtrfsSpare {
	s := BtrfsSpare{Disk: rec.Disk, DelayMinutes: rec.DelayMinutes, State: SpareReady}
	device, n := findDisk(rec.Disk, disks)
	switch {
	case n == 0:
		s.State, s.Reason = SpareAbsent, "no connected disk has its WWN, serial number and size"
		return s
	case n > 1:
		s.State, s.Reason = SpareAbsent, fmt.Sprintf("%d connected disks have its WWN, serial number and size", n)
		return s
	}
	s.Device = device
	if !known {
		s.State, s.Reason = SpareUnknown, "it could not be checked: the pool's devices could not all be read"
		return s
	}
	// A missing member keeps the size the filesystem knows it by, which is
	// what the kernel compares the target with.
	if i := slices.IndexFunc(members, func(m BtrfsDevice) bool { return m.SizeBytes > rec.Disk.SizeBytes }); i >= 0 {
		s.State, s.Reason = SpareTooSmall, fmt.Sprintf("it is smaller than device %d of the pool", members[i].ID)
		return s
	}
	reason, err := spareInUse(device)
	switch {
	case err != nil:
		s.State, s.Reason = SpareUnknown, "it could not be checked: "+err.Error()
	case reason != "":
		s.State, s.Reason = SpareInUse, reason
	}
	return s
}

// signatureNames words the signatures readDiskSignature finds.
var signatureNames = map[string]string{"md": "an md RAID", "lvm": "an LVM2", "btrfs": "a btrfs"}

// spareInUse says why device is not empty, or "", by sysfs and udev's
// database alone: partitions the kernel knows, a holder, or a filesystem or
// member signature udev reported. A partition table with no partitions is
// empty, as for a replace target. An error is a check that could not be
// made.
func spareInUse(device string) (string, error) {
	name := filepath.Base(device)
	parts, err := kernelPartitions(device)
	if err != nil {
		return "", err
	}
	if len(parts) > 0 {
		return "it has partitions", nil
	}
	holders, err := os.ReadDir(filepath.Join(sysBlockDir, name, "holders"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if len(holders) > 0 {
		return "it is used by /dev/" + holders[0].Name(), nil
	}
	props, err := udevProperties(name)
	if err != nil {
		return "", err
	}
	if fsType := props["ID_FS_TYPE"]; fsType != "" {
		return "it carries a " + fsType + " signature", nil
	}
	return "", nil
}

// readSpare adds to s, when it is ready, what only the disk itself shows,
// through one exclusive open (readDiskSignature): a claim (mounted, an active
// swap, held, or one of its partitions is) or an md, LVM2 or btrfs
// signature udev has not reported, as while LinuxIO holds a disk's lock.
func readSpare(s BtrfsSpare) BtrfsSpare {
	if s.State != SpareReady {
		return s
	}
	kind, err := readDiskSignature(s.Device)
	switch {
	case errors.Is(err, unix.EBUSY):
		s.State, s.Reason = SpareInUse, "it is mounted, an active swap or held"
	case err != nil:
		s.State, s.Reason = SpareUnknown, "it could not be checked: "+err.Error()
	case kind != "":
		s.State, s.Reason = SpareInUse, "it carries "+cmp.Or(signatureNames[kind], kind)+" signature"
	}
	return s
}

// readDiskSignature reads, through one exclusive read-only open, the md or
// LVM2 signature of device as ReadMemberSignature does, then the magic of its
// primary btrfs superblock: "md", "lvm", "btrfs" or "". The kernel refuses
// the open with EBUSY while the device is mounted, an active swap or held,
// or for a whole disk while one of its partitions is. A var so tests can
// stub the device.
var readDiskSignature = func(device string) (string, error) {
	parts, err := kernelPartitions(device)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := openExclusive(device, os.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sig, err := fileSignature(f, device, parts)
	if err != nil || sig.Kind != "" {
		return sig.Kind, err
	}
	var magic [8]byte
	if _, err = f.ReadAt(magic[:], superOffset+superMagicAt); err != nil {
		if errors.Is(err, io.EOF) {
			return "", nil // smaller than a superblock
		}
		return "", fmt.Errorf("read superblock of %s: %w", device, err)
	}
	if m := binary.LittleEndian.Uint64(magic[:]); m == superMagic || m == superMagicTemporary {
		return "btrfs", nil
	}
	return "", nil
}

// spareCandidate is a whole disk with the identity udev read from it.
type spareCandidate struct {
	device string
	disk   BtrfsSpareDisk
}

// notWholeDisks are the kernel name prefixes of block devices that are no
// disk of their own: device-mapper, md, loop, network, RAM, compressed RAM,
// optical and floppy devices.
var notWholeDisks = []string{"dm-", "md", "loop", "nbd", "ram", "zram", "sr", "fd"}

// wholeDisks lists every whole disk with a size and its identity from udev's
// database. Partitions, the devices of notWholeDisks and the hidden paths of
// a multipath disk are left out; a disk udev has not processed has no
// identity.
func wholeDisks() ([]spareCandidate, error) {
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return nil, fmt.Errorf("list block devices: %w", err)
	}
	var disks []spareCandidate
	for _, e := range entries {
		name := e.Name()
		dir := filepath.Join(sysBlockDir, name)
		if slices.ContainsFunc(notWholeDisks, func(p string) bool { return strings.HasPrefix(name, p) }) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "partition")); err == nil {
			continue
		}
		if hidden, _ := os.ReadFile(filepath.Join(dir, "hidden")); strings.TrimSpace(string(hidden)) == "1" {
			continue
		}
		sectors, err := sysNumber(filepath.Join(dir, "size"))
		if err != nil || sectors == 0 {
			continue
		}
		props, err := udevProperties(name)
		if err != nil {
			slog.Debug("failed to read the udev database entry of a block device", "device", name, "error", err)
			continue
		}
		disk := BtrfsSpareDisk{WWN: cmp.Or(props["ID_WWN_WITH_EXTENSION"], props["ID_WWN"]), Serial: props["ID_SERIAL"], SizeBytes: sectors * 512}
		disks = append(disks, spareCandidate{device: "/dev/" + name, disk: disk})
	}
	return disks, nil
}

// named reports whether d has an identity to recognise the disk by.
func (d BtrfsSpareDisk) named() bool { return d.WWN != "" || d.Serial != "" }

// matches reports whether other is the disk d names: the same size and every
// identity field d records.
func (d BtrfsSpareDisk) matches(other BtrfsSpareDisk) bool {
	return d.named() && d.SizeBytes == other.SizeBytes &&
		(d.WWN == "" || d.WWN == other.WWN) && (d.Serial == "" || d.Serial == other.Serial)
}

// findDisk returns the node of the one disk of disks d names, and how many
// it names; the node is empty unless that is one.
func findDisk(d BtrfsSpareDisk, disks []spareCandidate) (string, int) {
	device, n := "", 0
	for _, c := range disks {
		if d.matches(c.disk) {
			device, n = c.device, n+1
		}
	}
	if n != 1 {
		return "", n
	}
	return device, 1
}

func loadSpares() (map[string]spareRecord, error) {
	spares := map[string]spareRecord{}
	data, err := os.ReadFile(SparePath)
	if errors.Is(err, os.ErrNotExist) {
		return spares, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &spares); err != nil {
		return nil, fmt.Errorf("parse %s: %w", SparePath, err)
	}
	return spares, nil
}

// updateSpares applies fn to the designations under the state file's lock,
// and writes them unless fn fails.
func updateSpares(ctx context.Context, fn func(map[string]spareRecord) error) error {
	return filelock.RunExclusive(ctx, SparePath+".lock", func() error {
		spares, err := loadSpares()
		if err != nil {
			return err
		}
		if err = fn(spares); err != nil {
			return err
		}
		data, err := json.Marshal(spares, jsontext.WithIndent("  "), json.Deterministic(true))
		if err != nil {
			return fmt.Errorf("marshal %s: %w", SparePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(SparePath), 0o755); err != nil {
			return err
		}
		return atomicfile.Write(SparePath, append(data, '\n'), 0o644)
	}, filelock.WithTimeout(rulesLockWait))
}
