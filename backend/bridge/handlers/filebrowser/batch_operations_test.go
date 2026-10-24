package filebrowser

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func seedFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	return path
}

func readString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func newBatchTask(t *testing.T, route string, req any) *bridgeipc.Task {
	t.Helper()
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(route, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func TestPlanBatchTransferIntoOwnFolder(t *testing.T) {
	dir := t.TempDir()
	file := seedFile(t, filepath.Join(dir, "report.txt"), "report")
	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()
	req := apischema.BatchTransferRequest{Sources: []string{file}, Destination: dir}

	items, _, failures := planBatchTransfer(context.Background(), root, dir, req, "copy")
	if len(failures) != 0 || len(items) != 1 {
		t.Fatalf("copy: items=%d failures=%v", len(items), failures)
	}
	if want := filepath.Join(dir, "report (copy).txt"); items[0].dest != want || items[0].replaced {
		t.Fatalf("copy into own folder: dest=%q replaced=%v, want %q", items[0].dest, items[0].replaced, want)
	}

	items, _, failures = planBatchTransfer(context.Background(), root, dir, req, "move")
	if len(items) != 0 || len(failures) != 1 || failures[0].Error != "already in the destination folder" {
		t.Fatalf("move into own folder: items=%d failures=%v", len(items), failures)
	}
}

func TestPlanBatchTransferSkipsSizeForSameFilesystemMove(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	seedFile(t, filepath.Join(src, "a.txt"), "content")
	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()
	req := apischema.BatchTransferRequest{Sources: []string{src}}

	items, _, failures := planBatchTransfer(context.Background(), root, dest, req, "move")
	if len(failures) != 0 || len(items) != 1 || items[0].size.known {
		t.Fatalf("move plan computed a size: items=%+v failures=%v", items, failures)
	}

	items, _, failures = planBatchTransfer(context.Background(), root, dest, req, "copy")
	if len(failures) != 0 || len(items) != 1 || !items[0].size.known || items[0].size.total != int64(len("content")) {
		t.Fatalf("copy plan size: items=%+v failures=%v", items, failures)
	}
}

func TestPlanBatchTransferKeepBothLandsUnderUniqueName(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	file := seedFile(t, filepath.Join(src, "a.txt"), "new")
	seedFile(t, filepath.Join(dest, "a.txt"), "old")
	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()

	req := apischema.BatchTransferRequest{Sources: []string{file}, Destination: dest, KeepBoth: []string{file}}
	items, _, failures := planBatchTransfer(context.Background(), root, dest, req, "copy")
	if len(failures) != 0 || len(items) != 1 {
		t.Fatalf("items=%d failures=%v", len(items), failures)
	}
	if want := filepath.Join(dest, "a (copy).txt"); items[0].dest != want || items[0].replaced {
		t.Fatalf("keep both: dest=%q replaced=%v, want %q", items[0].dest, items[0].replaced, want)
	}
}

func TestPlanDeleteBatchRefusesProtectedPaths(t *testing.T) {
	_, _, failures := planDeleteBatch(context.Background(), []string{"/", "/etc"})
	if len(failures) != 2 {
		t.Fatalf("failures = %+v, want 2", failures)
	}
	if !strings.Contains(failures[0].Error, "root") || !strings.Contains(failures[1].Error, "system folder") {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestRunCopyBatchTaskReportsPerItemFailures(t *testing.T) {
	recordIndexerRequests(t)
	src, dest := t.TempDir(), t.TempDir()
	file := seedFile(t, filepath.Join(src, "a.txt"), "a")
	req := apischema.BatchTransferRequest{Sources: []string{file, filepath.Join(src, "missing.txt")}, Destination: dest}

	result, err := runCopyBatchTask(context.Background(), newBatchTask(t, "filebrowser.copy_batch", req), nil, req)
	detachedIndexerUpdates.Wait()
	if err != nil {
		t.Fatalf("runCopyBatchTask: %v", err)
	}
	if result.Total != 2 || result.Succeeded != 1 || len(result.Failed) != 1 || result.Failed[0].Error != "source not found" {
		t.Fatalf("result = %+v", result)
	}
	if got := readString(t, filepath.Join(dest, "a.txt")); got != "a" {
		t.Fatalf("copied content = %q", got)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("source should survive a copy: %v", err)
	}
}

func TestRunMoveBatchTaskKeepsBoth(t *testing.T) {
	recordIndexerRequests(t)
	src, dest := t.TempDir(), t.TempDir()
	file := seedFile(t, filepath.Join(src, "a.txt"), "new")
	seedFile(t, filepath.Join(dest, "a.txt"), "old")
	req := apischema.BatchTransferRequest{Sources: []string{file}, Destination: dest, KeepBoth: []string{file}}

	result, err := runMoveBatchTask(context.Background(), newBatchTask(t, "filebrowser.move_batch", req), nil, req)
	detachedIndexerUpdates.Wait()
	if err != nil || result.Succeeded != 1 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v, err %v", result, err)
	}
	if readString(t, filepath.Join(dest, "a.txt")) != "old" || readString(t, filepath.Join(dest, "a (copy).txt")) != "new" {
		t.Fatalf("keep both should leave the old file and add a copy")
	}
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("source should be gone after a move, got %v", err)
	}
}

func TestRunDeleteBatchTaskDeletesAndReportsFailures(t *testing.T) {
	recordIndexerRequests(t)
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(tree, "sub"), 0o755); err != nil {
		t.Fatalf("seed tree: %v", err)
	}
	seedFile(t, filepath.Join(tree, "sub", "f.txt"), "f")
	req := apischema.BatchPathRequest{Paths: []string{tree, filepath.Join(dir, "missing"), "/etc"}}

	result, err := runDeleteBatchTask(context.Background(), newBatchTask(t, "filebrowser.delete_batch", req), nil, req)
	detachedIndexerUpdates.Wait()
	if err != nil {
		t.Fatalf("runDeleteBatchTask: %v", err)
	}
	if result.Total != 3 || result.Succeeded != 1 || len(result.Failed) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Lstat(tree); !os.IsNotExist(err) {
		t.Fatalf("tree should be deleted, got %v", err)
	}
	if _, err := os.Stat("/etc"); err != nil {
		t.Fatalf("/etc must be untouched: %v", err)
	}
}

func TestParseChmodBatchRequest(t *testing.T) {
	tests := []struct {
		name          string
		req           apischema.FileChmodBatchRequest
		wantPaths     []string
		wantMode      os.FileMode
		wantOwner     string
		wantGroup     string
		wantRecursive bool
		wantErr       bool
	}{
		{
			name:    "missing mode",
			req:     apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}},
			wantErr: true,
		},
		{
			name:    "missing paths",
			req:     apischema.FileChmodBatchRequest{Mode: "0644"},
			wantErr: true,
		},
		{
			name:    "invalid mode",
			req:     apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "rw-r--r--"},
			wantErr: true,
		},
		{
			name:    "negative mode",
			req:     apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "-1"},
			wantErr: true,
		},
		{
			name:    "mode above 7777",
			req:     apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "10000"},
			wantErr: true,
		},
		{
			name:      "special bits map to FileMode flags",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "7755"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  os.ModeSetuid | os.ModeSetgid | os.ModeSticky | 0o755,
		},
		{
			name:      "paths and mode only",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "0644"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  0o644,
		},
		{
			name:      "owner only",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "0644", Owner: "miguel"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  0o644,
			wantOwner: "miguel",
		},
		{
			name:      "owner and group",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "0644", Owner: "miguel", Group: "staff"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  0o644,
			wantOwner: "miguel",
			wantGroup: "staff",
		},
		{
			name:          "many paths owner group recursive",
			req:           apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file", "/tmp/dir"}, Mode: "0644", Owner: "miguel", Group: "staff", Recursive: new(true)},
			wantPaths:     []string{"/tmp/file", "/tmp/dir"},
			wantMode:      0o644,
			wantOwner:     "miguel",
			wantGroup:     "staff",
			wantRecursive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, mode, owner, group, recursive, err := parseChmodBatchRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseChmodBatchRequest returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChmodBatchRequest returned error: %v", err)
			}
			if !slices.Equal(paths, tt.wantPaths) || mode != tt.wantMode || owner != tt.wantOwner || group != tt.wantGroup || recursive != tt.wantRecursive {
				t.Fatalf("parseChmodBatchRequest() = (%q, %v, %q, %q, %v), want (%q, %v, %q, %q, %v)", paths, mode, owner, group, recursive, tt.wantPaths, tt.wantMode, tt.wantOwner, tt.wantGroup, tt.wantRecursive)
			}
		})
	}
}
