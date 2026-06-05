package jobs

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var api = apischema.Bindings(
	apischema.DuplexRoute("jobs.attach", apischema.TypeOf[apischema.JobIDRequest](), apischema.NoResponse(), apischema.NoEndpoint()),
	apischema.Job("jobs.cancel", apischema.TypeOf[apischema.JobIDRequest](), apischema.TypeOf[apischema.JobSnapshot]()),
	apischema.DuplexRoute("jobs.data", apischema.TypeOf[apischema.JobDataRequest](), apischema.NoResponse(), apischema.NoEndpoint()),
	apischema.DuplexRoute("jobs.events", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint()),
	apischema.Query("jobs.get", apischema.TypeOf[apischema.JobIDRequest](), apischema.TypeOf[apischema.JobSnapshot]()),
	apischema.Query("jobs.list", apischema.TypeOf[apischema.JobListRequest](), apischema.TypeOf[[]apischema.JobSnapshot]()),
)

var Routes = api.Routes()
