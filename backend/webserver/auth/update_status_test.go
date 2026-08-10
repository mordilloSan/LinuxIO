package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const updateStatusTestID = "00000000-0000-4000-8000-000000000042"
const otherDurableTestID = "00000000-0000-4000-8000-000000000043"

func TestUpdateStatusIsUIDScopedAndReconcilesExecutorResult(t *testing.T) {
	oldRoot := updateStatusStoreRoot
	updateStatusStoreRoot = filepath.Join(t.TempDir(), "operations")
	defer func() { updateStatusStoreRoot = oldRoot }()
	store := durabletask.NewStore(updateStatusStoreRoot)
	record, _, err := store.Claim(context.Background(), durabletask.Claim{
		ID:                 updateStatusTestID,
		Route:              "control.app_update",
		UID:                1000,
		RequestFingerprint: durabletask.Fingerprint("control.app_update", "version=v2.3.4"),
		Target:             "v2.3.4",
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	started := time.Now().UTC()
	if _, updateErr := store.Update(context.Background(), record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateRunning
		current.StartedAt = &started
		current.Executor = durabletask.Executor{Kind: "systemd-transient-unit", Handle: "unit.service", Identity: "root:root"}
		return nil
	}); updateErr != nil {
		t.Fatalf("mark running: %v", updateErr)
	}

	handler := &Handlers{}
	owner := &session.Session{User: session.User{Username: "alice", UID: 1000}}
	response := serveUpdateStatus(t, handler, owner)
	if response.Status != "running" || response.ID != updateStatusTestID {
		t.Fatalf("running response = %+v", response)
	}
	other := &session.Session{User: session.User{Username: "bob", UID: 1001}}
	if hidden := serveUpdateStatus(t, handler, other); hidden.Status != "unknown" || hidden.ID != "" {
		t.Fatalf("cross-UID response = %+v", hidden)
	}
	if _, _, claimErr := store.Claim(context.Background(), durabletask.Claim{
		ID:                 otherDurableTestID,
		Route:              "future.durable_operation",
		UID:                owner.User.UID,
		RequestFingerprint: durabletask.Fingerprint("future.durable_operation", "request"),
	}); claimErr != nil {
		t.Fatalf("Claim other route: %v", claimErr)
	}
	if hidden := serveUpdateStatusID(t, handler, owner, otherDurableTestID); hidden.Status != "unknown" || hidden.ID != "" {
		t.Fatalf("other-route response = %+v", hidden)
	}

	result := durabletask.ExecutorResult{ID: record.ID, State: durabletask.StateCompleted, ExitCode: 0, FinishedAt: time.Now().UTC()}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, writeErr := store.WriteArtifact(record.ID, "executor-result.json", data, 0o600); writeErr != nil {
		t.Fatalf("WriteArtifact: %v", writeErr)
	}
	completed := serveUpdateStatus(t, handler, owner)
	if completed.Status != "ok" || completed.ExitCode == nil || *completed.ExitCode != 0 || completed.FinishedAt == nil {
		t.Fatalf("completed response = %+v", completed)
	}
	persisted, err := store.Get(context.Background(), record.ID, record.UID)
	if err != nil || persisted.State != durabletask.StateCompleted {
		t.Fatalf("persisted record = %+v, %v", persisted, err)
	}
}

func TestUpdateStatusRequiresAuthenticatedContext(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/update-status?id="+updateStatusTestID, nil)
	(&Handlers{}).UpdateStatus(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func serveUpdateStatus(t *testing.T, handler *Handlers, sess *session.Session) updateStatusResponse {
	return serveUpdateStatusID(t, handler, sess, updateStatusTestID)
}

func serveUpdateStatusID(t *testing.T, handler *Handlers, sess *session.Session, id string) updateStatusResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/update-status?id="+id, nil)
	request = request.WithContext(session.WithSession(request.Context(), sess))
	handler.UpdateStatus(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response updateStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}
