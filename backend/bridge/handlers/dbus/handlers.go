package dbus

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type dbusRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers() {
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
		{command: "start_service", handler: serviceActionHandler("start_service requested: unit=%s", StartService)},
		{command: "stop_service", handler: serviceActionHandler("stop_service requested: unit=%s", StopService)},
		{command: "restart_service", handler: serviceActionHandler("restart_service requested: unit=%s", RestartService)},
		{command: "reload_service", handler: serviceActionHandler("reload_service requested: unit=%s", ReloadService)},
		{command: "enable_service", handler: serviceActionHandler("enable_service requested: unit=%s", EnableService)},
		{command: "disable_service", handler: serviceActionHandler("disable_service requested: unit=%s", DisableService)},
		{command: "mask_service", handler: serviceActionHandler("mask_service requested: unit=%s", MaskService)},
		{command: "unmask_service", handler: serviceActionHandler("unmask_service requested: unit=%s", UnmaskService)},
		{command: "get_network_info", handler: dbusNoArgResultHandler(GetNetworkInfo)},
		{command: "set_ipv4_manual", handler: setIPv4ManualHandler()},
		{command: "set_ipv4", handler: setIPv4Handler()},
		{command: "set_ipv6", handler: setIPv6Handler()},
		{command: "set_mtu", handler: setMTUHandler()},
		{command: "enable_connection", handler: connectionActionHandler("enable_connection requested: connection=%s", EnableConnection)},
		{command: "disable_connection", handler: connectionActionHandler("disable_connection requested: connection=%s", DisableConnection)},
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
		logger.Infof("%s", message)
		if err := fn(); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func serviceActionHandler(logPattern string, fn func(string) error) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof(logPattern, args[0])
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
		logger.Infof("install_package requested: package=%s", args[0])
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
		logger.Infof("set_auto_updates requested: mode=%s", args[0])
		result, err := setAutoUpdates(args[0])
		return emitDBusResult(emit, result, err)
	}
}

func applyOfflineUpdatesHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("apply_offline_updates requested")
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
		logger.Infof(
			"set_ipv4_manual requested: iface=%s address=%s gateway=%s dns_count=%d",
			iface, addressCIDR, gateway, len(dnsServers),
		)
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
		logger.Infof("set_ipv4 requested: iface=%s method=%s", iface, method)
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
		logger.Infof("set_ipv6 requested: iface=%s method=%s", iface, method)
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
		logger.Infof("set_mtu requested: iface=%s mtu=%s", args[0], args[1])
		if err := SetMTU(args[0], args[1]); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func connectionActionHandler(logPattern string, fn func(string) error) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof(logPattern, args[0])
		if err := fn(args[0]); err != nil {
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
