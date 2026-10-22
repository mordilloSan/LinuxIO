package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var snapshotTime = time.Date(2026, 10, 19, 10, 15, 0, 0, time.UTC)

func TestSnapshotPathRoundTrip(t *testing.T) {
	if got, want := SnapshotPath("data/photos", snapshotTime, KindManual), "linuxio-snapshots/data%2Fphotos/20261019T101500Z-manual"; got != want {
		t.Fatalf("SnapshotPath = %q, want %q", got, want)
	}
	for _, source := range []string{"@", "data/photos", "a%2Fb", "50%"} {
		for _, kind := range snapshotKinds {
			// A non-UTC input must still produce a UTC path.
			at := snapshotTime.In(time.FixedZone("x", 3600))
			src, gotAt, gotKind, ok := ParseSnapshotPath(SnapshotPath(source, at, kind))
			if !ok || src != source || !gotAt.Equal(snapshotTime) || gotKind != kind {
				t.Errorf("round trip %q/%s = %q, %v, %q, %v", source, kind, src, gotAt, gotKind, ok)
			}
		}
	}
}

func TestParseSnapshotPathRejectsForeign(t *testing.T) {
	for _, p := range []string{
		"foo/bar", "linuxio-snapshots", "linuxio-snapshots/@", "linuxio-snapshots/@/",
		"linuxio-snapshots/@/20261019T101500Z-manual/extra",
		"linuxio-snapshots/@/2026-10-19-manual",
		"linuxio-snapshots/@/20261019T101500Z-weekly",
		"linuxio-snapshots/@/20261019T101500Z",
		"linuxio-snapshots//20261019T101500Z-manual",
		"other/@/20261019T101500Z-manual",
	} {
		if _, _, _, ok := ParseSnapshotPath(p); ok {
			t.Errorf("ParseSnapshotPath(%q) ok, want rejection", p)
		}
	}
}

func TestDeleteSnapshotRefusesNonSnapshotPath(t *testing.T) {
	stubMounts(t, subvolMounts)
	calls := stubSubvolumeBtrfs(t, 256)

	for _, p := range []string{"@", "data", "linuxio-snapshots/@-2026-10-19", "../etc"} {
		err := DeleteSnapshot(context.Background(), rootUUID, p)
		if err == nil || !strings.Contains(err.Error(), "not a linuxio snapshot path") {
			t.Errorf("DeleteSnapshot(%q) = %v, want refusal", p, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mutations: %v", *calls)
	}
}

const snapSource = "@home"

// stubSnapshotTop mounts the top level at a temp dir and serves a subvolume
// listing containing the snapshots the test expects to be created or deleted.
func stubSnapshotTop(t *testing.T, extra ...subvolumeRow) (top string, calls *[][]string) {
	t.Helper()
	top = t.TempDir()
	stubMounts(t, []Mount{
		{Mountpoint: top, UUID: rootUUID, SubvolID: 5},
		{Mountpoint: "/home", UUID: rootUUID, Subvol: "@home", SubvolID: 257},
	})
	recorded := &[][]string{}
	stubSubvolumes(t, append(fixtureRows(), extra...), 256, recorded)
	return top, recorded
}

// snapshotSubvolume is a read-only subvolume row at path, as a snapshot is.
func snapshotSubvolume(id int, path string) subvolumeRow {
	return subvolumeRow{ID: 900 + id, ParentID: 5, Path: path, UUID: localUUID(id), ReadOnly: true}
}

func TestCreateSnapshotRunsReadOnlySnapshotAtTopLevelPath(t *testing.T) {
	dest := SnapshotPath(snapSource, snapshotTime, KindManual)
	top, calls := stubSnapshotTop(t, snapshotSubvolume(1, dest))

	snap, err := CreateSnapshot(context.Background(), rootUUID, snapSource, KindManual, snapshotTime)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	want := [][]string{{"subvolume", "snapshot", "-r", filepath.Join(top, snapSource), filepath.Join(top, dest)}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("btrfs calls = %v, want %v", *calls, want)
	}
	if info, err := os.Stat(filepath.Join(top, SnapshotRoot, "@home")); err != nil || !info.IsDir() {
		t.Fatalf("snapshot directory not created: %v", err)
	}
	if snap.Source != snapSource || snap.Path != dest || snap.Kind != KindManual || !snap.CreatedAt.Equal(snapshotTime) || snap.UUID == "" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestCreateSnapshotRefusals(t *testing.T) {
	_, calls := stubSnapshotTop(t)
	for source, wantErr := range map[string]string{
		"linuxio-snapshots/@-2026-10-19": "reserved",
		"missing":                        "not found",
	} {
		_, err := CreateSnapshot(context.Background(), rootUUID, source, KindManual, snapshotTime)
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("CreateSnapshot(%q) = %v, want %q", source, err, wantErr)
		}
	}
	if _, err := CreateSnapshot(context.Background(), rootUUID, "@", "weekly", snapshotTime); err == nil {
		t.Error("invalid kind accepted")
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mutations: %v", *calls)
	}
}

func TestListSnapshotsFiltersSortsAndIgnoresForeign(t *testing.T) {
	older := SnapshotPath("@", snapshotTime.Add(-time.Hour), KindTimeline)
	newer := SnapshotPath("@", snapshotTime, KindManual)
	other := SnapshotPath("data/photos", snapshotTime.Add(time.Hour), KindPreUpdate)
	stubSnapshotTop(t, snapshotSubvolume(1, older), snapshotSubvolume(2, newer), snapshotSubvolume(3, other), snapshotSubvolume(4, "linuxio-snapshots/foreign"))

	got, err := ListSnapshots(context.Background(), rootUUID, nil)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	var paths []string
	for _, s := range got {
		paths = append(paths, s.Path)
	}
	if want := []string{other, newer, older}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if got[0].Source != "data/photos" || got[0].Kind != KindPreUpdate {
		t.Fatalf("first = %+v", got[0])
	}

	src := "@"
	got, err = ListSnapshots(context.Background(), rootUUID, &src)
	if err != nil || len(got) != 2 || got[0].Path != newer {
		t.Fatalf("filtered = %+v, %v", got, err)
	}
}

func TestDeleteSnapshotDeletesAndIsIdempotent(t *testing.T) {
	path := SnapshotPath("@", snapshotTime, KindManual)
	top, calls := stubSnapshotTop(t, snapshotSubvolume(1, path))

	if err := DeleteSnapshot(context.Background(), rootUUID, path); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if want := [][]string{{"subvolume", "delete", filepath.Join(top, path)}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("btrfs calls = %v, want %v", *calls, want)
	}

	// Not in the listing: already gone, nothing to run.
	_, calls = stubSnapshotTop(t)
	if err := DeleteSnapshot(context.Background(), rootUUID, path); err != nil || len(*calls) != 0 {
		t.Fatalf("missing snapshot: err = %v, calls = %v", err, *calls)
	}
}

func TestDeleteSnapshotRefusesMounted(t *testing.T) {
	path := SnapshotPath("@", snapshotTime, KindManual)
	top, calls := stubSnapshotTop(t, snapshotSubvolume(1, path))
	stubMounts(t, []Mount{
		{Mountpoint: top, UUID: rootUUID, SubvolID: 5},
		{Mountpoint: "/mnt/snap", UUID: rootUUID, Subvol: path, SubvolID: 400},
	})

	err := DeleteSnapshot(context.Background(), rootUUID, path)
	if err == nil || !strings.Contains(err.Error(), "mounted") {
		t.Fatalf("err = %v, want mounted refusal", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mutations: %v", *calls)
	}
}
