package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var GetConfig = routes.Query("indexer.get_config", apischema.NoRequest(), apischema.TypeOf[apischema.IndexerConfig](), apischema.Privileged())
var GetStatus = routes.Query("indexer.get_status", apischema.NoRequest(), apischema.TypeOf[apischema.IndexerDaemonStatus](), apischema.Privileged())
var SetConfig = routes.Job("indexer.set_config", apischema.TypeOf[apischema.IndexerConfigPatch](), apischema.TypeOf[apischema.IndexerConfigSetResult](), apischema.Privileged())
var SetTimerInterval = routes.Job("indexer.set_timer_interval", apischema.TypeOf[apischema.IntervalRequest](), apischema.TypeOf[apischema.IndexerTimerSetResult](), apischema.Privileged())

var Routes = routes.All()
