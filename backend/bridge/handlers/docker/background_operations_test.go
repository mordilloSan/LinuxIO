package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRunDockerComposeTaskUsesResultAsOnlyTerminalSignal(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		installFakeDocker(t, "exit 0\n")
		task := newComposeTestTask(t)
		composePath := filepath.Join(t.TempDir(), "compose.yml")

		result, err := runDockerComposeTask(context.Background(), task, "", nil, apischema.DockerComposeRequest{
			Action:      "up",
			ProjectName: "test-stack",
			ComposePath: &composePath,
		})
		if err != nil {
			t.Fatalf("runDockerComposeTask() error = %v", err)
		}
		if result.Type != "complete" {
			t.Fatalf("runDockerComposeTask() result = %#v, want complete ComposeTaskResult", result)
		}
		if progress := task.Snapshot().Progress; progress != nil {
			t.Fatalf("task progress = %#v, want no terminal progress", progress)
		}
		if replay := composeProgressReplay(task); len(replay) != 0 {
			t.Fatalf("progress replay = %#v, want no terminal progress", replay)
		}
	})

	t.Run("failure", func(t *testing.T) {
		installFakeDocker(t, "printf '%s\\n' 'compose failed'\nexit 1\n")
		task := newComposeTestTask(t)
		composePath := filepath.Join(t.TempDir(), "compose.yml")

		result, err := runDockerComposeTask(context.Background(), task, "", nil, apischema.DockerComposeRequest{
			Action:      "up",
			ProjectName: "test-stack",
			ComposePath: &composePath,
		})
		if err == nil {
			t.Fatal("runDockerComposeTask() error = nil, want command failure")
		}
		if result != (ComposeTaskResult{}) {
			t.Fatalf("runDockerComposeTask() result = %#v, want zero value", result)
		}
		if progress := task.Snapshot().Progress; progress != nil {
			t.Fatalf("task progress = %#v, want no terminal progress", progress)
		}

		replay := composeProgressReplay(task)
		if len(replay) != 1 {
			t.Fatalf("progress replay len = %d, want one stderr frame: %#v", len(replay), replay)
		}
		progress, ok := replay[0].Progress.(bridgetask.TaskProgress)
		message, detailOK := progress.Detail.(ComposeTaskMessage)
		if !ok || !detailOK || message.Type != "stderr" {
			t.Fatalf("progress replay = %#v, want one non-terminal stderr frame", replay)
		}
	})
}

func TestComposeTaskMessageProgressEnvelopeUsesStablePhase(t *testing.T) {
	detail := ComposeTaskMessage{
		Type:     "progress",
		Progress: &ComposeProgress{Percent: 37, Text: "Pulling image layer"},
	}
	progress := detail.ProgressEnvelope()
	if progress.Percentage == nil || *progress.Percentage != 37 {
		t.Fatalf("progress percentage = %#v, want 37", progress.Percentage)
	}
	if progress.Phase != detail.Type || progress.Message != detail.Progress.Text {
		t.Fatalf("progress summary = %#v, want typed phase and progress text", progress)
	}
	if progress.Detail != detail {
		t.Fatalf("progress detail = %#v, want %#v", progress.Detail, detail)
	}
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

func newComposeTestTask(t *testing.T) *bridgetask.Task {
	t.Helper()
	task, err := bridgetask.NewTaskService().Create("docker.compose", nil)
	if err != nil {
		t.Fatalf("create compose task: %v", err)
	}
	return task
}

func composeProgressReplay(task *bridgetask.Task) []bridgetask.TaskEvent {
	_, replay, unsubscribe := task.SubscribeWithReplay(8)
	unsubscribe()
	return replay
}
