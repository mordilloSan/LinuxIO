package alerts

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	uid := func() uint32 {
		if rt.Session == nil {
			return 0
		}
		return rt.Session.User.UID
	}
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.AlertList]("alerts.list", apischema.Privileged(), apischema.RetrySafe()).Handle(func(context.Context, apischema.NoRequest) (apischema.AlertList, error) {
			return listForUID(uid())
		}),
		apischema.Call[apischema.AlertIDsRequest, apischema.AlertList]("alerts.mark_seen", apischema.Privileged()).Handle(func(ctx context.Context, req apischema.AlertIDsRequest) (apischema.AlertList, error) {
			return markSeenForUID(ctx, uid(), req.IDs)
		}),
		apischema.Call[apischema.NoRequest, apischema.AlertList]("alerts.mark_all_seen", apischema.Privileged()).Handle(func(ctx context.Context, _ apischema.NoRequest) (apischema.AlertList, error) {
			return markSeenForUID(ctx, uid(), nil)
		}),
		apischema.Call[apischema.AlertDismissRequest, apischema.AlertList]("alerts.dismiss", apischema.Privileged()).Handle(func(ctx context.Context, req apischema.AlertDismissRequest) (apischema.AlertList, error) {
			return dismissForUID(ctx, uid(), req.ID)
		}),
	)
}
