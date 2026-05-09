package dbus

import (
	"context"
	"fmt"
	"log/slog"

	godbus "github.com/godbus/dbus/v5"
)

const (
	login1BusName      = "org.freedesktop.login1"
	login1ObjectPath   = godbus.ObjectPath("/org/freedesktop/login1")
	login1ManagerIface = "org.freedesktop.login1.Manager"
)

// Login1Manager abstracts the org.freedesktop.login1.Manager interface.
type Login1Manager struct {
	conn *godbus.Conn
	obj  godbus.BusObject
}

// NewLogin1Manager connects to system D-Bus and prepares the login1 interface.
// Note: Caller must hold systemDBusMu lock if needed.
func NewLogin1Manager(context.Context) (*Login1Manager, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	obj := conn.Object(login1BusName, login1ObjectPath)
	return &Login1Manager{conn: conn, obj: obj}, nil
}

// Properly closes D-Bus connection when done
func (m *Login1Manager) Close() {
	if m.conn != nil {
		if err := m.conn.Close(); err != nil {
			slog.Debug("failed to close login1 D-Bus connection", "component", "dbus", "subsystem", "login1", "error", err)
		}
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

// TerminateLogin1Session calls org.freedesktop.login1.Manager.TerminateSession.
func TerminateLogin1Session(ctx context.Context, sessionID string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		manager, err := NewLogin1Manager(ctx)
		if err != nil {
			return err
		}
		defer manager.Close()
		return manager.TerminateSession(ctx, sessionID)
	})
}

// call runs a generic login1 method.
func (m *Login1Manager) call(ctx context.Context, method string) error {
	call := m.obj.CallWithContext(ctx, login1ManagerIface+"."+method, 0, false)
	if call.Err != nil {
		return fmt.Errorf("failed to call %s: %w", method, call.Err)
	}
	return nil
}

// TerminateSession ends the given systemd-logind session.
func (m *Login1Manager) TerminateSession(ctx context.Context, sessionID string) error {
	call := m.obj.CallWithContext(ctx, login1ManagerIface+".TerminateSession", 0, sessionID)
	if call.Err != nil {
		return fmt.Errorf("failed to call TerminateSession: %w", call.Err)
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
