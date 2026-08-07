package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRunDockerComposeJobUsesResultAsOnlyTerminalSignal(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		installFakeDocker(t, "exit 0\n")
		job := newComposeTestJob(t)
		composePath := filepath.Join(t.TempDir(), "compose.yml")

		result, err := runDockerComposeJob(context.Background(), job, "", nil, apischema.DockerComposeRequest{
			Action:      "up",
			ProjectName: "test-stack",
			ComposePath: &composePath,
		})
		if err != nil {
			t.Fatalf("runDockerComposeJob() error = %v", err)
		}
		message, ok := result.(ComposeJobResult)
		if !ok || message.Type != "complete" {
			t.Fatalf("runDockerComposeJob() result = %#v, want complete ComposeJobResult", result)
		}
		if progress := job.Snapshot().Progress; progress != nil {
			t.Fatalf("job progress = %#v, want no terminal progress", progress)
		}
		if replay := composeProgressReplay(job); len(replay) != 0 {
			t.Fatalf("progress replay = %#v, want no terminal progress", replay)
		}
	})

	t.Run("failure", func(t *testing.T) {
		installFakeDocker(t, "printf '%s\\n' 'compose failed'\nexit 1\n")
		job := newComposeTestJob(t)
		composePath := filepath.Join(t.TempDir(), "compose.yml")

		result, err := runDockerComposeJob(context.Background(), job, "", nil, apischema.DockerComposeRequest{
			Action:      "up",
			ProjectName: "test-stack",
			ComposePath: &composePath,
		})
		if err == nil {
			t.Fatal("runDockerComposeJob() error = nil, want command failure")
		}
		if result != nil {
			t.Fatalf("runDockerComposeJob() result = %#v, want nil", result)
		}
		if progress := job.Snapshot().Progress; progress != nil {
			t.Fatalf("job progress = %#v, want no terminal progress", progress)
		}

		replay := composeProgressReplay(job)
		if len(replay) != 1 {
			t.Fatalf("progress replay len = %d, want one stderr frame: %#v", len(replay), replay)
		}
		message, ok := replay[0].Progress.(ComposeJobMessage)
		if !ok || message.Type != "stderr" {
			t.Fatalf("progress replay = %#v, want one non-terminal stderr frame", replay)
		}
	})
}

func installFakeDocker(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "docker")
	if err := os.WriteFile(dockerPath, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir)
}

func newComposeTestJob(t *testing.T) *bridgejobs.Job {
	t.Helper()
	job, err := bridgejobs.NewRegistry().Create("docker.compose", nil)
	if err != nil {
		t.Fatalf("create compose job: %v", err)
	}
	return job
}

func composeProgressReplay(job *bridgejobs.Job) []bridgejobs.Event {
	_, replay, unsubscribe := job.SubscribeWithReplay(8)
	unsubscribe()
	return replay
}
