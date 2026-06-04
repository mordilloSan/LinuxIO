package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var Attach = routes.Duplex("jobs.attach", apischema.TypeOf[apischema.JobIDRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var Cancel = routes.Job("jobs.cancel", apischema.TypeOf[apischema.JobIDRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var Data = routes.Duplex("jobs.data", apischema.TypeOf[apischema.JobDataRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var Events = routes.Duplex("jobs.events", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint())
var Get = routes.Query("jobs.get", apischema.TypeOf[apischema.JobIDRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var List = routes.Query("jobs.list", apischema.TypeOf[apischema.JobListRequest](), apischema.TypeOf[[]apischema.JobSnapshot]())

var Routes = routes.All()
