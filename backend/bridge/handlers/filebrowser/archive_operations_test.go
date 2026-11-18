package filebrowser

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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
	require.NoError(t, createArchive(t.Context(), "zip", target.temp, target.tempPath, nil, 1, services.ArchiveSources{}, []string{src}))
	size, err := target.finish()
	require.NoError(t, err)
	require.Positive(t, size)
	entries := dirEntries(t, victim)
	require.Len(t, entries, 1)
	require.Equal(t, "out.zip", entries[0].Name())
}

// On a root bridge the extract route does not read its archive through a
// link the session user planted: a link in their folder to an archive only
// root can read would hand them its content. Nothing is made.
func TestExtractRouteNeverReadsAnArchiveThroughAPlantedLinkAsRoot(t *testing.T) {
	base, home := trashTestHome(t)
	vault := filepath.Join(base, "vault")
	require.NoError(t, os.Mkdir(vault, 0o700))
	archive := filepath.Join(vault, "backup.zip")
	out, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)
	zipWriter := zip.NewWriter(out)
	entry, err := zipWriter.Create("payload.txt")
	require.NoError(t, err)
	_, err = entry.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())
	require.NoError(t, out.Close())
	// A relative target: one that starts at / is refused for every session.
	link := filepath.Join(home, "backup.zip")
	require.NoError(t, os.Symlink("../vault/backup.zip", link))
	require.NoError(t, os.Lchown(link, trashTestUID, trashTestGID))

	req := apischema.FileExtractRequest{ArchivePath: link}
	session := sessionOwner{uid: trashTestUID, gid: trashTestGID, set: true}
	_, err = runExtractTask(t.Context(), newBatchTask(t, "filebrowser.extract", req), nil, req, session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.Len(t, dirEntries(t, home), 1, "nothing is made beside the link")
}

// A folder, a FIFO or a device under an archive's name is refused as a bad
// request before anything is made.
func TestExtractRouteRefusesWhatIsNotAFile(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, "folder.zip"), 0o700))
	require.NoError(t, unix.Mkfifo(filepath.Join(home, "pipe.tar.gz"), 0o600))

	for _, name := range []string{"folder.zip", "pipe.tar.gz"} {
		req := apischema.FileExtractRequest{ArchivePath: filepath.Join(home, name)}
		_, err := runExtractTask(t.Context(), newBatchTask(t, "filebrowser.extract", req), nil, req, sessionOwner{})
		require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, name)
	}
	require.Len(t, dirEntries(t, home), 2, "nothing is made beside them")
}
