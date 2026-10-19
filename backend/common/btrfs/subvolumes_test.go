package btrfs

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
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

// stubSubvolumeBtrfs serves the fixtures for list/get-default and records
// every other btrfs invocation.
func stubSubvolumeBtrfs(t *testing.T, defaultOut string) *[][]string {
	t.Helper()
	var calls [][]string
	old := runBtrfs
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "subvolume list -a -p -u -q -R "):
			return readFixture(t, "subvol_list.txt"), nil
		case strings.HasPrefix(joined, "subvolume list -a -r "):
			return readFixture(t, "subvol_list_ro.txt"), nil
		case strings.HasPrefix(joined, "subvolume list -a -s "):
			return readFixture(t, "subvol_list_snap.txt"), nil
		case strings.HasPrefix(joined, "qgroup show "):
			return "", nil // quotas on, no data
		case strings.HasPrefix(joined, "subvolume get-default "):
			return defaultOut, nil
		}
		calls = append(calls, args)
		return "", nil
	}
	t.Cleanup(func() { runBtrfs = old })
	return &calls
}

var subvolMounts = []Mount{
	{Mountpoint: "/mnt/top", UUID: rootUUID, SubvolID: 5},
	{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256},
	{Mountpoint: "/home", UUID: rootUUID, Subvol: "@home", SubvolID: 257},
	{Mountpoint: "/other", UUID: dataUUID, Subvol: "data", SubvolID: 256},
}

func TestListSubvolumesMarksReadOnlyAndSnapshots(t *testing.T) {
	stubMounts(t, subvolMounts)
	stubSubvolumeBtrfs(t, "ID 256 gen 12345 top level 5 path @")

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
	calls := stubSubvolumeBtrfs(t, "ID 302 gen 12200 top level 5 path data")

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
	calls := stubSubvolumeBtrfs(t, "ID 256 gen 12345 top level 5 path @")
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
	calls := stubSubvolumeBtrfs(t, "ID 256 gen 12345 top level 5 path @")
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
		{"property", "set", "-ts", "/mnt/top/@home", "ro", "true"},
		{"subvolume", "set-default", "257", "/mnt/top"},
		{"subvolume", "create", "/mnt/top/data"},
		{"subvolume", "delete", "/mnt/top/data"},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}
