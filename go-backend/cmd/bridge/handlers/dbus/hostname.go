package dbus

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

func GetHostname() (string, error) {
	var result string
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()

		obj := conn.Object("org.freedesktop.hostname1", "/org/freedesktop/hostname1")
		var variant dbus.Variant
		err = obj.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.hostname1", "Hostname").Store(&variant)
		if err != nil {
			return err
		}
		hostname, ok := variant.Value().(string)
		if !ok {
			return fmt.Errorf("hostname value not a string (got %T)", variant.Value())
		}
		result = hostname
		return nil
	})
	return result, err
}
