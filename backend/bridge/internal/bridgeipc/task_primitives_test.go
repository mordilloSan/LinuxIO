package bridgeipc

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
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
		name               string
		raw                jsontext.Value
		wantTaskID         string
		wantSemanticError  bool
		wantSyntacticError bool
	}{
		{name: "valid object", raw: jsontext.Value(`{"taskId":"task-1"}`), wantTaskID: "task-1"},
		{name: "unknown field", raw: jsontext.Value(`{"taskId":"task-1","unexpected":true}`), wantSemanticError: true},
		{name: "case-mismatched field", raw: jsontext.Value(`{"TaskId":"task-1"}`), wantSemanticError: true},
		{name: "duplicate field", raw: jsontext.Value(`{"taskId":"task-1","taskId":"task-2"}`), wantSyntacticError: true},
		{name: "invalid UTF-8", raw: jsontext.Value("{\"taskId\":\"\xff\"}"), wantSyntacticError: true},
		{name: "trailing JSON value", raw: jsontext.Value(`{"taskId":"task-1"} {}`), wantSyntacticError: true},
		{name: "scalar type mismatch", raw: jsontext.Value(`{"taskId":123}`), wantSemanticError: true},
		{name: "empty input", raw: nil},
		{name: "null input", raw: jsontext.Value(`null`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTaskPrimitiveDecode(t, route.Decode, tc.raw, tc.wantTaskID, tc.wantSemanticError, tc.wantSyntacticError)
		})
	}
}

func assertTaskPrimitiveDecode(
	t *testing.T,
	decode RequestDecoder,
	raw jsontext.Value,
	wantTaskID string,
	wantSemanticError bool,
	wantSyntacticError bool,
) {
	t.Helper()
	decoded, err := decode(raw)
	if wantSemanticError {
		if _, ok := errors.AsType[*jsonv2.SemanticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *jsonv2.SemanticError", err)
		}
		return
	}
	if wantSyntacticError {
		if _, ok := errors.AsType[*jsontext.SyntacticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *jsontext.SyntacticError", err)
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
