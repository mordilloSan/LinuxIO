package btrfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Unit tests open folders for real below a temp dir, which is on whatever
// filesystem holds it. Every folder then counts as one pool, as on a pool
// with nothing else mounted inside; the integration tests run on real pools
// and read the real fsid.
func TestMain(m *testing.M) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		fsidOf = func(int) ([16]byte, error) { return [16]byte{1}, nil }
		// And the snapshot folders a unit test makes are its user's.
		snapshotsOwnerUID = uint32(os.Getuid())
	}
	// No test asks the machine's systemd what it is: without a version, the
	// health run leaves /etc/fstab and the mounts as they are.
	managerVersion = func(context.Context) (int, error) { return 0, errors.New("no systemd in unit tests") }
	// Every filesystem list reads the hot spares: never the host's.
	dir, err := os.MkdirTemp("", "btrfs-spares")
	if err != nil {
		panic(err)
	}
	SparePath = filepath.Join(dir, "btrfs-spares.json")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// A folder on another filesystem, or a top level that is no btrfs at all,
// is refused: the check fails closed.
func TestOnFilesystemOfRefusesAnotherFilesystem(t *testing.T) {
	top, _ := linkedTop(t)
	old := fsidOf
	t.Cleanup(func() { fsidOf = old })

	calls := 0
	fsidOf = func(int) ([16]byte, error) {
		calls++
		return [16]byte{byte(calls)}, nil // top and the folder differ
	}
	if _, err := openBelow(top, "shared"); !errors.Is(err, ErrNotOnFilesystem) {
		t.Errorf("folder on another filesystem: err = %v, want ErrNotOnFilesystem", err)
	}
	// The host's state, not a bad path: an error, but not the client's.
	fsidOf = func(int) ([16]byte, error) { return [16]byte{}, ErrNotOnFilesystem }
	if _, err := openBelow(top, "shared"); err == nil || errors.Is(err, ErrNotOnFilesystem) {
		t.Errorf("top level that is not btrfs: err = %v, want an error that is not ErrNotOnFilesystem", err)
	}
	if err := mkdirAllBelow(top, "linuxio-snapshots/@", 0o755, nil); err == nil || errors.Is(err, ErrNotOnFilesystem) {
		t.Errorf("mkdir below a top level that is not btrfs: err = %v, want an error that is not ErrNotOnFilesystem", err)
	}
	if _, err := os.Stat(filepath.Join(top, "linuxio-snapshots")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a folder was made below it: %v", err)
	}
}

// linkedTop is a top level with a real folder shared/real, a link
// shared/inbox to victim, a folder outside it, and a link shared/alias to
// real, beside it.
func linkedTop(t *testing.T) (top, victim string) {
	t.Helper()
	top, victim = t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, "shared", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(top, "shared", "inbox")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(top, "shared", "alias")); err != nil {
		t.Fatal(err)
	}
	return top, victim
}

func requireEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("%s holds %v (err %v), want nothing", dir, entries, err)
	}
}

func TestOpenBelowFollowsNoLinkAndStaysBelow(t *testing.T) {
	top, _ := linkedTop(t)
	for _, rel := range []string{"", ".", "shared", "shared/real"} {
		fd, err := openBelow(top, rel)
		if err != nil {
			t.Fatalf("openBelow(%q): %v", rel, err)
		}
		unix.Close(fd)
	}
	// A link at the end and one on the way, out of the pool and inside it.
	for _, rel := range []string{"shared/inbox", "shared/inbox/deeper", "shared/alias", "shared/alias/deeper"} {
		if _, err := openBelow(top, rel); !errors.Is(err, ErrLinkOnPath) {
			t.Errorf("openBelow(%q) = %v, want ErrLinkOnPath", rel, err)
		}
	}
	// A folder that exists beside top: only the kernel's refusal to climb
	// out keeps it shut.
	outside := t.TempDir()
	rel, err := filepath.Rel(top, outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = openBelow(top, rel); !errors.Is(err, unix.EXDEV) {
		t.Errorf("openBelow(%q), a folder beside top: err = %v, want EXDEV", rel, err)
	}
	if _, err := openBelow(top, "shared/missing"); !errors.Is(err, unix.ENOENT) {
		t.Errorf("missing folder: err = %v, want ENOENT", err)
	}
}

// The case the handles are for: a subvolume path typed through a folder
// that another user made a link. Nothing may happen where the link points.
func TestSubvolumeOpsRefuseAPathThroughALink(t *testing.T) {
	top, victim := linkedTop(t)
	for name, err := range map[string]error{
		"create":      createSubvolumeAt(top, "shared/inbox/2026"),
		"destroy":     destroySubvolume(top, "shared/inbox/2026"),
		"snapshot of": snapshotSubvolumeTo(top, "shared/inbox", "shared/real/copy", true),
		"snapshot to": snapshotSubvolumeTo(top, "shared/real", "shared/inbox/copy", true),
		"read-only":   setSubvolumeReadOnly(top, "shared/inbox", true),
		"compression": setCompressionProperty(top, "shared/inbox", "zstd"),
		"mkdir":       mkdirAllBelow(top, "shared/inbox/a/b", 0o755, nil),
		"send":        sendStream(top, "shared/inbox", "", os.Stdout),
		"send parent": sendStream(top, "shared/real", "shared/inbox", os.Stdout),
		"rename from": renameBelow(top, "shared/inbox/x", "shared/real/x"),
		"rename to":   renameBelow(top, "shared/real", "shared/inbox/real"),
		// A link to a folder of the same pool: the delete would otherwise
		// hit a subvolume of that name in the folder it points to.
		"destroy through a link inside": destroySubvolume(top, "shared/alias/2026"),
	} {
		if !errors.Is(err, ErrLinkOnPath) {
			t.Errorf("%s: err = %v, want ErrLinkOnPath", name, err)
		}
	}
	requireEmpty(t, victim)
}

func TestMkdirAllBelowAndRenameBelow(t *testing.T) {
	top, _ := linkedTop(t)
	if err := mkdirAllBelow(top, "linuxio-snapshots/@home", 0o755, nil); err != nil {
		t.Fatal(err)
	}
	// Again, with every folder there already.
	if err := mkdirAllBelow(top, "linuxio-snapshots/@home", 0o755, nil); err != nil {
		t.Fatalf("existing folders: %v", err)
	}
	if err := renameBelow(top, "linuxio-snapshots/@home", "shared/real/moved"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(top, "shared", "real", "moved")); err != nil || !info.IsDir() {
		t.Fatalf("renamed folder: %v, %v", info, err)
	}
	// Never onto a name that is taken, even by an empty folder.
	if err := os.Mkdir(filepath.Join(top, "shared", "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := renameBelow(top, "shared/real/moved", "shared/taken"); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("rename onto a taken name: err = %v, want EEXIST", err)
	}
	// A name is taken whatever holds it, and free when a folder on the way
	// is missing; a link on the way is an error, not "free".
	for rel, want := range map[string]bool{"shared/taken": true, "shared/inbox": true, "shared/free": false, "missing/x": false} {
		if got, err := existsBelow(top, rel); err != nil || got != want {
			t.Errorf("existsBelow(%q) = %v, %v, want %v", rel, got, err, want)
		}
	}
	if _, err := existsBelow(top, "shared/inbox/x"); !errors.Is(err, ErrLinkOnPath) {
		t.Errorf("existsBelow through a link: err = %v, want ErrLinkOnPath", err)
	}
	// The link itself is renamed, never what it points to.
	if err := renameBelow(top, "shared/inbox", "shared/renamed-link"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(top, "shared", "renamed-link")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("renamed link: %v, %v", info, err)
	}
}
