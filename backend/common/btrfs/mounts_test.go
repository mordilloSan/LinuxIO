package btrfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const (
	rootUUID = "3f1c0a52-7d1e-4b8a-9c11-0e5a6b7d8f90"
	dataUUID = "9a2b4c6d-1e3f-4a5b-8c7d-9e0f1a2b3c4d"
)

func TestParseFindmnt(t *testing.T) {
	data, err := os.ReadFile("testdata/findmnt.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseFindmnt(data)
	if err != nil {
		t.Fatalf("parseFindmnt: %v", err)
	}
	want := []Mount{
		{
			Mountpoint: "/", Device: "/dev/nvme0n1p2", UUID: rootUUID, Subvol: "@", SubvolID: 256, SubPath: "@",
			Options: []string{"rw", "relatime", "ssd", "space_cache=v2", "subvolid=256", "subvol=/@"},
		},
		{
			Mountpoint: "/var/www", Device: "/dev/nvme0n1p2", UUID: rootUUID, Subvol: "@", SubvolID: 256, SubPath: "@/www",
			Options: []string{"rw", "relatime", "ssd", "space_cache=v2", "subvolid=256", "subvol=/@"},
		},
		{
			Mountpoint: "/data", Device: "/dev/sdb1", UUID: dataUUID, Subvol: "", SubvolID: 5,
			Options: []string{"rw", "noatime", "subvolid=5", "subvol=/"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseFindmnt =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseFindmntInvalid(t *testing.T) {
	if _, err := parseFindmnt([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func stubMounts(t *testing.T, mounts []Mount) {
	t.Helper()
	old := listMounts
	listMounts = func(context.Context) ([]Mount, error) { return mounts, nil }
	t.Cleanup(func() { listMounts = old })
}

// stubExec records invocations and replaces the command with a no-op.
func stubExec(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { execCommand = old })
	return &calls
}

func TestTopLevelReusesExistingMount(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	calls := stubExec(t)

	dir, err := TopLevel(context.Background(), dataUUID)
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	if dir != "/data" {
		t.Fatalf("dir = %q, want /data", dir)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected commands: %v", *calls)
	}
}

func TestTopLevelMountsWhenMissing(t *testing.T) {
	stubMounts(t, nil)
	calls := stubExec(t)
	oldDir := TopLevelDir
	TopLevelDir = t.TempDir()
	t.Cleanup(func() { TopLevelDir = oldDir })

	dir, err := TopLevel(context.Background(), rootUUID)
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	top := filepath.Join(TopLevelDir, rootUUID)
	if dir != top {
		t.Fatalf("dir = %q, want %q", dir, top)
	}
	if info, err := os.Stat(top); err != nil || !info.IsDir() {
		t.Fatalf("top-level dir not created: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v, want one mount", *calls)
	}
	call := (*calls)[0]
	if got := strings.Join(call[1:], " "); got != "-t btrfs -o subvolid=5,noatime UUID="+rootUUID+" "+top {
		t.Fatalf("mount args = %v", call)
	}
}

func TestParseFindmntFlattensTree(t *testing.T) {
	data, err := os.ReadFile("testdata/findmnt_tree.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseFindmnt(data)
	if err != nil {
		t.Fatalf("parseFindmnt: %v", err)
	}
	if len(got) != 2 || got[1].Mountpoint != "/home" || got[1].Subvol != "@home" || got[1].SubvolID != 257 {
		t.Fatalf("nested mount missing: %+v", got)
	}
}

// stubFindmnt replaces findmnt with a shell script and records its args.
func stubFindmnt(t *testing.T, script string) *[]string {
	t.Helper()
	if _, err := findCommand("findmnt"); err != nil {
		t.Skip("findmnt not installed")
	}
	var args []string
	old := execCommand
	execCommand = func(ctx context.Context, _ string, a ...string) *exec.Cmd {
		args = a
		return exec.CommandContext(ctx, "sh", "-c", script)
	}
	t.Cleanup(func() { execCommand = old })
	return &args
}

func TestListMountsUsesFlatOutput(t *testing.T) {
	args := stubFindmnt(t, `echo '{"filesystems":[]}'`)
	if _, err := ListMounts(context.Background()); err != nil {
		t.Fatalf("ListMounts: %v", err)
	}
	if !slices.Contains(*args, "--list") {
		t.Fatalf("findmnt args %v lack --list", *args)
	}
}

func TestListMountsNoMatchIsEmpty(t *testing.T) {
	stubFindmnt(t, "exit 1")
	mounts, err := ListMounts(context.Background())
	if err != nil || len(mounts) != 0 {
		t.Fatalf("ListMounts = %v, %v; want empty, nil", mounts, err)
	}
}

func TestListMountsKeepsRealErrors(t *testing.T) {
	stubFindmnt(t, "echo 'findmnt: bad option' >&2; exit 1")
	if _, err := ListMounts(context.Background()); err == nil {
		t.Fatal("expected error when findmnt writes to stderr")
	}
}

func TestTopLevelRejectsBadUUID(t *testing.T) {
	stubMounts(t, nil)
	calls := stubExec(t)
	for _, uuid := range []string{"", "../..", "a/b", "/abs"} {
		if _, err := TopLevel(context.Background(), uuid); err == nil {
			t.Errorf("TopLevel(%q) succeeded, want error", uuid)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected commands: %v", *calls)
	}
}
