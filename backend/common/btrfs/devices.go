package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// AllowedProfiles are the block-group profiles LinuxIO converts to; raid5
// and raid6 are deliberately absent.
var AllowedProfiles = []string{"single", "dup", "raid0", "raid1", "raid1c3", "raid1c4", "raid10"}

// progressInterval is how often blocking operations report; a variable so
// tests can poll quickly.
var progressInterval = 2 * time.Second

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

// deviceName fills a vol_args name, refusing paths the struct cannot hold.
func deviceName(args *volArgs, device string) error {
	if len(device) >= len(args.Name) {
		return fmt.Errorf("device path too long: %s", device)
	}
	copy(args.Name[:], device)
	return nil
}

// edgeWipeBytes is how much of each end of a device is zeroed before it
// joins a filesystem with force: it covers every known signature and
// partition table, as mkfs.btrfs does.
const edgeWipeBytes = 2 << 20

// wipeDeviceEdges zeroes the first and last edgeWipeBytes of device; a var
// so tests can stub it.
var wipeDeviceEdges = func(device string) error {
	size, err := blockDeviceSize(device)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	n := min(size, edgeWipeBytes)
	zeros := make([]byte, n)
	for _, off := range []uint64{0, size - n} {
		if _, err := f.WriteAt(zeros, int64(off)); err != nil {
			return fmt.Errorf("wipe %s: %w", device, err)
		}
	}
	return f.Sync()
}

// AddDevice adds device to the filesystem; force first wipes the
// signatures on it. Callers check that device is free.
func AddDevice(ctx context.Context, uuid, device string, force bool) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	var args volArgs
	if err := deviceName(&args, device); err != nil {
		return err
	}
	if force {
		if err := wipeDeviceEdges(device); err != nil {
			return err
		}
	}
	if err := fsIoctl(fs.Mountpoints[0].Mountpoint, iocAddDev, unsafe.Pointer(&args)); err != nil {
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
	// Fail closed: inventory only logs a failed space read, leaving
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
	args := volArgsV2{Flags: devSpecByID}
	putU64(args.Name[:], 0, uint64(id))
	if err := fsIoctl(fs.Mountpoints[0].Mountpoint, iocRmDevV2, unsafe.Pointer(&args)); err != nil {
		return fmt.Errorf("remove device %d: %w", id, err)
	}
	return nil
}

// waitOp waits for a blocking kernel operation running in another
// goroutine, reporting its progress every interval and canceling it when
// ctx ends; the operation's own error is returned.
func waitOp(ctx context.Context, done <-chan error, cancel func(), poll func() (percent float64, running bool), report func(float64)) error {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	ctxDone := ctx.Done()
	for {
		select {
		case err := <-done:
			return err
		case <-ctxDone:
			ctxDone = nil // cancel once, then wait for the operation to return
			cancel()
		case <-ticker.C:
			if pct, running := poll(); running {
				report(pct)
			}
		}
	}
}

// Replace copies device id onto newDevice and makes it the member in its
// place, reporting progress until done; canceling ctx cancels the replace.
// Callers check that newDevice is free.
func Replace(ctx context.Context, uuid string, id int, newDevice string, report func(float64)) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if !hasDevice(fs, id) {
		return fmt.Errorf("device %d not found in filesystem %s", id, uuid)
	}
	mnt := fs.Mountpoints[0].Mountpoint
	args := devReplaceArgs{Cmd: replaceCmdStart}
	if len(newDevice) >= 1024 {
		return fmt.Errorf("device path too long: %s", newDevice)
	}
	putU64(args.Params[:], replaceSrcDevID, uint64(id))
	copy(args.Params[replaceTgtDevName:], newDevice)
	done := make(chan error, 1)
	go func() {
		ioctlErr := fsIoctl(mnt, iocDevReplace, unsafe.Pointer(&args))
		if ioctlErr == nil {
			if msg, refused := replaceResults[args.Result]; refused {
				ioctlErr = errors.New(msg)
			}
		}
		done <- ioctlErr
	}()
	err = waitOp(ctx, done, func() {
		cancel := devReplaceArgs{Cmd: replaceCmdCancel}
		_ = fsIoctl(mnt, iocDevReplace, unsafe.Pointer(&cancel))
	}, func() (float64, bool) {
		status := devReplaceArgs{Cmd: replaceCmdStatus}
		if fsIoctl(mnt, iocDevReplace, unsafe.Pointer(&status)) != nil || getU64(status.Params[:], replaceStatusState) != replaceStateStarted {
			return 0, false
		}
		return float64(getU64(status.Params[:], replaceStatusPerMill)) / 10, true
	}, report)
	if err != nil {
		if errors.Is(err, unix.ECANCELED) {
			return fmt.Errorf("replace of device %d canceled", id)
		}
		return fmt.Errorf("replace device %d: %w", id, err)
	}
	return nil
}

// ReplaceResult returns the refreshed filesystem once a replace ended,
// checking that newDevice took the slot.
func ReplaceResult(ctx context.Context, uuid string, id int, newDevice string) (BtrfsFilesystem, error) {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return BtrfsFilesystem{}, err
	}
	if !slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == id && d.Path == newDevice }) {
		return BtrfsFilesystem{}, fmt.Errorf("replace ended without installing %s (see dmesg)", newDevice)
	}
	return fs, nil
}

// BalanceOptions selects what a balance rewrites: a nil profile keeps the
// current one, DataUsageMax limits data to chunks used at most that percent
// (which frees unallocated space), Soft converts only the chunks not already
// in the target profile, and no option at all balances everything.
type BalanceOptions struct {
	DataProfile     *string
	MetadataProfile *string
	DataUsageMax    *int
	Soft            bool
}

// Balance runs a balance to completion, reporting progress; canceling ctx
// cancels it. Converting metadata converts system chunks with it, as
// btrfs-progs does.
func Balance(ctx context.Context, uuid string, opts BalanceOptions, report func(float64)) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if err = ValidateProfiles(opts.DataProfile, opts.MetadataProfile, len(fs.Devices)); err != nil {
		return err
	}
	if opts.DataUsageMax != nil && (*opts.DataUsageMax < 0 || *opts.DataUsageMax > 100) {
		return fmt.Errorf("usage filter %d is not a percentage", *opts.DataUsageMax)
	}
	var args balanceArgs
	convert := uint64(balanceArgsConvert)
	if opts.Soft {
		convert |= balanceArgsSoft
	}
	if opts.DataProfile != nil {
		args.Flags |= balanceData
		args.Data.Flags |= convert
		args.Data.Target = profileFlags[*opts.DataProfile]
	}
	if opts.MetadataProfile != nil {
		args.Flags |= balanceMetadata | balanceSystem
		args.Meta.Flags |= convert
		args.Meta.Target = profileFlags[*opts.MetadataProfile]
		args.Sys = args.Meta
	}
	if opts.DataUsageMax != nil {
		args.Flags |= balanceData
		args.Data.Flags |= balanceArgsUsage
		args.Data.Usage = uint64(*opts.DataUsageMax)
	}
	if args.Flags == 0 {
		args.Flags = balanceData | balanceMetadata | balanceSystem
	}
	mnt := fs.Mountpoints[0].Mountpoint
	done := make(chan error, 1)
	go func() { done <- fsIoctl(mnt, iocBalanceV2, unsafe.Pointer(&args)) }()
	err = waitOp(ctx, done, func() { _ = balanceCancel(mnt) }, func() (float64, bool) {
		running, pct, progressErr := balanceProgressOf(mnt)
		return pct, progressErr == nil && running
	}, report)
	switch {
	case errors.Is(err, unix.ECANCELED):
		return errors.New("balance canceled")
	case errors.Is(err, unix.EINPROGRESS):
		return errors.New("a balance is already running")
	case err != nil:
		return fmt.Errorf("balance: %w", err)
	}
	return nil
}

// BalanceResult returns the refreshed filesystem once a balance ended,
// checking that the requested profiles are now the only ones.
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
			return BtrfsFilesystem{}, fmt.Errorf("balance ended without converting %s to %s (see dmesg)", want.kind, *want.target)
		}
	}
	return fs, nil
}

// balanceProgressOf reports the kernel's balance state on a mountpoint.
func balanceProgressOf(mnt string) (running bool, percent float64, err error) {
	var args balanceArgs
	if err := fsIoctl(mnt, iocBalanceProgress, unsafe.Pointer(&args)); err != nil {
		if errors.Is(err, unix.ENOTCONN) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("balance progress: %w", err)
	}
	if args.State&balanceStatePauseReq != 0 {
		return false, 0, errors.New("balance is paused")
	}
	if args.Stat.Expected > 0 {
		percent = min(100, float64(args.Stat.Completed)*100/float64(args.Stat.Expected))
	}
	return args.State&balanceStateRunning != 0, percent, nil
}

func balanceCancel(mnt string) error {
	cmd := int32(balanceCtlCancel)
	if err := fsIoctl(mnt, iocBalanceCtl, unsafe.Pointer(&cmd)); err != nil {
		if errors.Is(err, unix.ENOTCONN) {
			return errors.New("no balance is running")
		}
		return fmt.Errorf("cancel balance: %w", err)
	}
	return nil
}

// BalanceCancel cancels the running balance.
func BalanceCancel(ctx context.Context, uuid string) error {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	return balanceCancel(mnt)
}

// GrowDevice resizes device id to the whole block device, as after
// replacing it with a bigger disk.
func GrowDevice(ctx context.Context, uuid string, id int) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if !hasDevice(fs, id) {
		return fmt.Errorf("device %d not found in filesystem %s", id, uuid)
	}
	var args volArgs
	copy(args.Name[:], fmt.Sprintf("%d:max", id))
	if err := fsIoctl(fs.Mountpoints[0].Mountpoint, iocResize, unsafe.Pointer(&args)); err != nil {
		return fmt.Errorf("grow device %d: %w", id, err)
	}
	return nil
}

// SetLabel renames the filesystem.
func SetLabel(ctx context.Context, uuid, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	var buf [labelSize]byte
	copy(buf[:], label)
	if err := fsIoctl(mnt, iocSetFSLabel, unsafe.Pointer(&buf)); err != nil {
		return fmt.Errorf("set label: %w", err)
	}
	return nil
}
