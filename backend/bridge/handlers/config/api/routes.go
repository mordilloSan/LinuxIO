package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var Get = routes.Query("config.get", apischema.NoRequest(), apischema.TypeOf[apischema.AppConfig]())
var Set = routes.Job("config.set", apischema.TypeOf[apischema.ConfigSetPayload](), apischema.TypeOf[apischema.ConfigSetResult]())

var Routes = routes.All()
