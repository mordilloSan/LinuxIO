package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var GetNTPServers = routes.Query("datetime.get_ntp_servers", apischema.NoRequest(), apischema.TypeOf[[]string]())
var GetNTPStatus = routes.Query("datetime.get_ntp_status", apischema.NoRequest(), apischema.TypeOf[bool]())
var GetTimezone = routes.Query("datetime.get_timezone", apischema.NoRequest(), apischema.TypeOf[string]())
var SetNTP = routes.Job("datetime.set_ntp", apischema.TypeOf[apischema.EnabledRequest](), apischema.NoResponse())
var SetNTPServers = routes.Job("datetime.set_ntp_servers", apischema.TypeOf[apischema.NTPServersRequest](), apischema.NoResponse())
var SetServerTime = routes.Job("datetime.set_server_time", apischema.TypeOf[apischema.ISOTimeRequest](), apischema.NoResponse())
var SetTimezone = routes.Job("datetime.set_timezone", apischema.TypeOf[apischema.TimezoneRequest](), apischema.NoResponse())

var Routes = routes.All()
