package bridge

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTaskPrimitiveDecoderEnforcesStrictSingleValuePolicy(t *testing.T) {
	service := NewTaskService()
	router := NewRouter(service)
	service.RegisterRoutes(router)
	route, ok := router.lookup("tasks.get")
	if !ok {
		t.Fatal("tasks.get is not registered")
	}

	tests := []struct {
		name             string
		raw              json.RawMessage
		wantTaskID       string
		wantError        string
		wantTypeMismatch bool
	}{
		{name: "valid object", raw: json.RawMessage(`{"taskId":"task-1"}`), wantTaskID: "task-1"},
		{name: "unknown field", raw: json.RawMessage(`{"taskId":"task-1","unexpected":true}`), wantError: `unknown field "unexpected"`},
		{name: "trailing JSON value", raw: json.RawMessage(`{"taskId":"task-1"} {}`), wantError: "exactly one JSON value"},
		{name: "scalar type mismatch", raw: json.RawMessage(`{"taskId":123}`), wantTypeMismatch: true},
		{name: "empty input", raw: nil},
		{name: "null input", raw: json.RawMessage(`null`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTaskPrimitiveDecode(t, route.Decode, tc.raw, tc.wantTaskID, tc.wantError, tc.wantTypeMismatch)
		})
	}
}

func assertTaskPrimitiveDecode(t *testing.T, decode RequestDecoder, raw json.RawMessage, wantTaskID, wantError string, wantTypeMismatch bool) {
	t.Helper()
	decoded, err := decode(raw)
	if wantTypeMismatch {
		if _, ok := errors.AsType[*json.UnmarshalTypeError](err); !ok {
			t.Fatalf("Decode() error = %v, want *json.UnmarshalTypeError", err)
		}
		return
	}
	if wantError != "" {
		if err == nil || !strings.Contains(err.Error(), wantError) {
			t.Fatalf("Decode() error = %v, want error containing %q", err, wantError)
		}
		return
	}
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	request, ok := decoded.(taskIDRequest)
	if !ok {
		t.Fatalf("Decode() result = %T, want taskIDRequest", decoded)
	}
	if request.TaskID != wantTaskID {
		t.Fatalf("taskId = %q, want %q", request.TaskID, wantTaskID)
	}
}
