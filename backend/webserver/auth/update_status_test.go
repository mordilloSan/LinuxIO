package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const updateStatusTestID = "00000000-0000-4000-8000-000000000042"

func TestUpdateStatusReadsSafeRuntimeProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status.json")
	oldPath := updateStatusPath
	updateStatusPath = path
	defer func() { updateStatusPath = oldPath }()

	exitCode := 0
	startedAt := int64(100)
	finishedAt := int64(200)
	data, err := json.Marshal(updateStatusFile{
		Version:    updateStatusVersion,
		ID:         updateStatusTestID,
		OwnerUID:   1000,
		Status:     "ok",
		ExitCode:   &exitCode,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
	})
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}

	response := serveUpdateStatus(t, updateStatusTestID, true)
	if response.Status != "ok" || response.ID != updateStatusTestID || response.ExitCode == nil || *response.ExitCode != 0 {
		t.Fatalf("response = %+v", response)
	}
	if hidden := serveUpdateStatus(t, "00000000-0000-4000-8000-000000000043", true); hidden.Status != "unknown" {
		t.Fatalf("mismatched response = %+v", hidden)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/update-status?id="+updateStatusTestID, nil)
	request = request.WithContext(session.WithSession(request.Context(), &session.Session{
		User: session.User{Username: "bob", UID: 1001},
	}))
	recorder := httptest.NewRecorder()
	(&Handlers{}).UpdateStatus(recorder, request)
	var hidden updateStatusResponse
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &hidden); decodeErr != nil || hidden.Status != "unknown" {
		t.Fatalf("cross-UID response = %+v, %v", hidden, decodeErr)
	}
}

func TestUpdateStatusRequiresAuthentication(t *testing.T) {
	responseRecorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/update-status?id="+updateStatusTestID, nil)
	(&Handlers{}).UpdateStatus(responseRecorder, request)
	if responseRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", responseRecorder.Code, http.StatusUnauthorized)
	}
}

func TestUpdateStatusReturnsUnknownWhenProjectionIsMissing(t *testing.T) {
	oldPath := updateStatusPath
	updateStatusPath = filepath.Join(t.TempDir(), "missing.json")
	defer func() { updateStatusPath = oldPath }()

	if response := serveUpdateStatus(t, updateStatusTestID, true); response.Status != "unknown" {
		t.Fatalf("response = %+v", response)
	}
}

func TestUpdateStatusRejectsLegacyAndMalformedProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status.json")
	oldPath := updateStatusPath
	updateStatusPath = path
	defer func() { updateStatusPath = oldPath }()

	for _, data := range [][]byte{
		[]byte(`{"id":"` + updateStatusTestID + `","owner_uid":1000,"status":"ok"}`),
		[]byte(`{"version":1,"id":"` + updateStatusTestID + `","owner_uid":1000,"status":"completed"}`),
		[]byte(`{"version":1,"id":"` + updateStatusTestID + `","owner_uid":1000,"status":"ok","started_at":100,"finished_at":200}`),
	} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write status: %v", err)
		}
		if response := serveUpdateStatus(t, updateStatusTestID, true); response.Status != "unknown" {
			t.Fatalf("response for %s = %+v", data, response)
		}
	}
}

func serveUpdateStatus(t *testing.T, id string, authenticated bool) updateStatusResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/update-status?id="+id, nil)
	if authenticated {
		request = request.WithContext(session.WithSession(request.Context(), &session.Session{
			User: session.User{Username: "alice", UID: 1000},
		}))
	}
	(&Handlers{}).UpdateStatus(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response updateStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}
