package dbusclient

import (
	"context"
	"fmt"
	"slices"
	"strings"

	godbus "github.com/godbus/dbus/v5"
)

// BusNameState describes whether a well-known D-Bus name is present now or can
// be activated by the bus.
type BusNameState struct {
	Active      bool
	Activatable bool
}

func (s BusNameState) Available() bool {
	return s.Active || s.Activatable
}

func BusNameAvailable(ctx context.Context, busName string) (bool, error) {
	var available bool
	err := UseSystemBusWithOptions(ctx, SystemBusOptions{
		Subsystem: "dbus",
	}, func(ctx context.Context, conn *godbus.Conn) error {
		var err error
		available, err = BusNameAvailableOnConnection(ctx, conn, busName)
		return err
	})
	return available, err
}

func BusNameAvailableOnConnection(ctx context.Context, conn *godbus.Conn, busName string) (bool, error) {
	state, err := ReadBusNameState(ctx, conn, busName)
	if err != nil {
		return false, err
	}
	return state.Available(), nil
}

func ReadBusNameState(ctx context.Context, conn *godbus.Conn, busName string) (BusNameState, error) {
	ctx = requireContext(ctx)
	if err := ctx.Err(); err != nil {
		return BusNameState{}, err
	}
	if conn == nil {
		return BusNameState{}, fmt.Errorf("nil D-Bus connection")
	}
	busName = strings.TrimSpace(busName)
	if busName == "" {
		return BusNameState{}, fmt.Errorf("empty D-Bus bus name")
	}

	var names []string
	if err := conn.BusObject().CallWithContext(ctx, DBusListNames, 0).Store(&names); err != nil {
		return BusNameState{}, fmt.Errorf("list D-Bus names: %w", err)
	}

	var activatable []string
	if err := conn.BusObject().CallWithContext(ctx, DBusListActivatableNames, 0).Store(&activatable); err != nil {
		return BusNameState{}, fmt.Errorf("list activatable D-Bus names: %w", err)
	}

	return BusNameState{
		Active:      slices.Contains(names, busName),
		Activatable: slices.Contains(activatable, busName),
	}, nil
}

func (o SystemObject) BusNameState(ctx context.Context) (BusNameState, error) {
	var state BusNameState
	err := UseSystemBusWithOptions(ctx, SystemBusOptions{
		Subsystem: o.Subsystem,
	}, func(ctx context.Context, conn *godbus.Conn) error {
		var err error
		state, err = o.BusNameStateOnConnection(ctx, conn)
		return err
	})
	return state, err
}

func (o SystemObject) BusNameStateOnConnection(ctx context.Context, conn *godbus.Conn) (BusNameState, error) {
	return ReadBusNameState(ctx, conn, o.BusName)
}

func (o SystemObject) Available(ctx context.Context) (bool, error) {
	state, err := o.BusNameState(ctx)
	if err != nil {
		return false, err
	}
	return state.Available(), nil
}

func (o SystemObject) AvailableOnConnection(ctx context.Context, conn *godbus.Conn) (bool, error) {
	state, err := o.BusNameStateOnConnection(ctx, conn)
	if err != nil {
		return false, err
	}
	return state.Available(), nil
}

func (o SystemObject) RequireAvailableOnConnection(ctx context.Context, conn *godbus.Conn) error {
	ok, err := o.AvailableOnConnection(ctx, conn)
	if err != nil {
		return err
	}
	if !ok {
		return o.unavailableError()
	}
	return nil
}

func (o SystemObject) unavailableError() error {
	if o.Unavailable != nil {
		return o.Unavailable
	}
	return fmt.Errorf("%s D-Bus service is unavailable", o.BusName)
}
