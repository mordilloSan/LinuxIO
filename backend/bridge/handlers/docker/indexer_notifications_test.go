package docker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

type dockerIndexerRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn dockerIndexerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestDockerComposeIndexerNotificationsUseSharedEntryContract(t *testing.T) {
	originalClient := indexer.Client
	var requests []*http.Request
	var entries []indexerapi.EntryRequest
	indexer.Client = &http.Client{Transport: dockerIndexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		if req.Body != nil {
			var entry indexerapi.EntryRequest
			if err := json.NewDecoder(req.Body).Decode(&entry); err != nil {
				t.Fatalf("decode indexer entry: %v", err)
			}
			entries = append(entries, entry)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
		}, nil
	})}
	t.Cleanup(func() { indexer.Client = originalClient })

	root := t.TempDir()
	composePath := filepath.Join(root, "demo", "compose.yaml")
	if err := os.MkdirAll(filepath.Dir(composePath), 0o700); err != nil {
		t.Fatalf("mkdir compose directory: %v", err)
	}
	if err := os.WriteFile(composePath, []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	notifyIndexerForComposeFile(context.Background(), composePath)
	if len(requests) != 1 || requests[0].Method != http.MethodPost || requests[0].URL.Path != indexerapi.RouteAdd {
		t.Fatalf("add requests = %#v", requests)
	}
	if len(entries) != 1 || entries[0].Path != composePath {
		t.Fatalf("entry = %#v", entries)
	}

	result, err := deleteComposeFile(context.Background(), apischema.DeleteStackResult{}, "demo", composePath)
	if err != nil {
		t.Fatalf("deleteComposeFile: %v", err)
	}
	if !result.FilesDeleted {
		t.Fatalf("delete result = %#v", result)
	}
	wantDeleteQuery := url.Values{"path": {composePath}}.Encode()
	if len(requests) != 2 || requests[1].Method != http.MethodDelete || requests[1].URL.Path != indexerapi.RouteDelete || requests[1].URL.RawQuery != wantDeleteQuery {
		t.Fatalf("delete request = %#v", requests)
	}
}

func TestDockerStackDirectoryDeleteNotifiesIndexer(t *testing.T) {
	originalClient := indexer.Client
	var deletedPath string
	indexer.Client = &http.Client{Transport: dockerIndexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		deletedPath = req.URL.Query().Get("path")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
		}, nil
	})}
	t.Cleanup(func() { indexer.Client = originalClient })

	dir := filepath.Join(t.TempDir(), "stack")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir stack: %v", err)
	}
	result, err := deleteStackDirectory(context.Background(), apischema.DeleteStackResult{}, "stack", dir)
	if err != nil {
		t.Fatalf("deleteStackDirectory: %v", err)
	}
	if !result.DirDeleted || deletedPath != dir {
		t.Fatalf("result = %#v, deleted path = %q", result, deletedPath)
	}
}
