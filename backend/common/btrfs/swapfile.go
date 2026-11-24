package btrfs

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"
)

// SwapfileLayout reports whether the btrfs filesystem holding the open folder
// dir can hold a swap file: the kernel takes one only on a filesystem of one
// device whose data is single, since every extent of the file must lie on one
// stripe of one device (btrfs_swap_activate). It reads the device count with
// BTRFS_IOC_FS_INFO and the data block group profiles with
// BTRFS_IOC_SPACE_INFO, both through dir's handle; neither needs privilege.
// A folder on another filesystem is an error.
func SwapfileLayout(dir *os.File) (bool, error) {
	// dir must outlive its descriptor's use below.
	defer runtime.KeepAlive(dir)
	fd := int(dir.Fd())
	var info fsInfoArgs
	if err := ioctlFD(fd, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		return false, fmt.Errorf("fs info: %w", err)
	}
	if info.NumDevices != 1 {
		return false, nil
	}
	args := spaceArgs{SpaceSlots: uint64(len(spaceArgs{}.Spaces))}
	if err := ioctlFD(fd, iocSpaceInfo, unsafe.Pointer(&args)); err != nil {
		return false, fmt.Errorf("space info: %w", err)
	}
	if args.TotalSpaces > args.SpaceSlots {
		return false, fmt.Errorf("space info: %d block group types, more than the %d asked for", args.TotalSpaces, args.SpaceSlots)
	}
	for _, s := range args.Spaces[:args.TotalSpaces] {
		if s.Flags&bgData != 0 && profileName(s.Flags) != "single" {
			return false, nil
		}
	}
	return true, nil
}

// CreateSubvolumeAt creates the empty subvolume name in the open folder dir
// and sets it 0755, through dir's handle and never by path, as
// createSubvolumeIn does. name is one name in dir, not a path. Unlike
// CreateSubvolumeIn it neither looks the pool up nor reads the subvolume
// back: it is for a caller that has reached dir itself, through folders it
// checked, and reads the result through a handle of its own.
func CreateSubvolumeAt(dir *os.File, name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return fmt.Errorf("%w: %q is not a name in a folder", ErrInvalidSubvolume, name)
	}
	if err := createSubvolumeIn(dir, name); err != nil {
		return fmt.Errorf("create subvolume %s: %w", name, err)
	}
	return nil
}

// SubvolumeRootInode is the inode number of the root folder of every
// subvolume: btrfs gives it the first free object id of the subvolume's tree.
const SubvolumeRootInode = firstFreeObjectID
