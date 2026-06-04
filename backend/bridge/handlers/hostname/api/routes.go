package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var SetHostname = routes.Job("hostname.set_hostname", apischema.TypeOf[apischema.HostnameRequest](), apischema.NoResponse())

var Routes = routes.All()
