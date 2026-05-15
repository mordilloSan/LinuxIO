package docker

import (
	"context"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListComposeProjects(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListComposeProjectsWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetComposeProject(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetComposeProjectWithStore(ctx, h.username, h.store, projectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeUp(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	composePath := ""
	if len(args) >= 2 {
		composePath = args[1]
	}
	result, err := ComposeUpWithStore(ctx, h.username, h.store, projectName, composePath)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeDown(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ComposeDownWithStore(ctx, h.username, h.store, projectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeStop(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ComposeStopWithStore(ctx, h.username, h.store, projectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleComposeRestart(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ComposeRestartWithStore(ctx, h.username, h.store, projectName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteStack(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	options := DeleteStackOptions{
		DeleteFile:      len(args) >= 2 && args[1] == "true",
		DeleteDirectory: len(args) >= 3 && args[2] == "true",
	}
	result, err := DeleteStackWithStore(ctx, h.username, h.store, projectName, options)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleGetDockerFolders(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetDockerFoldersWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateCompose(ctx context.Context, args []string, emit bridgeipc.Events) error {
	content, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ValidateComposeFile(ctx, content)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleNormalizeCompose(ctx context.Context, args []string, emit bridgeipc.Events) error {
	content, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	normalized, err := NormalizeComposeFile(ctx, content)
	return bridgeipc.EmitResult(emit, map[string]string{"content": normalized}, err)
}

func (h dockerHandlers) handleGetComposeFilePath(ctx context.Context, args []string, emit bridgeipc.Events) error {
	stackName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetComposeFilePathWithStore(ctx, h.username, h.store, stackName)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleValidateStackDirectory(ctx context.Context, args []string, emit bridgeipc.Events) error {
	dirPath, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := ValidateStackDirectory(ctx, dirPath)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReindexDockerFolders(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := IndexDockerFoldersWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteComposeStack(ctx context.Context, args []string, emit bridgeipc.Events) error {
	projectName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	if err := DeleteComposeStackWithStore(ctx, h.username, h.store, projectName); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{
		"success": true,
		"message": "Compose stack deleted successfully",
	}, nil)
}
