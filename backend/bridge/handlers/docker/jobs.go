package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

const (
	JobTypeDockerCompose = "docker.compose"
	JobTypeDockerIndexer = "docker.indexer"
)

// ComposeJobMessage represents a message emitted by a Docker compose job.
type ComposeJobMessage struct {
	Type    string `json:"type"`    // "stdout", "stderr", "error", "complete"
	Message string `json:"message"` // The actual message content
	Code    int    `json:"code,omitempty"`
}

type DockerIndexerJobResult struct {
	Path         string                  `json:"path"`
	FilesIndexed int64                   `json:"files_indexed"`
	DirsIndexed  int64                   `json:"dirs_indexed"`
	TotalSize    int64                   `json:"total_size"`
	DurationMs   int64                   `json:"duration_ms"`
	Folders      []indexer.IndexerResult `json:"folders"`
}

func RegisterJobRunners(username string) {
	bridgejobs.RegisterRunner(JobTypeDockerCompose, func(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
		return runDockerComposeJob(ctx, job, username, args)
	})
	bridgejobs.RegisterRunner(JobTypeDockerIndexer, func(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
		return runDockerIndexerJob(ctx, job, username)
	})
}

func runDockerComposeJob(ctx context.Context, job *bridgejobs.Job, username string, args []string) (any, error) {
	if len(args) < 2 {
		return nil, bridgejobs.NewError("missing required arguments: action, projectName", 400)
	}

	action := args[0]
	projectName := args[1]
	var composePath string
	if len(args) >= 3 {
		composePath = args[2]
	}
	slog.Info("docker compose job requested", "component", "docker", "job_type", JobTypeDockerCompose, "action", action, "service", projectName, "path", composePath, "user", username)

	configFile, workingDir, err := resolveComposeJobPaths(username, projectName, composePath)
	if err != nil {
		job.ReportProgress(ComposeJobMessage{Type: "error", Message: "compose file not found: " + err.Error()})
		return nil, bridgejobs.NewError("compose file not found: "+err.Error(), 404)
	}

	var reportMu sync.Mutex
	report := func(msgType, message string) {
		if strings.TrimSpace(message) == "" {
			return
		}
		reportMu.Lock()
		job.ReportProgress(ComposeJobMessage{Type: msgType, Message: message})
		reportMu.Unlock()
	}

	switch action {
	case "up":
		err = composeUpWithSDK(ctx, projectName, configFile, workingDir, false, report)
	case "down":
		err = composeDownWithSDK(ctx, projectName, configFile, workingDir, false, report)
	case "stop":
		err = composeStopWithSDK(ctx, projectName, configFile, workingDir, report)
	case "restart":
		err = composeUpWithSDK(ctx, projectName, configFile, workingDir, true, report)
	default:
		return nil, bridgejobs.NewError("unsupported action: "+action, 400)
	}

	if err != nil {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		msg := "command failed: " + err.Error()
		report("error", msg)
		return nil, bridgejobs.NewError(msg, 500)
	}

	result := ComposeJobMessage{Type: "complete", Message: "operation completed successfully"}
	job.ReportProgress(result)
	return result, nil
}

func resolveComposeJobPaths(username, projectName, composePath string) (string, string, error) {
	if composePath != "" {
		return composePath, filepath.Dir(composePath), nil
	}
	return findComposeFile(username, projectName)
}

func runDockerIndexerJob(ctx context.Context, job *bridgejobs.Job, username string) (any, error) {
	dockerFolders, err := configuredDockerFolders(username)
	if err != nil {
		return nil, bridgejobs.NewError("failed to load user config", 500)
	}

	aggregate := DockerIndexerJobResult{
		Path:    strings.Join(dockerFolders, ", "),
		Folders: make([]indexer.IndexerResult, 0, len(dockerFolders)),
	}
	if len(dockerFolders) > 1 {
		aggregate.Path = fmt.Sprintf("%d Docker folders", len(dockerFolders))
	}

	for _, dockerFolder := range dockerFolders {
		result, err := runDockerIndexerOperation(ctx, job, dockerFolder, false)
		if err != nil {
			return nil, err
		}

		if indexResult, ok := result.(indexer.IndexerResult); ok {
			aggregate.FilesIndexed += indexResult.FilesIndexed
			aggregate.DirsIndexed += indexResult.DirsIndexed
			aggregate.TotalSize += indexResult.TotalSize
			aggregate.DurationMs += indexResult.DurationMs
			aggregate.Folders = append(aggregate.Folders, indexResult)
		}
	}

	return aggregate, nil
}

func runDockerIndexerOperation(ctx context.Context, job *bridgejobs.Job, path string, attachOnly bool) (any, error) {
	var result any
	var jobErr *bridgejobs.Error
	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			job.ReportProgress(p)
			return nil
		},
		OnResult: func(r indexer.IndexerResult) error {
			result = r
			return nil
		},
		OnError: func(msg string, code int) error {
			jobErr = bridgejobs.NewError(msg, code)
			return nil
		},
	}

	var err error
	if attachOnly {
		err = indexer.StreamIndexerAttach(ctx, cb)
	} else {
		err = indexer.StreamIndexer(ctx, path, cb)
		if err != nil && jobErr != nil && jobErr.Code == 409 {
			jobErr = nil
			err = indexer.StreamIndexerAttach(ctx, cb)
		}
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ipc.ErrAborted) {
			return nil, context.Canceled
		}
		if jobErr != nil {
			return nil, jobErr
		}
		return nil, fmt.Errorf("docker indexer failed: %w", err)
	}

	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}
