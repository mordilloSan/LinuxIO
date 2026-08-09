package tasks

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var api = apischema.Bindings(
	apischema.DuplexRoute[apischema.TaskIDRequest, apischema.NoResponse]("tasks.watch", apischema.NoEndpoint()),
	apischema.DuplexRoute[apischema.TaskDataRequest, apischema.NoResponse]("tasks.data", apischema.NoEndpoint()),
	apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("tasks.events", apischema.NoEndpoint()),
	apischema.Call[apischema.TaskIDRequest, apischema.TaskSnapshot]("tasks.cancel"),
	apischema.Call[apischema.TaskIDRequest, apischema.TaskSnapshot]("tasks.get"),
	apischema.Call[apischema.TaskListRequest, []apischema.TaskSnapshot]("tasks.list"),
)

var Routes = api.Routes()
