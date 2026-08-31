package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type indexerRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn indexerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestDirectoryListingResponseHasOnlyListingFields(t *testing.T) {
	modified := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	response := directoryListingResponse(iteminfo.DirectoryListing{
		Folders: []iteminfo.ItemInfo{{Name: "docs", ModTime: modified, Symlink: true}},
		Files: []iteminfo.ItemInfo{{
			Name: "notes.txt", Size: 12, ModTime: modified,
			IsRegularFile: true, CanOpenAsText: true,
		}},
	})
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"folders":[{"name":"docs","modified":"2026-08-29T12:00:00Z","symlink":true}],
		"files":[{"name":"notes.txt","size":12,"modified":"2026-08-29T12:00:00Z","symlink":false,"isRegularFile":true,"canOpenAsText":true}]
	}`, string(encoded))
}

func TestReadCallsPreserveCancellationIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, listErr := listDirectory(ctx, apischema.PathRequest{Path: "/"})
	_, childrenErr := directoryChildren(ctx, apischema.DirectoryChildrenRequest{Path: "/"})
	_, textErr := readText(ctx, apischema.PathRequest{Path: "/missing"})
	require.ErrorIs(t, listErr, context.Canceled)
	require.ErrorIs(t, childrenErr, context.Canceled)
	require.ErrorIs(t, textErr, context.Canceled)
}

func TestDirSizeFetchesSizeAndCountsConcurrently(t *testing.T) {
	detachedIndexerUpdates.Wait()
	originalClient := indexer.Client
	started := make(chan string, 2)
	release := make(chan struct{})
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		started <- req.URL.Path
		<-release

		var body string
		switch req.URL.Path {
		case "/dirsize":
			body = `{"size":4096}`
		case "/entrycount":
			body = `{"files":12,"dirs":3}`
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     http.StatusText(http.StatusNotFound),
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() {
		indexer.Client = originalClient
		setIndexerAvailability(true)
	})

	type result struct {
		data apischema.DirectorySizeData
		err  error
	}
	done := make(chan result, 1)
	directory := t.TempDir()
	go func() {
		data, err := dirSize(context.Background(), apischema.PathRequest{Path: directory})
		done <- result{data: data, err: err}
	}()

	seen := make(map[string]bool, 2)
	for len(seen) < 2 {
		select {
		case path := <-started:
			seen[path] = true
		case <-time.After(time.Second):
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			t.Fatal("indexer requests did not start concurrently")
		}
	}
	close(release)
	got := <-done

	require.NoError(t, got.err)
	require.Equal(t, int64(4096), got.data.Size)
	require.Equal(t, int64(12), got.data.FileCount)
	require.Equal(t, int64(3), got.data.FolderCount)
	require.True(t, seen["/dirsize"])
	require.True(t, seen["/entrycount"])
}

func TestResourceStatReturnsStructuredClientErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
		code int
	}{
		{name: "missing request path", code: 400},
		{name: "path not found", path: filepath.Join(t.TempDir(), "missing"), code: 404},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resourceStat(context.Background(), apischema.PathRequest{Path: tc.path})
			var apiErr *bridgeipc.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("resourceStat() error = %v, want *bridgeipc.Error", err)
			}
			if apiErr.Code != tc.code {
				t.Fatalf("resourceStat() code = %d, want %d", apiErr.Code, tc.code)
			}
		})
	}
}

func TestResourceStatResolvesPathWithoutBuildingAListing(t *testing.T) {
	dir := t.TempDir()
	for i := range 300 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("sibling-%03d", i)), []byte("unused"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := resourceStat(context.Background(), apischema.PathRequest{Path: link})
	require.NoError(t, err)
	if got.Mode == "" || got.Mode[0] == 'L' || got.Permissions == "" || got.Owner == "" || got.Group == "" {
		t.Fatalf("stat = %+v, want target permission metadata", got)
	}
}

func TestExistsBatchReportsExistingPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	resp, err := existsBatch(context.Background(), apischema.BatchPathRequest{
		Paths: []string{file, sub, filepath.Join(dir, "missing")},
	})
	if err != nil {
		t.Fatalf("existsBatch: %v", err)
	}
	if len(resp.Existing) != 2 {
		t.Fatalf("existing = %+v, want 2 entries", resp.Existing)
	}
	if resp.Existing[0].Path != file || resp.Existing[0].IsDir {
		t.Fatalf("existing[0] = %+v", resp.Existing[0])
	}
	if resp.Existing[1].Path != sub || !resp.Existing[1].IsDir {
		t.Fatalf("existing[1] = %+v", resp.Existing[1])
	}
}

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
