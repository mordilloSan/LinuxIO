package handlers

import (
	"sort"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/alerts"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/assistant"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/capabilities"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/hostname"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/monitoring"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/schedules"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/tasks"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/virt"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

type family struct {
	Routes   []apischema.RouteSpec
	Register func(runtime.Runtime, *bridgeipc.Router)
}

var families = []family{
	{Routes: appupdate.Routes, Register: appupdate.RegisterHandlers},
	{Routes: system.Routes, Register: system.RegisterHandlers},
	{Routes: capabilities.Routes, Register: capabilities.RegisterHandlers},
	{Routes: accounts.Routes, Register: accounts.RegisterHandlers},
	{Routes: docker.Routes, Register: docker.RegisterHandlers},
	{Routes: filebrowser.Routes, Register: filebrowser.RegisterHandlers},
	{Routes: indexer.Routes, Register: indexer.RegisterHandlers},
	{Routes: monitoring.Routes, Register: monitoring.RegisterHandlers},
	{Routes: config.Routes, Register: config.RegisterHandlers},
	{Routes: control.Routes, Register: control.RegisterHandlers},
	{Routes: power.Routes, Register: power.RegisterHandlers},
	{Routes: systemd.Routes, Register: systemd.RegisterHandlers},
	{Routes: hostname.Routes, Register: hostname.RegisterHandlers},
	{Routes: datetime.Routes, Register: datetime.RegisterHandlers},
	{Routes: network.Routes, Register: network.RegisterHandlers},
	{Routes: packages.Routes, Register: packages.RegisterHandlers},
	{Routes: terminal.Routes, Register: terminal.RegisterHandlers},
	{Routes: assistant.Routes, Register: assistant.RegisterHandlers},
	{Routes: virt.Routes, Register: virt.RegisterHandlers},
	{Routes: wireguard.Routes, Register: wireguard.RegisterHandlers},
	{Routes: storage.Routes, Register: storage.RegisterHandlers},
	{Routes: schedules.Routes, Register: schedules.RegisterHandlers},
	{Routes: alerts.Routes, Register: alerts.RegisterHandlers},
	{Routes: shares.Routes, Register: shares.RegisterHandlers},
	{Routes: logs.Routes, Register: logs.RegisterHandlers},
	{Routes: tasks.Routes, Register: tasks.RegisterHandlers},
}

var Routes = collectRoutes(families)

func RegisterAllHandlers(rt runtime.Runtime) *bridgeipc.Router {
	router := bridgeipc.NewRouter(bridgeipc.DefaultTaskService)

	for _, f := range families {
		if f.Register != nil {
			f.Register(rt, router)
		}
	}

	return router
}

func collectRoutes(families []family) []apischema.RouteSpec {
	total := 0
	for _, f := range families {
		total += len(f.Routes)
	}
	routes := make([]apischema.RouteSpec, 0, total)
	for _, f := range families {
		routes = append(routes, f.Routes...)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Route < routes[j].Route })
	return routes
}
