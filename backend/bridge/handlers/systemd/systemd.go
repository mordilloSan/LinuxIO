package systemd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	godbus "github.com/godbus/dbus/v5"
)

const (
	systemdBusName    = "org.freedesktop.systemd1"
	systemdObjectPath = "/org/freedesktop/systemd1"
	systemdMgrIface   = "org.freedesktop.systemd1.Manager"
	systemdUnitIface  = "org.freedesktop.systemd1.Unit"
	propertiesIface   = "org.freedesktop.DBus.Properties"
)

type Client struct {
	conn    *godbus.Conn
	manager godbus.BusObject
}

func New() (*Client, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	return &Client{
		conn:    conn,
		manager: conn.Object(systemdBusName, godbus.ObjectPath(systemdObjectPath)),
	}, nil
}

func (c *Client) Close() {
	if c == nil || c.conn == nil {
		return
	}
	if err := c.conn.Close(); err != nil {
		slog.Debug("failed to close systemd D-Bus connection", "component", "dbus", "subsystem", "systemd", "error", err)
	}
}

func withClient(ctx context.Context, call func(*Client) error) error {
	ctx = requireContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := New()
	if err != nil {
		return err
	}
	defer client.Close()
	return call(client)
}

func requireContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func requireUnitName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("unit name is required")
	}
	return nil
}

func requireUnitNames(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("at least one unit name is required")
	}
	for _, name := range names {
		if err := requireUnitName(name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Start(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	var job godbus.ObjectPath
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".StartUnit", 0, name, "replace").Store(&job); err != nil {
		return fmt.Errorf("start unit %s: %w", name, err)
	}
	return nil
}

func (c *Client) Stop(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	var job godbus.ObjectPath
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".StopUnit", 0, name, "replace").Store(&job); err != nil {
		return fmt.Errorf("stop unit %s: %w", name, err)
	}
	return nil
}

func (c *Client) Restart(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	var job godbus.ObjectPath
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".RestartUnit", 0, name, "replace").Store(&job); err != nil {
		return fmt.Errorf("restart unit %s: %w", name, err)
	}
	return nil
}

func (c *Client) ReloadUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	var job godbus.ObjectPath
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".ReloadUnit", 0, name, "replace").Store(&job); err != nil {
		return fmt.Errorf("reload unit %s: %w", name, err)
	}
	return nil
}

func (c *Client) Enable(ctx context.Context, names ...string) error {
	if err := requireUnitNames(names); err != nil {
		return err
	}
	var carriesInstallInfo bool
	var changes [][]any
	if err := c.manager.CallWithContext(
		requireContext(ctx),
		systemdMgrIface+".EnableUnitFiles",
		0,
		names,
		false,
		true,
	).Store(&carriesInstallInfo, &changes); err != nil {
		return fmt.Errorf("enable unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func (c *Client) Disable(ctx context.Context, names ...string) error {
	if err := requireUnitNames(names); err != nil {
		return err
	}
	var changes [][]any
	if err := c.manager.CallWithContext(
		requireContext(ctx),
		systemdMgrIface+".DisableUnitFiles",
		0,
		names,
		false,
	).Store(&changes); err != nil {
		return fmt.Errorf("disable unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func (c *Client) Mask(ctx context.Context, names ...string) error {
	if err := requireUnitNames(names); err != nil {
		return err
	}
	var changes [][]any
	if err := c.manager.CallWithContext(
		requireContext(ctx),
		systemdMgrIface+".MaskUnitFiles",
		0,
		names,
		false,
		true,
	).Store(&changes); err != nil {
		return fmt.Errorf("mask unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func (c *Client) Unmask(ctx context.Context, names ...string) error {
	if err := requireUnitNames(names); err != nil {
		return err
	}
	var changes [][]any
	if err := c.manager.CallWithContext(
		requireContext(ctx),
		systemdMgrIface+".UnmaskUnitFiles",
		0,
		names,
		false,
	).Store(&changes); err != nil {
		return fmt.Errorf("unmask unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func (c *Client) ResetFailed(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".ResetFailedUnit", 0, name).Err; err != nil {
		return fmt.Errorf("reset failed unit %s: %w", name, err)
	}
	return nil
}

func (c *Client) GetUnitFileState(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var state string
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".GetUnitFileState", 0, name).Store(&state); err != nil {
		return "", fmt.Errorf("get unit file state %s: %w", name, err)
	}
	return state, nil
}

func (c *Client) Reload(ctx context.Context) error {
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".Reload", 0).Err; err != nil {
		return fmt.Errorf("reload systemd manager: %w", err)
	}
	return nil
}

func (c *Client) GetActiveState(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	ctx = requireContext(ctx)
	var path godbus.ObjectPath
	if err := c.manager.CallWithContext(ctx, systemdMgrIface+".GetUnit", 0, name).Store(&path); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Unit not loaded means inactive for the callers that use this helper.
		return "inactive", nil
	}

	unit := c.conn.Object(systemdBusName, path)
	var prop godbus.Variant
	if err := unit.CallWithContext(ctx, propertiesIface+".Get", 0, systemdUnitIface, "ActiveState").Store(&prop); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "unknown", nil
	}
	state, ok := prop.Value().(string)
	if !ok {
		return "unknown", nil
	}
	return state, nil
}

type UnitStatus struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
}

func (c *Client) ListUnitsWithPrefix(ctx context.Context, prefix string) ([]UnitStatus, error) {
	var result [][]any
	if err := c.manager.CallWithContext(requireContext(ctx), systemdMgrIface+".ListUnits", 0).Store(&result); err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}

	units := make([]UnitStatus, 0)
	for _, unit := range result {
		if len(unit) < 5 {
			continue
		}
		name, ok := unit[0].(string)
		if !ok || !strings.HasPrefix(name, prefix) {
			continue
		}
		description, _ := unit[1].(string)
		loadState, _ := unit[2].(string)
		activeState, _ := unit[3].(string)
		subState, _ := unit[4].(string)
		units = append(units, UnitStatus{
			Name:        name,
			Description: description,
			LoadState:   loadState,
			ActiveState: activeState,
			SubState:    subState,
		})
	}
	return units, nil
}

func StartUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		return client.Start(ctx, name)
	})
}

func StopUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		return client.Stop(ctx, name)
	})
}

func RestartUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		return client.Restart(ctx, name)
	})
}

func ReloadUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		return client.ReloadUnit(ctx, name)
	})
}

func EnableUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		if err := client.Enable(ctx, name); err != nil {
			return err
		}
		return client.Reload(ctx)
	})
}

func DisableUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		if err := client.Disable(ctx, name); err != nil {
			return err
		}
		return client.Reload(ctx)
	})
}

func MaskUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		if err := client.Mask(ctx, name); err != nil {
			return err
		}
		return client.Reload(ctx)
	})
}

func UnmaskUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		if err := client.Unmask(ctx, name); err != nil {
			return err
		}
		return client.Reload(ctx)
	})
}

func ResetFailedUnit(ctx context.Context, name string) error {
	return withClient(ctx, func(client *Client) error {
		return client.ResetFailed(ctx, name)
	})
}

func GetUnitFileState(ctx context.Context, name string) (string, error) {
	var state string
	if err := withClient(ctx, func(client *Client) error {
		var err error
		state, err = client.GetUnitFileState(ctx, name)
		return err
	}); err != nil {
		return "", err
	}
	return state, nil
}

func DaemonReload(ctx context.Context) error {
	return withClient(ctx, func(client *Client) error {
		return client.Reload(ctx)
	})
}

func GetActiveState(ctx context.Context, name string) (string, error) {
	var state string
	if err := withClient(ctx, func(client *Client) error {
		var err error
		state, err = client.GetActiveState(ctx, name)
		return err
	}); err != nil {
		return "", err
	}
	return state, nil
}

func ListUnitsWithPrefix(ctx context.Context, prefix string) ([]UnitStatus, error) {
	var units []UnitStatus
	if err := withClient(ctx, func(client *Client) error {
		var err error
		units, err = client.ListUnitsWithPrefix(ctx, prefix)
		return err
	}); err != nil {
		return nil, err
	}
	return units, nil
}
