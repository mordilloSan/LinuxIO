package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

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

func TestParseResourcePatchRequestRefusesInvalidNames(t *testing.T) {
	for _, dst := range []string{"/x/..", "/x/./", "/x/a\x00b"} {
		_, err := parseResourcePatchRequest(apischema.ActionSourceDestinationRequest{Action: "rename", Source: "/x/a", Dest: dst})
		if err == nil || !strings.Contains(err.Error(), "invalid name") {
			t.Fatalf("%q: err = %v, want invalid name", dst, err)
		}
	}
}

func TestPrepareResourcePatchRefusesSystemFolders(t *testing.T) {
	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()

	_, err = prepareResourcePatch(root, resourcePatchRequest{action: "rename", src: "/etc", dst: "/etc-renamed"})
	if err == nil || !strings.Contains(err.Error(), "system folder") {
		t.Fatalf("renaming /etc: err = %v, want system folder", err)
	}
}
