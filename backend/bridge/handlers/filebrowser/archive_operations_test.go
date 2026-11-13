package filebrowser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// On a root bridge a compress makes its archive, and any missing folder above
// it, without following a link in a folder the session user can write: a
// process of theirs could have swapped a folder on the path for one. A bridge
// that is not root acts as the user and follows their link.
func TestCompressNeverGoesThroughALinkTheUserCouldPlant(t *testing.T) {
	home, victim := t.TempDir(), t.TempDir()
	plant(t, victim, filepath.Join(home, "docs"))
	root := openTestRoot(t)
	session := selfOwner().asOwner()

	for _, target := range []string{filepath.Join(home, "docs", "out.zip"), filepath.Join(home, "docs", "missing", "out.zip")} {
		_, err := prepareArchiveTarget(root, target, session)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, target)
		require.Empty(t, dirEntries(t, victim))
	}

	src := filepath.Join(home, "notes.txt")
	require.NoError(t, os.WriteFile(src, []byte("notes"), 0o600))
	target, err := prepareArchiveTarget(root, filepath.Join(home, "docs", "out.zip"), services.Owner{})
	require.NoError(t, err)
	defer target.close()
	require.NoError(t, createArchive("zip", target.temp, target.tempPath, nil, 1, []string{src}))
	size, err := target.finish()
	require.NoError(t, err)
	require.Positive(t, size)
	entries := dirEntries(t, victim)
	require.Len(t, entries, 1)
	require.Equal(t, "out.zip", entries[0].Name())
}
