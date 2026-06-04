package handlers

import (
	"sort"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts"
	accountsapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate"
	appupdateapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	configapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	controlapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime"
	datetimeapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	dockerapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	filebrowserapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/hostname"
	hostnameapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/hostname/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer/api"
	jobsapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/jobs/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs"
	logsapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network"
	networkapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages"
	packagesapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	powerapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	sharesapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	storageapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	systemapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal"
	terminalapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/terminal/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	wireguardapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type Family struct {
	Name     string
	Routes   []apischema.RouteSpec
	Register func(runtime.Runtime, *bridgeipc.Router)
}

var Families = []Family{
	{Name: "appupdate", Routes: appupdateapi.Routes, Register: appupdate.RegisterHandlers},
	{Name: "system", Routes: systemapi.Routes, Register: system.RegisterHandlers},
	{Name: "accounts", Routes: accountsapi.Routes, Register: accounts.RegisterHandlers},
	{Name: "docker", Routes: dockerapi.Routes, Register: docker.RegisterHandlers},
	{Name: "filebrowser", Routes: filebrowserapi.Routes, Register: filebrowser.RegisterHandlers},
	{Name: "indexer", Routes: indexerapi.Routes, Register: indexer.RegisterHandlers},
	{Name: "config", Routes: configapi.Routes, Register: config.RegisterHandlers},
	{Name: "control", Routes: controlapi.Routes, Register: control.RegisterHandlers},
	{Name: "power", Routes: powerapi.Routes, Register: power.RegisterHandlers},
	{Name: "systemd", Routes: systemdapi.Routes, Register: systemd.RegisterHandlers},
	{Name: "hostname", Routes: hostnameapi.Routes, Register: hostname.RegisterHandlers},
	{Name: "datetime", Routes: datetimeapi.Routes, Register: datetime.RegisterHandlers},
	{Name: "network", Routes: networkapi.Routes, Register: network.RegisterHandlers},
	{Name: "packages", Routes: packagesapi.Routes, Register: packages.RegisterHandlers},
	{Name: "terminal", Routes: terminalapi.Routes, Register: terminal.RegisterHandlers},
	{Name: "wireguard", Routes: wireguardapi.Routes, Register: wireguard.RegisterHandlers},
	{Name: "storage", Routes: storageapi.Routes, Register: storage.RegisterHandlers},
	{Name: "shares", Routes: sharesapi.Routes, Register: shares.RegisterHandlers},
	{Name: "logs", Routes: logsapi.Routes, Register: logs.RegisterHandlers},
	{Name: "jobs", Routes: jobsapi.Routes},
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

func RoutesFor(handler string) []apischema.RouteSpec {
	var out []apischema.RouteSpec
	prefix := handler + "."
	for _, spec := range Routes {
		if strings.HasPrefix(spec.Route, prefix) {
			out = append(out, spec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Route < out[j].Route })
	return out
}
