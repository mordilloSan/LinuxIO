package docker

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// ComposeJobMessage represents a message emitted by a Docker compose job.
type ComposeJobMessage struct {
	Type     string           `json:"type"`    // "stdout", "stderr", or "progress".
	Message  string           `json:"message"` // The actual message content (humanized for progress)
	Code     int              `json:"code,omitempty"`
	Progress *ComposeProgress `json:"progress,omitempty"` // structured progress for "progress" messages
}

// ComposeJobResult is the terminal payload returned by a successful compose job.
type ComposeJobResult struct {
	Type    string `json:"type"` // "complete".
	Message string `json:"message"`
}

// ComposeProgress is a single structured progress event parsed from
// `docker compose --progress=json`. The JSON tags mirror Docker's own event
// schema so the same struct is used to both decode Docker's output and to
// encode the payload sent to the frontend.
type ComposeProgress struct {
	ID       string `json:"id"` // layer id (e.g. "fbcfea79c1c4") or group (e.g. "Image alpine:3.17")
	ParentID string `json:"parent_id,omitempty"`
	Text     string `json:"text"`              // "Pulling", "Downloading", "Extracting", "Pull complete", "Creating", "Started"…
	Status   string `json:"status"`            // "Working" | "Done" | "Error"
	Details  string `json:"details,omitempty"` // Docker's humanized current (e.g. "2.097MB")
	Current  int64  `json:"current,omitempty"`
	Total    int64  `json:"total,omitempty"`
	Percent  int    `json:"percent,omitempty"`
}

var dockerJobRoutes = dockerJobBindings(runtime.Runtime{}).Routes()

func dockerJobBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Runner[apischema.DockerComposeRequest, ComposeJobResult]("docker.compose", apischema.WithJobProgress[ComposeJobMessage](), apischema.WithJobMetadata(func(req apischema.DockerComposeRequest) bridgejobs.JobMetadata {
			return bridgejobs.JobMetadata{Identity: []string{req.Action, req.ProjectName}, Label: "Docker compose " + req.Action, Action: req.Action, ProjectName: req.ProjectName}
		})).Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.DockerComposeRequest) (any, error) {
				return runDockerComposeJob(ctx, job, rt.Username(), rt.Store, req)
			},
			bridgejobs.ActionDefault,
		),
	)
}

func RegisterJobRoutes(router *bridgejobs.Router, rt runtime.Runtime) {
	dockerJobBindings(rt).Register(router)
}

func runDockerComposeJob(ctx context.Context, job *bridgejobs.Job, username string, store *config.UserStore, req apischema.DockerComposeRequest) (any, error) {
	if req.Action == "" || req.ProjectName == "" {
		return nil, bridgejobs.NewError("missing required arguments: action, projectName", 400)
	}

	var composePath string
	if req.ComposePath != nil {
		composePath = *req.ComposePath
	}

	configFile, workingDir, err := resolveComposeJobPaths(ctx, username, store, req.ProjectName, composePath)
	if err != nil {
		return nil, bridgejobs.NewError("compose file not found: "+err.Error(), 404)
	}

	var reportMu sync.Mutex
	report := func(msgType, message string, progress *ComposeProgress) {
		if strings.TrimSpace(message) == "" && progress == nil {
			return
		}
		reportMu.Lock()
		msg := ComposeJobMessage{Type: msgType, Message: message, Progress: progress}
		// Compose output is non-terminal streaming progress. The runner's return
		// value or error is the job's single authoritative terminal signal.
		job.ReportTransientProgress(msg)
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
		return nil, bridgejobs.NewError("unsupported action: "+req.Action, 400)
	}

	if err != nil {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		msg := "command failed: " + err.Error()
		return nil, bridgejobs.NewError(msg, 500)
	}

	result := ComposeJobResult{Type: "complete", Message: "operation completed successfully"}
	return result, nil
}

func resolveComposeJobPaths(ctx context.Context, username string, store *config.UserStore, projectName, composePath string) (string, string, error) {
	if composePath != "" {
		return composePath, filepath.Dir(composePath), nil
	}
	return findComposeFile(ctx, username, store, projectName)
}
