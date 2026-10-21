package storage

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// BtrfsOpProgress reports a running device, balance or scrub operation.
type BtrfsOpProgress struct {
	Op      string  `json:"op"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
}

func (p BtrfsOpProgress) ProgressEnvelope() bridgetasks.TaskProgress {
	pct := int(p.Percent)
	return bridgetasks.TaskProgress{Percentage: &pct, Phase: p.Op, Message: p.Message, Detail: p}
}

var btrfsDeviceRoutes = btrfsDeviceBindings().Routes()

func btrfsDeviceBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.BtrfsDeviceRequest, btrfs.BtrfsFilesystem]("storage.btrfs_device_remove", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsDeviceRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.UUID, strconv.Itoa(req.DeviceID)}, Label: "Removing btrfs device"}
		})).Run(runBtrfsDeviceRemoveTask, bridgetasks.TaskSingletonSystem),
		apischema.TaskRunner[apischema.BtrfsDeviceReplaceRequest, btrfs.BtrfsFilesystem]("storage.btrfs_device_replace", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsDeviceReplaceRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.UUID, strconv.Itoa(req.DeviceID)}, Label: "Replacing btrfs device"}
		})).Run(runBtrfsDeviceReplaceTask, bridgetasks.TaskSingletonSystem),
		apischema.TaskRunner[apischema.BtrfsBalanceRequest, btrfs.BtrfsFilesystem]("storage.btrfs_balance", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsBalanceRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.UUID, "balance"}, Label: "Balancing btrfs filesystem"}
		})).Run(runBtrfsBalanceTask, bridgetasks.TaskSingletonSystem),
		apischema.TaskRunner[apischema.UUIDRequest, btrfs.BtrfsFilesystem]("storage.btrfs_scrub_start", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.UUIDRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.UUID, "scrub"}, Label: "Scrubbing btrfs filesystem"}
		})).Run(runBtrfsScrubTask, bridgetasks.TaskSingletonSystem),
	)
}

// ensureBtrfsDeviceFree refuses a device that is not free to join a btrfs
// filesystem; force only allows overwriting an existing signature.
func ensureBtrfsDeviceFree(ctx context.Context, device string, force bool) error {
	devices, err := ListBlockDevices(ctx)
	if err != nil {
		return err
	}
	filesystems, err := btrfs.ListFilesystems(ctx)
	if err != nil {
		return err
	}
	return checkBtrfsDeviceFree(devices, filesystems, device, force)
}

func checkBtrfsDeviceFree(devices []apischema.StorageBlockDevice, filesystems []btrfs.BtrfsFilesystem, device string, force bool) error {
	i := slices.IndexFunc(devices, func(d apischema.StorageBlockDevice) bool { return d.Path == device })
	if i < 0 {
		return fmt.Errorf("block device %s not found", device)
	}
	d := devices[i]
	if len(d.Mountpoints) > 0 {
		return fmt.Errorf("device %s is mounted at %s", device, d.Mountpoints[0])
	}
	// Writing to a disk whose partitions or holders (LVM, dm-crypt) are in
	// use destroys them, and force would not stop btrfs from doing it.
	if slices.ContainsFunc(devices, func(c apischema.StorageBlockDevice) bool { return slices.Contains(c.Parents, device) }) {
		return fmt.Errorf("device %s has partitions or holders", device)
	}
	for _, fs := range filesystems {
		if slices.ContainsFunc(fs.Devices, func(b btrfs.BtrfsDevice) bool { return b.Path == device }) {
			return fmt.Errorf("device %s already belongs to btrfs filesystem %s", device, fs.UUID)
		}
	}
	if d.FSType != "" && !force {
		return fmt.Errorf("device %s has a %s signature; force to overwrite it", device, d.FSType)
	}
	return nil
}

// progressReporter reports one operation's percentage through the task.
func progressReporter(task *bridgetasks.Task, op, label string) func(float64) {
	return func(pct float64) {
		task.ReportProgress(BtrfsOpProgress{Op: op, Percent: pct, Message: fmt.Sprintf("%s: %.1f%% complete", label, pct)})
	}
}

func runBtrfsDeviceRemoveTask(ctx context.Context, task *bridgetasks.Task, req apischema.BtrfsDeviceRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Info("removing btrfs device", "uuid", req.UUID, "device", req.DeviceID)
	task.ReportProgress(BtrfsOpProgress{Op: "remove", Message: fmt.Sprintf("Moving data off device %d", req.DeviceID)})
	if err := btrfs.RemoveDevice(ctx, req.UUID, req.DeviceID); err != nil {
		slog.Error("btrfs device remove failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("removed btrfs device", "uuid", req.UUID, "device", req.DeviceID)
	return btrfs.FindFilesystem(ctx, req.UUID)
}

func runBtrfsDeviceReplaceTask(ctx context.Context, task *bridgetasks.Task, req apischema.BtrfsDeviceReplaceRequest) (btrfs.BtrfsFilesystem, error) {
	// The kernel copies onto the target without checking it, so it must be blank.
	if err := ensureBtrfsDeviceFree(ctx, req.NewDevice, false); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("replacing btrfs device", "uuid", req.UUID, "device", req.DeviceID, "newDevice", req.NewDevice)
	task.ReportProgress(BtrfsOpProgress{Op: "replace", Message: fmt.Sprintf("Replacing device %d with %s", req.DeviceID, req.NewDevice)})
	if err := btrfs.Replace(ctx, req.UUID, req.DeviceID, req.NewDevice, progressReporter(task, "replace", "Replacing")); err != nil {
		slog.Error("btrfs replace failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	fs, err := btrfs.ReplaceResult(ctx, req.UUID, req.DeviceID, req.NewDevice)
	if err != nil {
		slog.Error("btrfs replace failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	// A bigger replacement disk is grown into right away.
	if i := slices.IndexFunc(fs.Devices, func(d btrfs.BtrfsDevice) bool { return d.ID == req.DeviceID }); i >= 0 && fs.Devices[i].Growable {
		if err := btrfs.GrowDevice(ctx, req.UUID, req.DeviceID); err != nil {
			slog.Warn("btrfs grow after replace failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		} else if grown, err := btrfs.FindFilesystem(ctx, req.UUID); err == nil {
			fs = grown
		}
	}
	slog.Info("replaced btrfs device", "uuid", req.UUID, "device", req.DeviceID, "newDevice", req.NewDevice)
	return fs, nil
}

func runBtrfsBalanceTask(ctx context.Context, task *bridgetasks.Task, req apischema.BtrfsBalanceRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Info("balancing btrfs filesystem", "uuid", req.UUID, "data", profileOrKeep(req.DataProfile), "metadata", profileOrKeep(req.MetadataProfile), "dataUsage", req.DataUsage)
	task.ReportProgress(BtrfsOpProgress{Op: "balance", Message: "Balance started"})
	opts := btrfs.BalanceOptions{DataProfile: req.DataProfile, MetadataProfile: req.MetadataProfile, DataUsageMax: req.DataUsage}
	if err := btrfs.Balance(ctx, req.UUID, opts, progressReporter(task, "balance", "Balancing")); err != nil {
		slog.Error("btrfs balance failed", "uuid", req.UUID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	fs, err := btrfs.BalanceResult(ctx, req.UUID, req.DataProfile, req.MetadataProfile)
	if err != nil {
		slog.Error("btrfs balance failed", "uuid", req.UUID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("balanced btrfs filesystem", "uuid", req.UUID)
	return fs, nil
}

func runBtrfsScrubTask(ctx context.Context, task *bridgetasks.Task, req apischema.UUIDRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Info("scrubbing btrfs filesystem", "uuid", req.UUID)
	task.ReportProgress(BtrfsOpProgress{Op: "scrub", Message: "Scrub started"})
	report := progressReporter(task, "scrub", "Scrubbing")
	status, err := btrfs.Scrub(ctx, req.UUID, func(s btrfs.BtrfsScrubStatus) { report(s.Percent) })
	if err != nil {
		slog.Error("btrfs scrub failed", "uuid", req.UUID, "status", status.Status, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("scrubbed btrfs filesystem", "uuid", req.UUID, "errors", status.ErrorCount, "uncorrectable", status.UncorrectableErrors)
	return btrfs.FindFilesystem(ctx, req.UUID)
}

// profileOrKeep renders an optional profile for logs.
func profileOrKeep(p *string) string {
	if p == nil {
		return "keep"
	}
	return *p
}
