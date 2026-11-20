package docker

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
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

func TestUpdateContainerMutationRejectsAnEmptyContainerID(t *testing.T) {
	original := updateContainer
	t.Cleanup(func() { updateContainer = original })
	updateContainer = func(context.Context, string, time.Duration, func(phase, message string)) (apischema.DockerContainerUpdateResult, error) {
		t.Error("updateContainer ran for an empty container ID")
		return apischema.DockerContainerUpdateResult{}, nil
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
			RawRequest: jsontext.Value(`{"containerId":""}`),
			ID:         "00000000-0000-4000-8000-0000000000ab",
			Session:    &session.Session{SessionID: "session", Privileged: true, User: session.User{Username: "root"}},
		})
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		frame, err := relay.ReadRelayFrame(client)
		if err != nil {
			t.Fatalf("ReadRelayFrame: %v", err)
		}
		if frame.Opcode != relay.OpStreamResult {
			continue // the task snapshot
		}
		var result relay.ResultFrame
		if err := json.Unmarshal(frame.Payload, &result); err != nil || result.Status != "error" || result.Code != 400 {
			t.Fatalf("result = %s (%v), want a 400 error", frame.Payload, err)
		}
		break
	}
	_ = client.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch = %v, want nil: the 400 is a result frame", err)
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

func TestUpdateContainerMutationRebuildsOnRequest(t *testing.T) {
	originalUpdate, originalRebuild := updateContainer, rebuildContainer
	t.Cleanup(func() { updateContainer, rebuildContainer = originalUpdate, originalRebuild })
	updateContainer = func(context.Context, string, time.Duration, func(phase, message string)) (apischema.DockerContainerUpdateResult, error) {
		t.Error("a rebuild request ran the update")
		return apischema.DockerContainerUpdateResult{}, nil
	}
	for name, tc := range map[string]struct {
		rebuildErr error
		wantStatus string
		wantCode   int
	}{
		"a built service":           {wantStatus: "ok"},
		"a container not built":     {rebuildErr: fmt.Errorf("rebuild container %q: %w: Docker Compose does not manage it", "web", dockerupdate.ErrNotRebuildable), wantStatus: "error", wantCode: 409},
		"a rebuild that fails late": {rebuildErr: errors.New("build Compose project \"media\" service \"web\": exit status 1"), wantStatus: "error", wantCode: 500},
	} {
		t.Run(name, func(t *testing.T) {
			rebuilt := ""
			rebuildContainer = func(_ context.Context, containerID string, _ time.Duration, _ func(phase, message string)) (apischema.DockerContainerUpdateResult, error) {
				rebuilt = containerID
				return apischema.DockerContainerUpdateResult{ContainerID: containerID, ContainerName: "web", Updated: true}, tc.rebuildErr
			}

			result, _ := dispatchUpdateMutation(t, bridgeipc.NewTaskService(), `{"containerId":"web","rebuild":true}`)
			if rebuilt != "web" || result.Status != tc.wantStatus || (tc.wantCode != 0 && result.Code != tc.wantCode) {
				t.Fatalf("rebuilt %q, result = %+v (%s); want %s %d", rebuilt, result, result.Data, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

// dispatchUpdateMutation runs one docker.update_container request on a
// router over tasks and returns its result frame and the phase of each
// progress frame the caller received.
func dispatchUpdateMutation(t *testing.T, tasks *bridgeipc.TaskService, request string) (relay.ResultFrame, []string) {
	t.Helper()
	router := bridgeipc.NewRouter(tasks)
	RegisterTaskRoutes(router, runtime.Runtime{})
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, bridgeipc.Request{
			Route:      dockerUpdateRoute,
			RawRequest: jsontext.Value(request),
			ID:         "00000000-0000-4000-8000-0000000000ac",
			Session:    &session.Session{SessionID: "session", Privileged: true, User: session.User{Username: "root"}},
		})
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var result relay.ResultFrame
	var phases []string
	for {
		frame, err := relay.ReadRelayFrame(client)
		if err != nil {
			t.Fatalf("ReadRelayFrame: %v", err)
		}
		if frame.Opcode == relay.OpStreamProgress {
			var progress bridgeipc.TaskProgress
			if json.Unmarshal(frame.Payload, &progress) == nil && progress.Phase != "" {
				phases = append(phases, progress.Phase)
			}
			continue
		}
		if frame.Opcode != relay.OpStreamResult {
			continue
		}
		if err := json.Unmarshal(frame.Payload, &result); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", frame.Payload, err)
		}
		break
	}
	_ = client.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch = %v, want nil: an error is a result frame", err)
	}
	return result, phases
}

// The build's output reaches the caller that follows the job, but is not the
// job's stored progress, nor sent to every session's task list.
func TestUpdateContainerMutationSendsBuildOutputOnlyToItsCaller(t *testing.T) {
	original := rebuildContainer
	t.Cleanup(func() { rebuildContainer = original })
	rebuildContainer = func(_ context.Context, containerID string, _ time.Duration, report func(phase, message string)) (apischema.DockerContainerUpdateResult, error) {
		report("building", "Building the image of Compose service web with fresh base images")
		report(dockerupdate.ProgressPhaseBuildOutput, "#1 [web 1/2] FROM alpine:3.20")
		report(dockerupdate.ProgressPhaseBuildOutput, "#2 DONE 0.1s")
		report("current", "The rebuilt image is the one the container already runs")
		return apischema.DockerContainerUpdateResult{ContainerID: containerID, ContainerName: "web"}, nil
	}
	tasks := bridgeipc.NewTaskService()
	broadcast, unsubscribe := tasks.Subscribe(64)
	t.Cleanup(unsubscribe)

	result, phases := dispatchUpdateMutation(t, tasks, `{"containerId":"web","rebuild":true}`)
	if result.Status != "ok" {
		t.Fatalf("result = %+v (%s), want ok", result, result.Data)
	}
	want := []string{"building", dockerupdate.ProgressPhaseBuildOutput, dockerupdate.ProgressPhaseBuildOutput, "current"}
	if !slices.Equal(phases, want) {
		t.Fatalf("caller's progress phases = %v, want %v", phases, want)
	}
	var stored []string
	for drained := false; !drained; {
		select {
		case event := <-broadcast:
			if progress, ok := event.Progress.(bridgeipc.TaskProgress); ok && event.Type == bridgeipc.TaskEventProgress {
				stored = append(stored, progress.Phase)
			}
		default:
			drained = true
		}
	}
	if !slices.Equal(stored, []string{"building", "current"}) {
		t.Fatalf("progress sent to every session = %v, want the phases without the build output", stored)
	}
}
