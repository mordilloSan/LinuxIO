// Package api is the wire contract between the linuxio-monitoring daemon and
// its clients: socket paths, routes, command names and every JSON payload. It
// imports only the standard library, common/version and the daemon's domain
// records, so the bridge can depend on it without reaching into the daemon.
package api

import "github.com/mordilloSan/LinuxIO/backend/common/version"

const (
	APISocketPath     = version.RunDir + "/monitoring/api.sock"
	ControlSocketPath = version.RunDir + "/monitoring/control.sock"
)

// Routes the daemon serves. Both sockets answer the read routes; command and
// history are control.sock only.
const (
	RoutePrefix        = "/api/v1/"
	RouteHealth        = "/healthz"
	RouteLive          = RoutePrefix + "live"
	RouteMeta          = RoutePrefix + "meta"
	RouteSystemSummary = RoutePrefix + "system/summary"
	RouteCommand       = RoutePrefix + "command"
	RouteProcesses     = RoutePrefix + "processes"
	RoutePrograms      = RoutePrefix + "programs"
)

// RouteHistory is the history route of one metrics plugin.
func RouteHistory(plugin string) string {
	return RoutePrefix + plugin + "/history"
}

// History query parameters.
const (
	QueryResolution = "resolution"
	QueryLimit      = "limit"
	QueryFrom       = "from"
	QueryTo         = "to"
)
