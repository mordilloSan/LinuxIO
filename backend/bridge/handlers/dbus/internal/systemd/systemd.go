package systemd

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

func New() (*Client, error) {
	c, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	obj := c.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))
	return &Client{conn: c, obj: obj}, nil
}

func (c *Client) Close() { _ = c.conn.Close() }

func (c *Client) Enable(ctx context.Context, names ...string) error {
	var carries_install_info bool
	var changes [][]any // Fixed: was [][]string
	return c.obj.CallWithContext(ctx,
		"org.freedesktop.systemd1.Manager.EnableUnitFiles", 0,
		names, false, true,
	).Store(&carries_install_info, &changes)
}

func (c *Client) Disable(ctx context.Context, names ...string) error {
	var changes [][]any // Fixed: was [][]string
	return c.obj.CallWithContext(ctx,
		"org.freedesktop.systemd1.Manager.DisableUnitFiles", 0,
		names, false,
	).Store(&changes)
}

func (c *Client) Start(ctx context.Context, name string) error {
	var job string
	return c.obj.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.StartUnit", 0, name, "replace").Store(&job)
}

func (c *Client) Stop(ctx context.Context, name string) error {
	var job string
	return c.obj.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.StopUnit", 0, name, "replace").Store(&job)
}

func (c *Client) Restart(ctx context.Context, name string) error {
	var job string
	return c.obj.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.RestartUnit", 0, name, "replace").Store(&job)
}

func (c *Client) Reload(ctx context.Context) error {
	return c.obj.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.Reload", 0).Err
}

func OnCalendarFor(freq string) (string, error) {
	switch freq {
	case "hourly":
		return "hourly", nil
	case "daily":
		return "daily", nil
	case "weekly":
		return "weekly", nil
	default:
		return "", fmt.Errorf("invalid frequency: %s", freq)
	}
}
