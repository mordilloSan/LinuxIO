package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var GetNTPServers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.get_ntp_servers", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]string]()}
var GetNTPStatus = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.get_ntp_status", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[bool]()}
var GetTimezone = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.get_timezone", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[string]()}
var SetNTP = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.set_ntp", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.EnabledRequest](), Result: apischema.NoResponse()}
var SetNTPServers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.set_ntp_servers", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NTPServersRequest](), Result: apischema.NoResponse()}
var SetServerTime = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.set_server_time", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ISOTimeRequest](), Result: apischema.NoResponse()}
var SetTimezone = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "datetime.set_timezone", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.TimezoneRequest](), Result: apischema.NoResponse()}

var Routes = []apischema.RouteSpec{
	GetNTPServers,
	GetNTPStatus,
	GetTimezone,
	SetNTP,
	SetNTPServers,
	SetServerTime,
	SetTimezone,
}
