package pkgkit

import (
	"errors"
	"fmt"
	"slices"

	godbus "github.com/godbus/dbus/v5"
)

const (
	BusName                 = "org.freedesktop.PackageKit"
	ObjectPath              = "/org/freedesktop/PackageKit"
	CreateTransactionMethod = BusName + ".CreateTransaction"
	TransactionInterface    = BusName + ".Transaction"
	OfflineInterface        = BusName + ".Offline"
)

var ErrUnavailable = errors.New("PackageKit D-Bus service is unavailable")

func Available() (bool, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return false, fmt.Errorf("connect to system bus: %w", err)
	}
	defer conn.Close()

	return AvailableOnConnection(conn)
}

func AvailableOnConnection(conn *godbus.Conn) (bool, error) {
	if conn == nil {
		return false, fmt.Errorf("nil D-Bus connection")
	}

	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return false, fmt.Errorf("list D-Bus names: %w", err)
	}
	if slices.Contains(names, BusName) {
		return true, nil
	}

	var activatable []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false, fmt.Errorf("list activatable D-Bus names: %w", err)
	}

	return slices.Contains(activatable, BusName), nil
}

func RequireAvailableOnConnection(conn *godbus.Conn) error {
	ok, err := AvailableOnConnection(conn)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnavailable
	}
	return nil
}
