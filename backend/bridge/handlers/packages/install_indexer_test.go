package packages

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDownloadIndexerInstallScript(t *testing.T) {
	const body = "#!/usr/bin/env bash\necho installing\n"

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != indexerInstallScriptURL {
				t.Fatalf("url = %q, want %q", req.URL.String(), indexerInstallScriptURL)
			}
			if accept := req.Header.Get("Accept"); !strings.Contains(accept, "text/x-shellscript") {
				t.Fatalf("Accept header = %q", accept)
			}
			return &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(strings.NewReader(body)),
				ContentLength: int64(len(body)),
				Header:        make(http.Header),
				Request:       req,
			}, nil
		}),
	}

	got, err := downloadIndexerInstallScript(context.Background(), client)
	if err != nil {
		t.Fatalf("downloadIndexerInstallScript: %v", err)
	}
	if string(got) != body {
		t.Fatalf("script = %q, want %q", got, body)
	}
}

func TestDownloadIndexerInstallScriptRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(strings.NewReader("")),
				ContentLength: indexerInstallScriptMaxBytes + 1,
				Header:        make(http.Header),
				Request:       req,
			}, nil
		}),
	}

	if _, err := downloadIndexerInstallScript(context.Background(), client); err == nil {
		t.Fatal("expected oversized installer error")
	}
}

func TestIndexerInstallCommandErrorIncludesOutput(t *testing.T) {
	err := indexerInstallCommandError(context.Canceled, []byte(" installer failed \n"))
	if err == nil || !strings.Contains(err.Error(), "installer failed") {
		t.Fatalf("error = %v, want command output", err)
	}
}
