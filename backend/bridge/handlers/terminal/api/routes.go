package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var ContainerOpen = routes.Duplex("container.open", apischema.TypeOf[apischema.ContainerOpenRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var ListShells = routes.Query("terminal.list_shells", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.TypeOf[[]string]())
var Open = routes.Duplex("terminal.open", apischema.TypeOf[apischema.TerminalOpenRequest](), apischema.NoResponse(), apischema.NoEndpoint())

var Routes = routes.All()
