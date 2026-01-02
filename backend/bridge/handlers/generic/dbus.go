package generic

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

var systemBus *dbus.Conn
var sessionBus *dbus.Conn

func init() {
	systemBus, _ = dbus.ConnectSystemBus()
	sessionBus, _ = dbus.ConnectSessionBus()
}

func DbusHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		// NOTE: Direct DBus calls are DISABLED for security
		// DBus calls must be defined in module YAML files
		// Use CallDbusMethodDirect() from module loader instead
		"call": disabledDbusHandler,
	}
}

// disabledDbusHandler returns an error explaining that direct DBus calls are disabled
func disabledDbusHandler(args []string) (any, error) {
	return nil, fmt.Errorf("direct DBus calls are disabled - calls must be defined in module YAML files")
}

// CallDbusMethodDirect executes a DBus call directly (used by module loader)
// This bypasses security checks and should only be called by whitelisted module handlers
func CallDbusMethodDirect(args []string) (any, error) {
	if len(args) < 5 {
		return nil, fmt.Errorf("dbus call requires: bus, destination, path, interface, method")
	}

	busType := args[0]
	destination := args[1]
	path := args[2]
	iface := args[3]
	method := args[4]
	methodArgs := args[5:]

	// Select bus
	var conn *dbus.Conn
	if busType == "session" {
		conn = sessionBus
	} else {
		conn = systemBus
	}

	if conn == nil {
		return nil, fmt.Errorf("failed to connect to %s bus", busType)
	}

	// Get object
	obj := conn.Object(destination, dbus.ObjectPath(path))

	// Convert string args to interface{} for DBus
	dbusArgs := make([]interface{}, len(methodArgs))
	for i, arg := range methodArgs {
		dbusArgs[i] = arg
	}

	// Call method
	call := obj.Call(iface+"."+method, 0, dbusArgs...)
	if call.Err != nil {
		return nil, call.Err
	}

	// Return result (if any)
	if len(call.Body) > 0 {
		return call.Body[0], nil
	}

	return nil, nil
}
