package btrfs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// AllowedProfiles are the block-group profiles LinuxIO converts to; raid5
// and raid6 are deliberately absent.
var AllowedProfiles = []string{"single", "dup", "raid0", "raid1", "raid1c3", "raid1c4", "raid10"}

// MinDevices returns how many devices profile needs, or 0 when unknown.
func MinDevices(profile string) int {
	switch profile {
	case "single", "dup":
		return 1
	case "raid0", "raid1":
		return 2
	case "raid1c3":
		return 3
	case "raid1c4", "raid10":
		return 4
	}
	return 0
}

// ValidateProfiles checks requested data and metadata profiles against the
// device count; a nil profile is left as it is.
func ValidateProfiles(data, meta *string, deviceCount int) error {
	for _, p := range []*string{data, meta} {
		if p == nil {
			continue
		}
		if strings.EqualFold(*p, "raid5") || strings.EqualFold(*p, "raid6") {
			return errors.New("raid5 and raid6 are not supported")
		}
		if !slices.Contains(AllowedProfiles, *p) {
			return fmt.Errorf("unsupported profile %s", *p)
		}
		if n := MinDevices(*p); deviceCount < n {
			return fmt.Errorf("profile %s needs at least %d devices", *p, n)
		}
	}
	return nil
}

// FindFilesystem returns the mounted btrfs filesystem with uuid.
func FindFilesystem(ctx context.Context, uuid string) (BtrfsFilesystem, error) {
	filesystems, err := ListFilesystems(ctx)
	if err != nil {
		return BtrfsFilesystem{}, err
	}
	for _, fs := range filesystems {
		if fs.UUID == uuid {
			return fs, nil
		}
	}
	return BtrfsFilesystem{}, fmt.Errorf("btrfs filesystem %s not found", uuid)
}

// mountpointFor returns a mountpoint of the filesystem using only findmnt,
// which keeps status polling cheap.
func mountpointFor(ctx context.Context, uuid string) (string, error) {
	mounts, err := listMounts(ctx)
	if err != nil {
		return "", fmt.Errorf("list btrfs mounts: %w", err)
	}
	for _, m := range mounts {
		if m.UUID == uuid {
			return m.Mountpoint, nil
		}
	}
	return "", fmt.Errorf("btrfs filesystem %s not found", uuid)
}

func hasDevice(fs BtrfsFilesystem, id int) bool {
	return slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == id })
}

// AddDevice adds device to the filesystem; force overwrites an existing
// signature. Callers check that device is free.
func AddDevice(ctx context.Context, uuid, device string, force bool) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	args := []string{"device", "add"}
	if force {
		args = append(args, "-f")
	}
	if _, err := runBtrfs(ctx, append(args, device, fs.Mountpoints[0].Mountpoint)...); err != nil {
		return fmt.Errorf("add device %s: %w", device, err)
	}
	return nil
}

// RemoveDevice removes device id, refusing when the remaining devices are
// too few for any current profile. It blocks until data has been moved off.
func RemoveDevice(ctx context.Context, uuid string, id int) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if !hasDevice(fs, id) {
		return fmt.Errorf("device %d not found in filesystem %s", id, uuid)
	}
	// Fail closed: inventory only logs a failed `filesystem usage`, leaving
	// Allocation empty, and an unknown profile has no known minimum.
	seen := map[string]bool{}
	for _, a := range fs.Allocation {
		for k := range strings.SplitSeq(a.Kind, "+") {
			seen[k] = true
		}
	}
	if !seen["data"] || !seen["metadata"] {
		return fmt.Errorf("cannot determine current profiles of %s", uuid)
	}
	// A missing device holds no data copy, so only present devices count.
	remaining := 0
	for _, d := range fs.Devices {
		if !d.Missing && d.ID != id {
			remaining++
		}
	}
	for _, a := range fs.Allocation {
		n := MinDevices(a.Profile)
		if n == 0 && a.Profile != "" {
			return fmt.Errorf("cannot determine minimum devices for profile %s", a.Profile)
		}
		if remaining < n {
			return fmt.Errorf("cannot remove device %d: %s profile %s needs at least %d devices", id, a.Kind, a.Profile, n)
		}
	}
	if _, err := runBtrfs(ctx, "device", "remove", strconv.Itoa(id), fs.Mountpoints[0].Mountpoint); err != nil {
		return fmt.Errorf("remove device %d: %w", id, err)
	}
	return nil
}

// ReplaceStart starts replacing device id with newDevice in the background.
// Callers check that newDevice is free.
func ReplaceStart(ctx context.Context, uuid string, id int, newDevice string) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if !hasDevice(fs, id) {
		return fmt.Errorf("device %d not found in filesystem %s", id, uuid)
	}
	if _, err := runBtrfs(ctx, "replace", "start", "-f", strconv.Itoa(id), newDevice, fs.Mountpoints[0].Mountpoint); err != nil {
		return fmt.Errorf("start replace of device %d: %w", id, err)
	}
	return nil
}

// ReplaceResult returns the refreshed filesystem once a replace stopped
// running. `replace start` daemonizes before the kernel call, so a refused or
// canceled replace exits 0; only the device list shows whether it happened.
func ReplaceResult(ctx context.Context, uuid string, id int, newDevice string) (BtrfsFilesystem, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return BtrfsFilesystem{}, err
	}
	if !slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == id && d.Path == newDevice }) {
		return BtrfsFilesystem{}, fmt.Errorf("replace ended without installing %s (canceled or failed; see dmesg)", newDevice)
	}
	return fs, nil
}

// ReplaceStatus reports whether a replace is running and its progress. A
// replace that ended without finishing is an error.
func ReplaceStatus(ctx context.Context, uuid string) (running bool, percent float64, err error) {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return false, 0, err
	}
	out, err := runBtrfs(ctx, "replace", "status", "-1", mnt)
	if err != nil {
		return false, 0, fmt.Errorf("replace status: %w", err)
	}
	return parseReplaceStatus(out)
}

var replaceDone = regexp.MustCompile(`([\d.]+)% done`)

// parseReplaceStatus parses `btrfs replace status -1`.
func parseReplaceStatus(out string) (running bool, percent float64, err error) {
	if m := replaceDone.FindStringSubmatch(out); m != nil {
		pct, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return false, 0, fmt.Errorf("parse replace progress %q: %w", m[0], err)
		}
		return true, pct, nil
	}
	if strings.Contains(out, "finished on") {
		return false, 100, nil
	}
	return false, 0, fmt.Errorf("replace did not finish: %s", strings.TrimSpace(out))
}

// BalanceStart validates the requested profiles and starts a balance in the
// background, converting to them; with both nil it balances everything.
func BalanceStart(ctx context.Context, uuid string, data, meta *string) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if err := ValidateProfiles(data, meta, len(fs.Devices)); err != nil {
		return err
	}
	args := []string{"balance", "start", "--background"}
	if data != nil {
		args = append(args, "-dconvert="+*data)
	}
	if meta != nil {
		args = append(args, "-mconvert="+*meta)
	}
	if data == nil && meta == nil {
		// Skips the 10 s warning delay btrfs-progs adds before a filterless balance.
		args = append(args, "--full-balance")
	}
	if _, err := runBtrfs(ctx, append(args, fs.Mountpoints[0].Mountpoint)...); err != nil {
		return fmt.Errorf("start balance: %w", err)
	}
	return nil
}

// BalanceResult returns the refreshed filesystem once a balance stopped
// running. `balance start --background` forks before the kernel call, so a
// refused or canceled conversion exits 0; only the profiles show the outcome.
func BalanceResult(ctx context.Context, uuid string, data, meta *string) (BtrfsFilesystem, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return BtrfsFilesystem{}, err
	}
	for _, want := range []struct {
		kind   string
		target *string
	}{{"data", data}, {"metadata", meta}} {
		if want.target == nil {
			continue
		}
		present := slices.ContainsFunc(fs.Allocation, func(a BtrfsAllocation) bool { return a.Kind == want.kind })
		stale := slices.ContainsFunc(fs.Allocation, func(a BtrfsAllocation) bool { return a.Kind == want.kind && a.Profile != *want.target })
		if !present || stale {
			return BtrfsFilesystem{}, fmt.Errorf("balance ended without converting %s to %s (canceled or failed; see dmesg)", want.kind, *want.target)
		}
	}
	return fs, nil
}

// BalanceStatus reports whether a balance is running and its progress.
func BalanceStatus(ctx context.Context, uuid string) (running bool, percent float64, err error) {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return false, 0, err
	}
	// `balance status` exits 1 while a balance is running or paused, so the
	// output decides and the exit error only counts when there is none.
	out, err := runBtrfs(ctx, "balance", "status", mnt)
	if err != nil && out == "" {
		return false, 0, fmt.Errorf("balance status: %w", err)
	}
	return parseBalanceStatus(out)
}

var balanceLeft = regexp.MustCompile(`(\d+)% left`)

// parseBalanceStatus parses `btrfs balance status`; percent is complete.
func parseBalanceStatus(out string) (running bool, percent float64, err error) {
	switch {
	case strings.Contains(out, "No balance found"):
		return false, 0, nil
	case strings.Contains(out, "is paused"):
		return false, 0, errors.New("balance is paused")
	case !strings.Contains(out, "is running"):
		return false, 0, fmt.Errorf("unexpected balance status %q", strings.TrimSpace(out))
	}
	if m := balanceLeft.FindStringSubmatch(out); m != nil {
		left, _ := strconv.Atoi(m[1])
		percent = float64(100 - left)
	}
	return true, percent, nil
}

// BalanceCancel cancels the running balance.
func BalanceCancel(ctx context.Context, uuid string) error {
	return runOnMount(ctx, uuid, "cancel balance", "balance", "cancel")
}

// ScrubStart starts a scrub in the background.
func ScrubStart(ctx context.Context, uuid string) error {
	return runOnMount(ctx, uuid, "start scrub", "scrub", "start")
}

// ScrubCancel cancels the running scrub.
func ScrubCancel(ctx context.Context, uuid string) error {
	return runOnMount(ctx, uuid, "cancel scrub", "scrub", "cancel")
}

// ScrubStatus returns the current scrub state.
func ScrubStatus(ctx context.Context, uuid string) (BtrfsScrubStatus, error) {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return BtrfsScrubStatus{}, err
	}
	out, err := runBtrfs(ctx, "scrub", "status", "--raw", mnt)
	if err != nil {
		return BtrfsScrubStatus{}, fmt.Errorf("scrub status: %w", err)
	}
	return parseScrubStatus(out)
}

func runOnMount(ctx context.Context, uuid, what string, args ...string) error {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	if _, err := runBtrfs(ctx, append(args, mnt)...); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}
