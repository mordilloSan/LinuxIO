package docker

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

// ComposeTaskMessage represents a message emitted by a Docker compose task.
type ComposeTaskMessage struct {
	Type     string           `json:"type"`    // "stdout", "stderr", or "progress".
	Message  string           `json:"message"` // The actual message content (humanized for progress)
	Code     int              `json:"code,omitzero"`
	Progress *ComposeProgress `json:"progress,omitzero"` // structured progress for "progress" messages
}

func (m ComposeTaskMessage) ProgressEnvelope() bridgeipc.TaskProgress {
	phase := m.Type
	message := m.Message
	var percentage *int
	if m.Progress != nil {
		value := m.Progress.Percent
		percentage = &value
		if message == "" {
			message = m.Progress.Text
		}
	}
	return bridgeipc.TaskProgress{
		Percentage: percentage,
		Phase:      phase,
		Message:    message,
		Detail:     m,
	}
}

// ComposeTaskResult is the terminal payload returned by a successful compose task.
type ComposeTaskResult struct {
	Type    string `json:"type"` // "complete".
	Message string `json:"message"`
}

// ComposeProgress is a single structured progress event parsed from
// `docker compose --progress=json`; the engine owns the type because it also
// runs the Compose-managed container updates.
type ComposeProgress = dockerupdate.ComposeProgress

const dockerUpdateRoute = "docker.update_container"

// updateContainer is the update engine; tests replace it.
var updateContainer = dockerupdate.UpdateContainerWait

var dockerTaskRoutes = dockerTaskBindings(runtime.Runtime{}).Routes()

func dockerTaskBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Mutation(dockerUpdateRoute, handleUpdateContainer, apischema.Privileged(), apischema.Progress[apischema.DockerContainerUpdateProgress]()),
		apischema.TaskRunner[apischema.DockerDaemonConfig, apischema.DockerDaemonConfigSetResult]("docker.apply_daemon_storage", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[apischema.DockerDaemonStorageProgress](), apischema.WithTaskMetadata(func(apischema.DockerDaemonConfig) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Label: "Docker storage change"}
		})).Run(runApplyDaemonStorageTask, bridgeipc.TaskExclusive),
		apischema.TaskRunner[apischema.DockerComposeRequest, ComposeTaskResult]("docker.compose", apischema.SessionTask(), apischema.WithTaskProgress[ComposeTaskMessage](), apischema.WithTaskMetadata(func(req apischema.DockerComposeRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.ProjectName}, Label: "Docker compose " + req.Action, Action: req.Action, ProjectName: req.ProjectName}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.DockerComposeRequest) (ComposeTaskResult, error) {
				return runDockerComposeTask(ctx, task, rt.Username(), rt.Store, req)
			},
			bridgeipc.TaskDefault,
		),
	)
}

func RegisterTaskRoutes(router *bridgeipc.Router, rt runtime.Runtime) {
	dockerTaskBindings(rt).Register(router)
}

func handleUpdateContainer(ctx context.Context, req apischema.DockerContainerUpdateRequest) (apischema.DockerContainerUpdateResult, error) {
	if err := dockerupdate.ValidateTarget(req.ContainerID); err != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	// A job queues behind another update: lock wait 0 waits until ctx ends.
	result, err := updateContainer(ctx, req.ContainerID, 0, func(phase, message string) {
		bridgeipc.ReportProgress(ctx, apischema.DockerContainerUpdateProgress{Phase: phase, Message: message}.ProgressEnvelope())
	})
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	if result.Updated {
		slog.Info("updated Docker container", "component", "docker", "subsystem", "update",
			"container", result.ContainerName, "old_image", result.PreviousImageID, "new_image", result.NewImageID)
	}
	return result, nil
}

func runDockerComposeTask(ctx context.Context, task *bridgeipc.Task, username string, store *userconfig.UserStore, req apischema.DockerComposeRequest) (ComposeTaskResult, error) {
	if req.Action == "" || req.ProjectName == "" {
		return ComposeTaskResult{}, fmt.Errorf("%w: missing required arguments: action, projectName", bridgeipc.ErrInvalidArgs)
	}

	var composePath string
	if req.ComposePath != nil {
		composePath = *req.ComposePath
	}

	configFile, workingDir, err := resolveComposeTaskPaths(ctx, username, store, req.ProjectName, composePath)
	if err != nil {
		return ComposeTaskResult{}, fmt.Errorf("%w: compose file: %w", bridgeipc.ErrNotFound, err)
	}

	var reportMu sync.Mutex
	report := func(msgType, message string, progress *ComposeProgress) {
		if strings.TrimSpace(message) == "" && progress == nil {
			return
		}
		reportMu.Lock()
		msg := ComposeTaskMessage{Type: msgType, Message: message, Progress: progress}
		// Compose output is non-terminal streaming progress. The runner's return
		// value or error is the task's single authoritative terminal signal.
		task.ReportTransientProgress(msg.ProgressEnvelope())
		reportMu.Unlock()
	}

	switch req.Action {
	case "up":
		err = composeUp(ctx, req.ProjectName, configFile, workingDir, false, report)
	case "down":
		err = composeDown(ctx, req.ProjectName, configFile, workingDir, false, report)
	case "stop":
		err = composeStop(ctx, req.ProjectName, configFile, workingDir, report)
	case "restart":
		err = composeUp(ctx, req.ProjectName, configFile, workingDir, true, report)
	default:
		return ComposeTaskResult{}, fmt.Errorf("%w: unsupported action %q", bridgeipc.ErrInvalidArgs, req.Action)
	}

	if err != nil {
		if ctx.Err() != nil {
			return ComposeTaskResult{}, context.Canceled
		}
		return ComposeTaskResult{}, fmt.Errorf("command failed: %w", err)
	}
	if req.Action == "up" || req.Action == "restart" {
		notifyIndexerForComposeFile(ctx, configFile)
	}

	result := ComposeTaskResult{Type: "complete", Message: "operation completed successfully"}
	return result, nil
}

func resolveComposeTaskPaths(ctx context.Context, username string, store *userconfig.UserStore, projectName, composePath string) (string, string, error) {
	if composePath != "" {
		return composePath, filepath.Dir(composePath), nil
	}
	return findComposeFile(ctx, username, store, projectName)
}
