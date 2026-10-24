package filebrowser

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// The client opens its data stream as soon as the start reply lands, so the
// archive task must register its transfer before anything slow: the
// heavy-archive slot and the size walk both outlast transferAttachWaitTimeout
// on a large folder, and registering after them handed the client a bogus
// "transfer task not ready" 404 while the task archived on regardless.
// Occupying the single heavy-archive slot reproduces that delay exactly.
func TestArchiveTaskRegistersTransferBeforeSlowStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	release, err := heavyArchiveLimiter.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("occupy heavy archive slot: %v", err)
	}
	defer release()

	registry := bridgeipc.NewTaskService()
	req := apischema.FileArchiveRequest{Format: "zip", Paths: []string{dir}}
	task, _, err := registry.ClaimForOwnerWithIdentity(routeArchive, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runArchiveTask(ctx, j, nil, req)
	})

	entry, ok := waitForFileTransferTask(context.Background(), task.ID())
	if !ok {
		t.Fatal("archive transfer not registered while the task waited for the heavy-archive slot")
	}
	transfer, ok := entry.(*archiveTransferTask)
	if !ok {
		t.Fatalf("registered transfer type = %T, want *archiveTransferTask", entry)
	}

	// Cancelling while the task is still blocked on the slot must also release
	// a client already parked in attach() on <-ready, rather than stranding it.
	task.Cancel()
	select {
	case <-transfer.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("ready gate never resolved after the task was cancelled")
	}
	if transfer.readyErr == nil {
		t.Fatal("ready gate resolved without an error after cancellation")
	}

	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgeipc.TaskStateCanceled {
		t.Fatalf("task state = %q, want canceled", snapshot.State)
	}
}

func TestArchiveDownloadProgressIncludesArtifactName(t *testing.T) {
	registry := bridgeipc.NewTaskService()
	task, _, err := registry.ClaimForOwnerWithIdentity(routeArchive, apischema.FileArchiveRequest{
		Format: "zip",
		Paths:  []string{"/photos"},
	}, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	transfer := &archiveTransferTask{
		transferLifecycle: &transferLifecycle{task: task},
		archiveName:       "photos.zip",
		archiveSize:       42,
	}
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		transfer.writeProgress(server, "streaming")
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var progress downloadStreamProgress
	if err := json.Unmarshal(frame.Payload, &progress); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if progress.FileName != "photos.zip" || progress.Total != 42 {
		t.Fatalf("progress = %+v, want filename photos.zip and total 42", progress)
	}
	<-done
}
