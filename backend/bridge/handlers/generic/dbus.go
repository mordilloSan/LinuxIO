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
		"call": callDbusMethod,
	}
}

func callDbusMethod(args []string) (any, error) {
	// args[0] = bus ("system" or "session")
	// args[1] = destination (e.g., "org.freedesktop.login1")
	// args[2] = path (e.g., "/org/freedesktop/login1")
	// args[3] = interface (e.g., "org.freedesktop.login1.Manager")
	// args[4] = method (e.g., "Reboot")
	// args[5+] = method arguments (optional)

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
