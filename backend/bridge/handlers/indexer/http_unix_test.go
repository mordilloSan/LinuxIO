package indexer

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func withUnixIndexerServer(t *testing.T, handler http.Handler) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "indexer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()

	orig := Client
	Client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}}
	t.Cleanup(func() {
		Client = orig
		_ = server.Close()
		_ = listener.Close()
	})
}

//nolint:gocognit,cyclop // One server switch verifies the complete Unix API contract.
func TestIndexerRequestsOverUnixSocket(t *testing.T) {
	withUnixIndexerServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case indexerapi.RouteAdd:
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("add request = %s %s", r.Method, r.Header.Get("Content-Type"))
			}
			var entry indexerapi.EntryRequest
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
				t.Errorf("decode add: %v", err)
			}
			if entry.Path != "/docs/readme.txt" || entry.Name != "readme.txt" {
				t.Errorf("add entry = %#v", entry)
			}
		case indexerapi.RouteDelete:
			if r.Method != http.MethodDelete || r.URL.Query().Get("path") != "/docs/readme.txt" {
				t.Errorf("delete request = %s %s", r.Method, r.URL.String())
			}
		case indexerapi.RouteReindex:
			if r.Method != http.MethodPost || r.URL.Query().Get("path") != "/docs" {
				t.Errorf("reindex request = %s %s", r.Method, r.URL.String())
			}
		case indexerapi.RouteDirSize:
			if r.Method != http.MethodGet || r.URL.Query().Get("path") != "/docs" {
				t.Errorf("dirsize request = %s %s", r.Method, r.URL.String())
			}
			_, _ = io.WriteString(w, `{"path":"/docs","size":42}`)
			return
		case indexerapi.RouteEntryCount:
			if r.Method != http.MethodGet || r.URL.Query().Get("path") != "/docs" {
				t.Errorf("entrycount request = %s %s", r.Method, r.URL.String())
			}
			_, _ = io.WriteString(w, `{"path":"/docs","files":2,"dirs":1}`)
			return
		case indexerapi.RouteSubfolders:
			if r.Method != http.MethodGet || r.URL.Query().Get("path") != "/docs" {
				t.Errorf("subfolders request = %s %s", r.Method, r.URL.String())
			}
			_, _ = io.WriteString(w, `[{"path":"/docs/a","name":"a"}]`)
			return
		case indexerapi.RouteSearch:
			if r.Method != http.MethodGet || r.URL.Query().Get("q") != "readme" || r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("base") != "/docs" {
				t.Errorf("search request = %s %s", r.Method, r.URL.String())
			}
			_, _ = io.WriteString(w, `[{"path":"/docs/readme.txt","name":"readme.txt"}]`)
			return
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	entry := indexerapi.EntryRequest{Path: "/docs/readme.txt", Name: "readme.txt", Type: "file"}
	if err := Add(context.Background(), entry); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Delete(context.Background(), "docs/readme.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := Reindex(context.Background(), "docs"); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if got, err := DirSize(context.Background(), "docs"); err != nil || got.Size != 42 {
		t.Fatalf("DirSize = %#v, %v", got, err)
	}
	if got, err := EntryCount(context.Background(), "docs"); err != nil || got.Files != 2 || got.Dirs != 1 {
		t.Fatalf("EntryCount = %#v, %v", got, err)
	}
	if got, err := Subfolders(context.Background(), "docs"); err != nil || len(got) != 1 || got[0].Path != "/docs/a" {
		t.Fatalf("Subfolders = %#v, %v", got, err)
	}
	if got, err := Search(context.Background(), "readme", "10", "docs"); err != nil || len(got) != 1 || got[0].Name != "readme.txt" {
		t.Fatalf("Search = %#v, %v", got, err)
	}
}
