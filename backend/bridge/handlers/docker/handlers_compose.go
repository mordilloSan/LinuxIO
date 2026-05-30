package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListComposeProjects(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListComposeProjectsWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetComposeProject(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := GetComposeProjectWithStore(ctx, h.username, h.store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeUp(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeUpWithStore(ctx, h.username, h.store, req.ProjectName, "")
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeDown(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeDownWithStore(ctx, h.username, h.store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeStop(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeStopWithStore(ctx, h.username, h.store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeRestart(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeRestartWithStore(ctx, h.username, h.store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteStack(ctx context.Context, req apischema.DeleteStackRequest, emit bridgeipc.Events) error {
	options := DeleteStackOptions{
		DeleteFile:      req.DeleteFile,
		DeleteDirectory: req.DeleteDirectory,
	}
	result, err := DeleteStackWithStore(ctx, h.username, h.store, req.ProjectName, options)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetDockerFolders(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetDockerFoldersWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateCompose(ctx context.Context, req apischema.ContentRequest, emit bridgeipc.Events) error {
	result, err := ValidateComposeFile(ctx, req.Content)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleNormalizeCompose(ctx context.Context, req apischema.ContentRequest, emit bridgeipc.Events) error {
	normalized, err := NormalizeComposeFile(ctx, req.Content)
	return bridgeipc.EmitResult(emit, map[string]string{"content": normalized}, err)
}

func (h dockerHandlers) handleGetComposeFilePath(ctx context.Context, req apischema.StackNameRequest, emit bridgeipc.Events) error {
	result, err := GetComposeFilePathWithStore(ctx, h.username, h.store, req.StackName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateStackDirectory(ctx context.Context, req apischema.DirPathRequest, emit bridgeipc.Events) error {
	result, err := ValidateStackDirectory(ctx, req.DirPath)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReindexDockerFolders(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := IndexDockerFoldersWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteComposeStack(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	if err := DeleteComposeStackWithStore(ctx, h.username, h.store, req.ProjectName); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{
		"success": true,
		"message": "Compose stack deleted successfully",
	}, nil)
}
