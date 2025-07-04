package dbus

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

func GetHostname() (result string, err error) {
	err = RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		// Handle close error if main operation succeeded
		defer func() {
			if cerr := conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()

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
	return
}
