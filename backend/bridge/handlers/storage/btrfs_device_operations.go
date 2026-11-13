package storage

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// BtrfsOpProgress reports a running pool create, device, balance or scrub
// operation.
type BtrfsOpProgress struct {
	Op      string  `json:"op"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
}

func (p BtrfsOpProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	pct := int(p.Percent)
	return bridgeipc.TaskProgress{Percentage: &pct, Phase: p.Op, Message: p.Message, Detail: p}
}

var btrfsDeviceRoutes = btrfsDeviceBindings().Routes()

// Seams for tests: the btrfs calls the resume hooks and the scrub cancel make.
var (
	btrfsPauseBalance         = btrfs.PauseBalance
	btrfsResumeBalance        = btrfs.ResumeBalance
	btrfsBalanceResult        = btrfs.BalanceResult
	btrfsScrubPositions       = btrfs.ScrubPositions
	btrfsScrubCancel          = btrfs.ScrubCancel
	btrfsMarkResyncCanceled   = markScheduledRunCanceled
	btrfsWithdrawResyncCancel = withdrawScheduledRunCancel
	btrfsScrubFrom            = btrfs.ScrubFrom
	btrfsResumeReplace        = btrfs.ResumeReplace
	btrfsCancelReplace        = btrfs.CancelReplace
	btrfsRemoveDevice         = btrfs.RemoveDevice
	btrfsResumeRemove         = btrfs.ResumeRemove
	btrfsGrowDevice           = btrfs.GrowDevice
	btrfsFindFilesystem       = btrfs.FindFilesystem
)

func btrfsDeviceBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.MutationTask("storage.btrfs_pool_create", runBtrfsPoolCreateTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsPoolCreateRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Label, "create"}, Label: "Creating btrfs pool"}
		})),
		apischema.MutationTask("storage.btrfs_device_remove", runBtrfsDeviceRemoveTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsDeviceRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.UUID, strconv.Itoa(req.DeviceID)}, Label: "Removing btrfs device"}
		})).Resumable(apischema.ResumeHooks[apischema.BtrfsDeviceRequest, btrfs.BtrfsFilesystem]{Suspend: suspendBtrfsRemove, Resume: resumeBtrfsRemove}),
		apischema.MutationTask("storage.btrfs_device_replace", runBtrfsDeviceReplaceTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsDeviceReplaceRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.UUID, strconv.Itoa(req.DeviceID)}, Label: "Replacing btrfs device"}
		})).Resumable(apischema.ResumeHooks[apischema.BtrfsDeviceReplaceRequest, btrfs.BtrfsFilesystem]{Suspend: suspendBtrfsReplace, Resume: resumeBtrfsReplace}),
		apischema.MutationTask("storage.btrfs_balance", runBtrfsBalanceTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsBalanceRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.UUID, "balance"}, Label: "Balancing btrfs filesystem"}
		})).Resumable(apischema.ResumeHooks[apischema.BtrfsBalanceRequest, btrfs.BtrfsFilesystem]{Suspend: suspendBtrfsBalance, Resume: resumeBtrfsBalance}),
		apischema.MutationTask("storage.btrfs_scrub_start", runBtrfsScrubTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.UUIDRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.UUID, "scrub"}, Label: "Scrubbing btrfs filesystem"}
		})).Resumable(apischema.ResumeHooks[apischema.UUIDRequest, btrfs.BtrfsFilesystem]{Suspend: suspendBtrfsScrub, Resume: resumeBtrfsScrub}),
	)
}

// ensureBtrfsDeviceFree refuses a device that is not free to join a btrfs
// filesystem; force only allows erasing an old filesystem left on it.
func ensureBtrfsDeviceFree(ctx context.Context, device string, force bool) error {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return err
	}
	return checkBtrfsDeviceFree(inv, device, force)
}

// checkBtrfsDeviceFree refuses what deviceInUse refuses and a device with
// partitions or holders. A filesystem signature needs force, and force never
// erases a member of a btrfs pool, md array or LVM volume group, mounted,
// running or active or not: the old pool is destroyed first. This is
// stricter than mkfs.btrfs -f and btrfs device add -f, which overwrite an
// unmounted member (see docs/btrfs.md).
func checkBtrfsDeviceFree(inv partitionInventory, device string, force bool) error {
	d, err := inv.wholeDeviceFree(device)
	if err != nil {
		return err
	}
	if m := memberOf(d); m != nil {
		return memberConflict(device, *m)
	}
	if d.FSType != "" && !force {
		return fmt.Errorf("%w: %s has an existing %s filesystem; turn on force to erase it", bridgeipc.ErrConflict, device, d.FSType)
	}
	return nil
}

// progressReporter reports one operation's percentage through the task.
func progressReporter(task *bridgeipc.Task, op, label string) func(float64) {
	return func(pct float64) {
		if task == nil { // a hook driven outside a task
			return
		}
		task.ReportProgress(BtrfsOpProgress{Op: op, Percent: pct, Message: fmt.Sprintf("%s: %.1f%% complete", label, pct)})
	}
}

func runBtrfsPoolCreateTask(ctx context.Context, task *bridgeipc.Task, req apischema.BtrfsPoolCreateRequest) (btrfs.BtrfsFilesystem, error) {
	return CreateBtrfsPool(ctx, req, func(step string, pct float64) {
		task.ReportProgress(BtrfsOpProgress{Op: "create", Percent: pct, Message: step})
	})
}

func runBtrfsDeviceRemoveTask(ctx context.Context, task *bridgeipc.Task, req apischema.BtrfsDeviceRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Debug("removing btrfs device", "uuid", req.UUID, "device", req.DeviceID)
	task.ReportProgress(BtrfsOpProgress{Op: "remove", Message: fmt.Sprintf("Moving data off device %d", req.DeviceID)})
	if err := btrfsRemoveDevice(ctx, req.UUID, req.DeviceID, removeReporter(task, req.DeviceID)); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("remove btrfs device %d of %s: %w", req.DeviceID, req.UUID, err)
	}
	slog.Info("removed btrfs device", "uuid", req.UUID, "device", req.DeviceID)
	return btrfsFindFilesystem(ctx, req.UUID)
}

func removeReporter(task *bridgeipc.Task, id int) func(float64) {
	return progressReporter(task, "remove", fmt.Sprintf("Moving data off device %d", id))
}

// suspendBtrfsRemove keeps no checkpoint: the job's context cancel that
// follows sends the kernel cancel, and the chunks already moved stay moved.
func suspendBtrfsRemove(context.Context, apischema.BtrfsDeviceRequest, jsontext.Value) (jsontext.Value, error) {
	return nil, nil
}

// resumeBtrfsRemove removes the device again, or accepts that it is gone.
// Device ids are reused (max devid + 1), so a disk added by hand after the
// removal finished would be removed here; practically unreachable at boot.
func resumeBtrfsRemove(ctx context.Context, req apischema.BtrfsDeviceRequest, _ jsontext.Value) (btrfs.BtrfsFilesystem, error) {
	slog.Debug("resuming btrfs device removal", "uuid", req.UUID, "device", req.DeviceID)
	task, _ := bridgeipc.TaskFromContext(ctx)
	if err := btrfsResumeRemove(ctx, req.UUID, req.DeviceID, removeReporter(task, req.DeviceID)); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("resume removal of btrfs device %d of %s: %w", req.DeviceID, req.UUID, err)
	}
	slog.Info("removed btrfs device", "uuid", req.UUID, "device", req.DeviceID, "resumed", true)
	return btrfsFindFilesystem(ctx, req.UUID)
}

func runBtrfsDeviceReplaceTask(ctx context.Context, task *bridgeipc.Task, req apischema.BtrfsDeviceReplaceRequest) (btrfs.BtrfsFilesystem, error) {
	// The kernel copies onto the target without checking it, so it must be blank.
	if err := ensureBtrfsDeviceFree(ctx, req.NewDevice, false); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Debug("replacing btrfs device", "uuid", req.UUID, "device", req.DeviceID, "newDevice", req.NewDevice)
	task.ReportProgress(BtrfsOpProgress{Op: "replace", Message: fmt.Sprintf("Replacing device %d with %s", req.DeviceID, req.NewDevice)})
	if err := btrfs.Replace(ctx, req.UUID, req.DeviceID, req.NewDevice, progressReporter(task, "replace", "Replacing")); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("replace btrfs device %d of %s with %s: %w", req.DeviceID, req.UUID, req.NewDevice, err)
	}
	fs, err := btrfs.ReplaceResult(ctx, req.UUID, req.DeviceID, req.NewDevice)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("replace btrfs device %d of %s with %s: %w", req.DeviceID, req.UUID, req.NewDevice, err)
	}
	fs = growReplaced(ctx, req.UUID, req.DeviceID, fs)
	slog.Info("replaced btrfs device", "uuid", req.UUID, "device", req.DeviceID, "newDevice", req.NewDevice)
	return fs, nil
}

// growReplaced grows device id of fs into a bigger replacement disk right
// away, returning the refreshed filesystem; a failed grow leaves it as is.
func growReplaced(ctx context.Context, uuid string, id int, fs btrfs.BtrfsFilesystem) btrfs.BtrfsFilesystem {
	if i := slices.IndexFunc(fs.Devices, func(d btrfs.BtrfsDevice) bool { return d.ID == id }); i >= 0 && fs.Devices[i].Growable {
		if err := btrfsGrowDevice(ctx, uuid, id); err != nil {
			slog.Warn("btrfs grow after replace failed", "uuid", uuid, "device", id, "error", err)
		} else if grown, err := btrfsFindFilesystem(ctx, uuid); err == nil {
			fs = grown
		}
	}
	return fs
}

// replaceSettle bounds the wait for a canceled replace to end either way,
// well inside the time a stopping job host is given.
const replaceSettle = 30 * time.Second

// suspendBtrfsReplace keeps no checkpoint. In the first run the job's context
// cancel that follows cancels the kernel replace, which keeps the source.
// Resume finds a replace running when a power loss cut it, or when a shutdown
// interrupted a resume following it, which leaves the kernel thread running:
// either way the kernel resumes it at mount.
func suspendBtrfsReplace(context.Context, apischema.BtrfsDeviceReplaceRequest, jsontext.Value) (jsontext.Value, error) {
	return nil, nil
}

// resumeBtrfsReplace follows a replace the kernel resumed, or accepts one it
// finished since the job started, never starting one: the target path may
// name another disk after a reboot. A user cancel cancels it, unless the
// kernel installed the new disk first, which is a success. An interrupt
// leaves it to the kernel: the unmount suspends it and the next mount
// resumes it.
func resumeBtrfsReplace(ctx context.Context, req apischema.BtrfsDeviceReplaceRequest, _ jsontext.Value) (btrfs.BtrfsFilesystem, error) {
	slog.Debug("following resumed btrfs replace", "uuid", req.UUID, "device", req.DeviceID)
	task, _ := bridgeipc.TaskFromContext(ctx)
	started, report := apischema.JobStartedAt(ctx), progressReporter(task, "replace", "Replacing")
	err := btrfsResumeReplace(ctx, req.UUID, req.DeviceID, started, report)
	if errors.Is(err, context.Canceled) && (task == nil || !task.Interrupted()) {
		// The cancel is too late for a replace in its last step, and how it
		// ended shows only once the kernel settled: follow it again, briefly.
		ctx = context.WithoutCancel(ctx)
		settle, stop := context.WithTimeout(ctx, replaceSettle)
		defer stop()
		if cancelErr := btrfsCancelReplace(ctx, req.UUID, req.DeviceID); cancelErr != nil {
			err = errors.Join(err, cancelErr)
		} else if followErr := btrfsResumeReplace(settle, req.UUID, req.DeviceID, started, report); followErr != nil {
			slog.Debug("canceled btrfs replace did not install the new disk", "uuid", req.UUID, "device", req.DeviceID, "error", followErr)
		} else {
			err = nil
		}
	}
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("resume replace of btrfs device %d of %s: %w", req.DeviceID, req.UUID, err)
	}
	fs, err := btrfsFindFilesystem(ctx, req.UUID)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	fs = growReplaced(ctx, req.UUID, req.DeviceID, fs)
	slog.Info("replaced btrfs device", "uuid", req.UUID, "device", req.DeviceID, "resumed", true)
	return fs, nil
}

func runBtrfsBalanceTask(ctx context.Context, task *bridgeipc.Task, req apischema.BtrfsBalanceRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Debug("balancing btrfs filesystem", "uuid", req.UUID, "data", profileOrKeep(req.DataProfile), "metadata", profileOrKeep(req.MetadataProfile), "dataUsage", req.DataUsage)
	task.ReportProgress(BtrfsOpProgress{Op: "balance", Message: "Balance started"})
	if err := btrfs.Balance(ctx, req.UUID, balanceOptions(req), progressReporter(task, "balance", "Balancing")); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("balance btrfs filesystem %s: %w", req.UUID, err)
	}
	fs, err := btrfsBalanceResult(ctx, req.UUID, req.DataProfile, req.MetadataProfile)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("balance btrfs filesystem %s: %w", req.UUID, err)
	}
	slog.Info("balanced btrfs filesystem", "uuid", req.UUID)
	return fs, nil
}

// balanceOptions is what a balance and its resume ask the kernel for.
func balanceOptions(req apischema.BtrfsBalanceRequest) btrfs.BalanceOptions {
	return btrfs.BalanceOptions{DataProfile: req.DataProfile, MetadataProfile: req.MetadataProfile, DataUsageMax: req.DataUsage}
}

// balanceCheckpoint records whether Suspend paused a running balance.
type balanceCheckpoint struct {
	Paused bool `json:"paused"`
}

// suspendBtrfsBalance pauses the balance at shutdown. The kernel keeps a
// paused balance and resumes it at the next mount, which Resume follows.
func suspendBtrfsBalance(ctx context.Context, req apischema.BtrfsBalanceRequest, _ jsontext.Value) (jsontext.Value, error) {
	paused, err := btrfsPauseBalance(ctx, req.UUID)
	if err != nil {
		return nil, fmt.Errorf("pause balance of btrfs filesystem %s: %w", req.UUID, err)
	}
	checkpoint, err := json.Marshal(balanceCheckpoint{Paused: paused})
	return jsontext.Value(checkpoint), err
}

// resumeBtrfsBalance continues the balance a shutdown interrupted. Without
// Suspend's checkpoint the balance was not paused.
func resumeBtrfsBalance(ctx context.Context, req apischema.BtrfsBalanceRequest, checkpoint jsontext.Value) (btrfs.BtrfsFilesystem, error) {
	var cp balanceCheckpoint
	if len(checkpoint) > 0 {
		if err := json.Unmarshal(checkpoint, &cp); err != nil {
			return btrfs.BtrfsFilesystem{}, fmt.Errorf("decode balance checkpoint of btrfs filesystem %s: %w", req.UUID, err)
		}
	}
	slog.Debug("resuming btrfs balance", "uuid", req.UUID, "paused", cp.Paused)
	task, _ := bridgeipc.TaskFromContext(ctx) // Resume always runs as a task
	if err := btrfsResumeBalance(ctx, req.UUID, balanceOptions(req), cp.Paused, progressReporter(task, "balance", "Balancing")); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("resume balance of btrfs filesystem %s: %w", req.UUID, err)
	}
	fs, err := btrfsBalanceResult(ctx, req.UUID, req.DataProfile, req.MetadataProfile)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("balance btrfs filesystem %s: %w", req.UUID, err)
	}
	slog.Info("balanced btrfs filesystem", "uuid", req.UUID, "resumed", true)
	return fs, nil
}

func runBtrfsScrubTask(ctx context.Context, task *bridgeipc.Task, req apischema.UUIDRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Debug("scrubbing btrfs filesystem", "uuid", req.UUID)
	task.ReportProgress(BtrfsOpProgress{Op: "scrub", Message: "Scrub started"})
	status, err := scrubBtrfs(ctx, task, req.UUID, nil)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("scrubbed btrfs filesystem", "uuid", req.UUID, "errors", status.ErrorCount, "uncorrectable", status.UncorrectableErrors)
	return btrfsFindFilesystem(ctx, req.UUID)
}

// scrubBtrfs scrubs from starts (nil: every device from 0) and fails unless
// the scrub finished. A scrub canceled while ctx is live was canceled by the
// scrub cancel route or by Suspend, so the job ends canceled or interrupted.
func scrubBtrfs(ctx context.Context, task *bridgeipc.Task, uuid string, starts map[int]uint64) (btrfs.BtrfsScrubStatus, error) {
	report := progressReporter(task, "scrub", "Scrubbing")
	status, err := btrfsScrubFrom(ctx, uuid, starts, func(s btrfs.BtrfsScrubStatus) { report(s.Percent) })
	switch {
	case err != nil:
		return status, fmt.Errorf("scrub btrfs filesystem %s (status %q): %w", uuid, status.Status, err)
	case status.Status == "canceled":
		return status, fmt.Errorf("scrub btrfs filesystem %s: %w", uuid, context.Canceled)
	case status.Status != "finished":
		return status, fmt.Errorf("scrub btrfs filesystem %s ended %s", uuid, status.Status)
	}
	return status, nil
}

// scrubCheckpoint is where each device's scrub stood when Suspend canceled
// it, by device id.
type scrubCheckpoint struct {
	Remaining map[int]uint64 `json:"remaining"`
}

// scrubStarts returns the offsets a resumed scrub starts from; nil, without
// a checkpoint, scrubs every device again from 0.
func scrubStarts(checkpoint jsontext.Value) (map[int]uint64, error) {
	if len(checkpoint) == 0 {
		return nil, nil
	}
	var cp scrubCheckpoint
	if err := json.Unmarshal(checkpoint, &cp); err != nil {
		return nil, err
	}
	return cp.Remaining, nil
}

// suspendBtrfsScrub cancels the scrub at shutdown and records where each
// device stood; the kernel keeps no scrub state. Without any position it
// records nothing, so the resume scrubs everything again: an idle device
// (ENOTCONN) may have finished or never started.
func suspendBtrfsScrub(ctx context.Context, req apischema.UUIDRequest, _ jsontext.Value) (jsontext.Value, error) {
	positions, err := btrfsScrubPositions(ctx, req.UUID)
	if err != nil {
		return nil, fmt.Errorf("read scrub positions of btrfs filesystem %s: %w", req.UUID, err)
	}
	if err = btrfsScrubCancel(ctx, req.UUID); err != nil && !errors.Is(err, btrfs.ErrScrubNotRunning) {
		return nil, fmt.Errorf("cancel scrub of btrfs filesystem %s: %w", req.UUID, err)
	}
	if len(positions) == 0 {
		return nil, nil
	}
	checkpoint, err := json.Marshal(scrubCheckpoint{Remaining: positions})
	return jsontext.Value(checkpoint), err
}

// resumeBtrfsScrub scrubs what Suspend recorded as left, from each device's
// offset, or the whole pool without a checkpoint.
func resumeBtrfsScrub(ctx context.Context, req apischema.UUIDRequest, checkpoint jsontext.Value) (btrfs.BtrfsFilesystem, error) {
	starts, err := scrubStarts(checkpoint)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("decode scrub checkpoint of btrfs filesystem %s: %w", req.UUID, err)
	}
	slog.Debug("resuming btrfs scrub", "uuid", req.UUID, "starts", starts)
	task, _ := bridgeipc.TaskFromContext(ctx) // Resume always runs as a task
	status, err := scrubBtrfs(ctx, task, req.UUID, starts)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("scrubbed btrfs filesystem", "uuid", req.UUID, "errors", status.ErrorCount, "uncorrectable", status.UncorrectableErrors, "resumed", true)
	return btrfsFindFilesystem(ctx, req.UUID)
}

// profileOrKeep renders an optional profile for logs.
func profileOrKeep(p *string) string {
	if p == nil {
		return "keep"
	}
	return *p
}

// AddBtrfsDevice adds a free device to a filesystem.
func AddBtrfsDevice(ctx context.Context, req apischema.BtrfsDeviceAddRequest) (btrfs.BtrfsFilesystem, error) {
	if err := ensureBtrfsDeviceFree(ctx, req.Device, req.Force); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	if err := btrfs.AddDevice(ctx, req.UUID, req.Device, req.Force); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("add btrfs device %s to %s: %w", req.Device, req.UUID, err)
	}
	slog.Info("btrfs device added", "uuid", req.UUID, "device", req.Device, "force", req.Force)
	return btrfs.FindFilesystem(ctx, req.UUID)
}

// CancelBtrfsBalance cancels a running balance.
func CancelBtrfsBalance(ctx context.Context, uuid string) error {
	if err := btrfs.BalanceCancel(ctx, uuid); err != nil {
		return fmt.Errorf("cancel btrfs balance of %s: %w", uuid, err)
	}
	slog.Info("btrfs balance canceled", "uuid", uuid)
	return nil
}

// markScheduledRunCanceled makes the end hook record the running run of unit
// as canceled. A scrub knows no invocation, so any running run of the unit is
// marked.
func markScheduledRunCanceled(ctx context.Context, unit string) error {
	return scheduledrun.RequestCancel(ctx, jobs.NewScheduledStore(), unit, "")
}

// withdrawScheduledRunCancel takes back markScheduledRunCanceled after a
// cancel that failed.
func withdrawScheduledRunCancel(ctx context.Context, unit string) error {
	return scheduledrun.WithdrawCancel(ctx, jobs.NewScheduledStore(), unit, "")
}

// CancelBtrfsScrub cancels a running scrub.
func CancelBtrfsScrub(ctx context.Context, uuid string) error {
	// A scrub that belongs to a timer's pool resync ends that run as canceled.
	// A cancel that fails takes the mark back.
	unit := btrfs.ResyncUnitName(uuid)
	if err := btrfsMarkResyncCanceled(ctx, unit); err != nil {
		slog.Warn("could not mark the pool resync canceled", "unit", unit, "error", err)
	}
	if err := btrfsScrubCancel(ctx, uuid); err != nil {
		if withdrawErr := btrfsWithdrawResyncCancel(ctx, unit); withdrawErr != nil {
			slog.Warn("could not withdraw the cancel request of the pool resync", "unit", unit, "error", withdrawErr)
		}
		return fmt.Errorf("cancel btrfs scrub of %s: %w", uuid, err)
	}
	slog.Info("btrfs scrub canceled", "uuid", uuid)
	return nil
}
