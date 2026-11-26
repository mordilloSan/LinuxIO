package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// recycleTop is a fake pool with a folder "media" and its bin, and a folder
// outside it that a link in the bin points to.
func recycleTop(t *testing.T) (uuid, top string) {
	t.Helper()
	uuid = aclFolder(t)
	top, err := TopLevel(context.Background(), uuid)
	if err != nil {
		t.Fatal(err)
	}
	return uuid, top
}

func writeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMakeFolder(t *testing.T) {
	uuid, top := recycleTop(t)
	ctx := context.Background()
	if err := MakeFolder(ctx, uuid, "media/#recycle", 0o777); err != nil {
		t.Fatalf("MakeFolder() = %v", err)
	}
	if info, err := os.Stat(filepath.Join(top, "media", "#recycle")); err != nil || !info.IsDir() {
		t.Fatalf("the folder: %v", err)
	}
	// Made again, it is left as it is.
	if err := MakeFolder(ctx, uuid, "media/#recycle", 0o777); err != nil {
		t.Fatalf("MakeFolder() again = %v", err)
	}
	// A link at the name is not followed.
	if err := os.Symlink(t.TempDir(), filepath.Join(top, "media", "link")); err != nil {
		t.Fatal(err)
	}
	if err := MakeFolder(ctx, uuid, "media/link", 0o777); err == nil {
		t.Error("MakeFolder() over a link = nil")
	}
}

// What has been in the bin since before the cut-off goes, folders and all;
// the bin itself stays, and a link in it is removed, not followed.
func TestRemoveOlderThan(t *testing.T) {
	stampUnitTests(t)
	uuid, top := recycleTop(t)
	ctx := context.Background()
	bin := filepath.Join(top, "media", "#recycle")
	writeTree(t, bin, "a.txt", "sub/b.txt", "sub/deeper/c.txt")
	outside := t.TempDir()
	writeTree(t, outside, "keep.txt")
	if err := os.Symlink(outside, filepath.Join(bin, "sub", "link")); err != nil {
		t.Fatal(err)
	}

	// Nothing is old enough yet.
	removed, err := RemoveOlderThan(ctx, uuid, "media/#recycle", time.Now().Add(-time.Hour))
	if errors.Is(err, unix.ENOTSUP) {
		t.Skip("the temp dir's filesystem keeps no extended attributes on folders")
	}
	if err != nil || removed != 0 {
		t.Fatalf("RemoveOlderThan(an hour ago) = %d, %v", removed, err)
	}
	if _, err = os.Stat(filepath.Join(bin, "sub", "deeper", "c.txt")); err != nil {
		t.Fatalf("a file too young went: %v", err)
	}

	// Everything is.
	removed, err = RemoveOlderThan(ctx, uuid, "media/#recycle", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("RemoveOlderThan(in an hour) = %v", err)
	}
	if removed != 4 {
		t.Errorf("removed %d files and links, want 4", removed)
	}
	entries, err := os.ReadDir(bin)
	if err != nil || len(entries) != 0 {
		t.Errorf("the bin holds %v, %v, want nothing", entries, err)
	}
	if _, err = os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Errorf("a file the link pointed to went: %v", err)
	}
}

// stampUnitTests has the stamps written where the test's own user may write
// them; the worker, root, keeps them where only root can. A temp dir whose
// filesystem keeps no such attribute on a folder skips the test.
func stampUnitTests(t *testing.T) {
	t.Helper()
	old := recycleStampAttr
	if os.Geteuid() != 0 {
		recycleStampAttr = "user.linuxio.recycled"
	}
	t.Cleanup(func() { recycleStampAttr = old })
	if err := unix.Setxattr(t.TempDir(), recycleStampAttr, []byte("1:1"), 0); errors.Is(err, unix.ENOTSUP) {
		t.Skip("the temp dir's filesystem keeps no extended attributes on folders")
	}
}

// A folder that cannot be noted (a full pool, a full limit, attributes a
// user filled) is new at every pass: kept with what it holds. The pass goes
// on to the rest of the bin and fails nothing, since emptying is what frees
// the room, and whoever may write the bin could otherwise stop it.
func TestRemoveOlderThanGoesOnPastAFolderItCannotNote(t *testing.T) {
	uuid, top := recycleTop(t)
	bin := filepath.Join(top, "media", "#recycle")
	writeTree(t, bin, "a.txt", "moved/old.txt", "z.txt")
	old := recycleStampAttr
	t.Cleanup(func() { recycleStampAttr = old })
	recycleStampAttr = "nosuchnamespace.linuxio.recycled" // no filesystem takes it
	// Every file is older than the cut-off, and now is after it, as in a
	// run, whose cut-off is a month back.
	cutoff := time.Now().Add(time.Hour)
	oldNow := recycleNow
	t.Cleanup(func() { recycleNow = oldNow })
	recycleNow = func() time.Time { return cutoff.Add(time.Hour) }

	for pass := 1; pass <= 2; pass++ {
		removed, err := RemoveOlderThan(context.Background(), uuid, "media/#recycle", cutoff)
		if err != nil {
			t.Fatalf("pass %d: RemoveOlderThan() = %v", pass, err)
		}
		if want := 4 - 2*pass; removed != want {
			t.Errorf("pass %d removed %d, want %d: the files beside the folder, once", pass, removed, want)
		}
		if _, err = os.Stat(filepath.Join(bin, "moved", "old.txt")); err != nil {
			t.Errorf("pass %d: what the folder holds went: %v", pass, err)
		}
	}
}

// A folder moved into the bin by hand brings files as old as they were:
// only the folder's own time changes with the move. What is in it is kept
// until the folder itself has been seen in the bin for as long as a file
// must have been, which the pass that first finds it starts counting.
func TestRemoveOlderThanKeepsWhatAFolderNewToTheBinHolds(t *testing.T) {
	stampUnitTests(t)
	uuid, top := recycleTop(t)
	ctx := context.Background()
	bin := filepath.Join(top, "media", "#recycle")
	writeTree(t, bin, "deleted.txt", "moved/old.txt", "moved/deeper/older.txt")
	oldNow := recycleNow
	t.Cleanup(func() { recycleNow = oldNow })

	// Every file is older than the cut-off; the folders are seen now, which
	// is after it.
	cutoff := time.Now().Add(time.Hour)
	recycleNow = func() time.Time { return cutoff.Add(time.Hour) }
	removed, err := RemoveOlderThan(ctx, uuid, "media/#recycle", cutoff)
	if errors.Is(err, unix.ENOTSUP) {
		t.Skip("the temp dir's filesystem keeps no extended attributes on folders")
	}
	if err != nil {
		t.Fatalf("RemoveOlderThan() = %v", err)
	}
	if removed != 1 {
		t.Errorf("removed %d, want the one file at the top of the bin", removed)
	}
	for _, kept := range []string{"moved/old.txt", "moved/deeper/older.txt"} {
		if _, err = os.Stat(filepath.Join(bin, kept)); err != nil {
			t.Errorf("%s went with its folder new to the bin: %v", kept, err)
		}
	}

	// A later pass, with the folders seen before its cut-off.
	removed, err = RemoveOlderThan(ctx, uuid, "media/#recycle", cutoff.Add(2*time.Hour))
	if err != nil || removed != 2 {
		t.Fatalf("the later pass removed %d, %v, want 2", removed, err)
	}
	if entries, _ := os.ReadDir(bin); len(entries) != 0 {
		t.Errorf("the bin holds %v after the later pass", entries)
	}
}

// A stamp is of the place it was written in: a folder taken out of the bin
// and put back elsewhere in it is new there.
func TestRemoveOlderThanReadsAStampOfAnotherPlaceAsNone(t *testing.T) {
	stampUnitTests(t)
	uuid, top := recycleTop(t)
	bin := filepath.Join(top, "media", "#recycle")
	writeTree(t, bin, "moved/old.txt")
	// Seen long ago, in a folder that is not the bin.
	err := unix.Setxattr(filepath.Join(bin, "moved"), recycleStampAttr, []byte("1000000000:1"), 0)
	if errors.Is(err, unix.ENOTSUP) {
		t.Skip("the temp dir's filesystem keeps no extended attributes on folders")
	}
	if err != nil {
		t.Fatal(err)
	}
	oldNow := recycleNow
	t.Cleanup(func() { recycleNow = oldNow })
	cutoff := time.Now().Add(time.Hour)
	recycleNow = func() time.Time { return cutoff.Add(time.Hour) }
	removed, err := RemoveOlderThan(context.Background(), uuid, "media/#recycle", cutoff)
	if err != nil || removed != 0 {
		t.Fatalf("RemoveOlderThan() = %d, %v, want nothing removed", removed, err)
	}
}

// A folder with more entries than one read brings is emptied all the same.
func TestRemoveOlderThanReadsABigFolderInParts(t *testing.T) {
	stampUnitTests(t)
	uuid, top := recycleTop(t)
	bin := filepath.Join(top, "media", "#recycle")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	const files = 2500
	for i := range files {
		if err := os.WriteFile(filepath.Join(bin, fmt.Sprintf("f%04d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := RemoveOlderThan(context.Background(), uuid, "media/#recycle", time.Now().Add(time.Hour))
	if err != nil || removed != files {
		t.Fatalf("RemoveOlderThan() = %d, %v, want %d", removed, err, files)
	}
}

func TestRemoveOlderThanWithoutABin(t *testing.T) {
	uuid, _ := recycleTop(t)
	removed, err := RemoveOlderThan(context.Background(), uuid, "media/#recycle", time.Now())
	if err != nil || removed != 0 {
		t.Errorf("RemoveOlderThan() of no bin = %d, %v", removed, err)
	}
}

func TestRemoveOlderThanRefusesALinkOnTheWay(t *testing.T) {
	uuid, top := recycleTop(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(top, "media", "#recycle")); err != nil {
		t.Fatal(err)
	}
	_, err := RemoveOlderThan(context.Background(), uuid, "media/#recycle", time.Now().Add(time.Hour))
	if !errors.Is(err, ErrLinkOnPath) {
		t.Errorf("RemoveOlderThan() through a link = %v, want ErrLinkOnPath", err)
	}
}
