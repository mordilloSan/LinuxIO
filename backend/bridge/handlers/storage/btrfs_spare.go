package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// A pool's hot spare is named and kept here; btrfs/spare.go stores the
// designation and decides whether the disk can serve, and the replace onto
// it is started elsewhere. While a disk is a pool's spare the inventory marks
// it (markHotSpares) and deviceInUse refuses it for every other write.

// Seams for tests: the designations in btrfs/spare.go.
var (
	btrfsSpareDisks    = btrfs.SpareDisks
	btrfsSetSpare      = btrfs.SetSpare
	btrfsSetSpareDelay = btrfs.SetSpareDelay
	btrfsClearSpare    = btrfs.ClearSpare
)

// The routes write LinuxIO's state under /var/lib/linuxio and read the disk
// through an exclusive open: root only.
func btrfsSpareBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.MutationVoid("storage.btrfs_spare_set", SetBtrfsSpare, apischema.Privileged()),
		apischema.MutationVoid("storage.btrfs_spare_delay", SetBtrfsSpareDelay, apischema.Privileged()),
		apischema.MutationVoid("storage.btrfs_spare_clear", ClearBtrfsSpare, apischema.Privileged()),
	)
}

// SetBtrfsSpare makes a free whole disk the hot spare of a mounted pool: a
// disk every other write may take (deviceInUse, no partitions or holders,
// no signature), the pool's own spare included, which btrfs.SetSpare then
// checks for an identity, its size and its own signatures.
func SetBtrfsSpare(ctx context.Context, req apischema.BtrfsSpareSetRequest) error {
	if req.UUID == "" || req.Device == "" {
		return fmt.Errorf("%w: a pool and a disk are required", bridgeipc.ErrInvalidArgs)
	}
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(inv.fss, func(fs btrfs.BtrfsFilesystem) bool { return fs.UUID == req.UUID })
	if i < 0 {
		return fmt.Errorf("%w: mounted btrfs pool %s", bridgeipc.ErrNotFound, req.UUID)
	}
	inv.releaseSpare(req.UUID)
	if _, err = inv.writableDisk(req.Device); err != nil {
		return err
	}
	if err = checkBtrfsDeviceFree(inv, req.Device, false); err != nil {
		return err
	}
	if err = btrfsSetSpare(ctx, inv.fss[i], req.Device, req.DelayMinutes); err != nil {
		return spareStatus(fmt.Errorf("set hot spare of btrfs pool %s: %w", req.UUID, err))
	}
	slog.Info("set btrfs hot spare", "uuid", req.UUID, "device", req.Device, "delayMinutes", req.DelayMinutes)
	return nil
}

// SetBtrfsSpareDelay changes how long a member of the pool may be missing
// before its spare replaces it.
func SetBtrfsSpareDelay(ctx context.Context, req apischema.BtrfsSpareDelayRequest) error {
	if req.UUID == "" {
		return fmt.Errorf("%w: a pool is required", bridgeipc.ErrInvalidArgs)
	}
	if err := btrfsSetSpareDelay(ctx, req.UUID, req.DelayMinutes); err != nil {
		return spareStatus(fmt.Errorf("set hot spare delay of btrfs pool %s: %w", req.UUID, err))
	}
	slog.Info("changed btrfs hot spare delay", "uuid", req.UUID, "delayMinutes", req.DelayMinutes)
	return nil
}

// ClearBtrfsSpare drops the pool's hot spare, if it has one; the disk is
// free again.
func ClearBtrfsSpare(ctx context.Context, req apischema.UUIDRequest) error {
	if req.UUID == "" {
		return fmt.Errorf("%w: a pool is required", bridgeipc.ErrInvalidArgs)
	}
	cleared, err := btrfsClearSpare(ctx, req.UUID)
	if err != nil {
		return fmt.Errorf("remove hot spare of btrfs pool %s: %w", req.UUID, err)
	}
	if cleared {
		slog.Info("removed btrfs hot spare", "uuid", req.UUID)
	}
	return nil
}

// spareStatus gives a refusal of btrfs/spare.go its client status.
func spareStatus(err error) error {
	switch {
	case errors.Is(err, btrfs.ErrSpareUnsuitable):
		return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	case errors.Is(err, btrfs.ErrSpareInUse):
		return fmt.Errorf("%w: %w", bridgeipc.ErrConflict, err)
	case errors.Is(err, btrfs.ErrNoSpare):
		return fmt.Errorf("%w: %w", bridgeipc.ErrNotFound, err)
	}
	return err
}

// takeOwnSpare drops the hot spare of pool uuid when device, which a
// replace installed as one of its members, is that spare.
func takeOwnSpare(ctx context.Context, uuid, device string) error {
	spares, err := btrfsSpareDisks()
	if err != nil {
		return fmt.Errorf("read btrfs hot spares: %w", err)
	}
	if spares[device] != uuid {
		return nil
	}
	if _, err = btrfsClearSpare(ctx, uuid); err != nil {
		return fmt.Errorf("remove hot spare of btrfs pool %s: %w", uuid, err)
	}
	slog.Info("took btrfs hot spare as replacement", "uuid", uuid, "device", device)
	return nil
}

// markHotSpares marks each designated hot spare among devices, and its
// partitions, with the pool it is kept for, while that pool is there: the
// kernel mounts it (fss), the superblocks of present disks name it
// (offline), or udev reports a member of it. The spare of a pool no disk of
// which is connected, such as one destroyed outside LinuxIO, reserves
// nothing. The topology passes no fss or offline: it reads udev alone.
func markHotSpares(devices []apischema.StorageBlockDevice, fss []btrfs.BtrfsFilesystem, offline []btrfs.BtrfsOfflinePool) error {
	spares, err := btrfsSpareDisks()
	if err != nil {
		return fmt.Errorf("read btrfs hot spares: %w", err)
	}
	for disk, uuid := range spares {
		pool, ok := poolPresent(uuid, devices, fss, offline)
		if !ok {
			continue
		}
		for j, d := range devices {
			if d.Path == disk || d.Type == "part" && slices.Contains(d.Parents, disk) {
				devices[j].HotSpareOf = pool
			}
		}
	}
	return nil
}

// poolPresent names pool uuid, and reports whether it is there, by the
// kernel's, the superblocks' or udev's view, in that order of trust for its
// label.
func poolPresent(uuid string, devices []apischema.StorageBlockDevice, fss []btrfs.BtrfsFilesystem, offline []btrfs.BtrfsOfflinePool) (*apischema.StoragePoolRef, bool) {
	if i := slices.IndexFunc(fss, func(fs btrfs.BtrfsFilesystem) bool { return fs.UUID == uuid }); i >= 0 {
		return &apischema.StoragePoolRef{UUID: uuid, Label: fss[i].Label}, true
	}
	if i := slices.IndexFunc(offline, func(p btrfs.BtrfsOfflinePool) bool { return p.UUID == uuid }); i >= 0 {
		return &apischema.StoragePoolRef{UUID: uuid, Label: offline[i].Label}, true
	}
	for _, d := range devices {
		if m := memberOf(d); m != nil && m.Kind == "btrfs" && m.UUID == uuid {
			return &apischema.StoragePoolRef{UUID: uuid, Label: m.Name}, true
		}
	}
	return nil, false
}

// releaseSpare lifts the reservation of pool uuid's hot spare from inv, for
// the requests that may take it: a replace in that pool, and setting it
// again. A partition of the spare stays reserved: only the whole disk may
// become the pool's member.
func (inv partitionInventory) releaseSpare(uuid string) {
	for i, d := range inv.devices {
		if d.HotSpareOf != nil && d.HotSpareOf.UUID == uuid && d.Type != "part" {
			inv.devices[i].HotSpareOf = nil
		}
	}
}

// dropTakenSpare drops the hot spare of pool uuid once a replace installed
// it as device id of fs: by the slot, since a resumed replace knows the new
// disk by nothing else. A failure leaves a designation of a disk that is a
// member now, which reads in-use and warns, so it is logged, not returned:
// the replace itself is done.
func dropTakenSpare(ctx context.Context, uuid string, id int, fs btrfs.BtrfsFilesystem) {
	i := slices.IndexFunc(fs.Devices, func(d btrfs.BtrfsDevice) bool { return d.ID == id })
	if i < 0 || fs.Devices[i].Path == "" {
		return
	}
	if err := takeOwnSpare(ctx, uuid, fs.Devices[i].Path); err != nil {
		slog.Warn("failed to remove the hot spare a replace installed", "uuid", uuid, "device", fs.Devices[i].Path, "error", err)
	}
}
