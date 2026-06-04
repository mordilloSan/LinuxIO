package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var Logoff = routes.Job("control.logoff", apischema.TypeOf[apischema.SessionIDRequest](), apischema.NoResponse())
var PowerOff = routes.Job("control.power_off", apischema.NoRequest(), apischema.NoResponse())
var Reboot = routes.Job("control.reboot", apischema.NoRequest(), apischema.NoResponse())
var Version = routes.Query("control.version", apischema.NoRequest(), apischema.TypeOf[apischema.VersionResponse]())

var Routes = routes.All()
