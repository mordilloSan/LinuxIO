package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "dbus", []bridgeipc.Command{
		{Name: "get_network_info", Mode: bridgeipc.ModeQuery, Handler: handleGetNetworkInfo},
		{Name: "set_ipv4_manual", Mode: bridgeipc.ModeJob, Handler: handleSetIPv4Manual},
		{Name: "set_ipv4", Mode: bridgeipc.ModeJob, Handler: handleSetIPv4},
		{Name: "set_ipv6", Mode: bridgeipc.ModeJob, Handler: handleSetIPv6},
		{Name: "set_mtu", Mode: bridgeipc.ModeJob, Handler: handleSetMTU},
		{Name: "enable_connection", Mode: bridgeipc.ModeJob, Handler: handleEnableConnection},
		{Name: "disable_connection", Mode: bridgeipc.ModeJob, Handler: handleDisableConnection},
	})
}

func handleGetNetworkInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetNetworkInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetIPv4Manual(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 4); err != nil {
		return err
	}
	iface := args[0]
	addressCIDR := args[1]
	gateway := args[2]
	dnsServers := args[3:]
	return bridgeipc.EmitResult(emit, nil, SetIPv4Manual(ctx, iface, addressCIDR, gateway, dnsServers))
}

func handleSetIPv4(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	iface, method := args[0], strings.ToLower(args[1])
	if method != "dhcp" && method != "auto" {
		return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
	}
	return bridgeipc.EmitResult(emit, nil, SetIPv4DHCP(ctx, iface))
}

func handleSetIPv6(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	iface, method := args[0], strings.ToLower(args[1])
	switch method {
	case "dhcp", "auto":
		return bridgeipc.EmitResult(emit, nil, SetIPv6DHCP(ctx, iface))
	case "static":
		if len(args) != 3 {
			return bridgeipc.ErrInvalidArgs
		}
		return bridgeipc.EmitResult(emit, nil, SetIPv6Static(ctx, iface, args[2]))
	default:
		return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'static'")
	}
}

func handleSetMTU(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if len(args) != 2 {
		return bridgeipc.ErrInvalidArgs
	}
	return bridgeipc.EmitResult(emit, nil, SetMTU(ctx, args[0], args[1]))
}

func handleEnableConnection(ctx context.Context, args []string, emit bridgeipc.Events) error {
	iface, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, EnableConnection(ctx, iface))
}

func handleDisableConnection(ctx context.Context, args []string, emit bridgeipc.Events) error {
	iface, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, DisableConnection(ctx, iface))
}
