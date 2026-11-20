package btrfs

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// deviceBound is the fstab option that says whether systemd stops a mount
// with the device it names; unboundOption is what a pool's line carries.
const (
	deviceBound   = "x-systemd.device-bound"
	unboundOption = deviceBound + "=false"
)

// FstabLockPath is the lock every read-modify-write of FstabPath by LinuxIO
// takes, the bridge's fstab editor included. It is in /run, which root alone
// writes: in /run/lock any local user could create the file first, and root
// then cannot open it (fs.protected_regular). A var so tests can use a temp
// path.
var FstabLockPath = "/run/linuxio-fstab.lock"

// mountStopsWith returns the units whose stop the manager propagates to a
// mount unit; swapped by tests.
var mountStopsWith = systemd.UnitStopPropagatedFrom

// boundSettle is how long systemd is given to read a mount's new record; in
// the test VM it took under three seconds.
var boundSettle = 10 * time.Second

// managerVersion is systemd's version; swapped by tests.
var managerVersion = systemd.ManagerVersion

// UnboundMountsHonoured says whether systemd reads
// x-systemd.device-bound=false as it is written. One older than 255 reads
// the option's mere presence as bound (fstab_test_option in its mount.c),
// which ties the mount to its device with BindsTo=, harder than without the
// option, so nothing writes the option or records it there, nor when the
// manager does not say what it is.
func UnboundMountsHonoured(ctx context.Context) bool {
	version, err := managerVersion(ctx)
	if err != nil {
		slog.Debug("failed to read systemd's version", "error", err)
		return false
	}
	return version >= 255
}

// utabPath is libmount's record of the userspace options of mounts; a var
// so tests can use a temp file.
var utabPath = "/run/mount/utab"

// recordMountSource has mount(8) record mount m with the userspace options,
// as it does for a mount it makes itself. Swapped by tests.
var recordMountSource = func(ctx context.Context, m Mount, options []string) error {
	_, err := runCommand(ctx, "mount", recordArgs(m, options)...)
	return err
}

// recordArgs are mount(8)'s arguments to record m: --fake makes no mount(2)
// call and writes the entry in libmount's utab, which is where systemd reads
// a mount's x-systemd options from once it is mounted. libmount joins an
// entry to a mount by source, target and root, so the source goes in as the
// kernel names it (--no-canonicalize: mount(8) would write /dev/mapper/x for
// /dev/dm-0), and the subvolume with it, from which libmount takes the root
// and which it keeps out of the entry's options (seen in the test VM: without
// it the entry of a subvolume mount says ROOT=/ and joins nothing).
func recordArgs(m Mount, options []string) []string {
	if m.SubvolID > 0 {
		options = append([]string{"subvolid=" + strconv.Itoa(m.SubvolID)}, options...)
	}
	return []string{"--fake", "--internal-only", "--no-canonicalize", "-t", "btrfs", "-o", strings.Join(options, ","), "--", m.Device, m.Mountpoint}
}

// unbindPoolMounts keeps systemd from unmounting the mounted pools in
// filesystems when the disk a mount names as its source goes away. It adds
// x-systemd.device-bound=false to each of their /etc/fstab lines that says
// nothing about it, and has systemd apply the option to the mounts there
// are (unbindMounts), raising the pool's bound alert for one it could not.
// The filesystem / is on is left as it is. A failure is logged: the health
// run goes on.
func unbindPoolMounts(ctx context.Context, filesystems []BtrfsFilesystem) {
	if !UnboundMountsHonoured(ctx) {
		return
	}
	pools := map[string]bool{}
	for _, fs := range filesystems {
		// The filesystem / is on goes with the system when it is on one disk.
		system := len(fs.Devices) < 2 && slices.ContainsFunc(fs.Mountpoints, func(m Mount) bool { return m.Mountpoint == "/" })
		if len(fs.Mountpoints) > 0 && !system {
			pools[fs.UUID] = true
		}
	}
	if len(pools) == 0 {
		return
	}
	content, err := unbindFstab(ctx, pools)
	if err != nil {
		// The lines stay as they are; the mounts whose line says false
		// already are still recorded.
		slog.Warn("failed to add x-systemd.device-bound=false to the fstab lines of btrfs pools", "path", FstabPath, "error", err)
		data, readErr := os.ReadFile(FstabPath)
		if readErr != nil {
			return
		}
		content = string(data)
	}
	for _, fs := range filesystems {
		if pools[fs.UUID] {
			unbindMounts(ctx, fs, content)
		}
	}
}

// unbindFstab adds the option to the lines of pools under the fstab lock,
// has systemd read the file again when it wrote it, and returns the file's
// content.
func unbindFstab(ctx context.Context, pools map[string]bool) (string, error) {
	var content string
	var edited []string
	err := filelock.RunExclusive(ctx, FstabLockPath, func() error {
		data, err := os.ReadFile(FstabPath)
		if err != nil {
			return err
		}
		content, edited = unboundFstab(string(data), pools)
		if len(edited) == 0 {
			return nil
		}
		return atomicfile.Write(FstabPath, []byte(content), 0o644)
	}, filelock.WithTimeout(10*time.Second))
	if err != nil || len(edited) == 0 {
		return content, err
	}
	slog.Info("added x-systemd.device-bound=false to the fstab lines of btrfs pools", "path", FstabPath, "mountpoints", edited)
	// The generator's mount units are what the next mount is made from; the
	// mounts there are do not wait for it.
	if err := unitDaemonReload(ctx); err != nil {
		slog.Warn("failed to reload systemd after adding x-systemd.device-bound=false to fstab", "error", err)
	}
	return content, nil
}

// unboundFstab returns content with unboundOption added to every btrfs line
// that names one of pools by UUID= and says nothing about device-bound, and
// the mountpoints of the lines it changed. Everything else stays byte for
// byte.
func unboundFstab(content string, pools map[string]bool) (string, []string) {
	var out strings.Builder
	var edited []string
	for line := range strings.Lines(content) {
		entry, ok := poolLine(line, pools)
		if !ok || entry.bound != "" {
			out.WriteString(line)
			continue
		}
		out.WriteString(line[:entry.optionsEnd] + "," + unboundOption + line[entry.optionsEnd:])
		edited = append(edited, entry.mountpoint)
	}
	return out.String(), edited
}

// fstabPoolLine is one fstab line of a pool. mountpoint is the path as the
// mount table names it; optionsEnd is where the options field ends in the
// line; bound is the line's device-bound option as written, "" when it has
// none, and unbound whether it says false.
type fstabPoolLine struct {
	uuid, mountpoint string
	options          []string
	optionsEnd       int
	bound            string
	unbound          bool
}

// fstabFields splits line the way mount(8) and systemd read fstab, on blanks
// and tabs alone, and returns its fields with where each starts.
func fstabFields(line string) (fields []string, starts []int) {
	line = strings.TrimRight(line, "\r\n")
	for i := 0; i < len(line); {
		if line[i] == ' ' || line[i] == '\t' {
			i++
			continue
		}
		end := i
		for end < len(line) && line[end] != ' ' && line[end] != '\t' {
			end++
		}
		fields, starts = append(fields, line[i:end]), append(starts, i)
		i = end
	}
	return fields, starts
}

// poolLine reads line as the entry of one of pools: a btrfs line whose
// source is UUID=<uuid>.
func poolLine(line string, pools map[string]bool) (fstabPoolLine, bool) {
	fields, starts := fstabFields(line)
	if len(fields) < 4 || fields[2] != "btrfs" || len(fields[0]) <= 5 || !strings.EqualFold(fields[0][:5], "UUID=") ||
		slices.ContainsFunc(fields[:4], func(f string) bool { return strings.HasPrefix(f, "#") }) {
		return fstabPoolLine{}, false
	}
	entry := fstabPoolLine{
		uuid:       strings.ToLower(fields[0][5:]),
		mountpoint: filepath.Clean(octalescape.Unescape(fields[1])),
		options:    strings.Split(fields[3], ","),
		optionsEnd: starts[3] + len(fields[3]),
	}
	// systemd ties no mount of the root to a device (mount.c).
	if !pools[entry.uuid] || entry.mountpoint == "/" {
		return fstabPoolLine{}, false
	}
	for _, o := range entry.options {
		name, value, _ := strings.Cut(o, "=")
		if name == deviceBound {
			// As systemd's parse_boolean reads it; the bare option is true.
			entry.bound, entry.unbound = o, slices.Contains([]string{"0", "no", "n", "false", "f", "off"}, strings.ToLower(value))
		}
	}
	return entry, true
}

// unbindMounts has systemd apply unboundOption to the mounts of pool fs
// whose fstab line, in content, carries it. systemd reads the option of a
// mount that exists from libmount's record of it, which mount(8) writes
// under the device /dev/disk/by-uuid/<uuid> named when it mounted, while the
// kernel names any member as the mount's source, and the two are merged only
// when they are the same device; a mount LinuxIO made with mount(2) has no
// record at all. So a mount that still stops with a device is recorded again
// under its source (recordMountSource), which systemd applies at once. The pool's
// bound alert is raised for a mount that stops with a device all the same,
// resolved when none does, and left as it was when systemd could not be
// asked. A line an administrator bound is theirs.
func unbindMounts(ctx context.Context, fs BtrfsFilesystem, content string) {
	var stopping []string
	unknown := false
	for line := range strings.Lines(content) {
		entry, ok := poolLine(line, map[string]bool{fs.UUID: true})
		if !ok || !entry.unbound {
			continue
		}
		at := slices.IndexFunc(fs.Mountpoints, func(m Mount) bool { return filepath.Clean(m.Mountpoint) == entry.mountpoint })
		if at < 0 {
			continue
		}
		stops, err := unbindMount(ctx, fs.Mountpoints[at], entry)
		switch {
		case err != nil:
			slog.Debug("failed to read what a btrfs pool's mount stops with", "uuid", fs.UUID, "mountpoint", entry.mountpoint, "error", err)
			unknown = true
		case stops:
			stopping = append(stopping, entry.mountpoint)
		}
	}
	key := "bound-" + fs.UUID
	switch {
	case len(stopping) > 0:
		raise(ctx, alerts.Observation{
			Key: key, Severity: alerts.SeverityWarning,
			Title:   poolName(fs.Label, fs.UUID) + " is unmounted when the disk its mount names is disconnected",
			Message: "systemd stops the mount at " + strings.Join(stopping, ", ") + " when the disk it names as its source is disconnected, whatever the pool's other disks hold, and LinuxIO could not have it apply x-systemd.device-bound=false from the pool's line in /etc/fstab. Restart the server; the log of linuxio-btrfs-health.service says what failed.",
			// One occurrence while it lasts: every health check sees it again.
			Link: "/storage?pool=" + fs.UUID, OccurrenceID: "bound",
		})
	case !unknown:
		resolve(ctx, key)
	}
}

// unbindMount says whether mount m, of fstab line entry, still stops with a
// device, after its record was written under its source when it did before
// and had none there. An error is systemd not answering.
func unbindMount(ctx context.Context, m Mount, entry fstabPoolLine) (bool, error) {
	name, err := systemd.EscapePath(m.Mountpoint)
	if err != nil {
		return false, nil // no unit has that name
	}
	unit := name + ".mount"
	devices, err := mountStopsWith(ctx, unit)
	if err != nil || len(devices) == 0 {
		return false, err
	}
	switch {
	case !strings.HasPrefix(m.Device, "/"):
		slog.Warn("no device path to record a btrfs pool's mount under", "mountpoint", m.Mountpoint, "device", m.Device)
		return true, nil
	case mountRecorded(m, entry.bound):
		// systemd read the record and keeps the dependency, as one older
		// than 255 does: the same record again changes nothing.
		return true, nil
	}
	// What mount(8) records for a mount it makes, since systemd reads the
	// newest record whole (_netdev and x-initrd.mount among it); x-mount
	// options are mount(8)'s own to act on.
	userspace := slices.DeleteFunc(slices.Clone(entry.options), func(o string) bool {
		return o != "_netdev" && (!strings.HasPrefix(o, "x-") || strings.HasPrefix(o, "x-mount."))
	})
	if err = recordMountSource(ctx, m, userspace); err != nil {
		slog.Warn("failed to record a btrfs pool's mount under its source", "mountpoint", m.Mountpoint, "device", m.Device, "error", err)
		return true, nil
	}
	slog.Info("recorded a btrfs pool's mount under its source for systemd to read its fstab options", "mountpoint", m.Mountpoint, "device", m.Device)
	// systemd reads the record when libmount signals it.
	for wait := time.Duration(0); ; wait += boundSettle / 10 {
		devices, err = mountStopsWith(ctx, unit)
		if err != nil || len(devices) == 0 {
			return false, err
		}
		if wait >= boundSettle {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(boundSettle / 10):
		}
	}
}

// mountRecorded says whether libmount's record holds mount m under its
// source and its subvolume, which is the root recordArgs has it written
// with, and with option among its options.
func mountRecorded(m Mount, option string) bool {
	data, err := os.ReadFile(utabPath)
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(data)) {
		entry := map[string]string{}
		for field := range strings.FieldsSeq(line) {
			if key, value, ok := strings.Cut(field, "="); ok {
				entry[key] = octalescape.Unescape(value)
			}
		}
		if entry["SRC"] == m.Device && entry["TARGET"] == m.Mountpoint && entry["ROOT"] == "/"+m.Subvol && slices.Contains(strings.Split(entry["OPTS"], ","), option) {
			return true
		}
	}
	return false
}
