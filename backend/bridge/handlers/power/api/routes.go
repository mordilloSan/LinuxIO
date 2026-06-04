package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var Disable = routes.Job("power.disable", apischema.NoRequest(), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())
var GetStatus = routes.Query("power.get_status", apischema.NoRequest(), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())
var SetProfile = routes.Job("power.set_profile", apischema.TypeOf[apischema.ProfileRequest](), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())
var Start = routes.Job("power.start", apischema.NoRequest(), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())

var Routes = routes.All()
