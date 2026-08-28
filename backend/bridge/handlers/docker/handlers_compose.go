package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func (h dockerHandlers) handleListComposeProjects(ctx context.Context, _ apischema.NoRequest) ([]*apischema.ComposeProject, error) {
	return ListComposeProjects(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleGetComposeProject(ctx context.Context, req apischema.ProjectNameRequest) (*apischema.ComposeProject, error) {
	return GetComposeProject(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleComposeUp(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeUp(ctx, h.rt.Username(), h.rt.Store, req.ProjectName, "")
}

func (h dockerHandlers) handleComposeDown(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeDown(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleComposeStop(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeStop(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleComposeRestart(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeRestart(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleDeleteStack(ctx context.Context, req apischema.DeleteStackRequest) (apischema.DeleteStackResult, error) {
	options := DeleteStackOptions{
		DeleteFile:      req.DeleteFile,
		DeleteDirectory: req.DeleteDirectory,
	}
	result, err := DeleteStack(ctx, h.rt.Username(), h.rt.Store, req.ProjectName, options)
	return result, err
}

func (h dockerHandlers) handleGetDockerFolders(ctx context.Context, _ apischema.NoRequest) (apischema.DockerFoldersResponse, error) {
	return GetDockerFolders(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleValidateCompose(ctx context.Context, req apischema.ValidateComposeRequest) (apischema.ValidateComposeResponse, error) {
	return ValidateComposeFile(ctx, req.Content, req.EnvContent, req.WorkingDir)
}

func (h dockerHandlers) handleGetComposeFilePath(ctx context.Context, req apischema.StackNameRequest) (apischema.ComposeFilePathResponse, error) {
	return GetComposeFilePath(ctx, h.rt.Username(), h.rt.Store, req.StackName)
}

func (h dockerHandlers) handleValidateStackDirectory(ctx context.Context, req apischema.DirPathRequest) (apischema.DirectoryValidationResult, error) {
	return ValidateStackDirectory(ctx, req.DirPath)
}

func (h dockerHandlers) handleDeleteComposeStack(ctx context.Context, req apischema.ProjectNameRequest) error {
	return DeleteComposeStack(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}
