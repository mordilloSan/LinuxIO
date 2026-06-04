package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	dockerapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := newDockerHandlers(rt)
	prepareDockerHandlers(router, handlers)

	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: dockerapi.ListContainers, Handle: handlers.handleListContainers},
		{Route: dockerapi.StartContainer, Handle: handlers.handleStartContainer},
		{Route: dockerapi.StopContainer, Handle: handlers.handleStopContainer},
		{Route: dockerapi.RemoveContainer, Handle: handlers.handleRemoveContainer},
		{Route: dockerapi.RestartContainer, Handle: handlers.handleRestartContainer},
		{Route: dockerapi.ListImages, Handle: handlers.handleListImages},
		{Route: dockerapi.DeleteImage, Handle: handlers.handleDeleteImage},
		{Route: dockerapi.ListNetworks, Handle: handlers.handleListNetworks},
		{Route: dockerapi.CreateNetwork, Handle: handlers.handleCreateNetwork},
		{Route: dockerapi.DeleteNetwork, Handle: handlers.handleDeleteNetwork},
		{Route: dockerapi.ListVolumes, Handle: handlers.handleListVolumes},
		{Route: dockerapi.CreateVolume, Handle: handlers.handleCreateVolume},
		{Route: dockerapi.DeleteVolume, Handle: handlers.handleDeleteVolume},
		{Route: dockerapi.ListComposeProjects, Handle: handlers.handleListComposeProjects},
		{Route: dockerapi.GetComposeProject, Handle: handlers.handleGetComposeProject},
		{Route: dockerapi.ComposeUp, Handle: handlers.handleComposeUp},
		{Route: dockerapi.ComposeDown, Handle: handlers.handleComposeDown},
		{Route: dockerapi.ComposeStop, Handle: handlers.handleComposeStop},
		{Route: dockerapi.ComposeRestart, Handle: handlers.handleComposeRestart},
		{Route: dockerapi.DeleteStack, Handle: handlers.handleDeleteStack},
		{Route: dockerapi.GetDockerFolders, Handle: handlers.handleGetDockerFolders},
		{Route: dockerapi.ValidateCompose, Handle: handlers.handleValidateCompose},
		{Route: dockerapi.NormalizeCompose, Handle: handlers.handleNormalizeCompose},
		{Route: dockerapi.GetComposeFilePath, Handle: handlers.handleGetComposeFilePath},
		{Route: dockerapi.ValidateStackDirectory, Handle: handlers.handleValidateStackDirectory},
		{Route: dockerapi.ReindexDockerFolders, Handle: handlers.handleReindexDockerFolders},
		{Route: dockerapi.DeleteComposeStack, Handle: handlers.handleDeleteComposeStack},
		{Route: dockerapi.GetDockerInfo, Handle: handlers.handleGetDockerInfo},
		{Route: dockerapi.GetIconURI, Handle: handlers.handleGetIconURI},
		{Route: dockerapi.GetIcon, Handle: handlers.handleGetIcon},
		{Route: dockerapi.GetIconInfo, Handle: handlers.handleGetIconInfo},
		{Route: dockerapi.ClearIconCache, Handle: handlers.handleClearIconCache},
		{Route: dockerapi.StartAllStopped, Handle: handlers.handleStartAllStopped},
		{Route: dockerapi.StopAllRunning, Handle: handlers.handleStopAllRunning},
		{Route: dockerapi.ListAutoUpdateContainers, Handle: handlers.handleListAutoUpdateContainers},
		{Route: dockerapi.SetAutoUpdate, Handle: handlers.handleSetAutoUpdate},
		{Route: dockerapi.GetCaddyStatus, Handle: handlers.handleGetCaddyStatus},
		{Route: dockerapi.EnableCaddy, Handle: handlers.handleEnableCaddy},
		{Route: dockerapi.DisableCaddy, Handle: handlers.handleDisableCaddy},
		{Route: dockerapi.ReloadCaddy, Handle: handlers.handleReloadCaddy},
		{Route: dockerapi.ConnectToProxy, Handle: handlers.handleConnectToProxy},
		{Route: dockerapi.SystemPrune, Handle: handlers.handleSystemPrune},
	})

	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: dockerapi.LogsFollow,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.DockerLogsFollowRequest) (any, error) {
			return runDockerLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
}
