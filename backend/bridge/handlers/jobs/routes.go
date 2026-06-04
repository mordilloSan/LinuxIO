package jobs

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var RouteAttach = routes.Duplex("jobs.attach", apischema.TypeOf[apischema.JobIDRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteCancel = routes.Job("jobs.cancel", apischema.TypeOf[apischema.JobIDRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteData = routes.Duplex("jobs.data", apischema.TypeOf[apischema.JobDataRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteEvents = routes.Duplex("jobs.events", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint())
var RouteGet = routes.Query("jobs.get", apischema.TypeOf[apischema.JobIDRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteList = routes.Query("jobs.list", apischema.TypeOf[apischema.JobListRequest](), apischema.TypeOf[[]apischema.JobSnapshot]())

var Routes = routes.All()
