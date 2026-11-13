package storage

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// Whether a device is free is decided here for every caller that writes to
// one: the Disks page (format, delete, initialize, mount), md RAID add and
// replace, and btrfs pool create, device add and replace. deviceInUse is
// what they all refuse; each caller adds its own rule on top (see
// checkMDDeviceFree, checkBtrfsDeviceFree and InitializeDisk).

// memberKinds maps a signature that makes a device part of something bigger,
// rather than the holder of a filesystem, to its StorageMember kind.
var memberKinds = map[string]string{"btrfs": "btrfs", "linux_raid_member": "md", "LVM2_member": "lvm"}

// signatureMember is what a device's signature says it belongs to, or nil.
// udev knows the pool label and the array name (host:name) from the
// signature; the volume group of a physical volume only from LVM's own udev
// rule, and only once the group is complete.
func signatureMember(fsType, uuid, label string, props map[string]string) *apischema.StorageMember {
	kind, ok := memberKinds[fsType]
	if !ok {
		// Any other signature that names itself a member, such as zfs_member
		// or a firmware RAID's isw_raid_member: part of something LinuxIO
		// does not manage, named by its signature.
		if !strings.HasSuffix(fsType, "_member") {
			return nil
		}
		return &apischema.StorageMember{Kind: "other", Name: fsType, UUID: uuid}
	}
	m := &apischema.StorageMember{Kind: kind, Name: label, UUID: uuid}
	if kind == "lvm" {
		m.Name = props["LVM_VG_NAME_COMPLETE"]
	}
	return m
}

// memberOf is d's membership, read from its signature when the inventory
// did not fill it.
func memberOf(d apischema.StorageBlockDevice) *apischema.StorageMember {
	if d.Member != nil {
		return d.Member
	}
	return signatureMember(d.FSType, d.UUID, d.Label, nil)
}

// describeMember names what m belongs to, such as "btrfs pool tank".
func describeMember(m apischema.StorageMember) string {
	name := cmp.Or(m.Name, m.UUID)
	switch {
	case m.Kind == "btrfs":
		return "btrfs pool " + name
	case m.Kind == "md" && name != "":
		return "md array " + name
	case m.Kind == "md":
		return "an md array"
	case m.Kind == "other":
		return "another storage set (" + m.Name + ")"
	case m.Name != "":
		return "LVM volume group " + m.Name
	case m.UUID != "":
		return "an LVM volume group (physical volume " + m.UUID + ")"
	}
	return "an LVM volume group"
}

// freeingHint says how a member of m is freed.
func freeingHint(m apischema.StorageMember) string {
	switch m.Kind {
	case "btrfs":
		return "destroy the pool, or remove the device from it, first"
	case "md":
		return "remove it from the array, or erase it with Initialize disk, first"
	case "other":
		return "erase it with Initialize disk first"
	}
	return "remove it from the volume group (vgreduce, then pvremove), or erase it with Initialize disk, first"
}

// memberConflict is the conflict refusing dev, a member of m.
func memberConflict(dev string, m apischema.StorageMember) error {
	return fmt.Errorf("%w: %s: Member of %s; %s", bridgeipc.ErrConflict, dev, describeMember(m), freeingHint(m))
}

// deviceInUse refuses dev when it is mounted, an active swap or held (Busy),
// a member of a btrfs filesystem mounted or not, or named in /etc/fstab. A
// mount just for now (forMount without persist) ignores /etc/fstab, so a
// partition listed there can still be mounted. Pool membership comes from
// the kernel's and the superblocks' view (fss, offline), which also knows
// the members of a mounted pool that no mount names.
func deviceInUse(dev apischema.StorageBlockDevice, fss []btrfs.BtrfsFilesystem, offline []btrfs.BtrfsOfflinePool, forMount, persist bool) error {
	inUse := func(reason string) error {
		return fmt.Errorf("%w: %s: %s", bridgeipc.ErrConflict, dev.Path, reason)
	}
	if dev.Busy != "" {
		return inUse(dev.Busy)
	}
	if len(dev.Mountpoints) > 0 {
		return inUse("Mounted at " + dev.Mountpoints[0])
	}
	for _, fs := range fss {
		if slices.ContainsFunc(fs.Devices, func(b btrfs.BtrfsDevice) bool { return b.Path == dev.Path }) {
			return memberConflict(dev.Path, apischema.StorageMember{Kind: "btrfs", Name: fs.Label, UUID: fs.UUID})
		}
	}
	for _, pool := range offline {
		if slices.Contains(pool.Devices, dev.Path) {
			return memberConflict(dev.Path, apischema.StorageMember{Kind: "btrfs", Name: pool.Label, UUID: pool.UUID})
		}
	}
	if dev.InFstab && (!forMount || persist) {
		return inUse("In /etc/fstab")
	}
	return nil
}

// wholeDeviceFree finds path for a caller that writes over the whole device
// (an md member, a btrfs device): not in use, and with no partitions or
// holders, which writing to it would destroy whatever they are used for.
func (inv partitionInventory) wholeDeviceFree(path string) (apischema.StorageBlockDevice, error) {
	d, err := inv.device(path)
	if err != nil {
		return d, err
	}
	if err = deviceInUse(d, inv.fss, inv.offline, false, false); err != nil {
		return d, err
	}
	if slices.ContainsFunc(inv.devices, func(c apischema.StorageBlockDevice) bool { return slices.Contains(c.Parents, path) }) {
		return d, fmt.Errorf("%w: %s has partitions or holders", bridgeipc.ErrConflict, path)
	}
	return d, nil
}
