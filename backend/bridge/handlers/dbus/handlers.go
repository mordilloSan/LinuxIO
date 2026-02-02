package dbus

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers() {
	// System control
	ipc.RegisterFunc("dbus", "Reboot", func(ctx context.Context, args []string, emit ipc.Events) error {
		if err := CallLogin1Action("Reboot"); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "PowerOff", func(ctx context.Context, args []string, emit ipc.Events) error {
		if err := CallLogin1Action("PowerOff"); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Updates management
	ipc.RegisterFunc("dbus", "GetUpdates", func(ctx context.Context, args []string, emit ipc.Events) error {
		updates, err := GetUpdatesWithDetails()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	ipc.RegisterFunc("dbus", "GetUpdatesBasic", func(ctx context.Context, args []string, emit ipc.Events) error {
		updates, err := GetUpdatesBasic()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	ipc.RegisterFunc("dbus", "GetUpdateDetail", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		detail, err := GetSingleUpdateDetail(args[0])
		if err != nil {
			return err
		}
		return emit.Result(detail)
	})

	ipc.RegisterFunc("dbus", "InstallPackage", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := InstallPackage(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "GetAutoUpdates", func(ctx context.Context, args []string, emit ipc.Events) error {
		state, err := getAutoUpdates()
		if err != nil {
			return err
		}
		return emit.Result(state)
	})

	ipc.RegisterFunc("dbus", "SetAutoUpdates", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := setAutoUpdates(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("dbus", "ApplyOfflineUpdates", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := applyOfflineUpdates()
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("dbus", "GetUpdateHistory", func(ctx context.Context, args []string, emit ipc.Events) error {
		history, err := GetUpdateHistory()
		if err != nil {
			return err
		}
		return emit.Result(history)
	})

	// Service management
	ipc.RegisterFunc("dbus", "ListServices", func(ctx context.Context, args []string, emit ipc.Events) error {
		services, err := ListServices()
		if err != nil {
			return err
		}
		return emit.Result(services)
	})

	ipc.RegisterFunc("dbus", "GetServiceInfo", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		info, err := GetServiceInfo(args[0])
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	ipc.RegisterFunc("dbus", "StartService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := StartService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "StopService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := StopService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "RestartService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := RestartService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "ReloadService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := ReloadService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "EnableService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := EnableService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "DisableService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := DisableService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "MaskService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := MaskService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "UnmaskService", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := UnmaskService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Network information
	ipc.RegisterFunc("dbus", "GetNetworkInfo", func(ctx context.Context, args []string, emit ipc.Events) error {
		info, err := GetNetworkInfo()
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	// Network configuration - IPv4
	ipc.RegisterFunc("dbus", "SetIPv4Manual", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 4 {
			return ipc.ErrInvalidArgs
		}
		iface := args[0]
		addressCIDR := args[1]
		gateway := args[2]
		dnsServers := args[3:]
		if err := SetIPv4Manual(iface, addressCIDR, gateway, dnsServers); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "SetIPv4", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		iface, method := args[0], strings.ToLower(args[1])
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
	ipc.RegisterFunc("dbus", "SetIPv6", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		iface, method := args[0], strings.ToLower(args[1])
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
	ipc.RegisterFunc("dbus", "SetMTU", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 2 {
			return ipc.ErrInvalidArgs
		}
		if err := SetMTU(args[0], args[1]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Network connection enable/disable
	ipc.RegisterFunc("dbus", "EnableConnection", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		if err := EnableConnection(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("dbus", "DisableConnection", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) != 1 {
			return ipc.ErrInvalidArgs
		}
		if err := DisableConnection(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})
}
