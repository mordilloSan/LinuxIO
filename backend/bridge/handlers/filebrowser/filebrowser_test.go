package filebrowser

import (
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

func TestExtendedFileInfoResponseMapsInternalFileInfo(t *testing.T) {
	modified := time.Date(2026, 6, 21, 22, 15, 30, 123, time.UTC)
	childModified := modified.Add(time.Minute)

	got := extendedFileInfoResponse(&iteminfo.ExtendedFileInfo{
		FileInfo: iteminfo.FileInfo{
			ItemInfo: iteminfo.ItemInfo{
				Name:       "media",
				Size:       4096,
				ModTime:    modified,
				Type:       "directory",
				Hidden:     true,
				HasPreview: true,
				Symlink:    true,
			},
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
			Path: "/srv/media",
		},
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
