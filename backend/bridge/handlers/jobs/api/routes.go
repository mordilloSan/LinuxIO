package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Attach = apischema.RouteSpec{Kind: apischema.KindDuplex, Route: "jobs.attach", Mode: bridgeipc.ModeDuplex, Request: apischema.TypeOf[apischema.JobIDRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var Cancel = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "jobs.cancel", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.JobIDRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var Data = apischema.RouteSpec{Kind: apischema.KindDuplex, Route: "jobs.data", Mode: bridgeipc.ModeDuplex, Request: apischema.TypeOf[apischema.JobDataRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var Events = apischema.RouteSpec{Kind: apischema.KindDuplex, Route: "jobs.events", Mode: bridgeipc.ModeDuplex, Request: apischema.NoRequest(), Result: apischema.NoResponse(), NoEndpoint: true}
var Get = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "jobs.get", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.JobIDRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var List = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "jobs.list", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.JobListRequest](), Result: apischema.TypeOf[[]apischema.JobSnapshot]()}

var Routes = []apischema.RouteSpec{
	Attach,
	Cancel,
	Data,
	Events,
	Get,
	List,
}
