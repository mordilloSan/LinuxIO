package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var GetConfig = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "indexer.get_config", Privileged: true, Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.IndexerConfig]()}
var GetStatus = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "indexer.get_status", Privileged: true, Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.IndexerDaemonStatus]()}
var SetConfig = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "indexer.set_config", Privileged: true, Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.IndexerConfigPatch](), Result: apischema.TypeOf[apischema.IndexerConfigSetResult]()}
var SetTimerInterval = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "indexer.set_timer_interval", Privileged: true, Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.IntervalRequest](), Result: apischema.TypeOf[apischema.IndexerTimerSetResult]()}

var Routes = []apischema.RouteSpec{
	GetConfig,
	GetStatus,
	SetConfig,
	SetTimerInterval,
}
