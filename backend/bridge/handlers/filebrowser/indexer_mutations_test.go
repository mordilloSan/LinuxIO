package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

type recordedIndexerRequest struct {
	method string
	path   string
	query  string
	entry  *indexerapi.EntryRequest
}

func recordIndexerRequests(t *testing.T) *[]recordedIndexerRequest {
	t.Helper()
	originalClient := indexer.Client
	requests := make([]recordedIndexerRequest, 0, 4)
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorded := recordedIndexerRequest{method: req.Method, path: req.URL.Path, query: req.URL.RawQuery}
		if req.Body != nil {
			var entry indexerapi.EntryRequest
			if err := json.NewDecoder(req.Body).Decode(&entry); err != nil {
				t.Fatalf("decode indexer request: %v", err)
			}
			recorded.entry = &entry
		}
		requests = append(requests, recorded)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
		}, nil
	})}
	t.Cleanup(func() {
		indexer.Client = originalClient
	})
	return &requests
}

func TestIndexerMutationNotificationsUseCanonicalOperations(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "created.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	dirPath := filepath.Join(root, "copied")
	if mkdirErr := os.Mkdir(dirPath, 0o700); mkdirErr != nil {
		t.Fatalf("mkdir fixture: %v", mkdirErr)
	}
	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat directory fixture: %v", err)
	}
	pathQuery := func(path string) string { return url.Values{"path": {path}}.Encode() }

	t.Run("create", func(t *testing.T) {
		requests := recordIndexerRequests(t)
		if err := addToIndexer(context.Background(), filePath); err != nil {
			t.Fatalf("addToIndexer: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{{
			method: http.MethodPost,
			path:   indexerapi.RouteAdd,
			entry:  &indexerapi.EntryRequest{Path: filePath},
		}})
	})

	t.Run("overwrite", func(t *testing.T) {
		requests := recordIndexerRequests(t)
		if err := addCopiedPathToIndexer(context.Background(), filePath, fileInfo, computedTransferSize{total: 5, known: true}, true); err != nil {
			t.Fatalf("addCopiedPathToIndexer: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{
			{method: http.MethodDelete, path: indexerapi.RouteDelete, query: pathQuery(filePath)},
			{method: http.MethodPost, path: indexerapi.RouteAdd, entry: &indexerapi.EntryRequest{Path: filePath}},
		})
	})

	t.Run("directory copy", func(t *testing.T) {
		requests := recordIndexerRequests(t)
		if err := addCopiedPathToIndexer(context.Background(), dirPath, dirInfo, computedTransferSize{total: 42, known: true}, false); err != nil {
			t.Fatalf("addCopiedPathToIndexer: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{
			{method: http.MethodPost, path: indexerapi.RouteReindex, query: pathQuery(dirPath)},
		})
	})

	t.Run("move over destination", func(t *testing.T) {
		requests := recordIndexerRequests(t)
		destination := filepath.Join(root, "moved.txt")
		if err := movePathInIndexer(context.Background(), filePath, destination, computedTransferSize{total: 5, known: true}, true, func() (os.FileInfo, error) {
			return fileInfo, nil
		}); err != nil {
			t.Fatalf("movePathInIndexer: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{
			{method: http.MethodDelete, path: indexerapi.RouteDelete, query: pathQuery(filePath)},
			{method: http.MethodDelete, path: indexerapi.RouteDelete, query: pathQuery(destination)},
			{method: http.MethodPost, path: indexerapi.RouteAdd, entry: &indexerapi.EntryRequest{Path: destination}},
		})
	})

	t.Run("delete", func(t *testing.T) {
		requests := recordIndexerRequests(t)
		if err := deleteFromIndexer(context.Background(), filePath); err != nil {
			t.Fatalf("deleteFromIndexer: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{{method: http.MethodDelete, path: indexerapi.RouteDelete, query: pathQuery(filePath)}})
	})
}

func TestCreatedResourceNotificationsUseTypeSpecificOperations(t *testing.T) {
	pathQuery := func(path string) string { return url.Values{"path": {path}}.Encode() }

	t.Run("directory uses scoped reindex", func(t *testing.T) {
		dir := t.TempDir()
		root, err := fsroot.OpenAt(dir)
		if err != nil {
			t.Fatalf("open root: %v", err)
		}
		defer root.Close()
		requests := recordIndexerRequests(t)
		path := filepath.Join(dir, "created-dir")
		if _, err := createDirectoryResource(t.Context(), root, resourcePostRequest{cleanPath: path, relPath: "created-dir", isDir: true}); err != nil {
			t.Fatalf("createDirectoryResource: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{{
			method: http.MethodPost,
			path:   indexerapi.RouteReindex,
			query:  pathQuery(path),
		}})
	})

	t.Run("file uses add", func(t *testing.T) {
		dir := t.TempDir()
		root, err := fsroot.OpenAt(dir)
		if err != nil {
			t.Fatalf("open root: %v", err)
		}
		defer root.Close()
		requests := recordIndexerRequests(t)
		path := filepath.Join(dir, "created.txt")
		if _, err := createFileResource(t.Context(), root, resourcePostRequest{cleanPath: path, relPath: "created.txt"}); err != nil {
			t.Fatalf("createFileResource: %v", err)
		}
		assertIndexerRequests(t, *requests, []recordedIndexerRequest{{
			method: http.MethodPost,
			path:   indexerapi.RouteAdd,
			entry:  &indexerapi.EntryRequest{Path: path},
		}})
	})
}

func TestIndexerMutationRetriesAfterUnavailableRequest(t *testing.T) {
	originalClient := indexer.Client
	attempts := 0
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("socket unavailable")
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: http.NoBody}, nil
	})}
	t.Cleanup(func() {
		indexer.Client = originalClient
	})

	if err := addToIndexer(context.Background(), "/data"); !errors.Is(err, errIndexerUnavailable) {
		t.Fatalf("addToIndexer error = %v, want errIndexerUnavailable", err)
	}
	if err := addToIndexer(context.Background(), "/data"); err != nil {
		t.Fatalf("second addToIndexer: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestIndexerMutationAuthorizationFailureIsNotUnavailable(t *testing.T) {
	originalClient := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("root privileges required")),
		}, nil
	})}
	t.Cleanup(func() {
		indexer.Client = originalClient
	})

	err := addToIndexer(context.Background(), "/data")
	if err == nil || errors.Is(err, errIndexerUnavailable) {
		t.Fatalf("addToIndexer error = %v, want non-availability response error", err)
	}
}

func assertIndexerRequests(t *testing.T, got, want []recordedIndexerRequest) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("requests = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i].method != want[i].method || got[i].path != want[i].path || got[i].query != want[i].query {
			t.Fatalf("request[%d] = %#v, want %#v", i, got[i], want[i])
		}
		if want[i].entry == nil {
			if got[i].entry != nil {
				t.Fatalf("request[%d] entry = %#v, want nil", i, got[i].entry)
			}
			continue
		}
		if got[i].entry == nil {
			t.Fatalf("request[%d] entry = nil, want %#v", i, want[i].entry)
		}
		if got[i].entry.Path != want[i].entry.Path {
			t.Fatalf("request[%d] entry = %#v, want matching %#v", i, got[i].entry, want[i].entry)
		}
	}
}
