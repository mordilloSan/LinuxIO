package filebrowser

import (
	"encoding/json/v2"
	"net"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

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
