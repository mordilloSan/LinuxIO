package filebrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestResourceStatReturnsStructuredClientErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
		code int
	}{
		{name: "missing request path", code: 400},
		{name: "path not found", path: filepath.Join(t.TempDir(), "missing"), code: 404},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resourceStat(context.Background(), apischema.PathRequest{Path: tc.path})
			var apiErr *bridgeipc.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("resourceStat() error = %v, want *bridgeipc.Error", err)
			}
			if apiErr.Code != tc.code {
				t.Fatalf("resourceStat() code = %d, want %d", apiErr.Code, tc.code)
			}
		})
	}
}

func TestExistsBatchReportsExistingPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	resp, err := existsBatch(context.Background(), apischema.BatchPathRequest{
		Paths: []string{file, sub, filepath.Join(dir, "missing")},
	})
	if err != nil {
		t.Fatalf("existsBatch: %v", err)
	}
	if len(resp.Existing) != 2 {
		t.Fatalf("existing = %+v, want 2 entries", resp.Existing)
	}
	if resp.Existing[0].Path != file || resp.Existing[0].IsDir {
		t.Fatalf("existing[0] = %+v", resp.Existing[0])
	}
	if resp.Existing[1].Path != sub || !resp.Existing[1].IsDir {
		t.Fatalf("existing[1] = %+v", resp.Existing[1])
	}
}

func TestGenerateUniquePathSkipsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(source, []byte("report"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Symlink("missing.txt", filepath.Join(dir, "report (copy).txt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()

	got := generateUniquePath(source, false, root)
	want := filepath.Join(dir, "report (copy 2).txt")
	if got != want {
		t.Fatalf("generateUniquePath = %q, want %q", got, want)
	}
}

func TestExtendedFileInfoResponseMapsInternalFileInfo(t *testing.T) {
	modified := time.Date(2026, 6, 21, 22, 15, 30, 123, time.UTC)
	childModified := modified.Add(time.Minute)

	got := extendedFileInfoResponse(&iteminfo.ExtendedFileInfo{
		Name:       "media",
		Size:       4096,
		ModTime:    modified,
		Type:       "directory",
		Hidden:     true,
		HasPreview: true,
		Symlink:    true,
		Files: []iteminfo.ItemInfo{{
			Name:    "haos.iso",
			Size:    1024,
			ModTime: childModified,
			Type:    "application/x-iso9660-image",
		}},
		Folders: []iteminfo.ItemInfo{{
			Name:    "nested",
			ModTime: childModified,
			Type:    "directory",
		}},
		Path:     "/srv/media",
		Content:  "hello",
		RealPath: "/mnt/storage/media",
	})

	if got.Name != "media" || got.Path != "/srv/media" || got.Content != "hello" {
		t.Fatalf("mapped top-level fields incorrectly: %+v", got)
	}
	if got.Modified != modified.Format(time.RFC3339Nano) {
		t.Fatalf("modified = %q, want %q", got.Modified, modified.Format(time.RFC3339Nano))
	}
	if len(got.Files) != 1 || got.Files[0].Name != "haos.iso" || got.Files[0].Modified != childModified.Format(time.RFC3339Nano) {
		t.Fatalf("mapped files incorrectly: %+v", got.Files)
	}
	if len(got.Folders) != 1 || got.Folders[0].Name != "nested" || got.Folders[0].Type != "directory" {
		t.Fatalf("mapped folders incorrectly: %+v", got.Folders)
	}
}

func TestExtendedFileInfoResponseUsesEmptyChildSlices(t *testing.T) {
	got := extendedFileInfoResponse(&iteminfo.ExtendedFileInfo{})
	if got.Files == nil {
		t.Fatal("Files is nil, want empty slice")
	}
	if got.Folders == nil {
		t.Fatal("Folders is nil, want empty slice")
	}
}
