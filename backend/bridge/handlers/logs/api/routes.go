package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var GeneralFollow = routes.Runner("logs.general.follow", apischema.TypeOf[apischema.GeneralLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var ServiceFollow = routes.Runner("logs.service.follow", apischema.TypeOf[apischema.ServiceLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint())

var Routes = routes.All()
