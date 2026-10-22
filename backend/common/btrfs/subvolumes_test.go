package btrfs

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseSubvolumeList(t *testing.T) {
	rows, err := parseSubvolumeList(readFixture(t, "subvol_list.txt"))
	if err != nil {
		t.Fatalf("parseSubvolumeList: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	want := subvolumeRow{
		ID: 301, ParentID: 5, Path: "linuxio-snapshots/@-2026-10-19",
		UUID: "6b2c1f0e-1111-4a2b-8c3d-000000000004", ParentUUID: "6b2c1f0e-1111-4a2b-8c3d-000000000001",
	}
	if rows[3] != want {
		t.Fatalf("row 3 = %+v, want %+v", rows[3], want)
	}
	if rows[0].Path != "@" || rows[0].ParentUUID != "" || rows[0].ReceivedUUID != "" {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[2].ParentID != 256 {
		t.Fatalf("nested ParentID = %d, want 256", rows[2].ParentID)
	}

	snaps, err := parseSubvolumeList(readFixture(t, "subvol_list_snap.txt"))
	if err != nil || len(snaps) != 1 || snaps[0].OTime != "2026-10-19 10:15:00" {
		t.Fatalf("snapshot list = %+v, %v", snaps, err)
	}
	noTime, err := parseSubvolumeList("ID 5 gen 1 cgen 1 top level 5 otime - path <FS_TREE>/old")
	if err != nil || len(noTime) != 1 || noTime[0].Path != "old" || noTime[0].OTime != "-" {
		t.Fatalf("otime - list = %+v, %v", noTime, err)
	}
	if _, err := parseSubvolumeList("garbage"); err == nil {
		t.Fatal("expected error for unparseable line")
	}
}

// stubSubvolumes serves rows and the default subvolume id from the root
// tree, quotas on with no data, and records read-only and compression
// changes into calls.
func stubSubvolumes(t *testing.T, rows []subvolumeRow, defaultID int, calls *[][]string) {
	t.Helper()
	oldRead, oldQgroups := readSubvolumes, readQgroups
	readSubvolumes = func(string) ([]subvolumeRow, int, error) { return rows, defaultID, nil }
	readQgroups = func(string) (map[int]QgroupUsage, error) { return map[int]QgroupUsage{}, nil }
	t.Cleanup(func() { readSubvolumes, readQgroups = oldRead, oldQgroups })
	stubSubvolOps(t, func(args []string) error {
		*calls = append(*calls, args)
		return nil
	})
}

// stubSubvolOps hands every subvolume and quota mutation to record, in the
// words of the btrfs command it replaced; record's error is the result.
func stubSubvolOps(t *testing.T, record func(args []string) error) {
	t.Helper()
	old := subvolOps
	t.Cleanup(func() { subvolOps = old })
	subvolOps.create = func(p string) error { return record([]string{"subvolume", "create", p}) }
	subvolOps.snapshot = func(src, dst string, ro bool) error {
		if ro {
			return record([]string{"subvolume", "snapshot", "-r", src, dst})
		}
		return record([]string{"subvolume", "snapshot", src, dst})
	}
	subvolOps.destroy = func(p string) error { return record([]string{"subvolume", "delete", p}) }
	subvolOps.setDefault = func(top string, id int) error {
		return record([]string{"subvolume", "set-default", strconv.Itoa(id), top})
	}
	subvolOps.readOnly = func(p string, ro bool) error { return record([]string{"ro", p, strconv.FormatBool(ro)}) }
	subvolOps.compression = func(p, v string) error { return record([]string{"xattr", p, "btrfs.compression", v}) }
	subvolOps.quota = func(top string, enable bool) error {
		if enable {
			return record([]string{"quota", "enable", top})
		}
		return record([]string{"quota", "disable", top})
	}
	subvolOps.limit = func(p string, max *uint64) error {
		if max == nil {
			return record([]string{"qgroup", "limit", "none", p})
		}
		return record([]string{"qgroup", "limit", strconv.FormatUint(*max, 10), p})
	}
}

// stubQgroups serves qgroup usage, or the error, for every filesystem.
func stubQgroups(t *testing.T, usage map[int]QgroupUsage, err error) {
	t.Helper()
	old := readQgroups
	readQgroups = func(string) (map[int]QgroupUsage, error) { return usage, err }
	t.Cleanup(func() { readQgroups = old })
}

// fixtureRows is a root filesystem with a home subvolume, a Docker layer, a
// LinuxIO snapshot of @ and a read-only data subvolume.
func fixtureRows() []subvolumeRow {
	snapAt := time.Date(2026, 10, 19, 10, 15, 0, 0, time.Local)
	return []subvolumeRow{
		{ID: 256, ParentID: 5, Path: "@", UUID: localUUID(1)},
		{ID: 257, ParentID: 5, Path: "@home", UUID: localUUID(2)},
		{ID: 300, ParentID: 256, Path: "@/var/lib/docker/btrfs/subvolumes/abc", UUID: localUUID(3)},
		{ID: 301, ParentID: 5, Path: "linuxio-snapshots/@-2026-10-19", UUID: localUUID(4), ParentUUID: localUUID(1), ReadOnly: true, CreatedAt: &snapAt},
		{ID: 302, ParentID: 5, Path: "data", UUID: localUUID(5), ReadOnly: true},
	}
}

// stubSubvolumeBtrfs serves fixtureRows with the given default subvolume and
// records every mutation.
func stubSubvolumeBtrfs(t *testing.T, defaultID int) *[][]string {
	t.Helper()
	calls := &[][]string{}
	stubSubvolumes(t, fixtureRows(), defaultID, calls)
	return calls
}

var subvolMounts = []Mount{
	{Mountpoint: "/mnt/top", UUID: rootUUID, SubvolID: 5},
	{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256},
	{Mountpoint: "/home", UUID: rootUUID, Subvol: "@home", SubvolID: 257},
	{Mountpoint: "/other", UUID: dataUUID, Subvol: "data", SubvolID: 256},
}

func TestListSubvolumesMarksReadOnlyAndSnapshots(t *testing.T) {
	stubMounts(t, subvolMounts)
	stubSubvolumeBtrfs(t, 256)

	got, err := ListSubvolumes(context.Background(), rootUUID)
	if err != nil {
		t.Fatalf("ListSubvolumes: %v", err)
	}
	var paths []string
	for _, sv := range got {
		paths = append(paths, sv.Path)
	}
	// Docker layer subvolume is filtered; sorted by path.
	if want := []string{"@", "@home", "data", "linuxio-snapshots/@-2026-10-19"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	root, home, data, snap := got[0], got[1], got[2], got[3]
	if !root.IsDefault || root.ReadOnly || root.IsSnapshot || root.CreatedAt != nil {
		t.Fatalf("root = %+v", root)
	}
	if root.Mountpoint == nil || *root.Mountpoint != "/" || home.Mountpoint == nil || *home.Mountpoint != "/home" {
		t.Fatalf("mountpoints = %v, %v", root.Mountpoint, home.Mountpoint)
	}
	if data.Mountpoint != nil || !data.ReadOnly || data.IsSnapshot {
		t.Fatalf("data = %+v", data)
	}
	if !snap.IsSnapshot || !snap.ReadOnly || snap.IsDefault || snap.CreatedAt == nil || snap.CreatedAt.Format("2006-01-02 15:04:05") != "2026-10-19 10:15:00" {
		t.Fatalf("snap = %+v", snap)
	}
}

func TestValidateSubvolumePathRejects(t *testing.T) {
	for _, p := range []string{"", "/@", "a/../b", "linuxio-snapshots/x", "linuxio-snapshots", ".", "a/./b", "a\x00b", "a//b", "x/", "@home/", "a\nb", "a,b", "a b", "a\tb", "a\u00a0b"} {
		if err := ValidateSubvolumePath(p); err == nil {
			t.Errorf("ValidateSubvolumePath(%q) = nil, want error", p)
		}
	}
	for _, p := range []string{"@", "a/b", "my-snapshots/x"} {
		if err := ValidateSubvolumePath(p); err != nil {
			t.Errorf("ValidateSubvolumePath(%q) = %v", p, err)
		}
	}
}

func TestDeleteSubvolumeRefusesDefault(t *testing.T) {
	stubMounts(t, subvolMounts)
	calls := stubSubvolumeBtrfs(t, 302)

	err := DeleteSubvolume(context.Background(), rootUUID, "data")
	if err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("err = %v, want default refusal", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mutations: %v", *calls)
	}
}

func TestDeleteSubvolumeRefusals(t *testing.T) {
	stubMounts(t, append([]Mount{{Mountpoint: "/mnt/data/x", UUID: rootUUID, Subvol: "data/x", SubvolID: 400}}, subvolMounts...))
	calls := stubSubvolumeBtrfs(t, 256)
	ctx := context.Background()

	for path, want := range map[string]string{
		"@home":                          "mounted",
		"data":                           "mounted", // descendant mounted
		"linuxio-snapshots/@-2026-10-19": "use the snapshot routes to delete snapshots",
		"../x":                           "invalid",
	} {
		if err := DeleteSubvolume(ctx, rootUUID, path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("DeleteSubvolume(%q) = %v, want %q", path, err, want)
		}
	}
	if err := DeleteSubvolume(ctx, rootUUID, "gone"); err != nil {
		t.Errorf("missing subvolume: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mutations: %v", *calls)
	}
}

func TestSubvolumeMutationsRunBtrfs(t *testing.T) {
	stubMounts(t, subvolMounts)
	calls := stubSubvolumeBtrfs(t, 256)
	ctx := context.Background()

	if _, err := SetSubvolumeReadOnly(ctx, rootUUID, "@home", true); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultSubvolume(ctx, rootUUID, "@home"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSubvolume(ctx, rootUUID, "data"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSubvolume(ctx, rootUUID, "data"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ro", "/mnt/top/@home", "true"},
		{"subvolume", "set-default", "257", "/mnt/top"},
		{"subvolume", "create", "/mnt/top/data"},
		{"subvolume", "delete", "/mnt/top/data"},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}
