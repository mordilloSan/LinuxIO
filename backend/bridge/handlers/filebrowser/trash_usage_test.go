package filebrowser

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
)

// trashUsageHome is a home for the bridge's own user with an indexer that
// answers what the index holds below a folder with subfolders(path), and
// returns what the bridge asked of it.
func trashUsageHome(t *testing.T, subfolders func(path string) (string, error)) (home string, requests func() []string) {
	t.Helper()
	detachedIndexerUpdates.Wait()
	// The indexer knows a folder by its real path.
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)

	var mu sync.Mutex
	var seen []string
	original := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		path := req.URL.Query().Get("path")
		mu.Lock()
		seen = append(seen, req.Method+" "+req.URL.Path+" "+path)
		mu.Unlock()
		body := `{"status":"queued"}`
		if req.URL.Path == "/subfolders" {
			answer, err := subfolders(path)
			if err != nil {
				return nil, err
			}
			body = answer
		}
		return &http.Response{StatusCode: http.StatusOK, Status: http.StatusText(http.StatusOK), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	t.Cleanup(func() {
		detachedIndexerUpdates.Wait()
		indexer.Client = original
	})
	return home, func() []string {
		detachedIndexerUpdates.Wait()
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// trashOne moves a new file of the home into its trash.
func trashOne(t *testing.T, home, name string) {
	t.Helper()
	path := seedFile(t, filepath.Join(home, name), "trashed")
	_, err := services.MoveToTrash(t.Context(), path, home, os.Geteuid(), os.Getegid(), false)
	require.NoError(t, err)
}

func TestTrashUsageIsTheSizeTheIndexHoldsForATrashInUse(t *testing.T) {
	var trash string
	home, requests := trashUsageHome(t, func(path string) (string, error) {
		return `{"subfolders":[{"path":"` + trash + `/files","size":36507222016},{"path":"` + trash + `/info","size":4096}],"pending":true}`, nil
	})
	trash = filepath.Join(home, ".local/share/Trash")
	trashOne(t, home, "go-build")

	usage, err := trashUsage(t.Context(), sessionOwner{sudo: true})
	require.NoError(t, err)
	require.Equal(t, int64(36507222016), usage.Size)
	require.True(t, usage.Pending)
	require.False(t, usage.Partial)
	require.Equal(t, []string{"GET /subfolders " + trash}, requests())
}

// A trash made since the last scan, as by the first move into it, has rows
// for its items and none for its own folders. The indexer is asked to read
// it, as it is for a folder someone opens, and the answer says the size is
// still to come.
func TestTrashUsageAsksTheIndexerToReadATrashItHoldsNoRowFor(t *testing.T) {
	asked := 0
	home, requests := trashUsageHome(t, func(string) (string, error) {
		asked++
		if asked == 1 {
			return `{"subfolders":[]}`, nil
		}
		// Asked again once the read is queued.
		return `{"subfolders":[],"pending":true}`, nil
	})
	trash := filepath.Join(home, ".local/share/Trash")
	trashOne(t, home, "notes.txt")

	usage, err := trashUsage(t.Context(), sessionOwner{sudo: true})
	require.NoError(t, err)
	require.Zero(t, usage.Size)
	require.True(t, usage.Pending)
	require.True(t, usage.Partial)
	require.Equal(t, []string{"GET /subfolders " + trash, "POST /refresh " + trash, "GET /subfolders " + trash}, requests())
}

// A trash the index leaves out stays without a size, and the page is not
// told to ask again.
func TestTrashUsageIsPartialWhenTheIndexLeavesATrashOut(t *testing.T) {
	home, _ := trashUsageHome(t, func(string) (string, error) { return `{"subfolders":[]}`, nil })
	trashOne(t, home, "notes.txt")

	usage, err := trashUsage(t.Context(), sessionOwner{sudo: true})
	require.NoError(t, err)
	require.Zero(t, usage.Size)
	require.False(t, usage.Pending)
	require.True(t, usage.Partial)
}

// The read was quick: the size it found is the answer.
func TestTrashUsageTakesTheSizeTheReadFound(t *testing.T) {
	var trash string
	asked := 0
	home, _ := trashUsageHome(t, func(string) (string, error) {
		asked++
		if asked == 1 {
			return `{"subfolders":[]}`, nil
		}
		return `{"subfolders":[{"path":"` + trash + `/files","size":8392704}]}`, nil
	})
	trash = filepath.Join(home, ".local/share/Trash")
	trashOne(t, home, "notes.txt")

	usage, err := trashUsage(t.Context(), sessionOwner{sudo: true})
	require.NoError(t, err)
	require.Equal(t, int64(8392704), usage.Size)
	require.False(t, usage.Pending)
	require.False(t, usage.Partial)
}

// An emptied trash keeps its folders: nothing is asked about it, so the page
// shows no size for the few blocks an empty folder takes.
func TestTrashUsageOfAnEmptyTrashAsksTheIndexerNothing(t *testing.T) {
	home, requests := trashUsageHome(t, func(string) (string, error) {
		return "", errors.New("the indexer must not be asked")
	})
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".local/share/Trash/files"), 0o700))

	usage, err := trashUsage(t.Context(), sessionOwner{sudo: true})
	require.NoError(t, err)
	require.Zero(t, usage.Size)
	require.False(t, usage.Pending)
	require.False(t, usage.Partial)
	require.Empty(t, requests())
}

func TestTrashUsageFailsWhenTheIndexerCannotBeReached(t *testing.T) {
	home, _ := trashUsageHome(t, func(string) (string, error) { return "", errors.New("connection refused") })
	trashOne(t, home, "notes.txt")

	_, err := trashUsage(t.Context(), sessionOwner{sudo: true})
	require.ErrorIs(t, err, errIndexerUnavailable)
}

// Listing the trash is where someone looks at it: the indexer is asked to
// refresh each trash in use, so an item another program trashed gets a size.
func TestListingTheTrashAsksTheIndexerToRefreshATrashInUse(t *testing.T) {
	home, requests := trashUsageHome(t, func(string) (string, error) { return `{"subfolders":[]}`, nil })
	trashOne(t, home, "notes.txt")

	listed, err := listTrash(t.Context(), sessionOwner{sudo: true})
	require.NoError(t, err)
	require.Len(t, listed.Entries, 1)
	require.Equal(t, []string{"POST /refresh " + filepath.Join(home, ".local/share/Trash/files")}, requests())
}

// A session without sudo cannot reach the index.
func TestListingTheTrashWithoutSudoAsksTheIndexerNothing(t *testing.T) {
	home, requests := trashUsageHome(t, func(string) (string, error) { return `{"subfolders":[]}`, nil })
	trashOne(t, home, "notes.txt")

	listed, err := listTrash(t.Context(), sessionOwner{})
	require.NoError(t, err)
	require.Len(t, listed.Entries, 1)
	require.Empty(t, requests())
}
