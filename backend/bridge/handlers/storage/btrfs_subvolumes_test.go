package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func TestPathStatus(t *testing.T) {
	link := fmt.Errorf("create subvolume %q: %w", "share/inbox/x", fmt.Errorf("%w: share/inbox", btrfs.ErrLinkOnPath))
	if err := pathStatus(link); bridgeipc.StatusCode(err) != 403 || !errors.Is(err, btrfs.ErrLinkOnPath) {
		t.Errorf("a path through a link: %v, status %d, want 403", err, bridgeipc.StatusCode(err))
	}
	other := fmt.Errorf("%w: home/alice", btrfs.ErrNotOnFilesystem)
	if err := pathStatus(other); bridgeipc.StatusCode(err) != 400 {
		t.Errorf("a path onto another filesystem: %v, status %d, want 400", err, bridgeipc.StatusCode(err))
	}
	plain := errors.New("no space left on device")
	if err := pathStatus(plain); err != plain {
		t.Errorf("another error was changed: %v", err)
	}
}

func TestCreateBtrfsSubvolumeInRefuses(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		req  apischema.BtrfsSubvolumeCreateInRequest
		want error
	}{
		{"relative folder", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: "mnt/data", Name: "x"}, bridgeipc.ErrInvalidArgs},
		{"empty name", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: "/mnt", Name: ""}, bridgeipc.ErrInvalidArgs},
		{"name with a slash", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: "/mnt", Name: "a/b"}, bridgeipc.ErrInvalidArgs},
		{"name too long", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: "/mnt", Name: strings.Repeat("a", 256)}, bridgeipc.ErrInvalidArgs},
		{"folder is a file", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: file, Name: "x"}, bridgeipc.ErrInvalidArgs},
		{"missing folder", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: missing, Name: "x"}, bridgeipc.ErrNotFound},
		{"folder not on btrfs", apischema.BtrfsSubvolumeCreateInRequest{UUID: "u", Folder: "/proc", Name: "x"}, btrfs.ErrNotOnFilesystem},
	} {
		_, err := CreateBtrfsSubvolumeIn(context.Background(), tc.req, linksafe.Owner{})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if tc.want == btrfs.ErrNotOnFilesystem && !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("%s: err = %v, want it to be a client error", tc.name, err)
		}
	}
}
