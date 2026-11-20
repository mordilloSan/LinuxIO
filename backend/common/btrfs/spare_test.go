package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

const spareTiB = uint64(1) << 40

// spareFixture is a block device of the fake host the spare tests build:
// its sysfs entry and what udev's database says of it.
type spareFixture struct {
	name    string
	size    uint64
	props   map[string]string
	parts   int      // partitions the kernel knows on it
	holders []string // its sysfs holders
	isPart  bool     // a partition itself
	hidden  bool     // a hidden multipath path
}

// disk is a whole disk of size bytes whose udev entry carries wwn and serial.
func disk(name string, size uint64, wwn, serial string) spareFixture {
	props := map[string]string{}
	if wwn != "" {
		props["ID_WWN"] = wwn
	}
	if serial != "" {
		props["ID_SERIAL"] = serial
	}
	return spareFixture{name: name, size: size, props: props}
}

// spareHost points sysfs, udev's database and SparePath at a fake host with
// devices, and the disks' own signature read at signatures (device to
// "md", "lvm", "btrfs" or an error); every other disk reads empty.
func spareHost(t *testing.T, signatures map[string]any, devices ...spareFixture) {
	t.Helper()
	sys, udev := t.TempDir(), t.TempDir()
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range devices {
		dir := filepath.Join(sys, d.name)
		devnum := fmt.Sprintf("8:%d", i*16)
		write(filepath.Join(dir, "dev"), devnum+"\n")
		write(filepath.Join(dir, "size"), fmt.Sprintf("%d\n", d.size/512))
		if d.isPart {
			write(filepath.Join(dir, "partition"), "1\n")
		}
		if d.hidden {
			write(filepath.Join(dir, "hidden"), "1\n")
		}
		for p := 1; p <= d.parts; p++ {
			part := filepath.Join(dir, partitionPath(d.name, p))
			write(filepath.Join(part, "partition"), fmt.Sprintf("%d\n", p))
			write(filepath.Join(part, "start"), "2048\n")
			write(filepath.Join(part, "size"), "2048\n")
		}
		for _, h := range d.holders {
			write(filepath.Join(dir, "holders", h), "")
		}
		if d.props != nil {
			var db strings.Builder
			for k, v := range d.props {
				db.WriteString("E:" + k + "=" + v + "\n")
			}
			write(filepath.Join(udev, "b"+devnum), db.String())
		}
	}
	oldSys, oldUdev, oldPath, oldRead := sysBlockDir, udevDataDir, SparePath, readDiskSignature
	sysBlockDir, udevDataDir, SparePath = sys, udev, filepath.Join(t.TempDir(), "spares.json")
	readDiskSignature = func(device string) (string, error) {
		switch v := signatures[device].(type) {
		case string:
			return v, nil
		case error:
			return "", v
		}
		return "", nil
	}
	t.Cleanup(func() { sysBlockDir, udevDataDir, SparePath, readDiskSignature = oldSys, oldUdev, oldPath, oldRead })
}

// mirror is a mounted raid1 pool of two members of memberSize bytes.
func mirror(memberSize uint64) BtrfsFilesystem {
	return BtrfsFilesystem{
		UUID: dataUUID, Label: "data", HealthKnown: true,
		Allocation: []BtrfsAllocation{{Kind: "data", Profile: "raid1"}, {Kind: "metadata", Profile: "raid1"}, {Kind: "system", Profile: "raid1"}},
		Devices: []BtrfsDevice{
			{ID: 1, Path: "/dev/sda", SizeBytes: memberSize},
			{ID: 2, Path: "/dev/sdb", SizeBytes: memberSize},
		},
	}
}

func readSpareFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(SparePath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The designation names the disk by what udev reads from it, with its size,
// and is stored, never written to the disk.
func TestSetSpareRecordsTheDiskByIdentity(t *testing.T) {
	spareHost(t, nil, disk("sda", spareTiB, "", "A"), disk("sdb", spareTiB, "", "B"), disk("sdc", 2*spareTiB, "0x5000c500a1b2c3d4", "ST2000_ZFN0C"))
	ctx := context.Background()

	if err := SetSpare(ctx, mirror(spareTiB), "/dev/sdc", 15); err != nil {
		t.Fatalf("SetSpare: %v", err)
	}
	want := `{
  "` + dataUUID + `": {
    "disk": {
      "wwn": "0x5000c500a1b2c3d4",
      "serial": "ST2000_ZFN0C",
      "sizeBytes": 2199023255552
    },
    "delayMinutes": 15
  }
}
`
	if got := readSpareFile(t); got != want {
		t.Errorf("state file =\n%s\nwant\n%s", got, want)
	}

	if err := SetSpareDelay(ctx, dataUUID, 0); err != nil {
		t.Fatalf("SetSpareDelay: %v", err)
	}
	if got := readSpareFile(t); !strings.Contains(got, `"delayMinutes": 0`) {
		t.Errorf("delay not changed:\n%s", got)
	}
	if err := SetSpareDelay(ctx, "other-uuid", 5); !errors.Is(err, ErrNoSpare) {
		t.Errorf("delay of a pool without a spare: err = %v, want ErrNoSpare", err)
	}

	cleared, err := ClearSpare(ctx, dataUUID)
	if err != nil || !cleared {
		t.Fatalf("ClearSpare = %v, %v", cleared, err)
	}
	if got := readSpareFile(t); got != "{}\n" {
		t.Errorf("state file after clear = %q", got)
	}
	if cleared, err = ClearSpare(ctx, dataUUID); err != nil || cleared {
		t.Errorf("second ClearSpare = %v, %v, want false, nil", cleared, err)
	}
}

func TestSetSpareRefuses(t *testing.T) {
	busy := fmt.Errorf("/dev/sdc is in use: %w", unix.EBUSY)
	single := mirror(spareTiB)
	single.Allocation[0].Profile = "single"
	small := disk("sdc", spareTiB/2, "", "SMALL")
	tests := []struct {
		name    string
		fs      BtrfsFilesystem
		sigs    map[string]any
		devices []spareFixture
		device  string
		delay   int
		want    error
		says    string
	}{
		{name: "negative delay", fs: mirror(spareTiB), devices: []spareFixture{disk("sdc", 2*spareTiB, "", "S")}, device: "/dev/sdc", delay: -1, want: ErrSpareUnsuitable, says: "delay"},
		{name: "delay over a week", fs: mirror(spareTiB), devices: []spareFixture{disk("sdc", 2*spareTiB, "", "S")}, device: "/dev/sdc", delay: MaxSpareDelayMinutes + 1, want: ErrSpareUnsuitable, says: "delay"},
		{name: "a pool that keeps one copy", fs: single, devices: []spareFixture{disk("sdc", 2*spareTiB, "", "S")}, device: "/dev/sdc", want: ErrSpareUnsuitable, says: "data single"},
		{name: "a partition", fs: mirror(spareTiB), devices: []spareFixture{{name: "sdc1", size: 2 * spareTiB, props: map[string]string{"ID_SERIAL": "S"}, isPart: true}}, device: "/dev/sdc1", want: ErrSpareUnsuitable, says: "not a whole disk"},
		{name: "a device-mapper device", fs: mirror(spareTiB), devices: []spareFixture{{name: "dm-0", size: 2 * spareTiB, props: map[string]string{"ID_SERIAL": "S"}}}, device: "/dev/dm-0", want: ErrSpareUnsuitable, says: "not a whole disk"},
		{name: "no serial or WWN", fs: mirror(spareTiB), devices: []spareFixture{disk("sdc", 2*spareTiB, "", "")}, device: "/dev/sdc", want: ErrSpareUnsuitable, says: "no WWN or serial"},
		{name: "a serial two disks share", fs: mirror(spareTiB), devices: []spareFixture{disk("sdc", 2*spareTiB, "", "SAME"), disk("sdd", 2*spareTiB, "", "SAME")}, device: "/dev/sdc", want: ErrSpareUnsuitable, says: "2 connected disks"},
		{name: "smaller than a member", fs: mirror(spareTiB), devices: []spareFixture{small}, device: "/dev/sdc", want: ErrSpareUnsuitable, says: "smaller than device"},
		{name: "partitions", fs: mirror(spareTiB), devices: []spareFixture{{name: "sdc", size: 2 * spareTiB, props: map[string]string{"ID_SERIAL": "S"}, parts: 1}}, device: "/dev/sdc", want: ErrSpareInUse, says: "has partitions"},
		{name: "a holder", fs: mirror(spareTiB), devices: []spareFixture{{name: "sdc", size: 2 * spareTiB, props: map[string]string{"ID_SERIAL": "S"}, holders: []string{"dm-3"}}}, device: "/dev/sdc", want: ErrSpareInUse, says: "used by /dev/dm-3"},
		{name: "a filesystem udev reported", fs: mirror(spareTiB), devices: []spareFixture{{name: "sdc", size: 2 * spareTiB, props: map[string]string{"ID_SERIAL": "S", "ID_FS_TYPE": "ext4"}}}, device: "/dev/sdc", want: ErrSpareInUse, says: "ext4 signature"},
		{name: "an md signature udev did not report", fs: mirror(spareTiB), sigs: map[string]any{"/dev/sdc": "md"}, devices: []spareFixture{disk("sdc", 2*spareTiB, "", "S")}, device: "/dev/sdc", want: ErrSpareInUse, says: "md RAID signature"},
		{name: "a btrfs superblock", fs: mirror(spareTiB), sigs: map[string]any{"/dev/sdc": "btrfs"}, devices: []spareFixture{disk("sdc", 2*spareTiB, "", "S")}, device: "/dev/sdc", want: ErrSpareInUse, says: "btrfs signature"},
		{name: "claimed", fs: mirror(spareTiB), sigs: map[string]any{"/dev/sdc": busy}, devices: []spareFixture{disk("sdc", 2*spareTiB, "", "S")}, device: "/dev/sdc", want: ErrSpareInUse, says: "mounted, an active swap or held"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spareHost(t, tc.sigs, tc.devices...)
			err := SetSpare(context.Background(), tc.fs, tc.device, tc.delay)
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("SetSpare(%s) = %v, want %v saying %q", tc.device, err, tc.want, tc.says)
			}
			if records, loadErr := loadSpares(); loadErr != nil || records[tc.fs.UUID] != (spareRecord{}) {
				t.Errorf("refused spare recorded: %v, %v", records, loadErr)
			}
		})
	}
}

// A disk is the spare of one pool at a time: setting it for another pool
// takes it from the first, whose designation may be stale (a pool destroyed
// outside LinuxIO). The bridge refuses the spare of a pool that exists
// before it gets here (deviceInUse).
func TestSetSpareKeepsADiskToOnePool(t *testing.T) {
	spareHost(t, nil, disk("sdd", 2*spareTiB, "", "TWIN"))
	ctx := context.Background()
	other := mirror(spareTiB)
	other.UUID = "other-uuid"
	if err := SetSpare(ctx, other, "/dev/sdd", 10); err != nil {
		t.Fatal(err)
	}
	if err := SetSpare(ctx, mirror(spareTiB), "/dev/sdd", 5); err != nil {
		t.Fatal(err)
	}
	records, err := loadSpares()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := records["other-uuid"]; ok || records[dataUUID].DelayMinutes != 5 || len(records) != 1 {
		t.Errorf("records = %+v, want the disk kept for %s alone", records, dataUUID)
	}
}

// The spare is found by its identity whatever the kernel names it now, and
// is absent rather than any other disk when its identity names none or
// several; it is checked against the pool and the disk itself each time.
func TestSpareStates(t *testing.T) {
	designate := func(t *testing.T, device string) {
		t.Helper()
		if err := SetSpare(context.Background(), mirror(spareTiB), device, 10); err != nil {
			t.Fatal(err)
		}
	}
	spare := disk("sdc", 2*spareTiB, "0x5000", "SPARE")
	tests := []struct {
		name    string
		after   []spareFixture // the host when the state is read
		sigs    map[string]any
		members uint64
		known   bool
		state   BtrfsSpareState
		device  string
		says    string
	}{
		{name: "ready", after: []spareFixture{spare}, members: spareTiB, known: true, state: SpareReady, device: "/dev/sdc"},
		{name: "renamed", after: []spareFixture{disk("sdf", 2*spareTiB, "0x5000", "SPARE")}, members: spareTiB, known: true, state: SpareReady, device: "/dev/sdf"},
		{name: "unplugged", after: []spareFixture{disk("sdc", 2*spareTiB, "0x6000", "OTHER")}, members: spareTiB, known: true, state: SpareAbsent, says: "no connected disk"},
		{name: "another size", after: []spareFixture{disk("sdc", 3*spareTiB, "0x5000", "SPARE")}, members: spareTiB, known: true, state: SpareAbsent, says: "no connected disk"},
		{name: "two disks", after: []spareFixture{spare, disk("sdd", 2*spareTiB, "0x5000", "SPARE")}, members: spareTiB, known: true, state: SpareAbsent, says: "2 connected disks"},
		{name: "a member grew", after: []spareFixture{spare}, members: 3 * spareTiB, known: true, state: SpareTooSmall, device: "/dev/sdc", says: "smaller than device 1"},
		{name: "written outside LinuxIO", after: []spareFixture{{name: "sdc", size: 2 * spareTiB, props: map[string]string{"ID_WWN": "0x5000", "ID_SERIAL": "SPARE", "ID_FS_TYPE": "LVM2_member"}}}, members: spareTiB, known: true, state: SpareInUse, device: "/dev/sdc", says: "LVM2_member signature"},
		{name: "partitioned outside LinuxIO", after: []spareFixture{{name: "sdc", size: 2 * spareTiB, props: map[string]string{"ID_WWN": "0x5000", "ID_SERIAL": "SPARE"}, parts: 1}}, members: spareTiB, known: true, state: SpareInUse, device: "/dev/sdc", says: "has partitions"},
		// A read of the pool opens no disk: what only the disk itself shows
		// waits for SpareOf.
		{name: "a signature udev has not reported", after: []spareFixture{spare}, sigs: map[string]any{"/dev/sdc": "lvm"}, members: spareTiB, known: true, state: SpareReady, device: "/dev/sdc"},
		{name: "the pool's disks unread", after: []spareFixture{spare}, members: spareTiB, known: false, state: SpareUnknown, device: "/dev/sdc", says: "could not all be read"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spareHost(t, nil, spare)
			designate(t, "/dev/sdc")
			path := SparePath
			spareHost(t, tc.sigs, tc.after...)
			SparePath = path

			fs := mirror(tc.members)
			fs.HealthKnown = tc.known
			filesystems := []BtrfsFilesystem{fs, {UUID: "no-spare-uuid", HealthKnown: true}}
			if err := attachSpares(filesystems); err != nil {
				t.Fatal(err)
			}
			got := filesystems[0].Spare
			if got == nil {
				t.Fatal("no spare attached")
			}
			if got.State != tc.state || got.Device != tc.device || !strings.Contains(got.Reason, tc.says) || got.DelayMinutes != 10 || got.Disk.Serial != "SPARE" {
				t.Errorf("spare = %+v, want state %s on %q saying %q", *got, tc.state, tc.device, tc.says)
			}
			if filesystems[1].Spare != nil || !filesystems[1].spareKnown {
				t.Errorf("pool without a spare: %+v, known %v", filesystems[1].Spare, filesystems[1].spareKnown)
			}
		})
	}
}

// SpareDisks maps each designated disk present to its pool, for the
// inventory's reservation, every one of them when its identity is shared;
// an absent one maps nothing.
func TestSpareDisks(t *testing.T) {
	spareHost(t, nil, disk("sdc", 2*spareTiB, "", "C"), disk("sdd", 2*spareTiB, "", "D"))
	ctx := context.Background()
	if err := SetSpare(ctx, mirror(spareTiB), "/dev/sdc", 10); err != nil {
		t.Fatal(err)
	}
	other := mirror(spareTiB)
	other.UUID = "other-uuid"
	if err := SetSpare(ctx, other, "/dev/sdd", 10); err != nil {
		t.Fatal(err)
	}
	path := SparePath
	spareHost(t, nil, disk("sdc", 2*spareTiB, "", "C"))
	SparePath = path

	got, err := SpareDisks()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["/dev/sdc"] != dataUUID {
		t.Errorf("SpareDisks = %v, want only /dev/sdc of %s", got, dataUUID)
	}

	// A second disk with the spare's identity, such as a clone: the spare is
	// absent, and neither disk may be taken by anything else.
	spareHost(t, nil, disk("sdc", 2*spareTiB, "", "C"), disk("sde", 2*spareTiB, "", "C"))
	SparePath = path
	if got, err = SpareDisks(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["/dev/sdc"] != dataUUID || got["/dev/sde"] != dataUUID {
		t.Errorf("SpareDisks with a shared identity = %v, want both disks of %s", got, dataUUID)
	}
}

// A spare that cannot serve raises a warning on its pool, one occurrence per
// state, resolved once it is ready or gone; an unknown state leaves it.
func TestSpareAlert(t *testing.T) {
	stubAlerts(t)
	ctx := context.Background()
	key := "spare-" + dataUUID
	fs := mirror(spareTiB)
	fs.spareKnown = true

	fs.Spare = &BtrfsSpare{State: SpareAbsent, Reason: "no connected disk has its WWN, serial number and size"}
	raiseSpareAlert(ctx, fs)
	if got := alertKeys(t)[key]; got != alerts.SeverityWarning {
		t.Fatalf("absent spare: alert severity %q, want warning", got)
	}
	fs.Spare.State = SpareUnknown
	raiseSpareAlert(ctx, fs)
	if _, ok := alertKeys(t)[key]; !ok {
		t.Fatal("an unknown state resolved the alert")
	}
	fs.Spare.State = SpareReady
	raiseSpareAlert(ctx, fs)
	if _, ok := alertKeys(t)[key]; ok {
		t.Fatal("a ready spare kept the alert")
	}

	fs.Spare.State = SpareInUse
	raiseSpareAlert(ctx, fs)
	fs.Spare = nil
	fs.spareKnown = false
	raiseSpareAlert(ctx, fs)
	if _, ok := alertKeys(t)[key]; !ok {
		t.Fatal("an unread state file resolved the alert")
	}
	fs.spareKnown = true
	raiseSpareAlert(ctx, fs)
	if _, ok := alertKeys(t)[key]; ok {
		t.Fatal("a cleared spare kept the alert")
	}
}

// RaiseAlerts carries the spare alert, and the health run drops it with
// the rest of a pool that is gone.
func TestRaiseAlertsCarriesTheSpareAlert(t *testing.T) {
	stubAlerts(t)
	ctx := context.Background()
	fs := mirror(spareTiB)
	fs.spareKnown = true
	fs.Spare = &BtrfsSpare{State: SpareTooSmall, Reason: "it is smaller than device 1 of the pool"}
	RaiseAlerts(ctx, fs)
	if _, ok := alertKeys(t)["spare-"+dataUUID]; !ok {
		t.Fatal("RaiseAlerts raised no spare alert")
	}
	resolveStale(ctx, map[string]bool{})
	if _, ok := alertKeys(t)["spare-"+dataUUID]; ok {
		t.Fatal("the spare alert of a pool that is gone stayed")
	}
}

// SpareOf, right before a replace, reads the disk itself through an
// exclusive open, which a read of the pool never does.
func TestSpareOfReadsTheDiskItself(t *testing.T) {
	stubKernel(t, raid1Kernel())
	spareHost(t, nil, disk("sdc", 200<<30, "", "S"))
	ctx := context.Background()
	fs, err := FindFilesystem(ctx, dataUUID)
	if err != nil {
		t.Fatal(err)
	}
	if err = SetSpare(ctx, fs, "/dev/sdc", 10); err != nil {
		t.Fatal(err)
	}
	busy := fmt.Errorf("/dev/sdc is in use: %w", unix.EBUSY)
	for _, c := range []struct {
		read  any
		state BtrfsSpareState
		says  string
	}{
		{"", SpareReady, ""},
		{"lvm", SpareInUse, "an LVM2 signature"},
		{busy, SpareInUse, "mounted, an active swap or held"},
		{os.ErrPermission, SpareUnknown, "could not be checked"},
	} {
		opened := 0
		readDiskSignature = func(device string) (string, error) {
			opened++
			switch v := c.read.(type) {
			case error:
				return "", v
			case string:
				return v, nil
			}
			return "", nil
		}
		if fs, err = FindFilesystem(ctx, dataUUID); err != nil || fs.Spare == nil || fs.Spare.State != SpareReady || opened != 0 {
			t.Fatalf("read of the pool: spare %+v, err %v, %d opens of the disk, want ready and none", fs.Spare, err, opened)
		}
		spare, err := SpareOf(ctx, dataUUID)
		if err != nil || spare.State != c.state || !strings.Contains(spare.Reason, c.says) || opened != 1 {
			t.Errorf("SpareOf with the disk reading %v = %+v, %v after %d opens, want %s saying %q", c.read, spare, err, opened, c.state, c.says)
		}
	}
}

// ListFilesystems carries each pool's spare.
func TestListFilesystemsCarriesTheSpare(t *testing.T) {
	stubKernel(t, raid1Kernel())
	spareHost(t, nil, disk("sdc", 200<<30, "", "S"))
	ctx := context.Background()
	fs, err := FindFilesystem(ctx, dataUUID)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Spare != nil {
		t.Fatalf("spare before one is set: %+v", fs.Spare)
	}
	if err = SetSpare(ctx, fs, "/dev/sdc", 10); err != nil {
		t.Fatal(err)
	}
	spare, err := SpareOf(ctx, dataUUID)
	if err != nil || spare.State != SpareReady || spare.Device != "/dev/sdc" {
		t.Fatalf("SpareOf = %+v, %v", spare, err)
	}
	if _, err = SpareOf(ctx, "other-uuid"); err == nil {
		t.Error("SpareOf of a pool that is not mounted succeeded")
	}
	if _, err = ClearSpare(ctx, dataUUID); err != nil {
		t.Fatal(err)
	}
	if _, err = SpareOf(ctx, dataUUID); !errors.Is(err, ErrNoSpare) {
		t.Errorf("SpareOf after clear: err = %v, want ErrNoSpare", err)
	}
	// Designations that cannot be read are an error, never "no spare".
	if err = os.WriteFile(SparePath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = SpareOf(ctx, dataUUID); err == nil || errors.Is(err, ErrNoSpare) {
		t.Errorf("SpareOf with an unreadable state file: err = %v, want an error that is not ErrNoSpare", err)
	}
}
