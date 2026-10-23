package daemon

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleStatusStreamFallsBackToUninitializedJSON(t *testing.T) {
	d, _ := newDaemonWithDB(t)

	req := httptest.NewRequest(http.MethodGet, "/status?stream=true", nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	d.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if !strings.Contains(rr.Body.String(), `"status":"uninitialized"`) {
		t.Fatalf("body = %q, expected uninitialized json status", rr.Body.String())
	}
}

func TestHandleSearchRejectsShortQueries(t *testing.T) {
	for _, query := range []string{"", "ab", "case:exact ab"} {
		req := httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(query), nil)
		rr := httptest.NewRecorder()
		(&daemon{}).handleSearch(rr, req)

		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "at least 3 characters") {
			t.Fatalf("query %q response = %d %q, want minimum-length rejection", query, rr.Code, rr.Body.String())
		}
	}
}
