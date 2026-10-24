package daemon

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRelativePath(t *testing.T) {
	valid := []string{"/", "/home/user", "notes..txt", "/a/b..c/d", "/a/.hidden", "a/./b"}
	for _, p := range valid {
		if !validateRelativePath(p) {
			t.Errorf("validateRelativePath(%q) = false, want true", p)
		}
	}
	invalid := []string{"..", "/..", "../etc", "/a/../b", "a/.."}
	for _, p := range invalid {
		if validateRelativePath(p) {
			t.Errorf("validateRelativePath(%q) = true, want false", p)
		}
	}
}

func TestWriteJSONStatusReplacesInvalidUTF8(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONStatus(rec, http.StatusOK, map[string]string{"name": "caf\xe9.txt"})

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not decodable: %v", rec.Body.String(), err)
	}
	if got["name"] != "caf�.txt" {
		t.Fatalf("name = %q, want U+FFFD replacement", got["name"])
	}
}

func TestSSESendEventReplacesInvalidUTF8(t *testing.T) {
	w := newTestSSEWriter()
	sse, err := newSSEWriter(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := sse.SendEvent("progress", map[string]string{"current_path": "/caf\xe9"}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if !strings.Contains(w.String(), "/caf�") {
		t.Fatalf("stream %q lacks replaced path", w.String())
	}
}
