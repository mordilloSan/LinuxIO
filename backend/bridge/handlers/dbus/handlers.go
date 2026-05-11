package dbus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime) {
	RegisterJobRunners()

	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "reboot", Handler: handleReboot},
		{Name: "power_off", Handler: handlePowerOff},
		{Name: "get_updates_basic", Handler: handleGetUpdatesBasic},
		{Name: "get_update_detail", Handler: handleGetUpdateDetail},
		{Name: "install_package", Handler: handleInstallPackage},
		{Name: "get_auto_updates", Handler: handleGetAutoUpdates},
		{Name: "set_auto_updates", Handler: handleSetAutoUpdates},
		{Name: "apply_offline_updates", Handler: handleApplyOfflineUpdates},
		{Name: "get_update_history", Handler: handleGetUpdateHistory},
		{Name: "get_network_info", Handler: handleGetNetworkInfo},
		{Name: "set_ipv4_manual", Handler: handleSetIPv4Manual},
		{Name: "set_ipv4", Handler: handleSetIPv4},
		{Name: "set_ipv6", Handler: handleSetIPv6},
		{Name: "set_mtu", Handler: handleSetMTU},
		{Name: "enable_connection", Handler: handleEnableConnection},
		{Name: "disable_connection", Handler: handleDisableConnection},
		{Name: "set_hostname", Handler: handleSetHostname},
		{Name: "get_ntp_status", Handler: handleGetNTPStatus},
		{Name: "set_ntp", Handler: handleSetNTP},
		{Name: "set_server_time", Handler: handleSetServerTime},
		{Name: "get_timezone", Handler: handleGetTimezone},
		{Name: "set_timezone", Handler: handleSetTimezone},
		{Name: "get_ntp_servers", Handler: handleGetNTPServers},
		{Name: "set_ntp_servers", Handler: handleSetNTPServers},
	})
}

func handleReboot(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("reboot requested", "component", "dbus")
	return rpc.EmitResult(emit, nil, Reboot(ctx))
}

func handlePowerOff(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("power_off requested", "component", "dbus")
	return rpc.EmitResult(emit, nil, PowerOff(ctx))
}

func handleGetUpdatesBasic(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetUpdatesBasic()
	return rpc.EmitResult(emit, result, err)
}

func handleGetUpdateDetail(ctx context.Context, args []string, emit ipc.Events) error {
	packageID, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetSingleUpdateDetail(packageID)
	return rpc.EmitResult(emit, result, err)
}

func handleInstallPackage(ctx context.Context, args []string, emit ipc.Events) error {
	packageName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("install_package requested", "component", "dbus", "package", packageName)
	return rpc.EmitResult(emit, nil, InstallPackage(packageName))
}

func handleGetAutoUpdates(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := getAutoUpdates()
	return rpc.EmitResult(emit, result, err)
}

func handleSetAutoUpdates(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}
	slog.Info("set_auto_updates requested", "component", "dbus", "mode", args[0])
	result, err := setAutoUpdates(args[0])
	return rpc.EmitResult(emit, result, err)
}

func handleApplyOfflineUpdates(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("apply_offline_updates requested")
	result, err := applyOfflineUpdates()
	return rpc.EmitResult(emit, result, err)
}

func handleGetUpdateHistory(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetUpdateHistory()
	return rpc.EmitResult(emit, result, err)
}

func handleGetNetworkInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetNetworkInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleSetIPv4Manual(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 4); err != nil {
		return err
	}
	iface := args[0]
	addressCIDR := args[1]
	gateway := args[2]
	dnsServers := args[3:]
	slog.Info("set_ipv4_manual requested", "component", "dbus", "interface", iface, "path", addressCIDR, "service", gateway, "dns_count", len(dnsServers))
	return rpc.EmitResult(emit, nil, SetIPv4Manual(iface, addressCIDR, gateway, dnsServers))
}

func handleSetIPv4(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	iface, method := args[0], strings.ToLower(args[1])
	slog.Info("set_ipv4 requested", "component", "dbus", "interface", iface, "mode", method)
	if method != "dhcp" && method != "auto" {
		return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
	}
	return rpc.EmitResult(emit, nil, SetIPv4DHCP(iface))
}

func handleSetIPv6(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	iface, method := args[0], strings.ToLower(args[1])
	slog.Info("set_ipv6 requested", "component", "dbus", "interface", iface, "mode", method)
	switch method {
	case "dhcp", "auto":
		return rpc.EmitResult(emit, nil, SetIPv6DHCP(iface))
	case "static":
		if len(args) != 3 {
			return ipc.ErrInvalidArgs
		}
		return rpc.EmitResult(emit, nil, SetIPv6Static(iface, args[2]))
	default:
		return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'static'")
	}
}

func handleSetMTU(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 2 {
		return ipc.ErrInvalidArgs
	}
	slog.Info("set_mtu requested", "component", "dbus", "interface", args[0], "mode", args[1])
	return rpc.EmitResult(emit, nil, SetMTU(args[0], args[1]))
}

func handleEnableConnection(ctx context.Context, args []string, emit ipc.Events) error {
	iface, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("enable_connection requested", "component", "dbus", "interface", iface)
	return rpc.EmitResult(emit, nil, EnableConnection(iface))
}

func handleDisableConnection(ctx context.Context, args []string, emit ipc.Events) error {
	iface, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("disable_connection requested", "component", "dbus", "interface", iface)
	return rpc.EmitResult(emit, nil, DisableConnection(iface))
}

func handleSetHostname(ctx context.Context, args []string, emit ipc.Events) error {
	hostname, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_hostname requested", "component", "dbus", "service", hostname)
	return rpc.EmitResult(emit, nil, SetHostname(hostname))
}

func handleGetNTPStatus(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetNTPStatus()
	return rpc.EmitResult(emit, result, err)
}

func handleSetNTP(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}
	enabled := args[0] == "true"
	slog.Info("set_ntp requested", "component", "dbus", "enabled", enabled)
	return rpc.EmitResult(emit, nil, SetNTP(enabled))
}

func handleSetServerTime(ctx context.Context, args []string, emit ipc.Events) error {
	mode, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_server_time requested", "component", "dbus", "mode", mode)
	return rpc.EmitResult(emit, nil, SetServerTime(mode))
}

func handleGetTimezone(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetTimezone()
	return rpc.EmitResult(emit, result, err)
}

func handleSetTimezone(ctx context.Context, args []string, emit ipc.Events) error {
	timezone, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_timezone requested", "component", "dbus", "mode", timezone)
	return rpc.EmitResult(emit, nil, SetTimezone(timezone))
}

func handleGetNTPServers(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetNTPServers()
	return rpc.EmitResult(emit, result, err)
}

func handleSetNTPServers(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("set_ntp_servers requested", "component", "dbus", "server_count", len(args))
	return rpc.EmitResult(emit, nil, SetNTPServers(args))
}
