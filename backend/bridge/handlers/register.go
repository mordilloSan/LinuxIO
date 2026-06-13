package handlers

import (
	"sort"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/hostname"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/jobs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type Family struct {
	Name     string
	Routes   []apischema.RouteSpec
	Register func(runtime.Runtime, *bridgeipc.Router)
}

var Families = []Family{
	{Name: "appupdate", Routes: appupdate.Routes, Register: appupdate.RegisterHandlers},
	{Name: "system", Routes: system.Routes, Register: system.RegisterHandlers},
	{Name: "accounts", Routes: accounts.Routes, Register: accounts.RegisterHandlers},
	{Name: "docker", Routes: docker.Routes, Register: docker.RegisterHandlers},
	{Name: "filebrowser", Routes: filebrowser.Routes, Register: filebrowser.RegisterHandlers},
	{Name: "indexer", Routes: indexer.Routes, Register: indexer.RegisterHandlers},
	{Name: "config", Routes: config.Routes, Register: config.RegisterHandlers},
	{Name: "control", Routes: control.Routes, Register: control.RegisterHandlers},
	{Name: "power", Routes: power.Routes, Register: power.RegisterHandlers},
	{Name: "systemd", Routes: systemd.Routes, Register: systemd.RegisterHandlers},
	{Name: "hostname", Routes: hostname.Routes, Register: hostname.RegisterHandlers},
	{Name: "datetime", Routes: datetime.Routes, Register: datetime.RegisterHandlers},
	{Name: "network", Routes: network.Routes, Register: network.RegisterHandlers},
	{Name: "packages", Routes: packages.Routes, Register: packages.RegisterHandlers},
	{Name: "terminal", Routes: terminal.Routes, Register: terminal.RegisterHandlers},
	{Name: "wireguard", Routes: wireguard.Routes, Register: wireguard.RegisterHandlers},
	{Name: "storage", Routes: storage.Routes, Register: storage.RegisterHandlers},
	{Name: "shares", Routes: shares.Routes, Register: shares.RegisterHandlers},
	{Name: "logs", Routes: logs.Routes, Register: logs.RegisterHandlers},
	{Name: "jobs", Routes: jobs.Routes},
}

var Routes = collectRoutes(Families)

func RegisterAllHandlers(rt runtime.Runtime) *bridgeipc.Router {
	router := bridgeipc.NewRouter(bridgeipc.DefaultRegistry)

	for _, family := range Families {
		if family.Register != nil {
			family.Register(rt, router)
		}
	}

	return router
}

func collectRoutes(families []Family) []apischema.RouteSpec {
	total := 0
	for _, family := range families {
		total += len(family.Routes)
	}
	routes := make([]apischema.RouteSpec, 0, total)
	for _, family := range families {
		routes = append(routes, family.Routes...)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Route < routes[j].Route })
	return routes
}

func Route(route string) (apischema.RouteSpec, bool) {
	for _, spec := range Routes {
		if spec.Route == route {
			return spec, true
		}
	}
	return apischema.RouteSpec{}, false
}

func MustRoute(route string) apischema.RouteSpec {
	spec, ok := Route(route)
	if !ok {
		panic("handlers: unknown route " + route)
	}
	return spec
}
