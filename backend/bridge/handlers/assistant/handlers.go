package assistant

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	plugins := pluginHandlers{rt: rt}
	return apischema.Bindings(
		apischema.DuplexRoute[apischema.AssistantOpenRequest, apischema.NoResponse]("assistant.open", apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.AssistantOpenRequest) error {
				return HandleAssistantSession(ctx, rt, stream, req)
			},
		),
		// The plugin routes run the agent's own CLI as the session user and
		// write only under that user's home, so none of them needs root.
		apischema.Call[apischema.AssistantAgentRequest, apischema.AssistantPluginList]("assistant.list_plugins", apischema.RetrySafe()).Handle(plugins.handleList),
		apischema.Call[apischema.AssistantPluginSearchRequest, apischema.AssistantPluginSearchResult]("assistant.search_plugins", apischema.RetrySafe()).Handle(plugins.handleSearch),
		apischema.Mutation("assistant.install_plugin", plugins.handleInstall),
		apischema.MutationVoid("assistant.uninstall_plugin", plugins.handleUninstall),
		apischema.MutationVoid("assistant.set_plugin_enabled", plugins.handleSetEnabled),
		apischema.MutationVoid("assistant.add_marketplace", plugins.handleAddMarketplace),
		apischema.MutationVoid("assistant.remove_marketplace", plugins.handleRemoveMarketplace),
		apischema.MutationVoid("assistant.update_marketplace", plugins.handleUpdateMarketplace),
	)
}

// RegisterHandlers registers the assistant routes with the bridge router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func (h pluginHandlers) handleList(ctx context.Context, req apischema.AssistantAgentRequest) (apischema.AssistantPluginList, error) {
	return ListPlugins(ctx, h.rt.Session.User, req.Agent)
}

func (h pluginHandlers) handleSearch(ctx context.Context, req apischema.AssistantPluginSearchRequest) (apischema.AssistantPluginSearchResult, error) {
	return SearchPlugins(ctx, h.rt.Session.User, req.Agent, req.Query)
}

func (h pluginHandlers) handleInstall(ctx context.Context, req apischema.AssistantPluginInstallRequest) (apischema.AssistantPluginInstallResult, error) {
	return InstallPlugin(ctx, h.rt.Session.User, req)
}

func (h pluginHandlers) handleUninstall(ctx context.Context, req apischema.AssistantPluginRequest) error {
	return UninstallPlugin(ctx, h.rt.Session.User, req)
}

func (h pluginHandlers) handleSetEnabled(ctx context.Context, req apischema.AssistantPluginEnabledRequest) error {
	return SetPluginEnabled(ctx, h.rt.Session.User, req)
}

func (h pluginHandlers) handleAddMarketplace(ctx context.Context, req apischema.AssistantMarketplaceAddRequest) error {
	return AddMarketplace(ctx, h.rt.Session.User, req)
}

func (h pluginHandlers) handleRemoveMarketplace(ctx context.Context, req apischema.AssistantMarketplaceRequest) error {
	return RemoveMarketplace(ctx, h.rt.Session.User, req)
}

func (h pluginHandlers) handleUpdateMarketplace(ctx context.Context, req apischema.AssistantMarketplaceRequest) error {
	return UpdateMarketplace(ctx, h.rt.Session.User, req)
}
