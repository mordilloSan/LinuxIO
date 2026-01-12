package dbus

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handler"
)

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers() {
	// System control
	handler.RegisterFunc("dbus", "Reboot", func(ctx context.Context, args []string, emit handler.Events) error {
		if err := CallLogin1Action("Reboot"); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "PowerOff", func(ctx context.Context, args []string, emit handler.Events) error {
		if err := CallLogin1Action("PowerOff"); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Updates management
	handler.RegisterFunc("dbus", "GetUpdates", func(ctx context.Context, args []string, emit handler.Events) error {
		updates, err := GetUpdatesWithDetails()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	handler.RegisterFunc("dbus", "GetUpdatesBasic", func(ctx context.Context, args []string, emit handler.Events) error {
		updates, err := GetUpdatesBasic()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	handler.RegisterFunc("dbus", "GetUpdateDetail", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		detail, err := GetSingleUpdateDetail(args[0])
		if err != nil {
			return err
		}
		return emit.Result(detail)
	})

	handler.RegisterFunc("dbus", "InstallPackage", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := InstallPackage(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "GetAutoUpdates", func(ctx context.Context, args []string, emit handler.Events) error {
		state, err := getAutoUpdates()
		if err != nil {
			return err
		}
		return emit.Result(state)
	})

	handler.RegisterFunc("dbus", "SetAutoUpdates", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) != 1 {
			return handler.ErrInvalidArgs
		}
		result, err := setAutoUpdates(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("dbus", "ApplyOfflineUpdates", func(ctx context.Context, args []string, emit handler.Events) error {
		result, err := applyOfflineUpdates()
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("dbus", "GetUpdateHistory", func(ctx context.Context, args []string, emit handler.Events) error {
		history, err := GetUpdateHistory()
		if err != nil {
			return err
		}
		return emit.Result(history)
	})

	// Service management
	handler.RegisterFunc("dbus", "ListServices", func(ctx context.Context, args []string, emit handler.Events) error {
		services, err := ListServices()
		if err != nil {
			return err
		}
		return emit.Result(services)
	})

	handler.RegisterFunc("dbus", "GetServiceInfo", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		info, err := GetServiceInfo(args[0])
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	handler.RegisterFunc("dbus", "StartService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := StartService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "StopService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := StopService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "RestartService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := RestartService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "ReloadService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := ReloadService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "EnableService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := EnableService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "DisableService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := DisableService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "MaskService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := MaskService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	handler.RegisterFunc("dbus", "UnmaskService", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) == 0 {
			return handler.ErrInvalidArgs
		}
		if err := UnmaskService(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Network information
	handler.RegisterFunc("dbus", "GetNetworkInfo", func(ctx context.Context, args []string, emit handler.Events) error {
		info, err := GetNetworkInfo()
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	// Network configuration - IPv4
	handler.RegisterFunc("dbus", "SetIPv4Manual", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 4 {
			return handler.ErrInvalidArgs
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

	handler.RegisterFunc("dbus", "SetIPv4", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 2 {
			return handler.ErrInvalidArgs
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
	handler.RegisterFunc("dbus", "SetIPv6", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 2 {
			return handler.ErrInvalidArgs
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
				return handler.ErrInvalidArgs
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
	handler.RegisterFunc("dbus", "SetMTU", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) != 2 {
			return handler.ErrInvalidArgs
		}
		if err := SetMTU(args[0], args[1]); err != nil {
			return err
		}
		return emit.Result(nil)
	})
}
