package apischema

import (
	"context"
	"encoding/json/jsontext"
	"testing"
)

func TestMutationAdaptersDecodeTheRecordRequestAndPassTheCheckpoint(t *testing.T) {
	type poolRequest struct {
		UUID string `json:"uuid"`
	}
	var gotUUID string
	var gotCheckpoint jsontext.Value
	binding := MutationVoid("test.adapters", func(_ context.Context, req poolRequest) error {
		gotUUID = req.UUID
		return nil
	}, Privileged()).Resumable(ResumeHooks[poolRequest, NoResponse]{
		Suspend: func(_ context.Context, req poolRequest, checkpoint jsontext.Value) (jsontext.Value, error) {
			gotUUID, gotCheckpoint = req.UUID, checkpoint
			return jsontext.Value(`{"offset":2}`), nil
		},
		Resume: func(_ context.Context, req poolRequest, checkpoint jsontext.Value) (NoResponse, error) {
			gotUUID, gotCheckpoint = req.UUID, checkpoint
			return NoResponse{}, nil
		},
	})

	result, err := wrapMutation(binding.Route, binding.fn)(context.Background(), nil, poolRequest{UUID: "run"})
	if err != nil || result != nil || gotUUID != "run" {
		t.Fatalf("runner = %#v, %v with uuid %q; want a nil void result for uuid run", result, err, gotUUID)
	}

	suspend, resume := resumeHooks(binding.Route, binding.hooks)
	record := jsontext.Value(`{"id":"job-1","` + recordRequestMember + `":{"uuid":"pool"},"` + recordCheckpointMember + `":{"offset":1}}`)
	checkpoint, err := suspend(context.Background(), record)
	if err != nil || string(checkpoint) != `{"offset":2}` || gotUUID != "pool" || string(gotCheckpoint) != `{"offset":1}` {
		t.Fatalf("suspend = %s, %v with uuid %q and checkpoint %s", checkpoint, err, gotUUID, gotCheckpoint)
	}
	gotUUID, gotCheckpoint = "", nil
	result, err = resume(context.Background(), record)
	if err != nil || result != nil || gotUUID != "pool" || string(gotCheckpoint) != `{"offset":1}` {
		t.Fatalf("resume = %#v, %v with uuid %q and checkpoint %s", result, err, gotUUID, gotCheckpoint)
	}

	if _, err := resume(context.Background(), jsontext.Value(`{"request":{"unexpected":true}}`)); err == nil {
		t.Fatal("resume accepted a record whose request the route decoder rejects")
	}
}
