package schedules

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = routeBindings(newManager())
var Routes = api.Routes()

func RegisterHandlers(_ runtime.Runtime, router *bridgeipc.Router) { api.Register(router) }

func routeBindings(m manager) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.SchedulesListResult]("schedules.list", apischema.Privileged(), apischema.RetrySafe()).Handle(func(ctx context.Context, _ apischema.NoRequest) (apischema.SchedulesListResult, error) {
			return m.list(ctx)
		}),
		apischema.Call[apischema.ScheduleIDRequest, apischema.ScheduleStatus]("schedules.get", apischema.Privileged(), apischema.RetrySafe()).Handle(func(ctx context.Context, req apischema.ScheduleIDRequest) (apischema.ScheduleStatus, error) {
			return m.get(ctx, req.ID)
		}),
		apischema.Call[apischema.ScheduleCreateRequest, apischema.ScheduleStatus]("schedules.create", apischema.Privileged()).Handle(m.create),
		apischema.Call[apischema.ScheduleUpdateRequest, apischema.ScheduleStatus]("schedules.update", apischema.Privileged()).Handle(func(ctx context.Context, req apischema.ScheduleUpdateRequest) (apischema.ScheduleStatus, error) {
			if err := m.change(ctx, req.ID, &req.Options, nil, false); err != nil {
				return apischema.ScheduleStatus{}, err
			}
			return m.get(ctx, req.ID)
		}),
		apischema.Call[apischema.ScheduleIDRequest, apischema.NoResponse]("schedules.delete", apischema.Privileged()).HandleVoid(func(ctx context.Context, req apischema.ScheduleIDRequest) error {
			return m.change(ctx, req.ID, nil, nil, true)
		}),
		apischema.Call[apischema.ScheduleIDRequest, apischema.NoResponse]("schedules.enable", apischema.Privileged()).HandleVoid(func(ctx context.Context, req apischema.ScheduleIDRequest) error {
			enabled := true
			return m.change(ctx, req.ID, nil, &enabled, false)
		}),
		apischema.Call[apischema.ScheduleIDRequest, apischema.NoResponse]("schedules.disable", apischema.Privileged()).HandleVoid(func(ctx context.Context, req apischema.ScheduleIDRequest) error {
			enabled := false
			return m.change(ctx, req.ID, nil, &enabled, false)
		}),
		apischema.Call[apischema.ScheduleIDRequest, apischema.NoResponse]("schedules.run_now", apischema.Privileged()).HandleVoid(func(ctx context.Context, req apischema.ScheduleIDRequest) error { return m.runNow(ctx, req.ID) }),
		apischema.Call[apischema.ScheduleStopRequest, apischema.NoResponse]("schedules.stop", apischema.Privileged()).HandleVoid(m.stop),
	)
}
