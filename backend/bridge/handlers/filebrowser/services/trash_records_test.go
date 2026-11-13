package services

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// TestMain keeps every test of the package out of the real record folder.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "linuxio-trash-records-")
	if err != nil {
		panic(err)
	}
	trashRecordsDir = filepath.Join(dir, "trash")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// useTestTrashRecords gives the test a record folder of its own and returns
// the session user's folder in it.
func useTestTrashRecords(t *testing.T, uid int) string {
	t.Helper()
	saved := trashRecordsDir
	trashRecordsDir = filepath.Join(t.TempDir(), "trash")
	t.Cleanup(func() { trashRecordsDir = saved })
	return filepath.Join(trashRecordsDir, strconv.Itoa(uid))
}

// otherSession is a session user who owns none of the test's folders, so
// that every folder the test makes is a system folder to them, as /etc is to
// a user on a root bridge.
func otherSession() (Owner, int, int) {
	uid, gid := os.Getuid()+1, os.Getgid()+1
	return Owner{UID: uint32(uid), GID: uint32(gid), Set: true}, uid, gid
}

// What a root bridge trashed for the session user goes back where its record
// says, whatever the user wrote in the .trashinfo or however they renamed the
// item in their trash, and the record goes with the restore.
func TestTrashRecordSendsARestoreWhereTheItemWasTrashedFrom(t *testing.T) {
	_, uid, gid := otherSession()
	records := useTestTrashRecords(t, uid)
	home, system, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	conf := createTestFile(t, system, "app.conf", []byte("conf"))
	require.NoError(t, os.Chmod(conf, 0o644))
	trash := filepath.Join(home, ".local/share/Trash")

	trashed, err := MoveToTrash(t.Context(), conf, home, uid, gid, true)
	require.NoError(t, err)
	require.Len(t, dirNames(t, records), 1)

	// The user's own edits: another destination and another name.
	require.NoError(t, os.Rename(trashed, filepath.Join(trash, "files", "renamed.conf")))
	require.NoError(t, os.Remove(filepath.Join(trash, "info", "app.conf.trashinfo")))
	writeTestTrashInfo(t, trash, "renamed.conf", filepath.Join(elsewhere, "app.conf"))

	restored, err := RestoreFromTrash(t.Context(), filepath.Join(trash, "files", "renamed.conf"), home, uid, gid, true, nil)
	require.NoError(t, err)
	assert.Equal(t, conf, restored)
	assert.Equal(t, "conf", readString(t, conf))
	requireEmptyDir(t, elsewhere)
	requireEmptyDir(t, records)
}

// An item without a record, trashed by another program, goes into a folder
// the session user does not own only when the request confirms that
// destination; Path= alone does not send it there.
func TestTrashRestoreWithoutARecordIntoASystemFolderNeedsConfirmation(t *testing.T) {
	session, uid, gid := otherSession()
	useTestTrashRecords(t, uid)
	home, system := t.TempDir(), t.TempDir()
	trash := filepath.Join(home, ".local/share/Trash")
	require.NoError(t, os.MkdirAll(filepath.Join(trash, "info"), 0o700))
	item := createTestFile(t, createTestDir(t, trash, "files"), "app.conf", []byte("conf"))
	require.NoError(t, os.Chmod(item, 0o644))
	dest := filepath.Join(system, "app.conf")
	writeTestTrashInfo(t, trash, "app.conf", dest)
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()

	assert.True(t, restoreNeedsConfirmation(root, dest, session))
	assert.False(t, restoreNeedsConfirmation(root, filepath.Join(home, "app.conf"), Owner{UID: uint32(os.Getuid()), Set: true}))

	for _, confirmed := range [][]string{nil, {filepath.Join(system, "other.conf")}} {
		_, err = RestoreFromTrash(t.Context(), item, home, uid, gid, true, confirmed)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden)
		require.ErrorContains(t, err, "confirm restoring it to "+dest)
		require.FileExists(t, item)
		requireEmptyDir(t, system)
	}

	restored, err := RestoreFromTrash(t.Context(), item, home, uid, gid, true, []string{dest})
	require.NoError(t, err)
	assert.Equal(t, dest, restored)
	assert.Equal(t, "conf", readString(t, dest))
}

// A record goes when its item is purged, and a purge drops the records of
// items no longer in the trash, such as ones another program emptied, while
// the record of an item still there stays.
func TestTrashRecordsGoWithPurgeAndEmpty(t *testing.T) {
	_, uid, gid := otherSession()
	records := useTestTrashRecords(t, uid)
	home, system := t.TempDir(), t.TempDir()
	trashItem := func(name string) string {
		t.Helper()
		trashed, err := MoveToTrash(t.Context(), createTestFile(t, system, name, []byte(name)), home, uid, gid, true)
		require.NoError(t, err)
		return trashed
	}

	purged := trashItem("purged.conf")
	require.Len(t, dirNames(t, records), 1)
	_, err := PurgeFromTrash(t.Context(), purged, home, uid, nil)
	require.NoError(t, err)
	requireEmptyDir(t, records)

	emptied := trashItem("emptied.conf")
	kept := trashItem("kept.conf")
	require.Len(t, dirNames(t, records), 2)
	require.NoError(t, os.Remove(emptied))
	PruneTrashRecords(t.Context(), home, uid)
	require.Len(t, dirNames(t, records), 1)

	restored, err := RestoreFromTrash(t.Context(), kept, home, uid, gid, true, nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(system, "kept.conf"), restored)
	requireEmptyDir(t, records)
}

// A bridge that is not root restores as before: no record is written or
// read and Path= is followed, as the user's own programs would.
func TestUnprivilegedTrashKeepsNoRecords(t *testing.T) {
	records := useTestTrashRecords(t, os.Getuid())
	home, system := t.TempDir(), t.TempDir()
	conf := createTestFile(t, system, "app.conf", []byte("conf"))

	trashed, err := MoveToTrash(t.Context(), conf, home, os.Getuid(), os.Getgid(), false)
	require.NoError(t, err)
	assert.NoDirExists(t, records)
	restored, err := RestoreFromTrash(t.Context(), trashed, home, os.Getuid(), os.Getgid(), false, nil)
	require.NoError(t, err)
	assert.Equal(t, conf, restored)
}

// On a root bridge the Trash view shows where the record says an item goes,
// and marks an item without a record whose restore goes into a system folder
// as one to confirm.
func TestListTrashShowsTheRecordAsRoot(t *testing.T) {
	home := rootSessionHome(t)
	useTestTrashRecords(t, rootTestUID)
	system := createTestDir(t, filepath.Dir(home), "system")
	conf := createTestFile(t, system, "app.conf", []byte("conf"))
	trash := filepath.Join(home, ".local/share/Trash")

	_, err := MoveToTrash(t.Context(), conf, home, rootTestUID, rootTestGID, true)
	require.NoError(t, err)
	writeTestTrashInfo(t, trash, "app.conf", filepath.Join(system, "elsewhere.conf"))
	foreign := createTestFile(t, filepath.Join(trash, "files"), "foreign.conf", []byte("foreign"))
	writeTestTrashInfo(t, trash, "foreign.conf", filepath.Join(system, "foreign.conf"))

	items, err := ListTrash(t.Context(), home, rootTestUID)
	require.NoError(t, err)
	byName := map[string]TrashItem{}
	for _, item := range items {
		byName[item.Name] = item
	}
	assert.Equal(t, conf, byName["app.conf"].OriginalPath)
	assert.False(t, byName["app.conf"].ConfirmRestore)
	assert.Equal(t, filepath.Join(system, "foreign.conf"), byName["foreign.conf"].OriginalPath)
	assert.True(t, byName["foreign.conf"].ConfirmRestore)
	assert.FileExists(t, foreign)
}
