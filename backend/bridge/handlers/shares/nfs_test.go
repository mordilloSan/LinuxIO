package shares

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// useTempExports points exportsFile at a fresh temporary file, stubs the
// availability gate and exportfs reload, and returns the file path.
func useTempExports(t *testing.T, initial string) string {
	t.Helper()
	origFile, origAvail, origApply := exportsFile, nfsServerAvailable, applyNFSExports
	t.Cleanup(func() {
		exportsFile, nfsServerAvailable, applyNFSExports = origFile, origAvail, origApply
	})

	exportsFile = filepath.Join(t.TempDir(), "exports")
	nfsServerAvailable = func() (bool, error) { return true, nil }
	applyNFSExports = func(context.Context) error { return nil }
	if initial != "" {
		require.NoError(t, os.WriteFile(exportsFile, []byte(initial), 0o644))
	}
	return exportsFile
}

var localClient = []apischema.NFSClient{{Host: "10.0.0.0/24", Options: []string{"rw", "sync", "anonuid=1000"}}}

func TestCreateNFSShareDirectoryIsWorldTraversableDespiteUmask(t *testing.T) {
	useTempExports(t, "")
	oldUmask := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	dir := filepath.Join(t.TempDir(), "share")
	require.NoError(t, CreateNFSShare(context.Background(), dir, localClient))

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestMkdirShareDirChmodsEveryCreatedLevel(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o700))
	parent := filepath.Join(root, "shares")
	dir := filepath.Join(parent, "public")
	require.NoError(t, mkdirShareDir(dir))

	for _, d := range []string{parent, dir} {
		info, err := os.Stat(d)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), d)
	}
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "existing parent keeps its mode")
}

func TestCreateNFSShareAppendsOnFreshLine(t *testing.T) {
	file := useTempExports(t, "/srv/a\t*(ro)")

	require.NoError(t, CreateNFSShare(context.Background(), filepath.Join(t.TempDir(), "b"), localClient))

	data, err := os.ReadFile(file)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "/srv/a\t*(ro)", lines[0])
}

func TestNFSShareRewriteKeepsOtherLines(t *testing.T) {
	file := useTempExports(t, "# keep\n/srv/a\t*(ro)\n/srv/b\t*(ro)\n")

	require.NoError(t, UpdateNFSShare(context.Background(), "/srv/a", localClient))
	require.NoError(t, DeleteNFSShare(context.Background(), "/srv/b"))

	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "# keep\n/srv/a\t10.0.0.0/24(rw,sync,anonuid=1000)\n", string(data))
}

func TestNFSShareRejectsUnsafeInput(t *testing.T) {
	file := useTempExports(t, "/srv/x\t*(ro)\n")
	root := t.TempDir()

	tests := []struct {
		name    string
		path    string
		clients []apischema.NFSClient
	}{
		{"no clients", root + "/a", nil},
		{"host with space", root + "/a", []apischema.NFSClient{{Host: "a (rw) /etc *"}}},
		{"host with newline", root + "/a", []apischema.NFSClient{{Host: "a\n/etc"}}},
		{"option with newline", root + "/a", []apischema.NFSClient{{Host: "a", Options: []string{"rw\n/etc *"}}}},
		{"root path", "/", localClient},
		{"root via dotdot", "/a/..", localClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, CreateNFSShare(context.Background(), tt.path, tt.clients))
			require.Error(t, UpdateNFSShare(context.Background(), tt.path, tt.clients), "update")
		})
	}

	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "/srv/x\t*(ro)\n", string(data))
}

func TestCreateNFSShareDuplicateCheckUsesCleanedPath(t *testing.T) {
	file := useTempExports(t, "/srv/x/\t*(ro)\n")

	for _, path := range []string{"/srv/x", "/srv/x/", "/srv//x"} {
		require.Error(t, CreateNFSShare(context.Background(), path, localClient), path)
	}

	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "/srv/x/\t*(ro)\n", string(data))
}
