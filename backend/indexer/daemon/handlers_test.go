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

func TestHandleAddRejectsRootAndDirectories(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/"}`))
		rr := httptest.NewRecorder()
		(&daemon{}).handleAdd(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "use /reindex") {
			t.Fatalf("response = %d %q, want reindex rejection", rr.Code, rr.Body.String())
		}
	})

	t.Run("directory", func(t *testing.T) {
		d, _ := newDaemonWithDB(t)
		if _, err := d.db.Exec(`INSERT INTO indexes (last_indexed) VALUES (1)`); err != nil {
			t.Fatalf("insert index: %v", err)
		}
		path := t.TempDir()
		req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"`+path+`"}`))
		rr := httptest.NewRecorder()
		d.handleAdd(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "use /reindex") {
			t.Fatalf("response = %d %q, want reindex rejection", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleAddSkipsExcludedPathBeforeOperationLock(t *testing.T) {
	d := &daemon{}
	d.running.Store(true)
	req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/proc/meminfo"}`))
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Fatalf("response = %d %q, want excluded-path success", rr.Code, rr.Body.String())
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
