package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

type dockerRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn dockerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSearchIndexerForYAMLUsesCappedEntriesPageSize(t *testing.T) {
	orig := indexerHTTPClient
	var offsets []string
	indexerHTTPClient = &http.Client{Transport: dockerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/entries" {
			t.Fatalf("path = %s, want /entries", req.URL.Path)
		}
		if got := req.URL.Query().Get("limit"); got != strconv.Itoa(indexerEntriesPageSize) {
			t.Fatalf("limit = %s, want %d", got, indexerEntriesPageSize)
		}

		offsetRaw := req.URL.Query().Get("offset")
		offsets = append(offsets, offsetRaw)
		offset, err := strconv.Atoi(offsetRaw)
		if err != nil {
			t.Fatalf("offset = %q: %v", offsetRaw, err)
		}

		results := make([]indexerSearchResult, 0)
		switch offset {
		case 0:
			for i := range indexerEntriesPageSize {
				results = append(results, indexerSearchResult{
					Path: "/stacks/readme-" + strconv.Itoa(i) + ".txt",
					Name: "readme-" + strconv.Itoa(i) + ".txt",
					Type: "file",
				})
			}
		case indexerEntriesPageSize:
			results = append(results, indexerSearchResult{
				Path: "/stacks/app/docker-compose.yml",
				Name: "docker-compose.yml",
				Type: "file",
			})
		default:
			t.Fatalf("unexpected offset %d", offset)
		}

		body, err := json.Marshal(results)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { indexerHTTPClient = orig })

	results, err := searchIndexerForYAML(context.Background(), "/stacks")
	if err != nil {
		t.Fatalf("searchIndexerForYAML: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].Path != "/stacks/app/docker-compose.yml" {
		t.Fatalf("result path = %s", results[0].Path)
	}
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != strconv.Itoa(indexerEntriesPageSize) {
		t.Fatalf("offsets = %v", offsets)
	}
}
