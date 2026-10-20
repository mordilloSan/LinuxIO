package storage

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

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

// btrfsPollInterval is a variable so tests can poll quickly.
var btrfsPollInterval = 2 * time.Second

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

// pollBtrfsOp waits one interval before each poll, since the btrfs start
// commands return before the kernel reports the operation, and reports
// progress until poll says it is no longer running.
func pollBtrfsOp(ctx context.Context, report func(bridgetasks.ProgressDetail), op, label string, poll func(context.Context) (running bool, percent float64, err error)) error {
	ticker := time.NewTicker(btrfsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		running, pct, err := poll(ctx)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		report(BtrfsOpProgress{Op: op, Percent: pct, Message: fmt.Sprintf("%s: %.1f%% complete", label, pct)})
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
	// `replace start -f` overwrites any signature, so the target must be blank.
	if err := ensureBtrfsDeviceFree(ctx, req.NewDevice, false); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("replacing btrfs device", "uuid", req.UUID, "device", req.DeviceID, "newDevice", req.NewDevice)
	if err := btrfs.ReplaceStart(ctx, req.UUID, req.DeviceID, req.NewDevice); err != nil {
		slog.Error("btrfs replace start failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	task.ReportProgress(BtrfsOpProgress{Op: "replace", Message: fmt.Sprintf("Replacing device %d with %s", req.DeviceID, req.NewDevice)})
	if err := pollBtrfsOp(ctx, task.ReportProgress, "replace", "Replacing", func(ctx context.Context) (bool, float64, error) {
		return btrfs.ReplaceStatus(ctx, req.UUID)
	}); err != nil {
		slog.Error("btrfs replace failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	fs, err := btrfs.ReplaceResult(ctx, req.UUID, req.DeviceID, req.NewDevice)
	if err != nil {
		slog.Error("btrfs replace failed", "uuid", req.UUID, "device", req.DeviceID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("replaced btrfs device", "uuid", req.UUID, "device", req.DeviceID, "newDevice", req.NewDevice)
	return fs, nil
}

func runBtrfsBalanceTask(ctx context.Context, task *bridgetasks.Task, req apischema.BtrfsBalanceRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Info("balancing btrfs filesystem", "uuid", req.UUID, "data", profileOrKeep(req.DataProfile), "metadata", profileOrKeep(req.MetadataProfile))
	if err := btrfs.BalanceStart(ctx, req.UUID, req.DataProfile, req.MetadataProfile); err != nil {
		slog.Error("btrfs balance start failed", "uuid", req.UUID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	task.ReportProgress(BtrfsOpProgress{Op: "balance", Message: "Balance started"})
	if err := pollBtrfsOp(ctx, task.ReportProgress, "balance", "Balancing", func(ctx context.Context) (bool, float64, error) {
		return btrfs.BalanceStatus(ctx, req.UUID)
	}); err != nil {
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
	if err := btrfs.ScrubStart(ctx, req.UUID); err != nil {
		slog.Error("btrfs scrub start failed", "uuid", req.UUID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	task.ReportProgress(BtrfsOpProgress{Op: "scrub", Message: "Scrub started"})
	if err := pollBtrfsOp(ctx, task.ReportProgress, "scrub", "Scrubbing", func(ctx context.Context) (bool, float64, error) {
		s, err := btrfs.ScrubStatus(ctx, req.UUID)
		switch {
		case err != nil:
			return false, 0, err
		case s.Status == "running":
			return true, s.Percent, nil
		case s.Status == "finished":
			return false, 100, nil
		}
		return false, 0, fmt.Errorf("scrub ended with status %s", s.Status)
	}); err != nil {
		slog.Error("btrfs scrub failed", "uuid", req.UUID, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("scrubbed btrfs filesystem", "uuid", req.UUID)
	return btrfs.FindFilesystem(ctx, req.UUID)
}

// profileOrKeep renders an optional profile for logs.
func profileOrKeep(p *string) string {
	if p == nil {
		return "keep"
	}
	return *p
}
