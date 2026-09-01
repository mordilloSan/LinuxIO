package daemon

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleAddRejectsLegacyEntryFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/tmp/file","name":"file"}`))
	rr := httptest.NewRecorder()
	(&daemon{}).handleAdd(rr, req)

	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "unknown field") {
		t.Fatalf("response = %d %q, want unknown-field rejection", rr.Code, rr.Body.String())
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
