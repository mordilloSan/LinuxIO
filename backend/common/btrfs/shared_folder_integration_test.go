package btrfs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The accounts the shared folder test acts as. None needs to exist: the
// kernel decides by number.
const (
	itWriter   = 4001
	itReader   = 4002
	itStranger = 4003
	itMember   = 4004 // reaches the folder only as a member of itGroup
	itGroup    = 4100
)

// as runs a command as the account uid, in a primary group of its own and,
// for itMember, in itGroup.
func (p *itPool) as(uid uint32, name string, args ...string) error {
	cmd := exec.CommandContext(p.ctx, name, args...)
	cred := &syscall.Credential{Uid: uid, Gid: uid}
	if uid == itMember {
		cred.Groups = []uint32{itGroup}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	cmd.Dir = "/"
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v as %d: %w: %s", name, args, uid, err, bytes.TrimSpace(out))
	}
	return nil
}

// allowed fails the test when the kernel refuses the command to uid.
func (p *itPool) allowed(uid uint32, name string, args ...string) {
	p.t.Helper()
	if err := p.as(uid, name, args...); err != nil {
		p.t.Fatalf("refused: %v", err)
	}
}

// refused fails the test when the kernel lets uid run the command. Each
// command refused here is also one allowed to another account, so a refusal
// is the kernel's and not a missing program.
func (p *itPool) refused(uid uint32, name string, args ...string) {
	p.t.Helper()
	if err := p.as(uid, name, args...); err == nil {
		p.t.Fatalf("%s %v as %d was allowed", name, args, uid)
	}
}

// stat returns the entry at a path from the pool's top level, itself and
// not what a link points to.
func (p *itPool) stat(rel string) unix.Stat_t {
	p.t.Helper()
	var st unix.Stat_t
	p.must("stat "+rel, unix.Lstat(filepath.Join(p.mnt, rel), &st))
	return st
}

// mode returns the permission bits of the entry at rel.
func (p *itPool) mode(rel string) uint32 {
	p.t.Helper()
	return p.stat(rel).Mode & 0o7777
}

// gone fails the test when the entry at rel is still there.
func (p *itPool) gone(rel string) {
	p.t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(filepath.Join(p.mnt, rel), &st); !errors.Is(err, unix.ENOENT) {
		p.t.Fatalf("%s is still there (%v)", rel, err)
	}
}

// TestBtrfsSharedFolderIntegration checks against a real kernel what a
// shared folder's access rests on (docs/shared-folders.md): that the kernel
// takes the ACL SetFolderACL encodes and decides by it, for the folder and
// for what is made in it afterwards; that the folders of snapshots are
// root's alone until SetSnapshotsACL opens one, and again after it closes
// it; and that the recycle bin's sweep keeps what a folder moved into the
// bin holds, keeps its note in an attribute only root reads, removes a link
// and never what it points to, and leaves a subvolume inside the bin alone.
// The unit tests of these run against a temporary folder, as a user, with
// the owner and the attribute's namespace swapped out. Run it with
// `make test-btrfs-integration`.
func TestBtrfsSharedFolderIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	dir := t.TempDir()
	stubStatePaths(t, dir) // MountPool records the pool for the boot check
	p := &itPool{t: t, ctx: t.Context()}
	// Under /run, which everyone may pass through: the checks reach the
	// pool as other accounts, and a test's temporary folder is root's alone.
	mnt, err := os.MkdirTemp("/run", "linuxio-shared-it-")
	p.must("make the mountpoint", err)
	t.Cleanup(func() { _ = os.Remove(mnt) })
	p.mnt = mnt
	disk := loopDevice(t, filepath.Join(dir, "disk.img"), 1<<30)
	p.disks = []string{disk}
	p.uuid, err = CreatePool(p.ctx, "linuxio-shared-it", "single", "dup", p.disks, true, nil, nil)
	p.must("create pool", err)
	p.must("mount pool", MountPool(p.ctx, disk, p.mnt, nil, false))
	t.Cleanup(func() { _ = unix.Unmount(p.mnt, 0) })
	_, err = CreateSubvolume(p.ctx, p.uuid, "media")
	p.must("create the folder", err)

	p.folderACL()
	p.snapshotFolders()
	p.recycleBin()
}

// folderACL gives media an access table and checks that the kernel decides
// by it: for the folder, for a file and a folder made in it, by a user's
// own entry and by a group's.
func (p *itPool) folderACL() {
	t := p.t
	media := filepath.Join(p.mnt, "media")
	// As an administrator may have left it: someone's own, open to all.
	p.must("hand the folder over", os.Chown(media, itStranger, itStranger))
	p.must("open the folder", os.Chmod(media, 0o777))
	acl := FolderACL{
		Users:  map[int]ACLPerm{itWriter: ACLReadWrite, itReader: ACLRead},
		Groups: map[int]ACLPerm{itGroup: ACLReadWrite},
		Other:  ACLNone,
	}
	p.must("set the ACL", SetFolderACL(p.ctx, p.uuid, "media", acl))
	// Root's, its group bits the ACL's mask, nothing for everyone else, and
	// its group handed on to what is made inside.
	if st := p.stat("media"); st.Uid != 0 || st.Gid != 0 || st.Mode&0o7777 != 0o2770 {
		t.Fatalf("media is %d:%d %o, want 0:0 2770", st.Uid, st.Gid, st.Mode&0o7777)
	}

	file, sub := filepath.Join(media, "by-writer"), filepath.Join(media, "sub")
	p.allowed(itWriter, "touch", file)
	p.allowed(itWriter, "mkdir", sub)
	p.refused(itReader, "touch", filepath.Join(media, "by-reader"))
	p.allowed(itReader, "cat", file)
	p.allowed(itReader, "ls", sub)
	p.refused(itStranger, "ls", media)
	p.refused(itStranger, "cat", file)

	// What is made inside follows: it carries root's group, not its maker's
	// (whose entry gives nothing), and the table as its own ACL, so another
	// writer may change it.
	if st := p.stat("media/by-writer"); st.Uid != itWriter || st.Gid != 0 {
		t.Fatalf("a file made inside is %d:%d, want %d:0", st.Uid, st.Gid, itWriter)
	}
	if st := p.stat("media/sub"); st.Gid != 0 || st.Mode&unix.S_ISGID == 0 {
		t.Fatalf("a folder made inside is group %d, mode %o: it hands no group on", st.Gid, st.Mode&0o7777)
	}
	p.allowed(itMember, "touch", file)
	p.allowed(itMember, "touch", filepath.Join(sub, "by-member"))
	p.refused(itReader, "touch", filepath.Join(sub, "by-reader"))
	p.refused(itStranger, "ls", sub)
}

// snapshotFolders checks who reaches media's snapshots: nobody but root
// until SetSnapshotsACL opens their folder, the accounts it names while it
// is open, and nobody again once it is closed. A folder an earlier version
// left open is narrowed by the next snapshot of any source.
func (p *itPool) snapshotFolders() {
	t := p.t
	root := filepath.Join(p.mnt, SnapshotRoot)
	stale := filepath.Join(root, "old-source")
	p.must("make an open snapshot folder", os.MkdirAll(stale, 0o755))
	for _, open := range []string{root, stale} {
		p.must("open "+open, os.Chmod(open, 0o755))
	}
	now := time.Now()
	snap, err := CreateSnapshot(p.ctx, p.uuid, "media", KindManual, now)
	p.must("snapshot", err)
	source := filepath.Join(SnapshotRoot, "media")
	for rel, want := range map[string]uint32{
		SnapshotRoot: 0o711,
		filepath.Join(SnapshotRoot, "old-source"): 0o700,
		source: 0o700,
	} {
		if got := p.mode(rel); got != want {
			t.Fatalf("%s is %o after a snapshot, want %o", rel, got, want)
		}
	}
	snapshots := filepath.Join(p.mnt, source)
	earlier := filepath.Join(p.mnt, snap.Path, "by-writer")
	p.refused(itReader, "ls", snapshots)
	p.refused(itReader, "cat", earlier)

	read := FolderACL{
		Users:  map[int]ACLPerm{itWriter: ACLRead, itReader: ACLRead},
		Groups: map[int]ACLPerm{itGroup: ACLRead},
	}
	p.must("open the snapshots", SetSnapshotsACL(p.ctx, p.uuid, "media", &read))
	p.allowed(itReader, "ls", snapshots)
	p.allowed(itReader, "cat", earlier)
	p.allowed(itMember, "ls", snapshots)
	p.refused(itWriter, "touch", filepath.Join(snapshots, "by-writer"))
	p.refused(itStranger, "ls", snapshots)
	// Passing through the root is all anyone gets of it.
	p.refused(itReader, "ls", root)
	p.refused(itReader, "ls", stale)

	// The next snapshot leaves an opened folder open.
	_, err = CreateSnapshot(p.ctx, p.uuid, "media", KindManual, now.Add(time.Second))
	p.must("second snapshot", err)
	p.allowed(itReader, "ls", snapshots)

	p.must("close the snapshots", SetSnapshotsACL(p.ctx, p.uuid, "media", nil))
	if got := p.mode(source); got != 0o700 {
		t.Fatalf("%s is %o once closed, want 700", source, got)
	}
	p.refused(itReader, "ls", snapshots)
	p.refused(itReader, "cat", earlier)

	// The health pass narrows through the mount table: of this pool alone
	// here, so the test leaves the machine's own pools as they are. A
	// subvolume and a folder of another owner in SnapshotRoot are not
	// LinuxIO's, and are left open.
	p.must("create a subvolume beside the sources", createSubvolumeAt(p.mnt, filepath.Join(SnapshotRoot, "foreign")))
	theirs := filepath.Join(root, "theirs")
	p.must("mkdir", os.Mkdir(theirs, 0o755))
	p.must("hand over", os.Chown(theirs, itStranger, itStranger))
	for _, open := range []string{stale, theirs, filepath.Join(root, "foreign")} {
		p.must("open "+open, os.Chmod(open, 0o755))
	}
	mounts, err := listMounts(p.ctx)
	p.must("list mounts", err)
	mounts = slices.DeleteFunc(mounts, func(m Mount) bool { return m.UUID != p.uuid })
	p.must("narrow as the health pass does", narrowSnapshotDirsOf(mounts))
	for rel, want := range map[string]uint32{"old-source": 0o700, "theirs": 0o755, "foreign": 0o755} {
		if got := p.mode(filepath.Join(SnapshotRoot, rel)); got != want {
			t.Fatalf("%s is %o after the health pass narrowed, want %o", rel, got, want)
		}
	}
}

// recycleBin makes media's recycle bin and sweeps it twice: once while a
// folder moved into it is new there, and once after everything aged.
func (p *itPool) recycleBin() {
	t := p.t
	const bin = "media/#recycle"
	at := func(rel string) string { return filepath.Join(p.mnt, bin, rel) }
	p.must("make the bin", MakeFolder(p.ctx, p.uuid, bin, 0o777))
	// Made in a folder with a table, the bin starts with that table.
	p.allowed(itWriter, "touch", at("old.txt"))
	p.refused(itReader, "touch", at("by-reader"))

	// A link to a file and one to a folder outside the bin.
	keep := filepath.Join(p.mnt, "media", "keep.txt")
	p.must("write", os.WriteFile(keep, []byte("keep"), 0o644))
	p.must("link", os.Symlink(keep, at("link")))
	p.must("link", os.Symlink(filepath.Join(p.mnt, "media", "sub"), at("dirlink")))
	// A folder deleted over SMB: moved in whole, its files as old as they were.
	album := filepath.Join(p.mnt, "media", "album")
	p.must("mkdir", os.Mkdir(album, 0o755))
	p.must("write", os.WriteFile(filepath.Join(album, "photo.txt"), []byte("photo"), 0o644))
	// A subvolume someone made inside the bin.
	_, err := CreateSubvolume(p.ctx, p.uuid, bin+"/nested")
	p.must("create a subvolume in the bin", err)
	p.must("write", os.WriteFile(at("nested/file.txt"), []byte("nested"), 0o644))

	// The cut-off is a whole second, as the note on a folder is: everything
	// above is older than it, the note the sweep writes is not, and a file
	// made a moment later is not either (a file's time may lag the clock by
	// a tick).
	time.Sleep(1100 * time.Millisecond)
	cutoff := time.Now().Truncate(time.Second)
	p.must("move the folder in", os.Rename(album, at("album")))
	time.Sleep(50 * time.Millisecond)
	p.must("write", os.WriteFile(at("new.txt"), []byte("new"), 0o644))

	removed, err := RemoveOlderThan(p.ctx, p.uuid, bin, cutoff)
	p.must("first sweep", err)
	if removed != 3 {
		t.Fatalf("the first sweep removed %d, want the old file and the two links", removed)
	}
	for _, name := range []string{"old.txt", "link", "dirlink"} {
		p.gone(filepath.Join(bin, name))
	}
	// What the links pointed to, the new file, the subvolume, and the files
	// of the folder that only just arrived are all there.
	for _, rel := range []string{"media/keep.txt", "media/sub/by-member", bin + "/new.txt", bin + "/nested/file.txt", bin + "/album/photo.txt"} {
		p.stat(rel)
	}
	// The folder carries when it was first seen and where, in an attribute
	// of root's own namespace; the subvolume, which the sweep never enters,
	// carries none.
	note := make([]byte, 64)
	n, err := unix.Getxattr(at("album"), "trusted.linuxio.recycled", note)
	p.must("read the folder's note", err)
	var seconds int64
	var parent uint64
	if _, err = fmt.Sscanf(string(note[:n]), "%d:%d", &seconds, &parent); err != nil {
		t.Fatalf("the folder's note is %q: %v", note[:n], err)
	}
	if binIno := p.stat(bin).Ino; parent != binIno || seconds < cutoff.Unix() {
		t.Fatalf("the folder's note is %q, want a time from %d on and the bin's inode %d", note[:n], cutoff.Unix(), binIno)
	}
	if _, err = unix.Getxattr(at("nested"), "trusted.linuxio.recycled", note); !errors.Is(err, unix.ENODATA) {
		t.Fatalf("the subvolume in the bin carries a note (%v)", err)
	}

	// A month on: everything in the bin goes but the subvolume, and the
	// bin itself stays.
	removed, err = RemoveOlderThan(p.ctx, p.uuid, bin, time.Now().Add(time.Hour))
	p.must("second sweep", err)
	if removed != 2 {
		t.Fatalf("the second sweep removed %d, want the new file and the folder's file", removed)
	}
	p.gone(bin + "/new.txt")
	p.gone(bin + "/album")
	for _, rel := range []string{bin, bin + "/nested/file.txt", "media/keep.txt"} {
		p.stat(rel)
	}

	// A file or a link where a bin should be, which whoever may write the
	// folder can put there, is told apart from a failure: neither the save
	// of the folder nor the nightly run stops at it.
	p.must("write", os.WriteFile(filepath.Join(p.mnt, "media/sub/#recycle"), nil, 0o644))
	p.must("mkdir", os.Mkdir(filepath.Join(p.mnt, "media/linked"), 0o755))
	p.must("link", os.Symlink(filepath.Join(p.mnt, "media"), filepath.Join(p.mnt, "media/linked/#recycle")))
	for _, taken := range []string{"media/sub/#recycle", "media/linked/#recycle"} {
		if err = MakeFolder(p.ctx, p.uuid, taken, 0o777); !NotAFolder(err) {
			t.Fatalf("MakeFolder(%s) = %v, want what NotAFolder tells", taken, err)
		}
		if _, err = RemoveOlderThan(p.ctx, p.uuid, taken, time.Now().Add(time.Hour)); !NotAFolder(err) {
			t.Fatalf("RemoveOlderThan(%s) = %v, want what NotAFolder tells", taken, err)
		}
	}
	p.stat("media/keep.txt")
}
