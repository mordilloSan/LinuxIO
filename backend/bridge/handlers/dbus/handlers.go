package dbus

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers() {
	// System control
	ipc.RegisterFunc("dbus", "reboot", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("reboot requested")
		if err := CallLogin1Action("Reboot"); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "power_off", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("power_off requested")
		if err := CallLogin1Action("PowerOff"); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Updates management
	ipc.RegisterFunc("dbus", "get_updates", func(ctx context.Context, args []string, emit ipc.Events) error {
		updates, err := GetUpdatesWithDetails()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	ipc.RegisterFunc("dbus", "get_updates_basic", func(ctx context.Context, args []string, emit ipc.Events) error {
		updates, err := GetUpdatesBasic()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	ipc.RegisterFunc("dbus", "get_update_detail", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		detail, err := GetSingleUpdateDetail(args[0])
		if err != nil {
			return err
		}
		return emit.Result(detail)
	})

	ipc.RegisterFunc("dbus", "install_package", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("install_package requested: package=%s", args[0])
		if err := InstallPackage(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "get_auto_updates", func(ctx context.Context, args []string, emit ipc.Events) error {
		state, err := getAutoUpdates()
		if err != nil {
			return err
		}
		return emit.Result(state)
	})

	ipc.RegisterFunc("dbus", "set_auto_updates", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("set_auto_updates requested: mode=%s", args[0])
		result, err := setAutoUpdates(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("dbus", "apply_offline_updates", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("apply_offline_updates requested")
		result, err := applyOfflineUpdates()
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("dbus", "get_update_history", func(ctx context.Context, args []string, emit ipc.Events) error {
		history, err := GetUpdateHistory()
		if err != nil {
			return err
		}
		return emit.Result(history)
	})

	// Timer management
	ipc.RegisterFunc("dbus", "list_timers", func(ctx context.Context, args []string, emit ipc.Events) error {
		timers, err := ListTimers()
		if err != nil {
			return err
		}
		return emit.Result(timers)
	})

	// Socket management
	ipc.RegisterFunc("dbus", "list_sockets", func(ctx context.Context, args []string, emit ipc.Events) error {
		sockets, err := ListSockets()
		if err != nil {
			return err
		}
		return emit.Result(sockets)
	})

	// Service management
	ipc.RegisterFunc("dbus", "list_services", func(ctx context.Context, args []string, emit ipc.Events) error {
		services, err := ListServices()
		if err != nil {
			return err
		}
		return emit.Result(services)
	})

	ipc.RegisterFunc("dbus", "get_unit_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		info, err := GetUnitInfo(args[0])
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	ipc.RegisterFunc("dbus", "start_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("start_service requested: unit=%s", args[0])
		if err := StartService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "stop_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("stop_service requested: unit=%s", args[0])
		if err := StopService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "restart_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("restart_service requested: unit=%s", args[0])
		if err := RestartService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "reload_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("reload_service requested: unit=%s", args[0])
		if err := ReloadService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "enable_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("enable_service requested: unit=%s", args[0])
		if err := EnableService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "disable_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("disable_service requested: unit=%s", args[0])
		if err := DisableService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "mask_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("mask_service requested: unit=%s", args[0])
		if err := MaskService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "unmask_service", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("unmask_service requested: unit=%s", args[0])
		if err := UnmaskService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Network information
	ipc.RegisterFunc("dbus", "get_network_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		info, err := GetNetworkInfo()
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	// Network configuration - IPv4
	ipc.RegisterFunc("dbus", "set_ipv4_manual", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 4 {
			return ipc.ErrInvalidArgs
		}
		iface := args[0]
		addressCIDR := args[1]
		gateway := args[2]
		dnsServers := args[3:]
		logger.Infof("set_ipv4_manual requested: iface=%s address=%s gateway=%s dns_count=%d", iface, addressCIDR, gateway, len(dnsServers))
		if err := SetIPv4Manual(iface, addressCIDR, gateway, dnsServers); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "set_ipv4", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		iface, method := args[0], strings.ToLower(args[1])
		logger.Infof("set_ipv4 requested: iface=%s method=%s", iface, method)
		switch method {
		case "dhcp", "auto":
			if err := SetIPv4DHCP(iface); err != nil {
				return err
			}
			return emit.Result(nil)
		default:
			return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
		}
	})

	// Network configuration - IPv6
	ipc.RegisterFunc("dbus", "set_ipv6", func(ctx context.Context, args []string, emit ipc.Events) error {
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
			return emit.Result(nil)
		case "static":
			if len(args) != 3 {
				return ipc.ErrInvalidArgs
			}
			if err := SetIPv6Static(iface, args[2]); err != nil {
				return err
			}
			return emit.Result(nil)
		default:
			return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'static'")
		}
	})

	// Network configuration - Other
	ipc.RegisterFunc("dbus", "set_mtu", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 2 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("set_mtu requested: iface=%s mtu=%s", args[0], args[1])
		if err := SetMTU(args[0], args[1]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Network connection enable/disable
	ipc.RegisterFunc("dbus", "enable_connection", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("enable_connection requested: connection=%s", args[0])
		if err := EnableConnection(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "disable_connection", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		logger.Infof("disable_connection requested: connection=%s", args[0])
		if err := DisableConnection(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})
}
