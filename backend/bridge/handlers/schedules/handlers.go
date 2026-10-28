package schedules

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
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
		apischema.Mutation("schedules.create", m.create, apischema.Privileged()),
		apischema.Mutation("schedules.update", func(ctx context.Context, req apischema.ScheduleUpdateRequest) (apischema.ScheduleStatus, error) {
			if err := m.change(ctx, req.ID, &req.Options, nil, false); err != nil {
				return apischema.ScheduleStatus{}, err
			}
			return m.get(ctx, req.ID)
		}, apischema.Privileged()),
		apischema.MutationVoid("schedules.delete", func(ctx context.Context, req apischema.ScheduleIDRequest) error {
			return m.change(ctx, req.ID, nil, nil, true)
		}, apischema.Privileged()),
		apischema.MutationVoid("schedules.enable", func(ctx context.Context, req apischema.ScheduleIDRequest) error {
			enabled := true
			return m.change(ctx, req.ID, nil, &enabled, false)
		}, apischema.Privileged()),
		apischema.MutationVoid("schedules.disable", func(ctx context.Context, req apischema.ScheduleIDRequest) error {
			enabled := false
			return m.change(ctx, req.ID, nil, &enabled, false)
		}, apischema.Privileged()),
		apischema.MutationVoid("schedules.run_now", func(ctx context.Context, req apischema.ScheduleIDRequest) error { return m.runNow(ctx, req.ID) }, apischema.Privileged()),
		apischema.MutationVoid("schedules.stop", m.stop, apischema.Privileged()),
	)
}
