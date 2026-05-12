package network

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "get_network_info", Handler: handleGetNetworkInfo},
		{Name: "set_ipv4_manual", Handler: handleSetIPv4Manual},
		{Name: "set_ipv4", Handler: handleSetIPv4},
		{Name: "set_ipv6", Handler: handleSetIPv6},
		{Name: "set_mtu", Handler: handleSetMTU},
		{Name: "enable_connection", Handler: handleEnableConnection},
		{Name: "disable_connection", Handler: handleDisableConnection},
	})
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
	slog.Info("set_ipv4_manual requested", "component", "dbus", "subsystem", "network", "interface", iface, "path", addressCIDR, "service", gateway, "dns_count", len(dnsServers))
	return rpc.EmitResult(emit, nil, SetIPv4Manual(iface, addressCIDR, gateway, dnsServers))
}

func handleSetIPv4(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	iface, method := args[0], strings.ToLower(args[1])
	slog.Info("set_ipv4 requested", "component", "dbus", "subsystem", "network", "interface", iface, "mode", method)
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
	slog.Info("set_ipv6 requested", "component", "dbus", "subsystem", "network", "interface", iface, "mode", method)
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
	slog.Info("set_mtu requested", "component", "dbus", "subsystem", "network", "interface", args[0], "mode", args[1])
	return rpc.EmitResult(emit, nil, SetMTU(args[0], args[1]))
}

func handleEnableConnection(ctx context.Context, args []string, emit ipc.Events) error {
	iface, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("enable_connection requested", "component", "dbus", "subsystem", "network", "interface", iface)
	return rpc.EmitResult(emit, nil, EnableConnection(iface))
}

func handleDisableConnection(ctx context.Context, args []string, emit ipc.Events) error {
	iface, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("disable_connection requested", "component", "dbus", "subsystem", "network", "interface", iface)
	return rpc.EmitResult(emit, nil, DisableConnection(iface))
}
