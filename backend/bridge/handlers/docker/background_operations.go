package docker

import (
	"context"
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
	Progress *ComposeProgress `json:"progress,omitempty"` // structured progress for "progress" messages
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

var dockerTaskRoutes = dockerTaskBindings(runtime.Runtime{}).Routes()

func dockerTaskBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.DockerContainerUpdateRequest, apischema.DockerContainerUpdateResult](
			dockerUpdateRoute,
			apischema.DurableTask(),
			apischema.WithTaskProgress[apischema.DockerContainerUpdateProgress](),
			apischema.WithTaskIdentity(dockerUpdateTaskIdentity),
			apischema.WithTaskMetadata(func(req apischema.DockerContainerUpdateRequest) bridgeipc.TaskMetadata {
				return bridgeipc.TaskMetadata{Identity: []string{req.ContainerID}, Label: "Docker container update"}
			}),
		).Run(runDockerUpdateTask, bridgeipc.TaskSingletonSystem),
		apischema.TaskRunner[apischema.DockerComposeRequest, ComposeTaskResult]("docker.compose", apischema.SessionTask(), apischema.WithTaskProgress[ComposeTaskMessage](), apischema.WithTaskMetadata(func(req apischema.DockerComposeRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Action, req.ProjectName}, Label: "Docker compose " + req.Action, Action: req.Action, ProjectName: req.ProjectName}
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

func runDockerComposeTask(ctx context.Context, task *bridgeipc.Task, username string, store *userconfig.UserStore, req apischema.DockerComposeRequest) (ComposeTaskResult, error) {
	if req.Action == "" || req.ProjectName == "" {
		return ComposeTaskResult{}, bridgeipc.NewError("missing required arguments: action, projectName", 400)
	}

	var composePath string
	if req.ComposePath != nil {
		composePath = *req.ComposePath
	}

	configFile, workingDir, err := resolveComposeTaskPaths(ctx, username, store, req.ProjectName, composePath)
	if err != nil {
		return ComposeTaskResult{}, bridgeipc.NewError("compose file not found: "+err.Error(), 404)
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
		return ComposeTaskResult{}, bridgeipc.NewError("unsupported action: "+req.Action, 400)
	}

	if err != nil {
		if ctx.Err() != nil {
			return ComposeTaskResult{}, context.Canceled
		}
		msg := "command failed: " + err.Error()
		return ComposeTaskResult{}, bridgeipc.NewError(msg, 500)
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
