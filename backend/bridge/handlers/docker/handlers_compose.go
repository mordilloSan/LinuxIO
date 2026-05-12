package docker

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func (h dockerHandlers) handleListComposeProjects(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListComposeProjectsWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetComposeProject(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetComposeProjectWithStore(h.username, h.store, projectName)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeUp(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	composePath := ""
	if len(args) >= 2 {
		composePath = args[1]
	}
	result, err := ComposeUpWithStore(h.username, h.store, projectName, composePath)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeDown(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ComposeDownWithStore(h.username, h.store, projectName)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeStop(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ComposeStopWithStore(h.username, h.store, projectName)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeRestart(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ComposeRestartWithStore(h.username, h.store, projectName)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteStack(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	options := DeleteStackOptions{
		DeleteFile:      len(args) >= 2 && args[1] == "true",
		DeleteDirectory: len(args) >= 3 && args[2] == "true",
	}
	result, err := DeleteStackWithStore(h.username, h.store, projectName, options)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetDockerFolders(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetDockerFoldersWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateCompose(ctx context.Context, args []string, emit ipc.Events) error {
	content, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ValidateComposeFile(content)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleNormalizeCompose(ctx context.Context, args []string, emit ipc.Events) error {
	content, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	normalized, err := NormalizeComposeFile(content)
	return rpc.EmitResult(emit, map[string]string{"content": normalized}, err)
}

func (h dockerHandlers) handleGetComposeFilePath(ctx context.Context, args []string, emit ipc.Events) error {
	stackName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetComposeFilePathWithStore(h.username, h.store, stackName)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateStackDirectory(ctx context.Context, args []string, emit ipc.Events) error {
	dirPath, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ValidateStackDirectory(dirPath)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReindexDockerFolders(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("reindex_docker_folders requested", "component", "docker", "user", h.username)
	result, err := IndexDockerFoldersWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteComposeStack(ctx context.Context, args []string, emit ipc.Events) error {
	projectName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	if err := DeleteComposeStackWithStore(h.username, h.store, projectName); err != nil {
		return err
	}
	return rpc.EmitResult(emit, map[string]any{
		"success": true,
		"message": "Compose stack deleted successfully",
	}, nil)
}
