package dbus

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// Needed to make sure one d-bus call at a time!
var systemDBusMu sync.Mutex

func DbusHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		// System control
		"Reboot":   func([]string) (any, error) { return nil, CallLogin1Action("Reboot") },
		"PowerOff": func([]string) (any, error) { return nil, CallLogin1Action("PowerOff") },

		// Updates management
		"GetUpdates":     func([]string) (any, error) { return GetUpdatesWithDetails() },
		"InstallPackage": func(args []string) (any, error) { return nil, InstallPackage(args[0]) },
		"GetAutoUpdates": func([]string) (any, error) { return getAutoUpdates() },
		"SetAutoUpdates": func(args []string) (any, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("SetAutoUpdates expects 1 JSON arg")
			}
			return setAutoUpdates(args[0])
		},
		"ApplyOfflineUpdates": func([]string) (any, error) { return applyOfflineUpdates() },

		// Service management
		"ListServices":   func([]string) (any, error) { return ListServices() },
		"GetServiceInfo": func(args []string) (any, error) { return GetServiceInfo(args[0]) },
		"GetServiceLogs": func(args []string) (any, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("GetServiceLogs requires serviceName and lines")
			}
			return GetServiceLogs(args[0], args[1])
		},
		"StartService":   func(args []string) (any, error) { return nil, StartService(args[0]) },
		"StopService":    func(args []string) (any, error) { return nil, StopService(args[0]) },
		"RestartService": func(args []string) (any, error) { return nil, RestartService(args[0]) },
		"ReloadService":  func(args []string) (any, error) { return nil, ReloadService(args[0]) },
		"EnableService":  func(args []string) (any, error) { return nil, EnableService(args[0]) },
		"DisableService": func(args []string) (any, error) { return nil, DisableService(args[0]) },
		"MaskService":    func(args []string) (any, error) { return nil, MaskService(args[0]) },
		"UnmaskService":  func(args []string) (any, error) { return nil, UnmaskService(args[0]) },

		// Network information
		"GetNetworkInfo": func([]string) (any, error) { return GetNetworkInfo() },

		// Network configuration - IPv4
		"SetIPv4Manual": func(args []string) (any, error) {
			if len(args) < 4 {
				return nil, fmt.Errorf("SetIPv4Manual requires interface, addressCIDR, gateway, and dns servers")
			}
			iface := args[0]
			addressCIDR := args[1]
			gateway := args[2]
			dnsServers := args[3:] // All remaining args are DNS servers
			return nil, SetIPv4Manual(iface, addressCIDR, gateway, dnsServers)
		},
		"SetIPv4": func(args []string) (any, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("SetIPv4 requires interface and method (dhcp/static)")
			}
			iface, method := args[0], strings.ToLower(args[1])
			switch method {
			case "dhcp", "auto":
				return nil, SetIPv4DHCP(iface)
			default:
				return nil, fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
			}
		},

		// Network configuration - IPv6
		"SetIPv6": func(args []string) (any, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("SetIPv6 requires interface and method (dhcp/static)")
			}
			iface, method := args[0], strings.ToLower(args[1])
			switch method {
			case "dhcp", "auto":
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

		// Network configuration - Other
		"SetMTU": func(args []string) (any, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("SetMTU requires interface and MTU value")
			}
			return nil, SetMTU(args[0], args[1])
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
