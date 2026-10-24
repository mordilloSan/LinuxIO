package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

var (
	// mdSysfsRoot and mdadmCommand are seams for tests.
	mdSysfsRoot       = "/sys"
	mdadmCommand      = runMdadm
	mdBlockDevices    = ListBlockDevices
	mdArrayNamePolicy = regexp.MustCompile(`^(md[0-9]+|md_d[0-9]+)$`)
)

func runMdadm(ctx context.Context, args ...string) error {
	path, err := hostcmd.LookPath("mdadm")
	if err != nil {
		return fmt.Errorf("mdadm is not installed: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := exec.CommandContext(ctx, path, args...).Output(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("run mdadm: %w", ctx.Err())
		}
		var stderr []byte
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = exitErr.Stderr
		}
		return hostcmd.OutputError("mdadm", args, stderr, err)
	}
	return nil
}

func mdAttr(dir, name string) string {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func mdUint(dir, name string) uint64 {
	v, _ := strconv.ParseUint(mdAttr(dir, name), 10, 64)
	return v
}

func readMDMembers(mdDir string) []apischema.MDMember {
	members := []apischema.MDMember{}
	entries, _ := os.ReadDir(mdDir)
	for _, e := range entries {
		raw, ok := strings.CutPrefix(e.Name(), "dev-")
		if !ok {
			continue
		}
		dir := filepath.Join(mdDir, e.Name())
		name := strings.ReplaceAll(raw, "!", "/")
		slot, err := strconv.Atoi(mdAttr(dir, "slot"))
		if err != nil {
			slot = -1
		}
		state := []string{}
		if s := mdAttr(dir, "state"); s != "" {
			state = strings.Split(s, ",")
		}
		members = append(members, apischema.MDMember{
			Name: name, Path: "/dev/" + name, Slot: slot, State: state, Errors: mdUint(dir, "errors"),
		})
	}
	slices.SortFunc(members, func(a, b apischema.MDMember) int {
		// Spares and faulty members (slot -1) sort last.
		if (a.Slot < 0) != (b.Slot < 0) {
			if a.Slot < 0 {
				return 1
			}
			return -1
		}
		if a.Slot != b.Slot {
			return a.Slot - b.Slot
		}
		return strings.Compare(a.Name, b.Name)
	})
	return members
}

// readMDArray reads one array from sysfs. The name is validated before it
// reaches a path, and the array must have md/array_state.
func readMDArray(name string) (apischema.MDArray, error) {
	if !mdArrayNamePolicy.MatchString(name) {
		return apischema.MDArray{}, fmt.Errorf("%w: md array %q", bridgeipc.ErrNotFound, name)
	}
	blockDir := filepath.Join(mdSysfsRoot, "block", name)
	mdDir := filepath.Join(blockDir, "md")
	if _, err := os.Stat(filepath.Join(mdDir, "array_state")); err != nil {
		return apischema.MDArray{}, fmt.Errorf("%w: md array %q", bridgeipc.ErrNotFound, name)
	}
	raidDisks, _ := strconv.Atoi(mdAttr(mdDir, "raid_disks"))
	degraded, _ := strconv.Atoi(mdAttr(mdDir, "degraded"))
	a := apischema.MDArray{
		Name:            name,
		Path:            "/dev/" + name,
		Level:           mdAttr(mdDir, "level"),
		State:           mdAttr(mdDir, "array_state"),
		MetadataVersion: mdAttr(mdDir, "metadata_version"),
		SizeBytes:       mdUint(blockDir, "size") * 512,
		RaidDisks:       raidDisks,
		Degraded:        degraded,
		SyncAction:      mdAttr(mdDir, "sync_action"),
		SyncSpeedBytes:  mdUint(mdDir, "sync_speed") * 1024,
		MismatchCount:   mdUint(mdDir, "mismatch_cnt"),
		Members:         readMDMembers(mdDir),
	}
	// sync_completed is "done / total" in sectors, or "none".
	if done, total, ok := strings.Cut(mdAttr(mdDir, "sync_completed"), " / "); ok {
		d, _ := strconv.ParseFloat(done, 64)
		t, _ := strconv.ParseFloat(total, 64)
		if t > 0 {
			a.SyncProgress = d / t * 100
		}
	}
	return a, nil
}

func ListMDArrays() ([]apischema.MDArray, error) {
	entries, err := os.ReadDir(filepath.Join(mdSysfsRoot, "block"))
	if err != nil {
		return nil, fmt.Errorf("list block devices: %w", err)
	}
	arrays := []apischema.MDArray{}
	for _, e := range entries {
		if a, err := readMDArray(e.Name()); err == nil {
			arrays = append(arrays, a)
		}
	}
	return arrays, nil
}

func writeMDAttr(dir, attr, value string) error {
	path := filepath.Join(dir, attr)
	if err := os.WriteFile(path, []byte(value), 0); err != nil {
		return fmt.Errorf("write %s to %s: %w", value, path, err)
	}
	return nil
}

// mdMemberDir returns the sysfs dir of a member currently listed for the array.
func mdMemberDir(a apischema.MDArray, member string) (apischema.MDMember, string, error) {
	i := slices.IndexFunc(a.Members, func(m apischema.MDMember) bool { return m.Name == member })
	if i < 0 {
		return apischema.MDMember{}, "", fmt.Errorf("%w: member %q of %s", bridgeipc.ErrNotFound, member, a.Name)
	}
	dir := filepath.Join(mdSysfsRoot, "block", a.Name, "md", "dev-"+strings.ReplaceAll(member, "/", "!"))
	return a.Members[i], dir, nil
}

func mdInSync(m apischema.MDMember) bool { return slices.Contains(m.State, "in_sync") }

// checkMDDeviceFree rejects a device that is in use or carries a signature.
func checkMDDeviceFree(devices []apischema.StorageBlockDevice, device string) error {
	i := slices.IndexFunc(devices, func(d apischema.StorageBlockDevice) bool { return d.Path == device })
	if i < 0 {
		return fmt.Errorf("%w: block device %s", bridgeipc.ErrNotFound, device)
	}
	d := devices[i]
	if d.Type != "disk" && d.Type != "part" {
		return fmt.Errorf("%w: device %s is a %s, not a disk or partition", bridgeipc.ErrInvalidArgs, device, d.Type)
	}
	if len(d.Mountpoints) > 0 {
		return fmt.Errorf("%w: device %s is mounted at %s", bridgeipc.ErrConflict, device, d.Mountpoints[0])
	}
	if slices.ContainsFunc(devices, func(c apischema.StorageBlockDevice) bool { return slices.Contains(c.Parents, device) }) {
		return fmt.Errorf("%w: device %s has partitions or holders", bridgeipc.ErrConflict, device)
	}
	if d.FSType != "" {
		return fmt.Errorf("%w: device %s has a %s signature; wipe it first", bridgeipc.ErrConflict, device, d.FSType)
	}
	return nil
}

func mdDeviceFree(ctx context.Context, device string) error {
	devices, err := mdBlockDevices(ctx)
	if err != nil {
		return err
	}
	return checkMDDeviceFree(devices, device)
}

// checkMDFailAllowed refuses to fail an in_sync member the level cannot lose.
func checkMDFailAllowed(a apischema.MDArray, member apischema.MDMember) error {
	if !mdInSync(member) {
		return nil
	}
	ok := false
	switch a.Level {
	case "raid1":
		ok = slices.ContainsFunc(a.Members, func(m apischema.MDMember) bool { return m.Name != member.Name && mdInSync(m) })
	case "raid4", "raid5", "raid10":
		ok = a.Degraded == 0
	case "raid6":
		ok = a.Degraded < 2
	}
	if !ok {
		return fmt.Errorf("%w: failing %s would take %s (%s, degraded %d) offline", bridgeipc.ErrConflict, member.Name, a.Name, a.Level, a.Degraded)
	}
	return nil
}

func MDSyncAction(req apischema.MDSyncActionRequest) (apischema.MDArray, error) {
	if req.Action != "check" && req.Action != "repair" && req.Action != "idle" {
		return apischema.MDArray{}, fmt.Errorf("%w: sync action %q must be check, repair or idle", bridgeipc.ErrInvalidArgs, req.Action)
	}
	a, err := readMDArray(req.Name)
	if err != nil {
		return apischema.MDArray{}, err
	}
	if a.SyncAction == "" {
		return apischema.MDArray{}, fmt.Errorf("%w: %s (%s) has no sync action", bridgeipc.ErrInvalidArgs, a.Name, a.Level)
	}
	if req.Action != "idle" && a.SyncAction != "idle" {
		return apischema.MDArray{}, fmt.Errorf("%w: %s is already running %s", bridgeipc.ErrConflict, a.Name, a.SyncAction)
	}
	if err := writeMDAttr(filepath.Join(mdSysfsRoot, "block", a.Name, "md"), "sync_action", req.Action); err != nil {
		return apischema.MDArray{}, err
	}
	slog.Info("set md sync action", "array", a.Name, "action", req.Action)
	return readMDArray(a.Name)
}

func MDMemberFail(req apischema.MDMemberRequest) (apischema.MDArray, error) {
	a, err := readMDArray(req.Name)
	if err != nil {
		return apischema.MDArray{}, err
	}
	m, dir, err := mdMemberDir(a, req.Member)
	if err != nil {
		return apischema.MDArray{}, err
	}
	if err := checkMDFailAllowed(a, m); err != nil {
		return apischema.MDArray{}, err
	}
	if err := writeMDAttr(dir, "state", "faulty"); err != nil {
		return apischema.MDArray{}, err
	}
	slog.Info("marked md member failed", "array", a.Name, "member", m.Name)
	return readMDArray(a.Name)
}

func MDMemberRemove(req apischema.MDMemberRequest) (apischema.MDArray, error) {
	a, err := readMDArray(req.Name)
	if err != nil {
		return apischema.MDArray{}, err
	}
	m, dir, err := mdMemberDir(a, req.Member)
	if err != nil {
		return apischema.MDArray{}, err
	}
	if !slices.Contains(m.State, "faulty") && !slices.Contains(m.State, "spare") && m.Slot != -1 {
		return apischema.MDArray{}, fmt.Errorf("%w: %s is still active in %s; mark it failed first", bridgeipc.ErrConflict, m.Name, a.Name)
	}
	if err := writeMDAttr(dir, "state", "remove"); err != nil {
		return apischema.MDArray{}, err
	}
	slog.Info("removed md member", "array", a.Name, "member", m.Name)
	return readMDArray(a.Name)
}

func MDMemberAdd(ctx context.Context, req apischema.MDMemberAddRequest) (apischema.MDArray, error) {
	a, err := readMDArray(req.Name)
	if err != nil {
		return apischema.MDArray{}, err
	}
	if err := mdDeviceFree(ctx, req.Device); err != nil {
		return apischema.MDArray{}, err
	}
	if err := mdadmCommand(ctx, "--manage", a.Path, "--add", req.Device); err != nil {
		return apischema.MDArray{}, err
	}
	slog.Info("added md member", "array", a.Name, "device", req.Device)
	return readMDArray(a.Name)
}

func MDMemberReplace(ctx context.Context, req apischema.MDMemberReplaceRequest) (apischema.MDArray, error) {
	a, err := readMDArray(req.Name)
	if err != nil {
		return apischema.MDArray{}, err
	}
	m, dir, err := mdMemberDir(a, req.Member)
	if err != nil {
		return apischema.MDArray{}, err
	}
	if !mdInSync(m) {
		return apischema.MDArray{}, fmt.Errorf("%w: %s is not in sync in %s", bridgeipc.ErrConflict, m.Name, a.Name)
	}
	if err := mdDeviceFree(ctx, req.Device); err != nil {
		return apischema.MDArray{}, err
	}
	if err := mdadmCommand(ctx, "--manage", a.Path, "--add", req.Device); err != nil {
		return apischema.MDArray{}, err
	}
	if err := writeMDAttr(dir, "state", "want_replacement"); err != nil {
		return apischema.MDArray{}, fmt.Errorf("%s was added to %s as a spare but %s could not be marked for replacement: %w", req.Device, a.Name, m.Name, err)
	}
	slog.Info("started md member replacement", "array", a.Name, "member", m.Name, "device", req.Device)
	return readMDArray(a.Name)
}
