package tasks

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var Routes = routeBindings().Routes()

// routeBindings declares the contract of the tasks.* routes. The handlers
// themselves live in bridgeipc.TaskService; handlers_test.go keeps both
// catalogs in step.
func routeBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.DuplexRoute[apischema.TaskIDRequest, apischema.NoResponse]("tasks.watch", apischema.NoEndpoint()),
		apischema.DuplexRoute[apischema.TaskDataRequest, apischema.NoResponse]("tasks.data", apischema.NoEndpoint()),
		apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("tasks.events", apischema.NoEndpoint()),
		apischema.Call[apischema.TaskIDRequest, apischema.TaskSnapshot]("tasks.cancel"),
		apischema.Call[apischema.TaskIDRequest, apischema.TaskSnapshot]("tasks.get", apischema.RetrySafe()),
		apischema.Call[apischema.TaskListRequest, []apischema.TaskSnapshot]("tasks.list", apischema.RetrySafe()),
	)
}

func RegisterHandlers(_ runtime.Runtime, router *bridgeipc.Router) {
	router.TaskService().RegisterRoutes(router)
}
