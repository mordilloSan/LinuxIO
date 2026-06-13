package jobs

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var api = apischema.Bindings(
	apischema.DuplexRoute[apischema.JobIDRequest, apischema.NoResponse]("jobs.attach", apischema.NoEndpoint()),
	apischema.Job[apischema.JobIDRequest, apischema.JobSnapshot]("jobs.cancel"),
	apischema.DuplexRoute[apischema.JobDataRequest, apischema.NoResponse]("jobs.data", apischema.NoEndpoint()),
	apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("jobs.events", apischema.NoEndpoint()),
	apischema.Query[apischema.JobIDRequest, apischema.JobSnapshot]("jobs.get"),
	apischema.Query[apischema.JobListRequest, []apischema.JobSnapshot]("jobs.list"),
)

var Routes = api.Routes()
