package docker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
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

func RegisterJobRoutes(router *bridgejobs.Router, rt runtime.Runtime) {
	username := rt.Username()
	store := rt.Store
	apischema.AttachRunner(router, RouteCompose.Run(func(ctx context.Context, job *bridgejobs.Job, req apischema.DockerComposeRequest) (any, error) {
		return runDockerComposeJob(ctx, job, username, store, req)
	}, bridgejobs.ActionDefault))
	apischema.AttachRunner(router, RouteIndexer.Run(func(ctx context.Context, job *bridgejobs.Job, _ bridgejobs.NoRequest) (any, error) {
		return runDockerIndexerJob(ctx, job, username, store)
	}, bridgejobs.SingletonSystem))
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
		job.ReportProgress(ComposeJobMessage{Type: "error", Message: "compose file not found: " + err.Error()})
		return nil, bridgejobs.NewError("compose file not found: "+err.Error(), 404)
	}

	var reportMu sync.Mutex
	report := func(msgType, message string) {
		if strings.TrimSpace(message) == "" {
			return
		}
		reportMu.Lock()
		msg := ComposeJobMessage{Type: msgType, Message: message}
		if msgType == "stdout" || msgType == "stderr" {
			job.ReportTransientProgress(msg)
		} else {
			job.ReportProgress(msg)
		}
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
		report("error", msg)
		return nil, bridgejobs.NewError(msg, 500)
	}

	result := ComposeJobMessage{Type: "complete", Message: "operation completed successfully"}
	job.ReportProgress(result)
	return result, nil
}

func resolveComposeJobPaths(ctx context.Context, username string, store *config.UserStore, projectName, composePath string) (string, string, error) {
	if composePath != "" {
		return composePath, filepath.Dir(composePath), nil
	}
	return findComposeFile(ctx, username, store, projectName)
}

func runDockerIndexerJob(ctx context.Context, job *bridgejobs.Job, username string, store *config.UserStore) (any, error) {
	dockerFolders, err := configuredDockerFolders(ctx, username, store)
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
