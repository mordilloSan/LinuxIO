package apischema

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

func TestMutationAdaptersDecodeTheRecordRequestAndPassTheCheckpoint(t *testing.T) {
	type poolRequest struct {
		UUID string `json:"uuid"`
	}
	var gotUUID string
	var gotCheckpoint jsontext.Value
	var gotStart time.Time
	binding := MutationVoid("test.adapters", func(_ context.Context, req poolRequest) error {
		gotUUID = req.UUID
		return nil
	}, Privileged()).Resumable(ResumeHooks[poolRequest, NoResponse]{
		Suspend: func(_ context.Context, req poolRequest, checkpoint jsontext.Value) (jsontext.Value, error) {
			gotUUID, gotCheckpoint = req.UUID, checkpoint
			return jsontext.Value(`{"offset":2}`), nil
		},
		Resume: func(ctx context.Context, req poolRequest, checkpoint jsontext.Value) (NoResponse, error) {
			gotUUID, gotCheckpoint, gotStart = req.UUID, checkpoint, JobStartedAt(ctx)
			return NoResponse{}, nil
		},
	})

	result, err := wrapMutation(binding.Route, binding.fn)(context.Background(), nil, poolRequest{UUID: "run"})
	if err != nil || result != nil || gotUUID != "run" {
		t.Fatalf("runner = %#v, %v with uuid %q; want a nil void result for uuid run", result, err, gotUUID)
	}

	suspend, resume := resumeHooks(binding.Route, binding.hooks)
	record := jsontext.Value(`{"id":"job-1","` + recordStartedMember + `":"2026-11-05T10:00:00.5Z","` + recordRequestMember + `":{"uuid":"pool"},"` + recordCheckpointMember + `":{"offset":1}}`)
	checkpoint, err := suspend(context.Background(), record)
	if err != nil || string(checkpoint) != `{"offset":2}` || gotUUID != "pool" || string(gotCheckpoint) != `{"offset":1}` {
		t.Fatalf("suspend = %s, %v with uuid %q and checkpoint %s", checkpoint, err, gotUUID, gotCheckpoint)
	}
	gotUUID, gotCheckpoint = "", nil
	result, err = resume(context.Background(), record)
	if err != nil || result != nil || gotUUID != "pool" || string(gotCheckpoint) != `{"offset":1}` {
		t.Fatalf("resume = %#v, %v with uuid %q and checkpoint %s", result, err, gotUUID, gotCheckpoint)
	}
	// Resume learns when the job it continues first started, from the
	// store's own record encoding.
	if want := time.Date(2026, 11, 5, 10, 0, 0, 5e8, time.UTC); !gotStart.Equal(want) {
		t.Fatalf("resume saw job start %v, want %v", gotStart, want)
	}
	stored, err := json.Marshal(jobs.Record{ID: "job-2", StartedAt: time.Unix(1_800_000_000, 0), Request: jsontext.Value(`{"uuid":"pool"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resume(context.Background(), stored); err != nil || !gotStart.Equal(time.Unix(1_800_000_000, 0)) {
		t.Fatalf("resume of a stored record = %v with job start %v", err, gotStart)
	}
	if !JobStartedAt(context.Background()).IsZero() {
		t.Fatal("a context outside a resume has a job start")
	}

	if _, err := resume(context.Background(), jsontext.Value(`{"request":{"unexpected":true}}`)); err == nil {
		t.Fatal("resume accepted a record whose request the route decoder rejects")
	}
}

type targetedRequest struct {
	Pool string `json:"pool"`
}

func (r targetedRequest) Target() []string { return []string{r.Pool} }

func TestTargetedRouteOptionNamesTheRequestTarget(t *testing.T) {
	binding := Mutation("test.target", func(context.Context, targetedRequest) (SuccessResponse, error) {
		return SuccessResponse{}, nil
	})
	var route bridgeipc.Route
	for _, opt := range routeOptions(binding.Route, nil) {
		opt(&route)
	}
	if route.Target == nil {
		t.Fatal("a targeted route has no target option")
	}
	if got := route.Target(targetedRequest{Pool: "tank"}); len(got) != 1 || got[0] != "tank" {
		t.Fatalf("target = %v, want [tank]", got)
	}
	if got := route.Target("not a request"); got != nil {
		t.Fatalf("target of a foreign request = %v, want nil", got)
	}
}
