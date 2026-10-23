package filebrowser

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

func TestPlanBatchTransferIntoOwnFolder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(file, []byte("report"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()

	items, _, failures := planBatchTransfer(context.Background(), root, dir, []string{file}, false, "copy")
	if len(failures) != 0 || len(items) != 1 {
		t.Fatalf("copy: items=%d failures=%v", len(items), failures)
	}
	if want := filepath.Join(dir, "report (copy).txt"); items[0].dest != want || items[0].replaced {
		t.Fatalf("copy into own folder: dest=%q replaced=%v, want %q", items[0].dest, items[0].replaced, want)
	}

	items, _, failures = planBatchTransfer(context.Background(), root, dir, []string{file}, false, "move")
	if len(items) != 0 || len(failures) != 1 || failures[0].Error != "already in the destination folder" {
		t.Fatalf("move into own folder: items=%d failures=%v", len(items), failures)
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
