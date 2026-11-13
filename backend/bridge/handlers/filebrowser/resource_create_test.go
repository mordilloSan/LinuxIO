package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func createRequest(path string, isDir bool) resourcePostRequest {
	return resourcePostRequest{cleanPath: path, relPath: strings.TrimPrefix(path, "/"), isDir: isDir}
}

// On a root bridge a create makes its file or folder, and any missing folder
// above it, without following a link in a folder the session user can write:
// a process of theirs could have swapped a folder on the path for one. A
// bridge that is not root acts as the user and follows their link.
func TestCreateNeverGoesThroughALinkTheUserCouldPlant(t *testing.T) {
	recordIndexerRequests(t)
	home, victim := t.TempDir(), t.TempDir()
	plant(t, victim, filepath.Join(home, "docs"))
	root := openTestRoot(t)
	session := selfOwner().asOwner()

	for _, path := range []string{filepath.Join(home, "docs", "new.txt"), filepath.Join(home, "docs", "missing", "new.txt")} {
		_, err := createFileResource(t.Context(), root, createRequest(path, false), session)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, path)
	}
	for _, path := range []string{filepath.Join(home, "docs", "made"), filepath.Join(home, "docs", "missing", "made")} {
		_, err := createDirectoryResource(t.Context(), root, createRequest(path, true), session)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, path)
	}
	require.Empty(t, dirEntries(t, victim))

	_, err := createFileResource(t.Context(), root, createRequest(filepath.Join(home, "docs", "new.txt"), false), services.Owner{})
	require.NoError(t, err)
	entries := dirEntries(t, victim)
	require.Len(t, entries, 1)
	require.Equal(t, "new.txt", entries[0].Name())
}

// A create never touches what is already at its name: a file is refused, and
// so is a link there, whose target would otherwise be truncated.
func TestCreateFileRefusesAnExistingNameWithoutTruncating(t *testing.T) {
	home, victim := t.TempDir(), t.TempDir()
	root := openTestRoot(t)
	kept := filepath.Join(victim, "kept.txt")
	require.NoError(t, os.WriteFile(kept, []byte("data"), 0o600))
	alias := filepath.Join(home, "alias.txt")
	require.NoError(t, os.Symlink(kept, alias))
	plain := filepath.Join(home, "plain.txt")
	require.NoError(t, os.WriteFile(plain, []byte("data"), 0o600))

	for _, owner := range []services.Owner{{}, selfOwner().asOwner()} {
		for _, path := range []string{alias, plain} {
			_, err := createFileResource(t.Context(), root, createRequest(path, false), owner)
			require.ErrorIs(t, err, bridgeipc.ErrConflict, path)
		}
	}
	for _, path := range []string{kept, plain} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "data", string(data), path)
	}
}

// A created folder gets its mode whatever the umask, and the folders made
// above it on the way are left as the umask made them.
func TestCreateDirectoryResourceSetsItsMode(t *testing.T) {
	recordIndexerRequests(t)
	home := t.TempDir()
	root := openTestRoot(t)
	for _, owner := range []services.Owner{{}, selfOwner().asOwner()} {
		path := filepath.Join(home, "a", "b")
		_, err := createDirectoryResource(t.Context(), root, createRequest(path, true), owner)
		require.NoError(t, err)
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, services.PermDir, info.Mode().Perm())
		require.NoError(t, os.RemoveAll(filepath.Join(home, "a")))
	}
}

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

	_, err = createDirectoryResource(t.Context(), root, resourcePostRequest{cleanPath: existing, relPath: "private", isDir: true}, services.Owner{})
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
