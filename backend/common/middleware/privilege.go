package middleware

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RequirePrivileged wraps a single handler to enforce privilege checking.
// Use this for fine-grained control when only some handlers need privilege.
//
// Usage:
//
//	func ModuleHandlers(sess *session.Session) map[string]func([]string) (any, error) {
//	    return map[string]func([]string) (any, error){
//	        "GetModules":      GetModules,                                      // Public
//	        "UninstallModule": middleware.RequirePrivileged(sess, Uninstall),   // Privileged
//	    }
//	}
func RequirePrivileged(sess *session.Session, handler func([]string) (any, error)) func([]string) (any, error) {
	return func(args []string) (any, error) {
		if !sess.Privileged {
			return nil, fmt.Errorf("operation requires administrator privileges")
		}
		return handler(args)
	}
}

// RequirePrivilegedAll wraps all handlers in a map to enforce privilege checking.
// Use this in register.go when ALL handlers in a package require privilege.
//
// Usage:
//
//	HandlersByType["wireguard"] = middleware.RequirePrivilegedAll(sess, wireguard.WireguardHandlers())
func RequirePrivilegedAll(sess *session.Session, handlers map[string]func([]string) (any, error)) map[string]func([]string) (any, error) {
	wrapped := make(map[string]func([]string) (any, error), len(handlers))
	for name, handler := range handlers {
		wrapped[name] = RequirePrivileged(sess, handler)
	}
	return wrapped
}
