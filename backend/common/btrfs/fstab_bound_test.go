package btrfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const otherUUID = "11111111-2222-4333-8444-555555555555"

func TestUnboundFstab(t *testing.T) {
	const bound = ",x-systemd.device-bound=false"
	cases := []struct {
		name, in, want string
		edited         []string
	}{
		{
			"a pool's line gains the option, the rest stays byte for byte",
			"# /etc/fstab\n\nUUID=" + otherUUID + " /srv ext4 defaults 0 2\nUUID=" + dataUUID + "\t/data  btrfs\tdefaults,noatime,nofail  0 0\n",
			"# /etc/fstab\n\nUUID=" + otherUUID + " /srv ext4 defaults 0 2\nUUID=" + dataUUID + "\t/data  btrfs\tdefaults,noatime,nofail" + bound + "  0 0\n",
			[]string{"/data"},
		},
		{
			"every line of the pool, as for its subvolumes, and a last line without a newline",
			"UUID=" + dataUUID + " /data btrfs defaults,subvol=@data 0 0\nuuid=" + dataUUID + " /media btrfs subvol=@media",
			"UUID=" + dataUUID + " /data btrfs defaults,subvol=@data" + bound + " 0 0\nuuid=" + dataUUID + " /media btrfs subvol=@media" + bound,
			[]string{"/data", "/media"},
		},
		{"a line that says so already", "UUID=" + dataUUID + " /data btrfs defaults,x-systemd.device-bound=false 0 0\n", "", nil},
		{"a line an administrator bound", "UUID=" + dataUUID + " /data btrfs defaults,x-systemd.device-bound 0 0\n", "", nil},
		{"a line an administrator bound with a value", "UUID=" + dataUUID + " /data btrfs x-systemd.device-bound=yes,noatime 0 0\n", "", nil},
		{"another filesystem's line", "UUID=" + otherUUID + " /data btrfs defaults 0 0\n", "", nil},
		{"a line that names the pool another way", "LABEL=tank /data btrfs defaults 0 0\n/dev/sdb /data2 btrfs defaults 0 0\n", "", nil},
		{"a commented line", "#UUID=" + dataUUID + " /data btrfs defaults 0 0\n# UUID=" + dataUUID + " /data btrfs defaults 0 0\n", "", nil},
		{"a line of another type", "UUID=" + dataUUID + " /data auto defaults 0 0\n", "", nil},
		{"a line too short to be an entry", "UUID=" + dataUUID + " /data btrfs\n", "", nil},
		{
			"a line ending in CR LF, and a mountpoint with an escaped blank",
			"UUID=" + dataUUID + " /mnt/my\\040disk btrfs defaults\r\n",
			"UUID=" + dataUUID + " /mnt/my\\040disk btrfs defaults" + bound + "\r\n",
			[]string{"/mnt/my disk"},
		},
		// Blanks no reader of fstab splits on leave fewer fields than an entry has.
		{"fields apart by a no-break space", "UUID=" + dataUUID + "\u00a0/data\u00a0btrfs\u00a0defaults 0 0\n", "", nil},
		{"fields apart by a vertical tab", "UUID=" + dataUUID + "\v/data\vbtrfs\vdefaults\v0\v0\n", "", nil},
		{"a line that names no UUID", "UUID= /data btrfs defaults 0 0\n", "", nil},
		{"a comment where the options would be", "UUID=" + dataUUID + " /data btrfs # no options yet\n", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, edited := unboundFstab(c.in, map[string]bool{dataUUID: true, "": true})
			want := c.want
			if want == "" {
				want = c.in
			}
			if got != want {
				t.Errorf("fstab:\n%q\nwant:\n%q", got, want)
			}
			if len(edited) != len(c.edited) {
				t.Fatalf("edited = %v, want %v", edited, c.edited)
			}
			for i := range edited {
				if edited[i] != c.edited[i] {
					t.Errorf("edited = %v, want %v", edited, c.edited)
				}
			}
		})
	}
}

// boundEnv is what unbindPoolMounts reads and calls.
type boundEnv struct {
	// version is systemd's, versionErr a manager that does not say.
	version    int
	versionErr error
	reloads    int
	reloadErr  error
	// stops answers for a mount unit: the devices its stop follows.
	stops    map[string][]string
	stopsErr error
	// recorded are the mounts handed to mount(8) to record, as
	// "source mountpoint options"; record is what it then does.
	recorded []string
	record   func(mountpoint string) error
}

func stubBound(t *testing.T) *boundEnv {
	t.Helper()
	dir := t.TempDir()
	stubStatePaths(t, dir)
	env := &boundEnv{stops: map[string][]string{}, version: 255}
	// As systemd does once the record names the mount's source.
	env.record = func(mountpoint string) error {
		delete(env.stops, strings.TrimPrefix(mountpoint, "/")+".mount")
		return nil
	}
	oldLock, oldReload, oldStops, oldRecord, oldSettle, oldUtab, oldVersion := FstabLockPath, unitDaemonReload, mountStopsWith, recordMountSource, boundSettle, utabPath, managerVersion
	FstabLockPath = filepath.Join(dir, "fstab.lock")
	managerVersion = func(context.Context) (int, error) { return env.version, env.versionErr }
	utabPath = filepath.Join(dir, "utab")
	boundSettle = 20 * time.Millisecond
	unitDaemonReload = func(context.Context) error {
		env.reloads++
		return env.reloadErr
	}
	mountStopsWith = func(_ context.Context, unit string) ([]string, error) {
		return env.stops[unit], env.stopsErr
	}
	recordMountSource = func(_ context.Context, m Mount, options []string) error {
		env.recorded = append(env.recorded, m.Device+" "+m.Mountpoint+" "+strings.Join(options, ","))
		return env.record(m.Mountpoint)
	}
	t.Cleanup(func() {
		FstabLockPath, unitDaemonReload, mountStopsWith, recordMountSource, boundSettle, utabPath, managerVersion = oldLock, oldReload, oldStops, oldRecord, oldSettle, oldUtab, oldVersion
	})
	return env
}

// mountedPool is the pool on devices disks, mounted from its first at
// mountpoints.
func mountedPool(devices int, mountpoints ...string) BtrfsFilesystem {
	fs := BtrfsFilesystem{UUID: dataUUID, Label: "tank"}
	for i := range devices {
		fs.Devices = append(fs.Devices, BtrfsDevice{ID: i + 1, Path: "/dev/sd" + string(rune('b'+i))})
	}
	for _, m := range mountpoints {
		fs.Mountpoints = append(fs.Mountpoints, Mount{Mountpoint: m, Device: "/dev/sdb", UUID: dataUUID})
	}
	return fs
}

func readFstab(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(FstabPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUnbindPoolMountsEditsTheLineOnce(t *testing.T) {
	env := stubBound(t)
	const old = "UUID=" + dataUUID + " /data btrfs defaults,noatime,nofail 0 0\n"
	const want = "UUID=" + dataUUID + " /data btrfs defaults,noatime,nofail,x-systemd.device-bound=false 0 0\n"
	writeFstab(t, old)

	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	if got := readFstab(t); got != want {
		t.Fatalf("fstab = %q, want %q", got, want)
	}
	if env.reloads != 1 {
		t.Fatalf("systemd reloaded %d times, want once, for the generator to read the line", env.reloads)
	}

	before, err := os.Stat(FstabPath)
	if err != nil {
		t.Fatal(err)
	}
	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	after, err := os.Stat(FstabPath)
	if err != nil {
		t.Fatal(err)
	}
	if env.reloads != 1 || !os.SameFile(before, after) {
		t.Fatalf("a run with nothing to add wrote fstab or reloaded systemd (%d reloads)", env.reloads)
	}
}

// systemd ties no mount of the root to a device, so the line of / is never
// touched. The other mounts of the filesystem / is on go with the system
// when it is on one disk, and are kept like any pool's when it is on more.
func TestUnbindPoolMountsOnTheSystemFilesystem(t *testing.T) {
	const fstab = "UUID=" + dataUUID + " / btrfs subvol=@ 0 0\nUUID=" + dataUUID + " /home btrfs subvol=@home 0 0\n"
	t.Run("on one disk", func(t *testing.T) {
		env := stubBound(t)
		writeFstab(t, fstab)
		env.stops["home.mount"] = []string{"dev-sda2.device"}

		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(1, "/", "/home")})
		if got := readFstab(t); got != fstab {
			t.Fatalf("fstab = %q, want it untouched", got)
		}
		if keys := alertKeys(t); len(keys) != 0 || env.reloads != 0 || len(env.recorded) != 0 {
			t.Fatalf("alerts %v, %d reloads, recorded %q; want nothing done", keys, env.reloads, env.recorded)
		}
	})
	t.Run("on two disks", func(t *testing.T) {
		env := stubBound(t)
		writeFstab(t, fstab)
		env.stops["-.mount"], env.stops["home.mount"] = []string{"dev-sdb.device"}, []string{"dev-sdb.device"}

		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/", "/home")})
		want := "UUID=" + dataUUID + " / btrfs subvol=@ 0 0\nUUID=" + dataUUID + " /home btrfs subvol=@home,x-systemd.device-bound=false 0 0\n"
		if got := readFstab(t); got != want {
			t.Fatalf("fstab = %q, want %q", got, want)
		}
		if !slices.Equal(env.recorded, []string{"/dev/sdb /home x-systemd.device-bound=false"}) {
			t.Fatalf("recorded %q, want /home alone", env.recorded)
		}
	})
}

func TestUnbindPoolMountsLeavesAPoolThatIsNotMounted(t *testing.T) {
	stubBound(t)
	const fstab = "UUID=" + dataUUID + " /data btrfs defaults 0 0\n"
	writeFstab(t, fstab)

	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2)})
	if got := readFstab(t); got != fstab {
		t.Fatalf("fstab = %q, want it untouched", got)
	}
}

// mount(8) files the option under the disk /dev/disk/by-uuid named, the
// kernel names another as the mount's source, and systemd reads the option
// only under the source: the mount is recorded again under that one.
func TestUnbindPoolMountsRecordsTheMountUnderItsSource(t *testing.T) {
	env := stubBound(t)
	writeFstab(t, "UUID="+dataUUID+" /data btrfs defaults,nofail,x-systemd.device-timeout=30s 0 0\nUUID="+dataUUID+" /media btrfs subvol=@media,x-systemd.device-bound=false 0 0\n")
	env.stops["data.mount"] = []string{"dev-sdb.device"}
	env.stops["media.mount"] = []string{"dev-sdb.device"}

	// /media is in fstab and not mounted: nothing of it is recorded.
	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	want := []string{"/dev/sdb /data x-systemd.device-timeout=30s,x-systemd.device-bound=false"}
	if !slices.Equal(env.recorded, want) {
		t.Fatalf("recorded %q, want %q", env.recorded, want)
	}
	if keys := alertKeys(t); len(keys) != 0 {
		t.Fatalf("alerts %v, want none once the mount no longer stops with a device", keys)
	}

	// The next run finds nothing to do.
	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	if !slices.Equal(env.recorded, want) {
		t.Fatalf("recorded %q on a run with nothing to do, want %q", env.recorded, want)
	}
}

func TestUnbindPoolMountsWarnsWhileAMountStillStopsWithADevice(t *testing.T) {
	key := "bound-" + dataUUID
	fstab := "UUID=" + dataUUID + " /data btrfs defaults,x-systemd.device-bound=false 0 0\n"
	t.Run("systemd keeps the dependency", func(t *testing.T) {
		env := stubBound(t)
		writeFstab(t, fstab)
		env.stops["data.mount"] = []string{"dev-sdb.device"}
		env.record = func(string) error { return nil }

		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
		if got := alertKeys(t)[key]; got != "warning" {
			t.Fatalf("alert %s = %q, want a warning", key, got)
		}

		// What systemd answers could not be read: the alert stays as it was,
		// and nothing is recorded on a guess.
		env.stopsErr, env.recorded = errors.New("no answer"), nil
		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
		if got := alertKeys(t)[key]; got != "warning" || len(env.recorded) != 0 {
			t.Fatalf("alert %s = %q, recorded %q after an unreadable unit; want the alert kept and nothing recorded", key, got, env.recorded)
		}

		env.stopsErr, env.stops["data.mount"] = nil, nil
		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
		if _, raised := alertKeys(t)[key]; raised {
			t.Fatalf("alert %s still raised once the mount no longer stops with a device", key)
		}
	})
	t.Run("the record could not be written", func(t *testing.T) {
		env := stubBound(t)
		writeFstab(t, fstab)
		env.stops["data.mount"] = []string{"dev-sdb.device"}
		env.record = func(string) error { return errors.New("mount: permission denied") }

		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
		if got := alertKeys(t)[key]; got != "warning" {
			t.Fatalf("alert %s = %q, want a warning", key, got)
		}
	})
}

// A line an administrator bound to its device is theirs: no edit, no record,
// no alert.
func TestUnbindPoolMountsLeavesABoundLineAlone(t *testing.T) {
	env := stubBound(t)
	const fstab = "UUID=" + dataUUID + " /data btrfs defaults,x-systemd.device-bound 0 0\n"
	writeFstab(t, fstab)
	env.stops["data.mount"] = []string{"dev-sdb.device"}

	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	if got := readFstab(t); got != fstab {
		t.Fatalf("fstab = %q, want it untouched", got)
	}
	if keys := alertKeys(t); len(keys) != 0 || len(env.recorded) != 0 {
		t.Fatalf("alerts %v, recorded %q; want none", keys, env.recorded)
	}
}

// The mount is matched as the mount table names it, and only the options
// systemd reads are handed on: mount(8) acts on x-mount ones itself.
func TestUnbindPoolMountsMatchesAndRecordsWhatSystemdReads(t *testing.T) {
	cases := []struct {
		name, line, mountpoint, want string
	}{
		{"an escaped blank and a trailing slash", "/mnt/my\\040disk/ btrfs defaults,x-systemd.device-bound=false", "/mnt/my disk", "/dev/sdb /mnt/my disk x-systemd.device-bound=false"},
		{"another spelling of false", "/data btrfs x-systemd.device-bound=no", "/data", "/dev/sdb /data x-systemd.device-bound=no"},
		// What mount(8) records and systemd reads from it; x-mount options
		// are mount(8)'s own to act on.
		{"options of mount(8)'s own", "/data btrfs noatime,x-mount.mkdir,_netdev,x-systemd.device-timeout=30s,x-systemd.device-bound=false,X-mount.mode=0700,x-initrd.mount", "/data", "/dev/sdb /data _netdev,x-systemd.device-timeout=30s,x-systemd.device-bound=false,x-initrd.mount"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := stubBound(t)
			writeFstab(t, "UUID="+dataUUID+" "+c.line+" 0 0\n")
			unit, err := systemd.EscapePath(c.mountpoint)
			if err != nil {
				t.Fatal(err)
			}
			env.stops[unit+".mount"] = []string{"dev-sdb.device"}
			env.record = func(string) error {
				delete(env.stops, unit+".mount")
				return nil
			}

			unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, c.mountpoint)})
			if !slices.Equal(env.recorded, []string{c.want}) {
				t.Fatalf("recorded %q, want %q", env.recorded, c.want)
			}
		})
	}
}

// A systemd that keeps the dependency whatever the record says, as one older
// than 255 does, is not handed the same record at every run.
func TestUnbindPoolMountsRecordsAMountOnce(t *testing.T) {
	env := stubBound(t)
	writeFstab(t, "UUID="+dataUUID+" /data btrfs defaults,x-systemd.device-bound=false 0 0\n")
	env.stops["data.mount"] = []string{"dev-sdb.device"}
	env.record = func(string) error {
		return os.WriteFile(utabPath, []byte("SRC=/dev/sdc TARGET=/data ROOT=/ OPTS=x-systemd.device-bound=false\nSRC=/dev/sdb TARGET=/data ROOT=/ OPTS=x-systemd.device-bound=false\n"), 0o644)
	}

	for range 3 {
		unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	}
	if len(env.recorded) != 1 {
		t.Fatalf("recorded %d times, want once: %q", len(env.recorded), env.recorded)
	}
	if got := alertKeys(t)["bound-"+dataUUID]; got != "warning" {
		t.Fatalf("alert = %q, want the warning kept while the mount stops with a device", got)
	}
}

// A reload that failed leaves the generator behind, not the mounts there are.
func TestUnbindPoolMountsRecordsAfterAReloadThatFailed(t *testing.T) {
	env := stubBound(t)
	writeFstab(t, "UUID="+dataUUID+" /data btrfs defaults 0 0\n")
	env.stops["data.mount"] = []string{"dev-sdb.device"}
	env.reloadErr = errors.New("no manager")

	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data")})
	if len(env.recorded) != 1 {
		t.Fatalf("recorded %q, want the mount recorded although systemd was not reloaded", env.recorded)
	}
}

// Without a device path to record the mount under, nothing is handed to
// mount(8), and the mount is reported as one that still stops.
func TestUnbindPoolMountsWarnsWithoutASource(t *testing.T) {
	env := stubBound(t)
	writeFstab(t, "UUID="+dataUUID+" /data btrfs defaults,x-systemd.device-bound=false 0 0\n")
	env.stops["data.mount"] = []string{"dev-sdb.device"}
	fs := mountedPool(2, "/data")
	fs.Mountpoints[0].Device = ""

	unbindPoolMounts(t.Context(), []BtrfsFilesystem{fs})
	if got := alertKeys(t)["bound-"+dataUUID]; got != "warning" || len(env.recorded) != 0 {
		t.Fatalf("alert = %q, recorded %q; want a warning and nothing recorded", got, env.recorded)
	}
}

// A systemd older than 255 reads the option's mere presence as bound, and
// binds the mount to its device harder than without it: nothing is written
// or recorded there, nor when the manager does not say what it is.
func TestUnbindPoolMountsLeavesAnOlderSystemdAlone(t *testing.T) {
	const fstab = "UUID=" + dataUUID + " /data btrfs defaults 0 0\nUUID=" + dataUUID + " /media btrfs subvol=@media,x-systemd.device-bound=false 0 0\n"
	for name, set := range map[string]func(*boundEnv){
		"systemd 254":         func(env *boundEnv) { env.version = 254 },
		"a version not known": func(env *boundEnv) { env.versionErr = errors.New("no manager") },
	} {
		t.Run(name, func(t *testing.T) {
			env := stubBound(t)
			set(env)
			writeFstab(t, fstab)
			env.stops["data.mount"], env.stops["media.mount"] = []string{"dev-sdb.device"}, []string{"dev-sdb.device"}

			unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data", "/media")})
			if got := readFstab(t); got != fstab || len(env.recorded) != 0 || env.reloads != 0 || len(alertKeys(t)) != 0 {
				t.Fatalf("fstab %q, recorded %q, %d reloads, alerts %v; want nothing done", got, env.recorded, env.reloads, alertKeys(t))
			}
		})
	}
}

// The record is written as the kernel names the mount: its source without
// mount(8) turning /dev/dm-0 into /dev/mapper/..., and its subvolume, from
// which libmount takes the record's root; neither joins the mount otherwise.
func TestRecordArgs(t *testing.T) {
	options := []string{"x-systemd.device-bound=false"}
	cases := map[string]struct {
		mount Mount
		want  []string
	}{
		"the top level": {
			Mount{Mountpoint: "/data", Device: "/dev/dm-0", SubvolID: 5},
			[]string{"--fake", "--internal-only", "--no-canonicalize", "-t", "btrfs", "-o", "subvolid=5,x-systemd.device-bound=false", "--", "/dev/dm-0", "/data"},
		},
		"a subvolume": {
			Mount{Mountpoint: "/mnt/my media", Device: "/dev/sdb", SubvolID: 256, Subvol: "@media"},
			[]string{"--fake", "--internal-only", "--no-canonicalize", "-t", "btrfs", "-o", "subvolid=256,x-systemd.device-bound=false", "--", "/dev/sdb", "/mnt/my media"},
		},
	}
	for name, c := range cases {
		if got := recordArgs(c.mount, options); !slices.Equal(got, c.want) {
			t.Errorf("%s: mount %q, want %q", name, got, c.want)
		}
	}
}

func TestMountRecorded(t *testing.T) {
	stubBound(t)
	if err := os.WriteFile(utabPath, []byte("SRC=/dev/sdc TARGET=/data ROOT=/ OPTS=x-systemd.device-bound=false\nSRC=/dev/sdb TARGET=/mnt/my\\040media ROOT=/@media OPTS=x-systemd.device-timeout=5s,x-systemd.device-bound=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const option = "x-systemd.device-bound=false"
	media := Mount{Device: "/dev/sdb", Mountpoint: "/mnt/my media", Subvol: "@media"}
	if !mountRecorded(media, option) {
		t.Error("the record of /mnt/my media under /dev/sdb was not found")
	}
	if mountRecorded(Mount{Device: "/dev/sdb", Mountpoint: "/data"}, option) {
		t.Error("the record of /data under another disk counted as its record under /dev/sdb")
	}
	if mountRecorded(media, "x-systemd.device-bound=no") {
		t.Error("a record with another option counted")
	}
	// The record a mount of another subvolume left at the mountpoint, which
	// LinuxIO's own unmount does not remove, is not this mount's.
	other := media
	other.Subvol = "@photos"
	if mountRecorded(other, option) {
		t.Error("the record of another subvolume at the mountpoint counted")
	}
	// A folder of a subvolume, bind-mounted: the record is written with the
	// subvolume's root, joins nothing, and is still not written twice.
	folder := media
	folder.SubPath = "@media/www"
	if !mountRecorded(folder, option) {
		t.Error("the record written for a folder of the subvolume did not count as written")
	}
}

// A lock on /etc/fstab that cannot be taken, or a file that cannot be
// replaced, leaves the lines as they are, not the mounts there are.
func TestUnbindPoolMountsRecordsWithoutTheLock(t *testing.T) {
	env := stubBound(t)
	const fstab = "UUID=" + dataUUID + " /data btrfs defaults,x-systemd.device-bound=false 0 0\nUUID=" + dataUUID + " /media btrfs subvol=@media 0 0\n"
	writeFstab(t, fstab)
	blocker := filepath.Join(t.TempDir(), "a file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	FstabLockPath = filepath.Join(blocker, "fstab.lock")
	env.stops["data.mount"] = []string{"dev-sdb.device"}

	unbindPoolMounts(t.Context(), []BtrfsFilesystem{mountedPool(2, "/data", "/media")})
	if got := readFstab(t); got != fstab {
		t.Fatalf("fstab = %q, want it as it was without the lock", got)
	}
	if !slices.Equal(env.recorded, []string{"/dev/sdb /data x-systemd.device-bound=false"}) {
		t.Fatalf("recorded %q, want the mount whose line says false", env.recorded)
	}
}

// The health run is what keeps the pools mounted, and an alert of a pool
// that is no longer mounted goes with its mount.
func TestCheckHealthUnbindsPoolMounts(t *testing.T) {
	stubKernel(t, raid1Kernel())
	stubDisks(t, nil)
	stubBound(t)
	writeFstab(t, "UUID="+dataUUID+" /data btrfs defaults 0 0\n")
	healthRun(t, "of the mounted pool")
	if got := readFstab(t); got != "UUID="+dataUUID+" /data btrfs defaults,x-systemd.device-bound=false 0 0\n" {
		t.Fatalf("fstab = %q after the health run, want the option added", got)
	}

	raise(t.Context(), alerts.Observation{Key: "bound-" + dataUUID, Severity: alerts.SeverityWarning, Title: "left from when it was mounted"})
	stubMounts(t, nil)
	stubDisks(t, map[string]Superblock{"sdb": {UUID: dataUUID, DevID: 1, NumDevices: 1}})
	healthRun(t, "of the pool unmounted")
	if _, raised := alertKeys(t)["bound-"+dataUUID]; raised {
		t.Fatal("the bound alert of a pool that is not mounted stayed raised")
	}
}
