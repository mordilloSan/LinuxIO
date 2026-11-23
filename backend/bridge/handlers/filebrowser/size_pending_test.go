package filebrowser

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
)

// answerSizesWith stands in for the indexer daemon and answers every size
// request with the given bodies.
func answerSizesWith(t *testing.T, dirSizeBody, subfoldersBody string) {
	t.Helper()
	detachedIndexerUpdates.Wait()
	original := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := dirSizeBody
		if req.URL.Path == "/subfolders" {
			body = subfoldersBody
		}
		return &http.Response{StatusCode: http.StatusOK, Status: http.StatusText(http.StatusOK), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	t.Cleanup(func() {
		detachedIndexerUpdates.Wait()
		indexer.Client = original
	})
}

func TestSizesPassOnThatTheIndexerStillHasAnUpdateForTheFolder(t *testing.T) {
	answerSizesWith(t, `{"size":4096,"pending":true}`, `{"subfolders":[{"path":"/x/a","size":7}],"pending":true}`)
	folder := t.TempDir()

	size, err := dirSize(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.True(t, size.Pending)
	require.Equal(t, int64(4096), size.Size)

	folders, err := subfolders(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.True(t, folders.Pending)
	require.Len(t, folders.Subfolders, 1)
	require.Equal(t, int64(7), folders.Subfolders[0].Size)
}

func TestSizesAreNotPendingWhenNothingWaits(t *testing.T) {
	answerSizesWith(t, `{"size":4096}`, `{"subfolders":[]}`)
	folder := t.TempDir()

	size, err := dirSize(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.False(t, size.Pending)
	folders, err := subfolders(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.False(t, folders.Pending)
}

// The bridge tells the indexer of a finished operation after it reports the
// operation done. A size the page asks for in between is pending, although
// the indexer has not heard of the update yet.
func TestSizesArePendingWhileTheBridgeStillTellsTheIndexer(t *testing.T) {
	answerSizesWith(t, `{"size":4096}`, `{"subfolders":[]}`)
	folder := t.TempDir()
	started, release := make(chan struct{}), make(chan struct{})
	runDetachedIndexerUpdate("test", func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	<-started

	size, err := dirSize(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.True(t, size.Pending)
	folders, err := subfolders(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.True(t, folders.Pending)

	close(release)
	detachedIndexerUpdates.Wait()
	size, err = dirSize(context.Background(), apischema.PathRequest{Path: folder})
	require.NoError(t, err)
	require.False(t, size.Pending)
}
