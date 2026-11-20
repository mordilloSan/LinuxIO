package btrfs

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const rebuildDiskBytes = uint64(1) << 40

// rebuildHost is the fake host of the rebuild tests: the stubbed raid1 pool
// /data (dataUUID) of two whole disks, sda (devid 1, serial SA) and sdb
// (devid 2, serial SB), at generation 100. Disks are plugged into a sysfs
// and udev database of its own; each device's own superblock is in supers,
// which the health run's scan reads, and an exclusive open of a disk reaches
// a file holding that superblock.
type rebuildHost struct {
	t      *testing.T
	k      *fakeKernel
	sys    string
	udev   string
	devnum int
	mu     sync.Mutex
	supers map[string]Superblock
	files  map[string]string
	// replace is the replace's state as STATUS reports it.
	replace atomic.Uint64
	// modes are the source-reading mode of each REPLACE_START.
	modes []uint64
	// onReplace, when set, answers REPLACE_START in place of a replace that
	// installs its target as devid 2 and finishes.
	onReplace func(target string) error
	locked    []string
	control   *[]string
	boot      string
	// whenErased, when set, runs as the kernel is asked to forget a device,
	// right after its superblocks were erased.
	whenErased func(device string)
}

func newRebuildHost(t *testing.T) *rebuildHost {
	t.Helper()
	k := &fakeKernel{
		label: "data", maxID: 2, generation: 100,
		devices: map[uint64]*fakeDevice{
			1: {path: "/dev/sda", uuid: devUUID(1), total: rebuildDiskBytes, used: 51 << 30, physical: rebuildDiskBytes},
			2: {path: "/dev/sdb", uuid: devUUID(2), total: rebuildDiskBytes, used: 51 << 30, physical: rebuildDiskBytes},
		},
		spaces: []spaceInfo{
			{Flags: bgData | bgRaid1, TotalBytes: 50 << 30, UsedBytes: 40 << 30},
			{Flags: bgMetadata | bgRaid1, TotalBytes: 1 << 30, UsedBytes: 512 << 20},
			{Flags: bgSystem | bgRaid1, TotalBytes: 32 << 20, UsedBytes: 16 << 10},
		},
		cancelled: make(chan struct{}),
	}
	stubKernel(t, k)
	h := &rebuildHost{t: t, k: k, sys: t.TempDir(), udev: t.TempDir(), supers: map[string]Superblock{}, files: map[string]string{}, boot: "boot-1"}
	oldSys, oldUdev, oldSpares, oldSig, oldRead := sysBlockDir, udevDataDir, SparePath, readDiskSignature, readSuperblock
	oldOpen, oldLock, oldBoot, oldNumber := openRebuildTarget, lockTargetDisk, bootID, deviceNumberOf
	sysBlockDir, udevDataDir, SparePath = h.sys, h.udev, filepath.Join(t.TempDir(), "spares.json")
	readDiskSignature = func(string) (string, error) { return "", nil }
	readSuperblock = func(device string) (Superblock, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if sb, ok := h.supers[device]; ok {
			return sb, nil
		}
		return Superblock{}, errNotBtrfs
	}
	openRebuildTarget = func(device string) (*os.File, error) {
		h.mu.Lock()
		name, ok := h.files[device]
		h.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("open %s: %w", device, os.ErrNotExist)
		}
		return os.OpenFile(name, os.O_RDWR, 0)
	}
	lockTargetDisk = func(_ context.Context, device string) (func(), uint64, error) {
		h.locked = append(h.locked, device)
		rdev, err := h.number(filepath.Base(device))
		return func() {}, rdev, err
	}
	// An open reaches the disk its file is named after (setSuper).
	deviceNumberOf = func(f *os.File) (uint64, error) { return h.number(filepath.Base(f.Name())) }
	bootID = func() (string, error) { return h.boot, nil }
	t.Cleanup(func() {
		sysBlockDir, udevDataDir, SparePath, readDiskSignature, readSuperblock = oldSys, oldUdev, oldSpares, oldSig, oldRead
		openRebuildTarget, lockTargetDisk, bootID, deviceNumberOf = oldOpen, oldLock, oldBoot, oldNumber
	})
	var control []string
	h.control = &control
	oldControl := controlIoctl
	controlIoctl = func(req uintptr, device string) error {
		control = append(control, map[uintptr]string{iocScanDev: "scan", iocForgetDev: "forget"}[req]+" "+device)
		if req == iocForgetDev && h.whenErased != nil {
			h.whenErased(device)
		}
		return nil
	}
	t.Cleanup(func() { controlIoctl = oldControl })
	k.replaceStatus = func() (uint64, uint64) { return h.replace.Load(), 1000 }
	k.replaceStarted = uint64(time.Now().Add(time.Hour).Unix())
	k.replaceStart = func(a *devReplaceArgs) error {
		target := cstring(a.Params[replaceTgtDevName:])
		// The kernel stamps every replace it starts with the time.
		k.mu.Lock()
		k.replaceStarted = max(k.replaceStarted+1, uint64(time.Now().Unix()))
		k.mu.Unlock()
		h.mu.Lock()
		h.modes = append(h.modes, getU64(a.Params[:], replaceContReading))
		h.mu.Unlock()
		if h.onReplace != nil {
			return h.onReplace(target)
		}
		h.install(2, target)
		h.replace.Store(replaceStateFinished)
		return nil
	}
	h.plug("sda", "SA", memberSuper(1, 90))
	h.plug("sdb", "SB", memberSuper(2, 90))
	return h
}

// memberSuper is the superblock of devid id of the pool at generation gen.
func memberSuper(id, gen uint64) Superblock {
	return Superblock{UUID: dataUUID, Label: "data", DevID: id, DevUUID: devUUID(int(id)), NumDevices: 2, TotalBytes: 2 * rebuildDiskBytes, Generation: gen}
}

// plug connects whole disk name with serial (none when empty), carrying sb
// (no btrfs superblock when sb.UUID is empty). Each plug is a new attach,
// with a diskseq of its own.
func (h *rebuildHost) plug(name, serial string, sb Superblock) {
	h.t.Helper()
	h.devnum++
	dir := filepath.Join(h.sys, name)
	devnum := fmt.Sprintf("8:%d", h.devnum*16)
	h.write(filepath.Join(dir, "dev"), devnum+"\n")
	h.write(filepath.Join(dir, "size"), fmt.Sprintf("%d\n", rebuildDiskBytes/512))
	h.write(filepath.Join(dir, "diskseq"), fmt.Sprintf("%d\n", h.devnum))
	if err := os.MkdirAll(filepath.Join(dir, "holders"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	db := ""
	if serial != "" {
		db = "E:ID_SERIAL=" + serial + "\n"
	}
	h.write(filepath.Join(h.udev, "b"+devnum), db)
	h.setSuper("/dev/"+name, sb)
}

// number is the device number of the disk plugged as name.
func (h *rebuildHost) number(name string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(h.sys, name, "dev"))
	if err != nil {
		return 0, err
	}
	var major, minor uint32
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d:%d", &major, &minor); err != nil {
		return 0, err
	}
	return unix.Mkdev(major, minor), nil
}

// setSuper gives device the superblock sb, in what the scan reads and in the
// file an exclusive open of it reaches.
func (h *rebuildHost) setSuper(device string, sb Superblock) {
	h.t.Helper()
	b := make([]byte, superOffset+superSize)
	if sb.UUID != "" {
		raw := superblock(uuidBytes(sb.UUID), sb.Label, sb.DevID, sb.NumDevices, sb.TotalBytes)
		devUUIDBytes := uuidBytes(sb.DevUUID)
		copy(raw[superDevUUID:], devUUIDBytes[:])
		binary.LittleEndian.PutUint64(raw[superGeneration:], sb.Generation)
		copy(b[superOffset:], raw)
	}
	name := filepath.Join(h.t.TempDir(), filepath.Base(device))
	if err := os.WriteFile(name, b, 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.files[device] = name
	if sb.UUID == "" {
		delete(h.supers, device)
	} else {
		h.supers[device] = sb
	}
}

// pull disconnects disk name.
func (h *rebuildHost) pull(name string) {
	h.t.Helper()
	if err := os.RemoveAll(filepath.Join(h.sys, name)); err != nil {
		h.t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.supers, "/dev/"+name)
	delete(h.files, "/dev/"+name)
}

func (h *rebuildHost) write(path, content string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// dropOut pulls member 2's disk sdb off the bus while the pool is mounted:
// the kernel keeps the member with its path, and its link reaches nothing.
func (h *rebuildHost) dropOut() {
	h.t.Helper()
	h.pull("sdb")
	goneLink(h.t, "sdb")
}

// kernelMissing has the kernel call member 2 missing, as after a degraded
// mount without it, and then the scan of the returned disk at device give
// it that path, the member staying unwriteable.
func (h *rebuildHost) kernelMissing(device string) {
	h.t.Helper()
	if err := os.RemoveAll(filepath.Join(sysFSBtrfs, dataUUID, "devices", "sdb")); err != nil {
		h.t.Fatal(err)
	}
	h.k.devices[2].path = device
	for id, w := range map[string]string{"1": "1\n", "2": "0\n"} {
		h.write(filepath.Join(sysFSBtrfs, dataUUID, "devinfo", id, "writeable"), w)
	}
}

// install makes target devid id, as a replace that finished does: the
// kernel drops its link to the source and links the target.
func (h *rebuildHost) install(id uint64, target string) {
	devices := filepath.Join(sysFSBtrfs, dataUUID, "devices")
	_ = os.RemoveAll(filepath.Join(devices, filepath.Base(h.k.devices[id].path)))
	_ = os.MkdirAll(filepath.Join(devices, filepath.Base(target)), 0o755)
	_ = os.WriteFile(filepath.Join(sysFSBtrfs, dataUUID, "devinfo", fmt.Sprint(id), "writeable"), []byte("1\n"), 0o644)
	h.k.devices[id].path = target
}

// health runs the health check and returns what it starts.
func (h *rebuildHost) health() HealthActions {
	h.t.Helper()
	actions, err := CheckHealth(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return actions
}

// state is the pool's recorded state.
func (h *rebuildHost) state() poolState {
	h.t.Helper()
	states, err := loadPoolStates()
	if err != nil {
		h.t.Fatal(err)
	}
	return states[dataUUID]
}

// erased reports whether device's superblock on its file lost its magic.
func (h *rebuildHost) erased(device string) bool {
	h.t.Helper()
	h.mu.Lock()
	name := h.files[device]
	h.mu.Unlock()
	b, err := os.ReadFile(name)
	if err != nil {
		h.t.Fatal(err)
	}
	return binary.LittleEndian.Uint64(b[superOffset+superMagicAt:]) == 0
}

// alert returns the alert under key, if raised.
func (h *rebuildHost) alert(key string) (alerts.Alert, bool) {
	h.t.Helper()
	doc, err := alerts.Load()
	if err != nil {
		h.t.Fatal(err)
	}
	for _, a := range doc.Alerts {
		if a.Key == key {
			return a, true
		}
	}
	return alerts.Alert{}, false
}

// rebuilds says whether the health run starts the pool's rebuild.
func rebuilds(a HealthActions) bool { return slices.Contains(a.Rebuild, dataUUID) }

// returnAs brings member 2's disk back as name, under its own serial and
// carrying its superblock from before the dropout.
func (h *rebuildHost) returnAs(name string) string {
	h.plug(name, "SB", memberSuper(2, 95))
	return "/dev/" + name
}

// The disk that dropped out of the mounted pool comes back under another
// name. Two health runs in a row see it, the second starts the rebuild, and
// the rebuild erases its superblocks through an exclusive open under its
// disk lock, after writing its record, then replaces devid 2 onto it reading
// the other mirror, checks the result, and records it. The next health run
// starts the resync, the pool's disks all in use.
func TestRebuildOntoTheDiskThatCameBack(t *testing.T) { //nolint:gocognit,cyclop // one story, from the whole pool to the resync, told in order
	h := newRebuildHost(t)
	if a := h.health(); rebuilds(a) || len(a.Resync) != 0 {
		t.Fatalf("whole pool: %+v", a)
	}
	if ids := h.state().Identities; len(ids) != 2 || ids[1] != (memberIdentity{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}}) {
		t.Fatalf("identities = %+v, want each member's serial and size", ids)
	}
	h.dropOut()
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild started with no disk back")
	}
	back := h.returnAs("sdc")
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild started at the first sighting")
	}
	if r := h.filesystem().Rebuild; r == nil || r.State != RebuildWaiting || r.Device != back || r.DeviceID != 2 {
		t.Fatalf("rebuild state = %+v, want waiting on %s", r, back)
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatalf("second sighting: %+v, want the rebuild started", a)
	}

	h.whenErased = func(string) {
		if rec := h.state().Rebuild; rec == nil || rec.Device != back || rec.ID != 2 || rec.Disk.Serial != "SB" || rec.Boot != "boot-1" {
			t.Errorf("record at the erase = %+v, want the rebuild of devid 2 onto %s", rec, back)
		}
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !h.erased(back) {
		t.Fatalf("%s was not erased", back)
	}
	if want := []string{"forget " + back}; !slices.Equal(*h.control, want) {
		t.Fatalf("control calls = %v, want %v", *h.control, want)
	}
	if want := []string{"REPLACE_START 2 " + back}; !slices.Equal(h.k.Calls(), want) || !slices.Equal(h.modes, []uint64{replaceReadAvoid}) {
		t.Fatalf("calls = %v modes %v, want %v reading the other mirror", h.k.Calls(), h.modes, want)
	}
	if !slices.Equal(h.locked, []string{back}) {
		t.Fatalf("locked = %v, want the target's disk lock", h.locked)
	}
	s := h.state()
	if s.Rebuild != nil || s.Returned != nil || len(s.Rebuilt) != 1 || s.Rebuilt[0].Disk.Serial != "SB" {
		t.Fatalf("state = %+v, want no record and the disk noted rebuilt onto", s)
	}
	if i := slices.IndexFunc(s.Identities, func(m memberIdentity) bool { return m.ID == 2 }); i < 0 || s.Identities[i].Disk.Serial != "SB" {
		t.Fatalf("identities = %+v, want devid 2 recorded as the rebuilt disk", s.Identities)
	}
	if a, ok := h.alert("rebuilt-" + dataUUID); !ok || a.Severity != alerts.SeverityInfo || !strings.Contains(a.Message, back) {
		t.Fatalf("rebuilt alert = %+v, %v", a, ok)
	}
	if _, ok := h.alert("rebuild-" + dataUUID); ok {
		t.Fatal("the rebuild alert stayed up")
	}
	if a := h.health(); !slices.Equal(a.Resync, []string{dataUUID}) || rebuilds(a) {
		t.Fatalf("after the rebuild: %+v, want the resync", a)
	}
}

// filesystem reads the pool as the page does.
func (h *rebuildHost) filesystem() BtrfsFilesystem {
	h.t.Helper()
	fs, err := FindFilesystem(context.Background(), dataUUID)
	if err != nil {
		h.t.Fatal(err)
	}
	return fs
}

// In the state every kernel reaches after a degraded mount without the disk,
// the returned disk's scan names the member by its path, and the kernel then
// refuses to forget the disk once erased (EBUSY), which the rebuild accepts
// there and only there: in the dropped-out state the forget must succeed.
func TestRebuildToleratesTheRefusedForgetOnlyOfAKernelMissingMember(t *testing.T) {
	for _, kernelMissing := range []bool{true, false} {
		h := newRebuildHost(t)
		h.health()
		back := "/dev/sdc"
		if kernelMissing {
			h.pull("sdb")
			h.returnAs("sdc")
			h.kernelMissing(back)
		} else {
			h.dropOut()
			h.returnAs("sdc")
		}
		h.health()
		if a := h.health(); !rebuilds(a) {
			t.Fatalf("kernel missing %v: %+v, want the rebuild started", kernelMissing, a)
		}
		controlIoctl = func(uintptr, string) error { return fmt.Errorf("forget: %w", unix.EBUSY) }
		err := Rebuild(context.Background(), dataUUID, func(float64) {})
		if kernelMissing {
			if err != nil || len(h.k.Calls()) != 1 {
				t.Fatalf("kernel missing: %v, calls %v; want the rebuild done", err, h.k.Calls())
			}
			continue
		}
		if !scheduledrun.AllReported(err) || len(h.k.Calls()) != 0 {
			t.Fatalf("dropped out: %v, calls %v; want a reported failure and no replace", err, h.k.Calls())
		}
		if rec := h.state().Rebuild; rec == nil || !strings.Contains(rec.Failed, "could not be erased") {
			t.Fatalf("record = %+v, want the failed erase", rec)
		}
	}
}

// Rule 1: a disk is rebuilt onto only when it is the member: a whole disk
// nothing holds, whose superblock names a missing member's devid and device
// UUID at a generation no newer than the pool's, and whose WWN or serial
// and size are those recorded for that devid while the pool was whole.
// Anything else is refused with an error alert that says why, and nothing
// is written to it.
func TestRebuildRefusesADiskThatIsNotTheMember(t *testing.T) { //nolint:gocognit // a table of refusals, each checked the same way
	for _, c := range []struct {
		name  string
		setup func(h *rebuildHost) // after the dropout, before the two health runs
		want  string
	}{
		{"a clone on another disk", func(h *rebuildHost) { h.plug("sdc", "SC", memberSuper(2, 95)) }, "/dev/sdc is not the disk that was device 2"},
		{"another member's device UUID", func(h *rebuildHost) {
			sb := memberSuper(2, 95)
			sb.DevUUID = devUUID(7)
			h.plug("sdc", "SB", sb)
		}, "carries device 2's ID with another device UUID"},
		{"a newer generation", func(h *rebuildHost) { h.plug("sdc", "SB", memberSuper(2, 101)) }, "carries a newer generation (101) than the pool"},
		{"no serial", func(h *rebuildHost) { h.plug("sdc", "", memberSuper(2, 95)) }, "/dev/sdc has no WWN or serial number"},
		{"two candidates", func(h *rebuildHost) {
			h.returnAs("sdc")
			h.plug("sdd", "SD", memberSuper(2, 95))
		}, "/dev/sdc and /dev/sdd all carry the superblock of device 2"},
		{"partitioned", func(h *rebuildHost) {
			h.returnAs("sdc")
			h.write(filepath.Join(h.sys, "sdc", "sdc1", "partition"), "1\n")
			h.write(filepath.Join(h.sys, "sdc", "sdc1", "start"), "2048\n")
			h.write(filepath.Join(h.sys, "sdc", "sdc1", "size"), "2048\n")
		}, "/dev/sdc, which carries device 2's superblock, has partitions"},
		{"held", func(h *rebuildHost) {
			h.returnAs("sdc")
			h.write(filepath.Join(h.sys, "sdc", "holders", "dm-0"), "")
		}, "is used by /dev/dm-0"},
		{"no recorded identity", func(h *rebuildHost) {
			if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
				st := s[dataUUID]
				st.Identities = nil
				s[dataUUID] = st
			}); err != nil {
				h.t.Fatal(err)
			}
			h.returnAs("sdc")
		}, "LinuxIO has no record of which disk was device 2"},
		{"rebuilt onto within a day", func(h *rebuildHost) {
			if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
				st := s[dataUUID]
				st.Rebuilt = []rebuiltDisk{{Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}, At: time.Now().Add(-3 * time.Hour).Unix()}}
				s[dataUUID] = st
			}); err != nil {
				h.t.Fatal(err)
			}
			h.returnAs("sdc")
		}, "the disk keeps dropping out; replace it, or check its cable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.dropOut()
			c.setup(h)
			for i := range 3 {
				if a := h.health(); rebuilds(a) {
					t.Fatalf("health run %d started the rebuild", i+1)
				}
			}
			a, ok := h.alert("rebuild-" + dataUUID)
			if !ok || a.Severity != alerts.SeverityError || !strings.Contains(a.Message, c.want) {
				t.Fatalf("rebuild alert = %+v, %v; want an error saying %q", a, ok, c.want)
			}
			if r := h.filesystem().Rebuild; r == nil || r.State != RebuildRefused || !strings.Contains(r.Reason, c.want) {
				t.Fatalf("rebuild state = %+v, want refused", r)
			}
			// The runner decides again on what it reads, and refuses as well.
			if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) {
				t.Fatalf("Rebuild = %v, want a reported refusal", err)
			}
			if len(h.k.Calls()) != 0 || len(*h.control) != 0 || len(h.locked) != 0 {
				t.Fatalf("calls %v control %v locked %v; want nothing touched", h.k.Calls(), *h.control, h.locked)
			}
			for device := range h.files {
				if device != "/dev/sda" && h.erased(device) {
					t.Fatalf("%s was erased", device)
				}
			}
		})
	}
}

// A refusal is one occurrence however many health runs see it, so it is
// delivered once.
func TestRebuildRefusalIsOneOccurrence(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.plug("sdc", "SC", memberSuper(2, 95))
	h.health()
	first, _ := h.alert("rebuild-" + dataUUID)
	h.health()
	h.health()
	if again, _ := h.alert("rebuild-" + dataUUID); again.OccurrenceID != first.OccurrenceID || first.OccurrenceID == "" {
		t.Fatalf("occurrences %q then %q, want one", first.OccurrenceID, again.OccurrenceID)
	}
	if s := h.state(); len(s.Refused) != 1 || s.Refused[0].Disk.Serial != "SC" {
		t.Fatalf("refused = %+v, want the clone refused for the episode", s.Refused)
	}
}

// A disk the rebuild refused at its final check, here one whose own
// superblock, read through the exclusive open, no longer names the member's
// device UUID while the scan still reads it as before, is refused for the
// episode: the health runs that follow, which see nothing wrong with it,
// neither start the rebuild again nor deliver the refusal again.
func TestRebuildRefusedAtItsFinalCheckStaysRefused(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	sb := memberSuper(2, 95)
	sb.DevUUID = devUUID(5)
	h.setSuper(back, sb)
	h.mu.Lock()
	h.supers[back] = memberSuper(2, 95)
	h.mu.Unlock()
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "another device UUID") {
		t.Fatalf("Rebuild = %v, want the changed disk refused", err)
	}
	first, _ := h.alert("rebuild-" + dataUUID)
	for range 3 {
		if a := h.health(); rebuilds(a) {
			t.Fatalf("health run started the rebuild of %s again", back)
		}
	}
	if again, _ := h.alert("rebuild-" + dataUUID); again.OccurrenceID != first.OccurrenceID || !strings.Contains(again.Message, "another device UUID") {
		t.Fatalf("alert %+v, want the refusal of %+v kept", again, first)
	}
}

// A check that could not be made, as an exclusive open refused while
// something holds the disk for a moment, is no refusal: the run fails, the
// disk is not written, and the next health run starts the rebuild again.
func TestRebuildRetriesACheckItCouldNotMake(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	open := openRebuildTarget
	openRebuildTarget = func(device string) (*os.File, error) {
		return nil, fmt.Errorf("%s is in use: %w", device, unix.EBUSY)
	}
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if err == nil || scheduledrun.AllReported(err) || !errors.Is(err, unix.EBUSY) {
		t.Fatalf("Rebuild = %v, want an unreported busy error", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Refused) != 0 {
		t.Fatalf("state = %+v, want no record and no refusal", s)
	}
	openRebuildTarget = open
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the next health run did not start the rebuild again")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("Rebuild once %s is free: %v", back, err)
	}
}

// A rebuild killed between its record and its erase left the disk as it
// was: the next run starts it again, taking the disk still carrying the
// member's superblock.
func TestRebuildCutBeforeTheErase(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	if err := writeRebuildRecord(context.Background(), dataUUID, &rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}, Device: back, Started: time.Now().Unix(), Boot: "boot-1"}, nil); err != nil {
		t.Fatal(err)
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the record in flight was not settled")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !h.erased(back) || !slices.Equal(h.k.Calls(), []string{"REPLACE_START 2 " + back}) {
		t.Fatalf("erased %v calls %v, want the rebuild started again onto %s", h.erased(back), h.k.Calls(), back)
	}
	if s := h.state(); s.Rebuild != nil {
		t.Fatalf("record = %+v, want it cleared", s.Rebuild)
	}
}

// A replace on record that the rebuild did not start, the one the kernel
// reported when the record was written, is left to run.
func TestRebuildLeavesAReplaceItDidNotStart(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.k.replaceStarted = uint64(time.Now().Add(-time.Hour).Unix())
	if err := writeRebuildRecord(context.Background(), dataUUID, &rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}, Device: "/dev/sdc", Started: time.Now().Unix(), Boot: "boot-0", Prior: int64(h.k.replaceStarted)}, nil); err != nil {
		t.Fatal(err)
	}
	h.k.trees.items = []treeItem{replaceItem(2)}
	h.replace.Store(replaceStateStarted)
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !errors.Is(err, ErrNothingToRebuild) {
		t.Fatalf("Rebuild = %v, want nothing done", err)
	}
	if calls := h.k.Calls(); slices.Contains(calls, "REPLACE_CANCEL") {
		t.Fatalf("calls = %v, want the replace left to run", calls)
	}
}

// A verified rebuild whose record cannot be cleared fails its run, keeping
// the record, and the next run in that boot checks the replace again and
// clears it: a record left behind would have LinuxIO's next mount erase the
// good disk.
func TestRebuildKeepsItsRecordUntilItIsCleared(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.returnAs("sdc")
	h.health()
	h.health()
	dir := filepath.Dir(PoolStatePath)
	h.onReplace = func(target string) error {
		h.install(2, target)
		h.replace.Store(replaceStateFinished)
		return os.Chmod(dir, 0o555)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if chmodErr := os.Chmod(dir, 0o755); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	if err == nil || scheduledrun.AllReported(err) {
		t.Fatalf("Rebuild = %v, want an unreported failure", err)
	}
	if rec := h.state().Rebuild; rec == nil || rec.Failed != "" {
		t.Fatalf("record = %+v, want it kept in flight", rec)
	}
	h.onReplace = nil
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the record in flight was not settled")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("resumed Rebuild: %v", err)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Rebuilt) != 1 {
		t.Fatalf("state = %+v, want the rebuild cleared", s)
	}
}

// A replace target (devid 0) of the pool on a disk is never rebuilt onto.
func TestRebuildNeverTakesAReplaceTarget(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	sb := memberSuper(0, 95)
	sb.DevUUID = devUUID(9)
	h.plug("sdc", "SB", sb)
	h.health()
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild onto a replace target")
	}
	if s := h.state(); s.Returned != nil {
		t.Fatalf("sighting = %+v, want none", s.Returned)
	}
}

// Rule 2: two health runs in a row must see the same disk; one that goes
// away, or comes back as another, starts the count over.
func TestRebuildWaitsForTwoSightingsInARow(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	h.returnAs("sdc")
	h.health()
	h.pull("sdc")
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild of a disk gone again")
	}
	h.returnAs("sdd")
	if a := h.health(); rebuilds(a) {
		t.Fatal("rebuild at the first sighting under a new name")
	}
	if a := h.health(); !rebuilds(a) {
		t.Fatal("no rebuild at the second sighting in a row")
	}
}

// Rule 3: the switch off, a returned disk is left for the pool to take
// back at its next mount.
func TestRebuildSwitchOff(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	if err := SetAutoRebuild(context.Background(), dataUUID, false); err != nil {
		t.Fatal(err)
	}
	if fs := h.filesystem(); fs.AutoRebuild {
		t.Fatal("the pool reads the switch on")
	}
	h.dropOut()
	h.returnAs("sdc")
	for range 3 {
		if a := h.health(); rebuilds(a) {
			t.Fatal("rebuild with the switch off")
		}
	}
	if err := SetAutoRebuild(context.Background(), dataUUID, true); err != nil {
		t.Fatal(err)
	}
	if fs := h.filesystem(); !fs.AutoRebuild {
		t.Fatal("the pool reads the switch off")
	}
}

// Rule 4: nothing starts on a pool that is read-only, keeps one copy, or runs
// another operation; a busy pool waits without an alert.
func TestRebuildOnlyWhereItCanWork(t *testing.T) {
	for _, c := range []struct {
		name    string
		setup   func(h *rebuildHost)
		refused string // "" waits
	}{
		{"read-only", func(h *rebuildHost) { h.k.readOnly = true }, "the pool is mounted read-only"},
		{"single data", func(h *rebuildHost) { h.k.spaces[0].Flags = bgData }, "its data is single, which keeps one copy"},
		{"a balance", func(h *rebuildHost) {
			h.k.balanceProgress = func() (balanceArgs, error) { return balanceArgs{State: balanceStateRunning}, nil }
		}, ""},
		{"a replace", func(h *rebuildHost) { h.replace.Store(replaceStateStarted) }, ""},
		{"an exclusive operation", func(h *rebuildHost) {
			h.write(filepath.Join(sysFSBtrfs, dataUUID, "exclusive_operation"), "device remove\n")
		}, ""},
		{"the resync", func(h *rebuildHost) {
			unitActiveState = func(_ context.Context, unit string) (string, error) {
				if unit == ResyncUnitName(dataUUID) {
					return "activating", nil
				}
				return "inactive", nil
			}
		}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRebuildHost(t)
			h.health()
			h.dropOut()
			h.returnAs("sdc")
			c.setup(h)
			for range 3 {
				if a := h.health(); rebuilds(a) {
					t.Fatal("rebuild started")
				}
			}
			a, ok := h.alert("rebuild-" + dataUUID)
			switch {
			case c.refused == "" && ok:
				t.Fatalf("alert %+v while the pool is busy", a)
			case c.refused != "" && (!ok || !strings.Contains(a.Message, sentenceOf(c.refused))):
				t.Fatalf("alert = %+v, %v; want %q", a, ok, c.refused)
			}
		})
	}
}

// The final check runs again under the disk lock, through the exclusive
// open the erase uses: a disk that changed since the health run is refused
// before anything is recorded or written.
func TestRebuildChecksAgainThroughTheExclusiveOpen(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	// The disk now carries another member's superblock; the scan's view is
	// as the health run read it.
	sb := memberSuper(2, 95)
	sb.DevUUID = devUUID(5)
	h.setSuper(back, sb)
	h.mu.Lock()
	h.supers[back] = memberSuper(2, 95)
	h.mu.Unlock()
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "another device UUID") {
		t.Fatalf("Rebuild = %v, want the changed disk refused", err)
	}
	if h.state().Rebuild != nil || h.erased(back) || len(h.k.Calls()) != 0 {
		t.Fatalf("record %+v erased %v calls %v; want nothing written", h.state().Rebuild, h.erased(back), h.k.Calls())
	}
}

// With no disk back, the pool's ready spare replaces the member once it has
// been missing for the spare's delay, and its designation is dropped once
// verified. A returned disk that waits for its second sighting wins.
func TestRebuildOntoTheSpare(t *testing.T) {
	h := newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 10); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	if a := h.health(); rebuilds(a) {
		t.Fatal("spare used before its delay")
	}
	if err := SetSpareDelay(context.Background(), dataUUID, 0); err != nil {
		t.Fatal(err)
	}
	h.returnAs("sdc")
	if a := h.health(); rebuilds(a) {
		t.Fatal("spare used while a returned disk waits for its second sighting")
	}
	h.pull("sdc")
	if a := h.health(); !rebuilds(a) {
		t.Fatal("spare not used")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"REPLACE_START 2 /dev/sdd"}; !slices.Equal(h.k.Calls(), want) || !slices.Equal(h.modes, []uint64{replaceReadAvoid}) || len(*h.control) != 0 {
		t.Fatalf("calls %v modes %v control %v; want the spare taken as it is", h.k.Calls(), h.modes, *h.control)
	}
	if spares, _ := loadSpares(); len(spares) != 0 {
		t.Fatalf("spares = %+v, want the designation dropped", spares)
	}
	if ids := h.state().Identities; !slices.ContainsFunc(ids, func(m memberIdentity) bool { return m.ID == 2 && m.Disk.Serial == "SD" }) {
		t.Fatalf("identities = %+v, want devid 2 recorded as the spare it is now", ids)
	}
	if a, ok := h.alert("rebuilt-" + dataUUID); !ok || !strings.Contains(a.Message, "the hot spare /dev/sdd") {
		t.Fatalf("rebuilt alert = %+v", a)
	}
}

// Rules 6 and 8: the kernel reports the replace finished, but the pool went
// read-only, as when the target dies while the replace ends: the rebuild
// fails, keeps its record as failed, raises an error, and is not tried onto
// that disk again. A failed replace onto the spare leaves it the spare.
func TestRebuildFailureIsLoudAndFinal(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	h.onReplace = func(target string) error {
		h.install(2, target)
		h.replace.Store(replaceStateFinished)
		h.k.readOnly = true
		return nil
	}
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Rebuild = %v, want a reported failure", err)
	}
	s := h.state()
	if s.Rebuild == nil || !strings.Contains(s.Rebuild.Failed, "read-only") || len(s.Refused) != 1 || s.Refused[0].Disk.Serial != "SB" {
		t.Fatalf("state = %+v, want the failed record kept and the disk refused", s)
	}
	if a, ok := h.alert("rebuild-" + dataUUID); !ok || a.Severity != alerts.SeverityError || !strings.Contains(a.Message, "does not rebuild onto "+back+" again") {
		t.Fatalf("alert = %+v, %v", a, ok)
	}
	if r := h.filesystem().Rebuild; r == nil || r.State != RebuildFailed {
		t.Fatalf("rebuild state = %+v, want failed", r)
	}
	h.k.readOnly = false
	if a := h.health(); rebuilds(a) || len(a.Resync) != 0 {
		t.Fatalf("after the failure: %+v, want neither a rebuild nor a resync", a)
	}

	// Onto the spare.
	h = newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 0); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	if a := h.health(); !rebuilds(a) {
		t.Fatal("spare not used")
	}
	h.onReplace = func(string) error { return unix.EIO }
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) {
		t.Fatalf("Rebuild onto the spare = %v, want a reported failure", err)
	}
	if spares, _ := loadSpares(); len(spares) != 1 {
		t.Fatalf("spares = %+v, want the spare kept", spares)
	}
	if a := h.health(); rebuilds(a) {
		t.Fatal("spare tried again")
	}
}

// Rule 7: a rebuild on record whose replace still runs in the boot it
// started in, after the runner died, is followed and verified.
func TestRebuildFollowsTheReplaceItStarted(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	// The runner wrote its record, erased the disk, started the replace and
	// died; the kernel copies on.
	if err := writeRebuildRecord(context.Background(), dataUUID, &rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}, Device: back, Started: time.Now().Add(-time.Minute).Unix(), Boot: "boot-1"}, nil); err != nil {
		t.Fatal(err)
	}
	h.setSuper(back, Superblock{})
	h.k.trees.items = []treeItem{replaceItem(2)}
	h.replace.Store(replaceStateStarted)
	if a := h.health(); !rebuilds(a) {
		t.Fatal("record in flight: the unit was not started again")
	}
	if a, ok := h.alert("rebuild-" + dataUUID); !ok || a.Severity != alerts.SeverityWarning {
		t.Fatalf("alert = %+v, %v; want the running rebuild", a, ok)
	}
	var once sync.Once
	err := Rebuild(context.Background(), dataUUID, func(float64) {
		once.Do(func() {
			h.install(2, back)
			h.replace.Store(replaceStateFinished)
		})
	})
	if err != nil {
		t.Fatalf("resumed Rebuild: %v", err)
	}
	if len(h.k.Calls()) != 0 {
		t.Fatalf("calls = %v, want the kernel's replace followed, nothing started", h.k.Calls())
	}
	if s := h.state(); s.Rebuild != nil || len(s.Rebuilt) != 1 {
		t.Fatalf("state = %+v, want the rebuild verified", s)
	}
}

// Rule 7: a replace the kernel finished after a reboot, at a mount LinuxIO
// did not make, is not trusted: the pool is not called whole, the alert says
// what to do, and a ready spare replaces the member at once.
func TestRebuildDoesNotTrustAFinishNobodyWatched(t *testing.T) {
	h := newRebuildHost(t)
	h.plug("sdd", "SD", Superblock{})
	h.health()
	if err := SetSpare(context.Background(), h.filesystem(), "/dev/sdd", 60); err != nil {
		t.Fatal(err)
	}
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	h.onReplace = func(string) error { return unix.EINTR } // cut, as by a power loss
	h.replace.Store(0)
	_ = Rebuild(context.Background(), dataUUID, func(float64) {})
	// A power loss leaves no failure: forge the record back in flight.
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
		st := s[dataUUID]
		st.Rebuild.Failed, st.Refused = "", nil
		s[dataUUID] = st
	}); err != nil {
		t.Fatal(err)
	}
	h.boot = "boot-2"
	h.install(2, back)
	h.replace.Store(replaceStateFinished)
	h.k.replaceStarted = uint64(time.Now().Unix())
	if a := h.health(); !rebuilds(a) || len(a.Resync) != 0 {
		t.Fatalf("%+v, want the rebuild unit to settle the record and no resync", a)
	}
	err := Rebuild(context.Background(), dataUUID, func(float64) {})
	if !scheduledrun.AllReported(err) {
		t.Fatalf("Rebuild = %v, want a reported failure", err)
	}
	a, ok := h.alert("rebuild-" + dataUUID)
	if !ok || a.Severity != alerts.SeverityError || !strings.Contains(a.Message, "before LinuxIO could check it") || !strings.Contains(a.Message, "LinuxIO then erases that disk") {
		t.Fatalf("alert = %+v, %v", a, ok)
	}
	if acts := h.health(); !rebuilds(acts) || len(acts.Resync) != 0 {
		t.Fatalf("%+v, want the spare to take the untrusted member and no resync", acts)
	}
	// The spare's record carries the untrusted disk, which LinuxIO's mount
	// erases too while it is set.
	h.onReplace = func(target string) error {
		if rec := h.state().Rebuild; rec == nil || rec.Untrusted.Serial != "SB" {
			t.Errorf("record of the spare's rebuild = %+v, want the untrusted disk in it", rec)
		}
		h.install(2, target)
		h.replace.Store(replaceStateFinished)
		return nil
	}
	h.replace.Store(0)
	before := len(h.k.Calls())
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("Rebuild onto the spare: %v", err)
	}
	if calls := h.k.Calls()[before:]; !slices.Equal(calls, []string{"REPLACE_START 2 /dev/sdd"}) || h.modes[len(h.modes)-1] != replaceReadAvoid {
		t.Fatalf("calls %v, want devid 2 replaced onto the spare reading the other mirror", calls)
	}
	if s := h.state(); s.Rebuild != nil {
		t.Fatalf("record = %+v, want it cleared by the verified replace", s.Rebuild)
	}
}

// Rule 7: LinuxIO's own mount of a pool with a rebuild on record erases the
// recorded target first, mounts degraded, and cancels the replace the kernel
// then suspends; the next rebuild starts again onto the same disk, blank by
// then, which it finds by its hardware identity.
func TestMountOfflinePoolErasesAnUnverifiedTarget(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	h.health()
	h.health()
	h.onReplace = func(string) error { return unix.EINTR }
	_ = Rebuild(context.Background(), dataUUID, func(float64) {})
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
		st := s[dataUUID]
		st.Rebuild.Failed, st.Refused = "", nil
		s[dataUUID] = st
	}); err != nil {
		t.Fatal(err)
	}
	// After the power loss: the disk is the replace target, devid 0.
	target := memberSuper(0, 99)
	target.DevUUID = devUUID(9)
	h.setSuper(back, target)
	h.boot = "boot-2"
	pool := BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, Devices: []string{"/dev/sda", back}, devids: []uint64{1, 0}, generations: []uint64{100, 99}}
	stubMemberSignatures(t, nil)
	var mounted []string
	oldMount := mountFS
	mountFS = func(source, target, fstype string, flags uintptr, data string) error {
		mounted = append(mounted, source+" "+data)
		return nil
	}
	t.Cleanup(func() { mountFS = oldMount })
	h.replace.Store(replaceStateSuspended)
	h.k.replaceCancel = func() uint64 {
		h.replace.Store(replaceStateCanceled)
		return 0
	}
	*h.control = nil
	if err := MountOfflinePool(context.Background(), pool, t.TempDir(), nil, true); err != nil {
		t.Fatal(err)
	}
	if !h.erased(back) {
		t.Fatalf("%s was not erased before the mount", back)
	}
	if want := []string{"forget " + back, "scan /dev/sda"}; !slices.Equal(*h.control, want) {
		t.Fatalf("control = %v, want the target forgotten and only the survivor registered", *h.control)
	}
	if !slices.Equal(mounted, []string{"/dev/sda degraded"}) {
		t.Fatalf("mounts = %v, want a degraded mount", mounted)
	}
	if state := h.replace.Load(); state != replaceStateCanceled {
		t.Fatalf("replace %s after the mount, want the suspended replace canceled", replaceStateName(state))
	}
	if rec := h.state().Rebuild; rec == nil || !rec.Restart {
		t.Fatalf("record = %+v, want it marked to start again", rec)
	}

	// The member is missing now, by the kernel's own mark.
	if err := os.RemoveAll(filepath.Join(sysFSBtrfs, dataUUID, "devices", "sdb")); err != nil {
		t.Fatal(err)
	}
	h.k.devices[2].path = missingDiskName
	h.onReplace = nil
	if a := h.health(); !rebuilds(a) {
		t.Fatal("no restart")
	}
	before := len(h.k.Calls())
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if calls := h.k.Calls()[before:]; !slices.Equal(calls, []string{"REPLACE_START 2 " + back}) {
		t.Fatalf("calls = %v, want the rebuild started again onto %s", calls, back)
	}
	if s := h.state(); s.Rebuild != nil || len(s.Rebuilt) != 1 {
		t.Fatalf("state = %+v, want the restarted rebuild verified", s)
	}
}

// A rebuild to start again onto its disk that finds the disk no longer
// blank fails, keeping its record as failed, rather than being started
// again at every health run.
func TestRebuildRestartRefusedFails(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	if err := writeRebuildRecord(context.Background(), dataUUID, &rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}, Device: back, Started: 1, Boot: "boot-0", Restart: true}, nil); err != nil {
		t.Fatal(err)
	}
	other := memberSuper(1, 5)
	other.UUID = "11111111-2222-4333-8444-555555555555"
	h.setSuper(back, other)
	if a := h.health(); !rebuilds(a) {
		t.Fatal("the restart was not started")
	}
	if err := Rebuild(context.Background(), dataUUID, func(float64) {}); !scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "is no longer blank") {
		t.Fatalf("Rebuild = %v, want the restart failed", err)
	}
	if rec := h.state().Rebuild; rec == nil || !strings.Contains(rec.Failed, "is no longer blank") {
		t.Fatalf("record = %+v, want it failed", rec)
	}
	if a := h.health(); rebuilds(a) {
		t.Fatal("the failed restart is started again")
	}
	if h.erased(back) || len(h.k.Calls()) != 0 {
		t.Fatalf("erased %v calls %v, want nothing written", h.erased(back), h.k.Calls())
	}
}

// A disk udev has not reported yet has no identity to check: it waits,
// with no refusal, and is rebuilt onto once udev reports it.
func TestRebuildWaitsForUdev(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	h.dropOut()
	back := h.returnAs("sdc")
	entry := filepath.Join(h.udev, "b8:48")
	saved, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if a := h.health(); rebuilds(a) {
			t.Fatal("rebuild of a disk udev has not reported")
		}
	}
	if s := h.state(); len(s.Refused) != 0 || s.RebuildRefusal != "" {
		t.Fatalf("state = %+v, want no refusal", s)
	}
	if _, ok := h.alert("rebuild-" + dataUUID); ok {
		t.Fatal("an alert while the disk waits for udev")
	}
	h.write(entry, string(saved))
	h.health()
	if a := h.health(); !rebuilds(a) {
		t.Fatalf("no rebuild of %s once udev reported it", back)
	}
}

// The boot check meets the pool after a power loss mid-rebuild: one member
// and the replace target (devid 0). Without a record it refuses a target
// that missed writes; with the rebuild on record it erases the target, mounts
// degraded and cancels the suspended replace.
func TestMountDegradedPoolsWithAReplaceTarget(t *testing.T) { //nolint:gocognit // the same boot told with and without the record
	for _, recorded := range []bool{false, true} {
		h := newRebuildHost(t)
		h.health()
		h.pull("sda")
		h.pull("sdb")
		target := memberSuper(0, 97)
		target.DevUUID = devUUID(9)
		h.plug("sda", "SA", memberSuper(1, 100))
		h.plug("sdc", "SB", target)
		stubMounts(t, nil)
		stubMemberSignatures(t, nil)
		stubPeekSignatures(t, nil)
		mnt := filepath.Join(t.TempDir(), "data")
		h.write(FstabPath, "UUID="+dataUUID+" "+mnt+" btrfs defaults,nofail 0 0\n")
		if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
			st := s[dataUUID]
			st.Generation = 100
			if recorded {
				st.Rebuild = &rebuildRecord{ID: 2, UUID: devUUID(2), Disk: BtrfsSpareDisk{Serial: "SB", SizeBytes: rebuildDiskBytes}, Device: "/dev/sdc", Started: 1, Boot: "boot-0"}
			}
			s[dataUUID] = st
		}); err != nil {
			t.Fatal(err)
		}
		var mounted []string
		oldMount := mountFS
		mountFS = func(source, mnt, fstype string, flags uintptr, data string) error {
			mounted = append(mounted, source+" "+data)
			stubMounts(t, []Mount{{Mountpoint: mnt, UUID: dataUUID, SubvolID: 5}})
			return nil
		}
		t.Cleanup(func() { mountFS = oldMount })
		h.replace.Store(replaceStateSuspended)
		h.k.replaceCancel = func() uint64 {
			h.replace.Store(replaceStateCanceled)
			return 0
		}
		err := MountDegradedPools(context.Background(), 0)
		if !recorded {
			if err != nil || len(mounted) != 0 || h.erased("/dev/sdc") {
				t.Fatalf("without a record: %v, mounts %v, erased %v; want the pool left unmounted", err, mounted, h.erased("/dev/sdc"))
			}
			if a, ok := h.alert("offline-" + dataUUID); !ok || !strings.Contains(a.Message, "the replace target /dev/sdc missed writes") {
				t.Fatalf("offline alert = %+v, %v", a, ok)
			}
			continue
		}
		if err != nil || !slices.Equal(mounted, []string{"/dev/sda degraded"}) || !h.erased("/dev/sdc") {
			t.Fatalf("with a record: %v, mounts %v, erased %v; want the target erased and a degraded mount", err, mounted, h.erased("/dev/sdc"))
		}
		if state := h.replace.Load(); state != replaceStateCanceled {
			t.Fatalf("replace %s after the mount, want the suspended replace canceled", replaceStateName(state))
		}
		if rec := h.state().Rebuild; rec == nil || !rec.Restart {
			t.Fatalf("record = %+v, want it marked to start again", rec)
		}
	}
}

// The resync never runs, nor declares the pool whole, while a rebuild is on
// record unverified, and that waiting counts no failed attempt.
func TestResyncWaitsForAnUnverifiedRebuild(t *testing.T) {
	h := newRebuildHost(t)
	h.health()
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
		st := s[dataUUID]
		st.Degraded, st.Rebuild = true, &rebuildRecord{ID: 2, UUID: devUUID(2), Device: "/dev/sdc", Failed: "x"}
		s[dataUUID] = st
	}); err != nil {
		t.Fatal(err)
	}
	if err := Resync(context.Background(), dataUUID, nil); !errors.Is(err, errRebuildPending) {
		t.Fatalf("Resync = %v, want %v", err, errRebuildPending)
	}
	if s := h.state(); s.ResyncFailures != 0 || !s.Degraded {
		t.Fatalf("state = %+v, want no failure counted", s)
	}
}
