package packages

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDownloadMonitoringInstallScript(t *testing.T) {
	const body = "#!/bin/sh\necho installing\n"

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != monitoringInstallScriptURL {
				t.Fatalf("url = %q, want %q", req.URL.String(), monitoringInstallScriptURL)
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

	got, err := downloadMonitoringInstallScript(context.Background(), client)
	if err != nil {
		t.Fatalf("downloadMonitoringInstallScript: %v", err)
	}
	if string(got) != body {
		t.Fatalf("script = %q, want %q", got, body)
	}
}

func TestDownloadMonitoringInstallScriptRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(strings.NewReader("")),
				ContentLength: monitoringInstallScriptMaxBytes + 1,
				Header:        make(http.Header),
				Request:       req,
			}, nil
		}),
	}

	if _, err := downloadMonitoringInstallScript(context.Background(), client); err == nil {
		t.Fatal("expected oversized installer error")
	}
}
