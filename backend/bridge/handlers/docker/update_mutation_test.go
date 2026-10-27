package docker

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestUpdateContainerMutationReportsProgressAndResult(t *testing.T) {
	want := apischema.DockerContainerUpdateResult{ContainerID: "web", ContainerName: "web", Image: "nginx:latest", Updated: true}
	proceed := make(chan struct{})
	original := updateContainer
	t.Cleanup(func() { updateContainer = original })
	updateContainer = func(ctx context.Context, containerID string, lockWait time.Duration, report func(phase, message string)) (apischema.DockerContainerUpdateResult, error) {
		if containerID != "web" {
			t.Errorf("containerID = %q, want web", containerID)
		}
		if lockWait != 0 {
			t.Errorf("lockWait = %v, want 0 (wait until the job's context ends)", lockWait)
		}
		select {
		case <-proceed:
		case <-ctx.Done():
			return apischema.DockerContainerUpdateResult{}, ctx.Err()
		}
		report("pulling", "Pulling nginx:latest")
		report("replacing", "Replacing web")
		return want, nil
	}

	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	RegisterTaskRoutes(router, runtime.Runtime{})
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, bridgeipc.Request{
			Route:      dockerUpdateRoute,
			RawRequest: jsontext.Value(`{"containerId":"web"}`),
			ID:         "00000000-0000-4000-8000-0000000000aa",
			Session:    &session.Session{SessionID: "session", Privileged: true, User: session.User{Username: "root"}},
		})
	}()

	var event struct {
		Type bridgeipc.TaskEventType `json:"type"`
		Task bridgeipc.TaskSnapshot  `json:"task"`
	}
	readMutationFrame(t, client, relay.OpStreamProgress, &event)
	if event.Type != bridgeipc.TaskEventSnapshot || event.Task.Type != dockerUpdateRoute {
		t.Fatalf("first frame = %#v, want the %s snapshot", event, dockerUpdateRoute)
	}
	if event.Task.Metadata == nil || !slices.Equal(event.Task.Metadata.Identity, []string{"web"}) {
		t.Fatalf("snapshot metadata = %#v, want the container as target", event.Task.Metadata)
	}
	close(proceed)

	for _, phase := range []string{"pulling", "replacing"} {
		var progress bridgeipc.TaskProgress
		readMutationFrame(t, client, relay.OpStreamProgress, &progress)
		if progress.Phase != phase || progress.Message == "" {
			t.Fatalf("progress = %#v, want phase %q with a message", progress, phase)
		}
	}

	var result relay.ResultFrame
	readMutationFrame(t, client, relay.OpStreamResult, &result)
	var got apischema.DockerContainerUpdateResult
	if err := json.Unmarshal(result.Data, &got); err != nil || result.Status != "ok" || got != want {
		t.Fatalf("result = %#v (%s, %v), want ok with %#v", result, result.Data, err, want)
	}
	if frame, err := relay.ReadRelayFrame(client); err != nil || frame.Opcode != relay.OpStreamClose {
		t.Fatalf("frame after result = %#v, %v; want close", frame, err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func readMutationFrame(t *testing.T, conn net.Conn, opcode byte, into any) {
	t.Helper()
	frame, err := relay.ReadRelayFrame(conn)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	if frame.Opcode != opcode {
		t.Fatalf("opcode = 0x%02x, want 0x%02x (payload %s)", frame.Opcode, opcode, frame.Payload)
	}
	if err := json.Unmarshal(frame.Payload, into); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", frame.Payload, err)
	}
}
