package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListComposeProjects(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListComposeProjects(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetComposeProject(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := GetComposeProject(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeUp(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeUp(ctx, h.rt.Username(), h.rt.Store, req.ProjectName, "")
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeDown(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeDown(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeStop(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeStop(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeRestart(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	result, err := ComposeRestart(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteStack(ctx context.Context, req apischema.DeleteStackRequest, emit bridgeipc.Events) error {
	options := DeleteStackOptions{
		DeleteFile:      req.DeleteFile,
		DeleteDirectory: req.DeleteDirectory,
	}
	result, err := DeleteStack(ctx, h.rt.Username(), h.rt.Store, req.ProjectName, options)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetDockerFolders(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetDockerFolders(ctx, h.rt.Username(), h.rt.Store)
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
	result, err := GetComposeFilePath(ctx, h.rt.Username(), h.rt.Store, req.StackName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateStackDirectory(ctx context.Context, req apischema.DirPathRequest, emit bridgeipc.Events) error {
	result, err := ValidateStackDirectory(ctx, req.DirPath)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReindexDockerFolders(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := IndexDockerFolders(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteComposeStack(ctx context.Context, req apischema.ProjectNameRequest, emit bridgeipc.Events) error {
	if err := DeleteComposeStack(ctx, h.rt.Username(), h.rt.Store, req.ProjectName); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{
		"success": true,
		"message": "Compose stack deleted successfully",
	}, nil)
}
