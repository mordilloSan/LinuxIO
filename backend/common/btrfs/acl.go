package btrfs

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

// The POSIX ACL of a folder on a pool, written natively: the kernel takes an
// ACL as the value of two extended attributes, and a folder reached through
// openBelow is worked on through its one handle (see below.go), which
// setfacl, given a path, would not do.

// ACLPerm is what an ACL entry allows in a folder.
type ACLPerm uint16

const (
	ACLNone      ACLPerm = 0
	ACLRead      ACLPerm = 5 // r-x: list and read
	ACLReadWrite ACLPerm = 7 // rwx
)

// FolderACL is who may use a shared folder: users and groups by ID, and
// everyone else. The folder's owner, root, keeps everything and its owning
// group gets nothing, so only the entries here decide.
type FolderACL struct {
	Users  map[int]ACLPerm
	Groups map[int]ACLPerm
	Other  ACLPerm
}

// linux/posix_acl_xattr.h and linux/posix_acl.h.
const (
	aclXattrAccess  = "system.posix_acl_access"
	aclXattrDefault = "system.posix_acl_default"
	aclVersion      = 2

	aclTagUserObj  = 0x01
	aclTagUser     = 0x02
	aclTagGroupObj = 0x04
	aclTagGroup    = 0x08
	aclTagMask     = 0x10
	aclTagOther    = 0x20

	aclUndefinedID = 0xffffffff
)

// encodeACL returns the attribute value of acl: a version, then one entry
// each for the owner, every user, the owning group, every group, the mask
// and everyone else. The kernel refuses another order, and users and groups
// out of ID order.
func encodeACL(acl FolderACL) []byte {
	buf := binary.LittleEndian.AppendUint32(nil, aclVersion)
	entry := func(tag uint16, perm ACLPerm, id uint32) {
		buf = binary.LittleEndian.AppendUint16(buf, tag)
		buf = binary.LittleEndian.AppendUint16(buf, uint16(perm))
		buf = binary.LittleEndian.AppendUint32(buf, id)
	}
	entry(aclTagUserObj, ACLReadWrite, aclUndefinedID)
	for _, uid := range slices.Sorted(maps.Keys(acl.Users)) {
		entry(aclTagUser, acl.Users[uid], uint32(uid))
	}
	entry(aclTagGroupObj, ACLNone, aclUndefinedID)
	for _, gid := range slices.Sorted(maps.Keys(acl.Groups)) {
		entry(aclTagGroup, acl.Groups[gid], uint32(gid))
	}
	// The mask caps the user and group entries; rwx leaves them as written.
	entry(aclTagMask, ACLReadWrite, aclUndefinedID)
	entry(aclTagOther, acl.Other, aclUndefinedID)
	return buf
}

// folderOwner gives the folder to root, so the ACL's owner entry is root's
// and no user keeps the folder through owning it. A var so unit tests, which
// are not root, can leave the owner alone.
var folderOwner = func(f *os.File) error { return f.Chown(0, 0) }

// SetFolderACL makes acl the access and the default ACL of the folder at
// path, a path from the top level of the filesystem uuid, root its owner,
// and its group the group of what is made inside. The access ACL decides who may use the folder; the default ACL is
// what a file or folder made in it starts with, so new contents follow. What
// is already in the folder is left as it is.
func SetFolderACL(ctx context.Context, uuid, path string, acl FolderACL) error {
	if err := ValidateSubvolumePath(path); err != nil {
		return err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return err
	}
	fd, err := openBelow(top, path)
	if err != nil {
		return err
	}
	folder := os.NewFile(uintptr(fd), filepath.Join(top, path))
	defer folder.Close()
	if err = folderOwner(folder); err != nil {
		return fmt.Errorf("give %s to root: %w", folder.Name(), err)
	}
	value := encodeACL(acl)
	for _, attr := range []string{aclXattrAccess, aclXattrDefault} {
		if err = unix.Fsetxattr(fd, attr, value, 0); err != nil {
			return fmt.Errorf("set %s on %s: %w", attr, folder.Name(), err)
		}
	}
	// The setgid bit hands the folder's group, root's, on to what is made
	// inside. Without it a file belongs to its maker's primary group, and
	// the kernel stops at that group's entry, which gives nothing: on a
	// host where users share a primary group, its members would be kept
	// from a file everyone else may read.
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", folder.Name(), err)
	}
	if err = unix.Fchmod(fd, st.Mode&0o7777|unix.S_ISGID); err != nil {
		return fmt.Errorf("set the setgid bit on %s: %w", folder.Name(), err)
	}
	return nil
}
