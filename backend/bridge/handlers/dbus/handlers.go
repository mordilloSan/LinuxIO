package dbus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type dbusRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers() {
	RegisterJobRunners()

	registerDBusHandlers([]dbusRegistration{
		{command: "reboot", handler: dbusNoArgActionHandler("reboot requested", func() error { return CallLogin1Action("Reboot") })},
		{command: "power_off", handler: dbusNoArgActionHandler("power_off requested", func() error { return CallLogin1Action("PowerOff") })},
		{command: "get_updates_basic", handler: dbusNoArgResultHandler(GetUpdatesBasic)},
		{command: "get_update_detail", handler: dbusOneArgResultHandler(GetSingleUpdateDetail)},
		{command: "install_package", handler: installPackageHandler()},
		{command: "get_auto_updates", handler: dbusNoArgResultHandler(getAutoUpdates)},
		{command: "set_auto_updates", handler: setAutoUpdatesHandler()},
		{command: "apply_offline_updates", handler: applyOfflineUpdatesHandler()},
		{command: "get_update_history", handler: dbusNoArgResultHandler(GetUpdateHistory)},
		{command: "list_timers", handler: dbusNoArgResultHandler(ListTimers)},
		{command: "list_sockets", handler: dbusNoArgResultHandler(ListSockets)},
		{command: "list_services", handler: dbusNoArgResultHandler(ListServices)},
		{command: "get_unit_info", handler: dbusOneArgResultHandler(GetUnitInfo)},
		{command: "start_service", handler: oneArgActionHandler("start_service requested", "unit", StartService)},
		{command: "stop_service", handler: oneArgActionHandler("stop_service requested", "unit", StopService)},
		{command: "restart_service", handler: oneArgActionHandler("restart_service requested", "unit", RestartService)},
		{command: "reload_service", handler: oneArgActionHandler("reload_service requested", "unit", ReloadService)},
		{command: "enable_service", handler: oneArgActionHandler("enable_service requested", "unit", EnableService)},
		{command: "disable_service", handler: oneArgActionHandler("disable_service requested", "unit", DisableService)},
		{command: "mask_service", handler: oneArgActionHandler("mask_service requested", "unit", MaskService)},
		{command: "unmask_service", handler: oneArgActionHandler("unmask_service requested", "unit", UnmaskService)},
		{command: "reset_failed_service", handler: oneArgActionHandler("reset_failed_service requested", "unit", ResetFailedService)},
		{command: "get_network_info", handler: dbusNoArgResultHandler(GetNetworkInfo)},
		{command: "set_ipv4_manual", handler: setIPv4ManualHandler()},
		{command: "set_ipv4", handler: setIPv4Handler()},
		{command: "set_ipv6", handler: setIPv6Handler()},
		{command: "set_mtu", handler: setMTUHandler()},
		{command: "enable_connection", handler: oneArgActionHandler("enable_connection requested", "interface", EnableConnection)},
		{command: "disable_connection", handler: oneArgActionHandler("disable_connection requested", "interface", DisableConnection)},
		{command: "set_hostname", handler: oneArgActionHandler("set_hostname requested", "service", SetHostname)},
		{command: "get_ntp_status", handler: dbusNoArgResultHandler(GetNTPStatus)},
		{command: "set_ntp", handler: setNTPHandler()},
		{command: "set_server_time", handler: oneArgActionHandler("set_server_time requested", "mode", SetServerTime)},
		{command: "get_timezone", handler: dbusNoArgResultHandler(GetTimezone)},
		{command: "set_timezone", handler: oneArgActionHandler("set_timezone requested", "mode", SetTimezone)},
		{command: "get_ntp_servers", handler: dbusNoArgResultHandler(GetNTPServers)},
		{command: "set_ntp_servers", handler: setNTPServersHandler()},
	})
}

func registerDBusHandlers(registrations []dbusRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("dbus", registration.command, registration.handler)
	}
}

func dbusNoArgResultHandler[T any](fn func() (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fn()
		return emitDBusResult(emit, result, err)
	}
}

func dbusOneArgResultHandler[T any](fn func(string) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		result, err := fn(args[0])
		return emitDBusResult(emit, result, err)
	}
}

func dbusNoArgActionHandler(message string, fn func() error) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info(message, "component", "dbus")
		if err := fn(); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func oneArgActionHandler(message, attrKey string, fn func(string) error) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		slog.Info(message, "component", "dbus", attrKey, args[0])
		if err := fn(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func installPackageHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		slog.Info("install_package requested", "component", "dbus", "package", args[0])
		if err := InstallPackage(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func setAutoUpdatesHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		slog.Info("set_auto_updates requested", "component", "dbus", "mode", args[0])
		result, err := setAutoUpdates(args[0])
		return emitDBusResult(emit, result, err)
	}
}

func applyOfflineUpdatesHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info("apply_offline_updates requested")
		result, err := applyOfflineUpdates()
		return emitDBusResult(emit, result, err)
	}
}

func setIPv4ManualHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 4 {
			return ipc.ErrInvalidArgs
		}
		iface := args[0]
		addressCIDR := args[1]
		gateway := args[2]
		dnsServers := args[3:]
		slog.Info("set_ipv4_manual requested", "component", "dbus", "interface", iface, "path", addressCIDR, "service", gateway, "dns_count", len(dnsServers))
		if err := SetIPv4Manual(iface, addressCIDR, gateway, dnsServers); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func setIPv4Handler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		iface, method := args[0], strings.ToLower(args[1])
		slog.Info("set_ipv4 requested", "component", "dbus", "interface", iface, "mode", method)
		if method != "dhcp" && method != "auto" {
			return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
		}
		if err := SetIPv4DHCP(iface); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func setIPv6Handler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		iface, method := args[0], strings.ToLower(args[1])
		slog.Info("set_ipv6 requested", "component", "dbus", "interface", iface, "mode", method)
		switch method {
		case "dhcp", "auto":
			if err := SetIPv6DHCP(iface); err != nil {
				return err
			}
		case "static":
			if len(args) != 3 {
				return ipc.ErrInvalidArgs
			}
			if err := SetIPv6Static(iface, args[2]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'static'")
		}
		return emit.Result(nil)
	}
}

func setMTUHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 2 {
			return ipc.ErrInvalidArgs
		}
		slog.Info("set_mtu requested", "component", "dbus", "interface", args[0], "mode", args[1])
		if err := SetMTU(args[0], args[1]); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func setNTPServersHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info("set_ntp_servers requested", "component", "dbus", "server_count", len(args))
		if err := SetNTPServers(args); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func setNTPHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		enabled := args[0] == "true"
		slog.Info("set_ntp requested", "component", "dbus", "enabled", enabled)
		if err := SetNTP(enabled); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func emitDBusResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}
