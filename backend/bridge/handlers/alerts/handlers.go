package alerts

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	store "github.com/mordilloSan/LinuxIO/backend/common/alerts"
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
		apischema.Call[apischema.NoRequest, apischema.NotificationSettingsView]("alerts.get_notifications", apischema.Privileged(), apischema.RetrySafe()).Handle(func(context.Context, apischema.NoRequest) (apischema.NotificationSettingsView, error) {
			n, err := store.LoadNotifications()
			return notificationsView(n), err
		}),
		apischema.Call[apischema.NotificationSettings, apischema.NotificationSettingsView]("alerts.set_notifications", apischema.Privileged()).Handle(func(_ context.Context, req apischema.NotificationSettings) (apischema.NotificationSettingsView, error) {
			n, err := store.SaveNotifications(notificationsFromRequest(req))
			if err == nil {
				slog.Info("alert notification settings saved", "minSeverity", req.MinSeverity, "webhook", req.WebhookURL != "", "email", req.SMTPHost != "")
			}
			return notificationsView(n), err
		}),
		apischema.Call[apischema.NoRequest, apischema.SuccessResponse]("alerts.test_notifications", apischema.Privileged()).Handle(handleTestNotifications),
	)
}
