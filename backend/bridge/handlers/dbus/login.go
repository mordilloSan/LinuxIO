package dbus

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

// Login1Manager abstracts the org.freedesktop.login1.Manager interface.
type Login1Manager struct {
	conn *godbus.Conn
	obj  godbus.BusObject
}

// NewLogin1Manager connects to system D-Bus and prepares the login1 interface.
// Note: Caller must hold systemDBusMu lock if needed.
func NewLogin1Manager(context.Context) (*Login1Manager, error) {
	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	obj := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	return &Login1Manager{conn: conn, obj: obj}, nil
}

// Properly closes D-Bus connection when done
func (m *Login1Manager) Close() {
	if m.conn != nil {
		_ = m.conn.Close()
	}
}

// CallLogin1Action is a helper function to call a login1 action, retried if D-Bus is closed.
func CallLogin1Action(action string) error {
	systemDBusMu.Lock() // LOCK at top
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		manager, err := NewLogin1Manager(context.Background())
		if err != nil {
			return err
		}
		defer manager.Close()
		return manager.call(context.Background(), action)
	})
}

// call runs a generic login1 method.
func (m *Login1Manager) call(ctx context.Context, method string) error {
	call := m.obj.CallWithContext(ctx, "org.freedesktop.login1.Manager."+method, 0, false)
	if call.Err != nil {
		return fmt.Errorf("failed to call %s: %w", method, call.Err)
	}
	return nil
}

// Reboot calls the D-Bus method to reboot the system.
func (m *Login1Manager) Reboot(ctx context.Context) error {
	return m.call(ctx, "Reboot")
}

// PowerOff calls the D-Bus method to power off the system.
func (m *Login1Manager) PowerOff(ctx context.Context) error {
	return m.call(ctx, "PowerOff")
}
