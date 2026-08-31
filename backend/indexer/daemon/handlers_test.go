package daemon

import (
	"net/http"
	"net/http/httptest"
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
