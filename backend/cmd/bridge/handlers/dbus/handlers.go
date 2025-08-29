package dbus

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/internal/ipc"
)

// Needed to make sure one d-bus call at a time!
var systemDBusMu sync.Mutex

func DbusHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"Reboot":         func([]string) (any, error) { return nil, CallLogin1Action("Reboot") },
		"PowerOff":       func([]string) (any, error) { return nil, CallLogin1Action("PowerOff") },
		"GetUpdates":     func([]string) (any, error) { return GetUpdatesWithDetails() },
		"InstallPackage": func(args []string) (any, error) { return nil, InstallPackage(args[0]) },
		"ListServices":   func([]string) (any, error) { return ListServices() },
		"GetServiceInfo": func(args []string) (any, error) { return GetServiceInfo(args[0]) },
		"StartService":   func(args []string) (any, error) { return nil, StartService(args[0]) },
		"StopService":    func(args []string) (any, error) { return nil, StopService(args[0]) },
		"RestartService": func(args []string) (any, error) { return nil, RestartService(args[0]) },
		"ReloadService":  func(args []string) (any, error) { return nil, ReloadService(args[0]) },
		"EnableService":  func(args []string) (any, error) { return nil, EnableService(args[0]) },
		"DisableService": func(args []string) (any, error) { return nil, DisableService(args[0]) },
		"MaskService":    func(args []string) (any, error) { return nil, MaskService(args[0]) },
		"UnmaskService":  func(args []string) (any, error) { return nil, UnmaskService(args[0]) },
		"GetNetworkInfo": func([]string) (any, error) { return GetNetworkInfo() },
		"SetDNS":         func(args []string) (any, error) { return nil, SetDNS(args[0], args[1:]) },
		"SetGateway":     func(args []string) (any, error) { return nil, SetGateway(args[0], args[1]) },
		"SetMTU":         func(args []string) (any, error) { return nil, SetMTU(args[0], args[1]) },
		"SetIPv4": func(args []string) (any, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("SetIPv4 requires interface and method (dhcp/static)")
			}
			iface, method := args[0], strings.ToLower(args[1])
			switch method {
			case "dhcp":
				return nil, SetIPv4DHCP(iface)
			case "static":
				if len(args) != 3 {
					return nil, fmt.Errorf("SetIPv4 static requires addressCIDR")
				}
				return nil, SetIPv4Static(iface, args[2])
			default:
				return nil, fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
			}
		},
		"SetIPv6": func(args []string) (any, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("SetIPv6 requires interface and method (dhcp/static)")
			}
			iface, method := args[0], strings.ToLower(args[1])
			switch method {
			case "dhcp":
				return nil, SetIPv6DHCP(iface)
			case "static":
				if len(args) != 3 {
					return nil, fmt.Errorf("SetIPv6 static requires addressCIDR")
				}
				return nil, SetIPv6Static(iface, args[2])
			default:
				return nil, fmt.Errorf("SetIPv6 method must be 'dhcp' or 'static'")
			}
		},
	}
}

// --- Retry Wrapper ---
func RetryOnceIfClosed(initialErr error, do func() error) error {
	if initialErr == nil {
		err := do()
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			time.Sleep(150 * time.Millisecond)
			return do()
		}
		return err
	}
	if strings.Contains(initialErr.Error(), "use of closed network connection") {
		time.Sleep(150 * time.Millisecond)
		return do()
	}
	return initialErr
}
