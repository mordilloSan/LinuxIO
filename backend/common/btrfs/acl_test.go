package btrfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The value of a system.posix_acl_* attribute, as linux/posix_acl_xattr.h
// lays it out: a 32-bit version, then entries of a 16-bit tag, 16-bit
// permissions and a 32-bit ID, little-endian, in the order the kernel
// requires. These are the bytes "setfacl -m u:1000:rwx,g:2000:r-x" leaves on
// a folder of mode 0700.
func TestEncodeACLMatchesTheKernelLayout(t *testing.T) {
	got := encodeACL(FolderACL{
		Users:  map[int]ACLPerm{1000: ACLReadWrite},
		Groups: map[int]ACLPerm{2000: ACLRead},
		Other:  ACLNone,
	})
	want := []byte{
		0x02, 0x00, 0x00, 0x00, // version 2
		0x01, 0x00, 0x07, 0x00, 0xff, 0xff, 0xff, 0xff, // owner rwx
		0x02, 0x00, 0x07, 0x00, 0xe8, 0x03, 0x00, 0x00, // user 1000 rwx
		0x04, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, // owning group ---
		0x08, 0x00, 0x05, 0x00, 0xd0, 0x07, 0x00, 0x00, // group 2000 r-x
		0x10, 0x00, 0x07, 0x00, 0xff, 0xff, 0xff, 0xff, // mask rwx
		0x20, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, // other ---
	}
	if !bytes.Equal(got, want) {
		t.Errorf("encodeACL() =\n% x\nwant\n% x", got, want)
	}
}

// The kernel refuses entries out of order, so users and groups are sorted by
// ID whatever order the map gives them.
func TestEncodeACLSortsEntriesByID(t *testing.T) {
	acl := FolderACL{Users: map[int]ACLPerm{}, Other: ACLRead}
	for id := 1010; id >= 1000; id-- {
		acl.Users[id] = ACLRead
	}
	got := encodeACL(acl)
	for i := range 11 {
		entry := got[4+8*(1+i):]
		if id := int(entry[4]) | int(entry[5])<<8; id != 1000+i {
			t.Fatalf("user entry %d has ID %d, want %d", i, id, 1000+i)
		}
	}
}

// aclFolder is a folder the test's own user may set an ACL on, below a fake
// top-level mount; the test is skipped where the temp dir's filesystem
// keeps no ACLs.
func aclFolder(t *testing.T) (uuid string) {
	t.Helper()
	top := t.TempDir()
	if err := os.Mkdir(filepath.Join(top, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldMounts, oldOwner := listMounts, folderOwner
	listMounts = func(context.Context) ([]Mount, error) {
		return []Mount{{UUID: "uuid-acl", SubvolID: topLevelSubvolID, Mountpoint: top}}, nil
	}
	folderOwner = func(*os.File) error { return nil }
	t.Cleanup(func() { listMounts, folderOwner = oldMounts, oldOwner })
	return "uuid-acl"
}

// What the kernel makes of the value: it takes it (a malformed one is
// EINVAL), shows the mask and everyone else's level in the mode, and gives a
// new file and a new folder the default entries.
func TestSetFolderACLIsTakenByTheKernel(t *testing.T) {
	uuid := aclFolder(t)
	acl := FolderACL{
		Users:  map[int]ACLPerm{os.Getuid() + 1: ACLReadWrite, os.Getuid() + 2: ACLNone},
		Groups: map[int]ACLPerm{os.Getgid() + 1: ACLRead},
		Other:  ACLRead,
	}
	err := SetFolderACL(context.Background(), uuid, "media", acl)
	if errors.Is(err, unix.ENOTSUP) {
		t.Skip("the temp dir's filesystem keeps no POSIX ACLs")
	}
	if err != nil {
		t.Fatalf("SetFolderACL() = %v", err)
	}
	top, err := TopLevel(context.Background(), uuid)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(top, "media")
	info, err := os.Stat(folder)
	if err != nil {
		t.Fatal(err)
	}
	// Owner rwx, the mask rwx in the group bits, everyone else r-x.
	if got := info.Mode().Perm(); got != 0o775 {
		t.Errorf("folder mode = %o, want 775", got)
	}
	// What is made inside belongs to the folder's group, which gets nothing,
	// not to its maker's primary group.
	if info.Mode()&os.ModeSetgid == 0 {
		t.Error("the folder does not hand its group on (no setgid bit)")
	}
	want := encodeACL(acl)
	for _, name := range []string{"sub", "file"} {
		path := filepath.Join(folder, name)
		if name == "sub" {
			err = os.Mkdir(path, 0o777)
		} else {
			err = os.WriteFile(path, nil, 0o666)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	buf := make([]byte, 256)
	n, err := unix.Getxattr(filepath.Join(folder, "sub"), "system.posix_acl_default", buf)
	if err != nil {
		t.Fatalf("read the new folder's default ACL: %v", err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Errorf("a new folder's default ACL =\n% x\nwant\n% x", buf[:n], want)
	}
	sub, err := os.Stat(filepath.Join(folder, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if sub.Mode()&os.ModeSetgid == 0 {
		t.Error("a new folder does not hand the group on in turn")
	}
	n, err = unix.Getxattr(filepath.Join(folder, "file"), "system.posix_acl_access", buf)
	if err != nil {
		t.Fatalf("read the new file's ACL: %v", err)
	}
	// The same named entries; the file's own bits come from its create mode.
	if !bytes.Contains(buf[:n], want[12:20]) || !bytes.Contains(buf[:n], want[20:28]) {
		t.Errorf("a new file's ACL lacks the folder's user entries:\n% x", buf[:n])
	}
}

func TestSetFolderACLRefusesALink(t *testing.T) {
	uuid := aclFolder(t)
	top, err := TopLevel(context.Background(), uuid)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(top, "media"), filepath.Join(top, "link")); err != nil {
		t.Fatal(err)
	}
	err = SetFolderACL(context.Background(), uuid, "link", FolderACL{Other: ACLReadWrite})
	if !errors.Is(err, ErrLinkOnPath) {
		t.Fatalf("SetFolderACL() through a link = %v, want ErrLinkOnPath", err)
	}
}

// Samba reads the time in a snapshot's name with strptime, given
// SnapshotStrftime; it must say what snapshotTimeLayout writes.
func TestSnapshotStrftimeIsTheLayout(t *testing.T) {
	layout := strings.NewReplacer("%Y", "2006", "%m", "01", "%d", "02", "%H", "15", "%M", "04", "%S", "05").Replace(SnapshotStrftime)
	if layout != snapshotTimeLayout {
		t.Fatalf("SnapshotStrftime %q reads as %q, but names are written as %q", SnapshotStrftime, layout, snapshotTimeLayout)
	}
}

// A shared folder's earlier versions are read by Samba as the person signed
// in: the folder of its snapshots lets read who may read the folder, and
// SnapshotRoot lets them pass through. Turned off, the folder is root's
// alone again; one never made is not made for that.
func TestSetSnapshotsACL(t *testing.T) {
	uuid := aclFolder(t)
	top, err := TopLevel(context.Background(), uuid)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dir := filepath.Join(top, SnapshotRoot, "media")
	if err = SetSnapshotsACL(ctx, uuid, "media", nil); err != nil {
		t.Fatalf("clear before any snapshot = %v", err)
	}
	if _, err = os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clearing made %s: %v", dir, err)
	}
	acl := FolderACL{Users: map[int]ACLPerm{os.Getuid() + 1: ACLRead}, Other: ACLNone}
	err = SetSnapshotsACL(ctx, uuid, "media", &acl)
	if errors.Is(err, unix.ENOTSUP) {
		t.Skip("the temp dir's filesystem keeps no POSIX ACLs")
	}
	if err != nil {
		t.Fatalf("SetSnapshotsACL() = %v", err)
	}
	buf := make([]byte, 256)
	n, err := unix.Getxattr(dir, aclXattrAccess, buf)
	if err != nil || !bytes.Equal(buf[:n], encodeACL(acl)) {
		t.Fatalf("ACL of %s = % x, %v", dir, buf[:n], err)
	}
	info, err := os.Stat(filepath.Join(top, SnapshotRoot))
	if err != nil || info.Mode().Perm() != 0o711 {
		t.Fatalf("%s mode = %v, %v, want 711", SnapshotRoot, info, err)
	}
	// A new snapshot leaves the opened folder as it is.
	if err = ensureSnapshotDir(top, filepath.Join(SnapshotRoot, "media")); err != nil {
		t.Fatal(err)
	}
	if _, err = unix.Getxattr(dir, aclXattrAccess, buf); err != nil {
		t.Fatalf("a snapshot took the ACL away: %v", err)
	}
	if err = SetSnapshotsACL(ctx, uuid, "media", nil); err != nil {
		t.Fatalf("clear = %v", err)
	}
	if _, err = unix.Getxattr(dir, aclXattrAccess, buf); !errors.Is(err, unix.ENODATA) {
		t.Errorf("ACL after clearing: %v", err)
	}
	if info, err = os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("%s mode after clearing = %v, %v, want 700", dir, info, err)
	}
}
