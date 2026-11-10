package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// too few for any current profile. It blocks until data has been moved off,
// reporting the share of the device's allocation freed since this call: the
// kernel has no progress for a removal, and a resumed one starts again at 0
// for what is left. Canceling ctx cancels the removal in the kernel (5.14
// and later).
func RemoveDevice(ctx context.Context, uuid string, id int, report func(float64)) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	if err = checkRemovable(fs, id); err != nil {
		return err
	}
	mnt := fs.Mountpoints[0].Mountpoint
	held := fs.Devices[slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == id })].UsedBytes
	args := volArgsV2{Flags: devSpecByID}
	putU64(args.Name[:], 0, uint64(id))
	done := make(chan error, 1)
	go func() { done <- blockingIoctl(mnt, iocRmDevV2, unsafe.Pointer(&args)) }()
	// A cancel sent before the removal claimed the pool is lost with
	// ENOTCONN, so every tick sends it again until one is not.
	lost := false
	cancel := func() {
		cancelErr := rmDevCancel(mnt)
		lost = errors.Is(cancelErr, unix.ENOTCONN)
		if cancelErr != nil && !lost {
			slog.Warn("btrfs device removal not canceled", "uuid", uuid, "device", id, "error", cancelErr)
		}
	}
	err = waitOp(ctx, done, cancel, func() (float64, bool) {
		if lost {
			cancel()
		}
		return removalPercent(mnt, id, held)
	}, report)
	switch {
	case errors.Is(err, unix.ECANCELED):
		return fmt.Errorf("removal of device %d canceled", id)
	case err != nil:
		return fmt.Errorf("remove device %d: %w", id, err)
	}
	return nil
}

// removalPercent is the share of the held bytes device id had allocated that
// its removal has freed: the kernel frees them chunk by chunk. It is unknown
// for a device that held nothing or has left the pool.
func removalPercent(mnt string, id int, held uint64) (float64, bool) {
	info := devInfoArgs{DevID: uint64(id)}
	if held == 0 || fsIoctl(mnt, iocDevInfo, unsafe.Pointer(&info)) != nil {
		return 0, false
	}
	return 100 * float64(held-min(info.BytesUsed, held)) / float64(held), true
}

// checkRemovable refuses removing device id when the remaining devices are
// too few for any current profile.
func checkRemovable(fs BtrfsFilesystem, id int) error {
	if !hasDevice(fs, id) {
		return fmt.Errorf("device %d not found in filesystem %s", id, fs.UUID)
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
		return fmt.Errorf("cannot determine current profiles of %s", fs.UUID)
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
	return nil
}

// ResumeRemove removes device id again after an interruption stopped its
// removal; the chunks already moved stay moved and the kernel skips them. A
// device no longer in the pool means the removal had finished.
func ResumeRemove(ctx context.Context, uuid string, id int, report func(float64)) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	// Inventory skips a device it failed to read, which would look removed.
	if !fs.HealthKnown {
		return fmt.Errorf("could not read the devices of pool %s", uuid)
	}
	if !hasDevice(fs, id) {
		return nil
	}
	if fs.Degraded {
		return fmt.Errorf("pool %s is degraded; removal not resumed", uuid)
	}
	return RemoveDevice(ctx, uuid, id, report)
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
		ioctlErr := blockingIoctl(mnt, iocDevReplace, unsafe.Pointer(&args))
		// The kernel copies Result back only on success; a refusal it
		// returns instead, such as a running replace, is a code.
		var code kernelCode
		switch {
		case errors.As(ioctlErr, &code):
			if msg, refused := replaceResults[uint64(code)]; refused {
				ioctlErr = errors.New(msg)
			}
		case ioctlErr == nil:
			if msg, refused := replaceResults[args.Result]; refused {
				ioctlErr = errors.New(msg)
			}
		}
		done <- ioctlErr
	}()
	err = waitOp(ctx, done, func() { _ = cancelReplace(mnt) }, func() (float64, bool) {
		state, pct, _, statusErr := replaceStatusOf(mnt)
		return pct, statusErr == nil && state == replaceStateStarted
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

// replaceStatusOf returns the state of the pool's replace, its percent and
// when it started. The state persists, so it may be an earlier replace's end.
func replaceStatusOf(mnt string) (state uint64, pct float64, started time.Time, err error) {
	status := devReplaceArgs{Cmd: replaceCmdStatus}
	if err = fsIoctl(mnt, iocDevReplace, unsafe.Pointer(&status)); err != nil {
		return 0, 0, time.Time{}, fmt.Errorf("replace status: %w", err)
	}
	p := status.Params[:]
	return getU64(p, replaceStatusState), float64(getU64(p, replaceStatusPerMill)) / 10, time.Unix(int64(getU64(p, replaceStatusTimeStarted)), 0), nil
}

func cancelReplace(mnt string) error {
	args := devReplaceArgs{Cmd: replaceCmdCancel}
	return fsIoctl(mnt, iocDevReplace, unsafe.Pointer(&args))
}

// replaceSource returns the id of the device the pool's replace copies from,
// read from the replace item of the dev tree (struct btrfs_dev_replace_item,
// src_devid first). The kernel names none once the replace ended, or when a
// degraded mount lacked the source (dev-replace.c btrfs_run_dev_replace).
func replaceSource(mnt string) (id uint64, known bool, err error) {
	key := searchKey{TreeID: devTreeObjectID, MinType: devReplaceKey, MaxType: devReplaceKey, MaxTransid: ^uint64(0)}
	err = treeSearch(mnt, key, func(_ searchHeader, item []byte) {
		if len(item) >= 8 {
			id, known = getU64(item, 0), true
		}
	})
	if err != nil {
		return 0, false, fmt.Errorf("read the replace item: %w", err)
	}
	return id, known && id != ^uint64(0), nil
}

// checkReplaceSource refuses the pool's replace when the kernel says it
// copies from a device other than id: a pool runs one replace at a time, and
// it need not be the one a job is about.
func checkReplaceSource(mnt, uuid string, id int) error {
	src, known, err := replaceSource(mnt)
	switch {
	case err != nil:
		return err
	case known && src != uint64(id):
		return fmt.Errorf("pool %s is replacing device %d, not device %d", uuid, src, id)
	}
	return nil
}

// ResumeReplace follows the replace of device id that the kernel resumed at
// mount after a power loss, reporting progress until it ends. It never starts
// one: the request names the target by path, which a reboot may give to
// another disk. A running replace the kernel says copies from another device
// is refused. A replace already FINISHED counts only when it started no
// earlier than since, the job's start: the state persists, so it may be an
// earlier replace's. When ctx ends it returns ctx.Err() and leaves the
// replace running; the caller decides whether to cancel it.
func ResumeReplace(ctx context.Context, uuid string, id int, since time.Time, report func(float64)) error {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	state, pct, started, err := replaceStatusOf(mnt)
	if err == nil && state == replaceStateStarted {
		if err = checkReplaceSource(mnt, uuid, id); err != nil {
			return err
		}
	}
	ours := !since.IsZero() && !started.Before(since.Truncate(time.Second)) // the kernel keeps whole seconds
	for err == nil && state == replaceStateStarted {
		ours = true
		report(pct)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		state, pct, _, err = replaceStatusOf(mnt)
	}
	switch {
	case err != nil:
		return err
	case state == replaceStateFinished && ours:
		fs, findErr := FindFilesystem(ctx, uuid)
		if findErr != nil {
			return findErr
		}
		if !slices.ContainsFunc(fs.Devices, func(d BtrfsDevice) bool { return d.ID == id && !d.Missing }) {
			return fmt.Errorf("the replace of device %d finished, but the device is missing from pool %s", id, uuid)
		}
		return nil
	case state == replaceStateSuspended:
		return fmt.Errorf("the kernel suspended the replace of device %d; remount the pool to continue it", id)
	}
	return fmt.Errorf("the replace of device %d did not survive the interruption; start it again", id)
}

// CancelReplace cancels the running replace of device id of filesystem uuid;
// the kernel destroys the target and keeps the source. A replace of another
// device is left running.
func CancelReplace(ctx context.Context, uuid string, id int) error {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return err
	}
	if err = checkReplaceSource(mnt, uuid, id); err != nil {
		return err
	}
	if err = cancelReplace(mnt); err != nil {
		return fmt.Errorf("cancel replace: %w", err)
	}
	return nil
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
	err = runBalance(ctx, fs.Mountpoints[0].Mountpoint, &args, report)
	if errors.Is(err, unix.EINPROGRESS) {
		return errors.New("a balance is already running")
	}
	return err
}

var (
	// errBalancePaused ends a balance a pause stopped. The kernel keeps it
	// and resumes it at the next mount.
	errBalancePaused = errors.New("balance paused")
	// errBalanceCanceledElsewhere ends a balance canceled by another caller,
	// such as the cancel route, so its job ends canceled, not failed.
	errBalanceCanceledElsewhere = fmt.Errorf("%w: balance canceled", context.Canceled)
)

// runBalance issues BALANCE_V2 with args on mnt and waits for it, reporting
// progress. Canceling ctx cancels a running balance but never a paused one.
func runBalance(ctx context.Context, mnt string, args *balanceArgs, report func(float64)) error {
	done := make(chan error, 1)
	go func() { done <- blockingIoctl(mnt, iocBalanceV2, unsafe.Pointer(args)) }()
	err := waitOp(ctx, done, func() { cancelRunningBalance(mnt) }, func() (float64, bool) {
		state, pct, progressErr := balanceProgressOf(mnt)
		return pct, progressErr == nil && state&balanceStateRunning != 0
	}, report)
	// The kernel copies the state back on ECANCELED, saying who stopped it.
	canceled := errors.Is(err, unix.ECANCELED)
	switch {
	case canceled && args.State&balanceStatePauseReq != 0:
		return errBalancePaused
	case canceled && args.State&balanceStateCancelReq != 0 && ctx.Err() == nil:
		return errBalanceCanceledElsewhere
	case canceled:
		return errors.New("balance canceled")
	case err != nil:
		return fmt.Errorf("balance: %w", err)
	}
	return nil
}

// PauseBalance pauses the running balance of filesystem uuid and returns
// once it stopped; the kernel keeps it and resumes it at the next mount. It
// reports whether there was a running balance to pause.
func PauseBalance(ctx context.Context, uuid string) (bool, error) {
	mnt, err := mountpointFor(ctx, uuid)
	if err != nil {
		return false, err
	}
	err = balanceCtl(mnt, balanceCtlPause)
	switch {
	case errors.Is(err, unix.ENOTCONN):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("pause balance: %w", err)
	}
	return true, nil
}

// ResumeBalance continues a balance a shutdown interrupted. It resumes a
// paused one and follows one the kernel resumed at mount. With none, it runs
// the balance again unless this job had paused it: the kernel resumes a
// paused balance at mount, so one that is gone has ended.
func ResumeBalance(ctx context.Context, uuid string, opts BalanceOptions, paused bool, report func(float64)) error {
	fs, err := FindFilesystem(ctx, uuid)
	if err != nil {
		return err
	}
	// Inventory skips a device it failed to read, which hides a missing one.
	if !fs.HealthKnown {
		return fmt.Errorf("could not read the devices of pool %s", uuid)
	}
	if fs.Degraded {
		return fmt.Errorf("pool %s is degraded; balance not resumed", uuid)
	}
	mnt := fs.Mountpoints[0].Mountpoint
	state, _, err := balanceProgressOf(mnt)
	switch {
	case errors.Is(err, unix.ENOTCONN) && paused:
		return nil
	case errors.Is(err, unix.ENOTCONN):
		// The chunks it already converted need no second pass.
		opts.Soft = opts.DataProfile != nil || opts.MetadataProfile != nil
		return Balance(ctx, uuid, opts, report)
	case err != nil:
		return err
	case state&balanceStateRunning != 0:
		return observeBalance(ctx, mnt, report)
	}
	err = runBalance(ctx, mnt, &balanceArgs{Flags: balanceResume}, report)
	if errors.Is(err, unix.EINPROGRESS) { // the kernel resumed it meanwhile
		return observeBalance(ctx, mnt, report)
	}
	return err
}

// observeBalance follows a balance another thread runs, such as the
// kernel's after a mount, until it ends; canceling ctx cancels it.
func observeBalance(ctx context.Context, mnt string, report func(float64)) error {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	ctxDone := ctx.Done()
	for {
		select {
		case <-ctxDone:
			ctxDone = nil // cancel once, then wait for the balance to end
			cancelRunningBalance(mnt)
		case <-ticker.C:
			state, pct, err := balanceProgressOf(mnt)
			switch {
			case errors.Is(err, unix.ENOTCONN):
				return ctx.Err() // it finished, or the cancel ended it
			case err != nil:
				return err
			case state&balanceStateRunning == 0:
				return errBalancePaused
			}
			report(pct)
		}
	}
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

// balanceProgressOf reads the kernel's balance state on a mountpoint and how
// far it got: RUNNING while it runs, 0 while paused, ENOTCONN for none.
func balanceProgressOf(mnt string) (state uint64, percent float64, err error) {
	var args balanceArgs
	if err := fsIoctl(mnt, iocBalanceProgress, unsafe.Pointer(&args)); err != nil {
		return 0, 0, fmt.Errorf("balance progress: %w", err)
	}
	if args.Stat.Expected > 0 {
		percent = min(100, float64(args.Stat.Completed)*100/float64(args.Stat.Expected))
	}
	return args.State, percent, nil
}

// cancelRunningBalance cancels the balance on mnt only while it runs and no
// pause is under way: a cancel deletes a paused balance, and a shutdown
// pauses the balance before it cancels the job's context.
func cancelRunningBalance(mnt string) {
	state, _, err := balanceProgressOf(mnt)
	if err == nil && state&(balanceStateRunning|balanceStatePauseReq) == balanceStateRunning {
		err = balanceCtl(mnt, balanceCtlCancel)
	}
	// ENOTCONN: the balance ended before the read or the cancel.
	if err != nil && !errors.Is(err, unix.ENOTCONN) {
		slog.Warn("btrfs balance not canceled", "mountpoint", mnt, "error", err)
	}
}

func balanceCancel(mnt string) error {
	if err := balanceCtl(mnt, balanceCtlCancel); err != nil {
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
