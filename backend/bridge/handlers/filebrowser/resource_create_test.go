package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

func TestCreateDirectoryResourceRefusesExistingFolder(t *testing.T) {
	dir := t.TempDir()
	root, err := fsroot.OpenAt(dir)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()
	existing := filepath.Join(dir, "private")
	if mkErr := os.Mkdir(existing, 0o700); mkErr != nil {
		t.Fatalf("seed folder: %v", mkErr)
	}

	_, err = createDirectoryResource(t.Context(), root, resourcePostRequest{cleanPath: existing, relPath: "private", isDir: true})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("createDirectoryResource on an existing folder: err=%v, want already exists", err)
	}
	info, err := os.Stat(existing)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("existing folder mode changed to %o", got)
	}
}

func TestParseResourcePostRequestRefusesInvalidNames(t *testing.T) {
	for _, path := range []string{"/x/y/../", "/x/.", "/x/y/..", "/x/a\x00b"} {
		if _, err := parseResourcePostRequest(apischema.FileResourcePostRequest{Path: path}); err == nil || !strings.Contains(err.Error(), "invalid name") {
			t.Fatalf("%q: err = %v, want invalid name", path, err)
		}
	}
	req, err := parseResourcePostRequest(apischema.FileResourcePostRequest{Path: "/x/new folder/"})
	if err != nil || !req.isDir || req.relPath != "x/new folder" {
		t.Fatalf("valid folder: req=%+v err=%v", req, err)
	}
}

func TestParseResourcePostRequestKeepsPlusAndPercent(t *testing.T) {
	for _, name := range []string{"c++.txt", "100%.txt", "a%2Fb.txt"} {
		req, err := parseResourcePostRequest(apischema.FileResourcePostRequest{Path: "/x/" + name})
		if err != nil || req.relPath != "x/"+name {
			t.Fatalf("%q: req=%+v err=%v, want the name kept verbatim", name, req, err)
		}
	}
}
