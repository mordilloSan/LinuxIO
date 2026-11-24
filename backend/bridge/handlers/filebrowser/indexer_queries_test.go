package filebrowser

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func TestSearchResultFromIndexerExposesEditorEligibility(t *testing.T) {
	got := searchResultFromIndexer(indexerapi.EntryResult{
		Path: "/plain",
		Type: "file",
	})
	if !got.IsRegularFile || got.CanOpenAsText == nil || !*got.CanOpenAsText {
		t.Fatalf("search result eligibility = %#v, want regular text file", got)
	}
}

func TestSubfoldersFromIndexerUsesCanonicalSizeAndIgnoresBytes(t *testing.T) {
	upstream := []indexerapi.SubfolderResult{{Path: "/data/cache", Name: "cache"}}
	got := subfoldersFromIndexer(upstream)
	if len(got) != 1 {
		t.Fatalf("subfolder count = %d, want 1", len(got))
	}
	if got[0].Size != 0 || got[0].Path != "/data/cache" {
		t.Fatalf("subfolder = %#v, want only canonical indexer fields", got[0])
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"path":"/data/cache","size":0}`; string(encoded) != want {
		t.Fatalf("public subfolder JSON = %s, want %s (bytes must not escape)", encoded, want)
	}
}

func TestSubfoldersFromIndexerReturnsNonNilEmptySlice(t *testing.T) {
	got := subfoldersFromIndexer(nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("subfoldersFromIndexer(nil) = %#v, want non-nil empty slice", got)
	}
}

func TestSearchResultFromIndexerPreservesCanonicalFields(t *testing.T) {
	modified := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	upstream := indexerapi.EntryResult{
		Path: "/data/report.txt", Name: "report.txt", Type: "file",
		Size: 42, Inode: 99, ModTime: modified,
	}
	got := searchResultFromIndexer(upstream)
	if got.ModTime != "2026-07-30T12:00:00Z" || got.Path != "/data/report.txt" || got.Name != "report.txt" || got.IsDir || got.Size == nil || *got.Size != 42 || got.CanOpenAsText == nil || !*got.CanOpenAsText {
		t.Fatalf("search result = %#v, want canonical field preservation", got)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	if want := `{"isDir":false,"isRegularFile":true,"mod_time":"2026-07-30T12:00:00Z","name":"report.txt","path":"/data/report.txt","size":42,"canOpenAsText":true}`; string(encoded) != want {
		t.Fatalf("public search JSON = %s, want %s", encoded, want)
	}
}

func TestSearchResultFromIndexerRecognizesDirectory(t *testing.T) {
	got := searchResultFromIndexer(indexerapi.EntryResult{Path: "/data", Type: "folder"})
	if !got.IsDir || got.IsRegularFile || got.CanOpenAsText != nil {
		t.Fatalf("directory result = %#v", got)
	}
}

func TestSearchResponseFromIndexerReturnsNonNilEmptyResults(t *testing.T) {
	got := searchResponseFromIndexer(indexerapi.SearchResponse{})
	if got.Results == nil {
		t.Fatalf("response = %#v, want non-nil empty results", got)
	}
}

func TestSearchFilesRejectsShortQueries(t *testing.T) {
	for _, query := range []string{"", "ab", "case:exact ab"} {
		_, err := searchFiles(context.Background(), apischema.FileSearchRequest{Query: query})
		if err == nil || !strings.Contains(err.Error(), "at least 3 characters") {
			t.Fatalf("searchFiles(%q) error = %v, want minimum-length rejection", query, err)
		}
	}
}

func TestDirSizeFetchesSizeAndCountsTogether(t *testing.T) {
	detachedIndexerUpdates.Wait()
	originalClient := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/dirsize" {
			t.Errorf("request path = %s, want /dirsize", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Body:       io.NopCloser(strings.NewReader(`{"size":4096,"files":12,"dirs":3}`)),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() {
		indexer.Client = originalClient
	})

	type result struct {
		data apischema.DirectorySizeData
		err  error
	}
	directory := t.TempDir()
	data, err := dirSize(context.Background(), apischema.PathRequest{Path: directory})
	got := result{data: data, err: err}

	require.NoError(t, got.err)
	require.Equal(t, int64(4096), got.data.Size)
	require.Equal(t, int64(12), got.data.FileCount)
	require.Equal(t, int64(3), got.data.FolderCount)
}

func TestSearchFilesPassesOnHowTheFolderWasReadAndUnknownSizes(t *testing.T) {
	detachedIndexerUpdates.Wait()
	folder := t.TempDir()
	originalClient := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/search" || req.URL.Query().Get("base") != folder {
			t.Errorf("request = %s, want /search below %s", req.URL, folder)
		}
		body := `{"results":[` +
			`{"path":"` + folder + `/report.txt","name":"report.txt","type":"file","size":42},` +
			`{"path":"` + folder + `/reports","name":"reports","type":"folder","size":4096},` +
			`{"path":"` + folder + `/report-new","name":"report-new","type":"folder","size_unknown":true}` +
			`],"walk":"complete"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() { indexer.Client = originalClient })

	got, err := searchFiles(context.Background(), apischema.FileSearchRequest{Query: "report", BasePath: &folder})

	require.NoError(t, err)
	require.Equal(t, apischema.FileSearchWalkComplete, got.Walk)
	require.Len(t, got.Results, 3)
	require.Equal(t, int64(42), *got.Results[0].Size)
	require.Equal(t, int64(4096), *got.Results[1].Size)
	require.Nil(t, got.Results[2].Size, "a folder the index holds no size for has none")
	encoded, err := json.Marshal(got.Results[2])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"size"`)
}

func TestSearchFilesFromTheRootSaysNothingOfAWalk(t *testing.T) {
	detachedIndexerUpdates.Wait()
	originalClient := indexer.Client
	indexer.Client = &http.Client{Transport: indexerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Body:       io.NopCloser(strings.NewReader(`{"results":[]}`)),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() { indexer.Client = originalClient })

	got, err := searchFiles(context.Background(), apischema.FileSearchRequest{Query: "report"})

	require.NoError(t, err)
	require.Empty(t, got.Walk)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, `{"results":[]}`, string(encoded))
}
