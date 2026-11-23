package filebrowser

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
)

// recordDetachedIndexerRequests stands in for the indexer daemon and returns what the
// bridge has asked of it once the detached requests have ended.
func recordDetachedIndexerRequests(t *testing.T) (requests func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	original := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.URL.Query().Get("path"))
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"queued"}`))}, nil
	})}
	t.Cleanup(func() {
		detachedIndexerUpdates.Wait()
		indexer.Client = original
	})
	return func() []string {
		detachedIndexerUpdates.Wait()
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

func TestListingAFolderAsksTheIndexerToRefreshIt(t *testing.T) {
	requests := recordDetachedIndexerRequests(t)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	listing, err := listOpenedDirectory(context.Background(), apischema.PathRequest{Path: folder}, sessionOwner{sudo: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listing.Files) != 1 {
		t.Fatalf("listed %d files, want 1", len(listing.Files))
	}
	if got, want := requests(), []string{"POST /refresh " + folder}; !slices.Equal(got, want) {
		t.Fatalf("indexer requests = %v, want %v", got, want)
	}
}

func TestListingThroughALinkRefreshesTheFolderItPointsTo(t *testing.T) {
	requests := recordDetachedIndexerRequests(t)
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if _, err := listOpenedDirectory(context.Background(), apischema.PathRequest{Path: link}, sessionOwner{sudo: true}); err != nil {
		t.Fatalf("list: %v", err)
	}
	// The index knows a folder by its real path.
	if got, want := requests(), []string{"POST /refresh " + real}; !slices.Equal(got, want) {
		t.Fatalf("indexer requests = %v, want %v", got, want)
	}
}

func TestASessionWithoutSudoDoesNotAskForARefresh(t *testing.T) {
	requests := recordDetachedIndexerRequests(t)

	if _, err := listOpenedDirectory(context.Background(), apischema.PathRequest{Path: t.TempDir()}, sessionOwner{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("a session that cannot use the index asked the indexer: %v", got)
	}
}

func TestAListingThatFailsAsksForNoRefresh(t *testing.T) {
	requests := recordDetachedIndexerRequests(t)

	if _, err := listOpenedDirectory(context.Background(), apischema.PathRequest{Path: filepath.Join(t.TempDir(), "missing")}, sessionOwner{sudo: true}); err == nil {
		t.Fatal("listing a missing folder succeeded")
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("indexer requests = %v, want none", got)
	}
}
