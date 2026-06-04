package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var ControlAppUpdate = routes.Runner("control.app_update", apischema.TypeOf[apischema.AppUpdateRequest](), apischema.NoResponse(), apischema.NoEndpoint())

var Routes = routes.All()
