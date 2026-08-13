// Package transientunit contains the small systemd D-Bus adapter shared by
// durable executors.  Route packages own their unit properties and command
// policy; this package only deals with the common transient-unit lifecycle.
package transientunit

import (
	"context"
	"errors"
	"fmt"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

var ErrNotFound = errors.New("transient unit not found")

type Property struct {
	Name  string
	Value godbus.Variant
}

type AuxiliaryUnit struct {
	Name       string
	Properties []Property
}

type ExecCommand struct {
	Path          string
	Arguments     []string
	IgnoreFailure bool
}

type State struct {
	ActiveState   string
	SubState      string
	ServiceResult string
	ExitCode      int
}

// Options keeps D-Bus timeouts and subsystem labels at the call site without
// duplicating the lifecycle implementation in each executor adapter.
type Options struct {
	Subsystem      string
	StartTimeout   time.Duration
	InspectTimeout time.Duration
	CollectTimeout time.Duration
	NoRetry        bool
}

func (o Options) start() dbusclient.SystemBusOptions {
	return dbusclient.SystemBusOptions{Subsystem: o.Subsystem, Timeout: durationOr(o.StartTimeout, 15*time.Second), NoRetry: o.NoRetry}
}

func (o Options) inspect() dbusclient.SystemBusOptions {
	return dbusclient.SystemBusOptions{Subsystem: o.Subsystem, Timeout: durationOr(o.InspectTimeout, 10*time.Second), NoRetry: o.NoRetry}
}

func (o Options) collect() dbusclient.SystemBusOptions {
	return dbusclient.SystemBusOptions{Subsystem: o.Subsystem, Timeout: durationOr(o.CollectTimeout, 5*time.Second), NoRetry: o.NoRetry}
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func Start(ctx context.Context, unit string, properties []Property, options Options) error {
	var job dbusclient.ObjectPath
	err := dbusclient.SystemdManager.UseSessionWithOptions(ctx, options.start(), func(session dbusclient.SystemSession) error {
		if err := session.RequireAvailable(); err != nil {
			return err
		}
		auxiliary := []AuxiliaryUnit{}
		return session.CallStore(
			dbusclient.SystemdManagerIface+".StartTransientUnit",
			dbusclient.CallPolicy{},
			[]any{unit, "fail", properties, auxiliary},
			&job,
		)
	})
	if err != nil {
		return fmt.Errorf("start transient unit %s: %w", unit, err)
	}
	return nil
}

func Inspect(ctx context.Context, unitName, expectedDescription string, options Options) (State, error) {
	var state State
	err := dbusclient.SystemdManager.UseSessionWithOptions(ctx, options.inspect(), func(session dbusclient.SystemSession) error {
		found, inspectErr := inspect(session, unitName, expectedDescription)
		state = found
		return inspectErr
	})
	if err != nil {
		return State{}, fmt.Errorf("inspect transient unit %s: %w", unitName, err)
	}
	return state, nil
}

func inspect(session dbusclient.SystemSession, unitName, expectedDescription string) (State, error) {
	var path dbusclient.ObjectPath
	if err := session.CallStore(
		dbusclient.SystemdManagerIface+".GetUnit",
		dbusclient.CallPolicy{},
		[]any{unitName},
		&path,
	); err != nil {
		if session.Context().Err() != nil {
			return State{}, session.Context().Err()
		}
		if isNoSuchUnitError(err) {
			return State{}, ErrNotFound
		}
		return State{}, err
	}

	unit := session.ObjectAt(path)
	id, err := dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "Id")
	if err != nil {
		return State{}, err
	}
	description, err := dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "Description")
	if err != nil {
		return State{}, err
	}
	transient, err := dbusclient.GetProperty[bool](session.Context(), unit, dbusclient.SystemdUnitIface, "Transient")
	if err != nil {
		return State{}, err
	}
	if id != unitName || description != expectedDescription || !transient {
		return State{}, fmt.Errorf("transient unit identity mismatch for %s", unitName)
	}
	return readState(session, unit)
}

func readState(session dbusclient.SystemSession, unit godbus.BusObject) (State, error) {
	var state State
	var err error
	state.ActiveState, err = dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "ActiveState")
	if err != nil {
		return State{}, err
	}
	state.SubState, _ = dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "SubState")
	state.ServiceResult, _ = dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdServiceIface, "Result")
	if exitCode, getErr := dbusclient.GetProperty[int32](session.Context(), unit, dbusclient.SystemdServiceIface, "ExecMainStatus"); getErr == nil {
		state.ExitCode = int(exitCode)
	}
	return state, nil
}

func Stop(ctx context.Context, unit string) error {
	err := systemd.StopUnit(ctx, unit)
	if isNoSuchUnitError(err) {
		return ErrNotFound
	}
	return err
}

func Collect(ctx context.Context, unit string, options Options) {
	properties := []Property{{Name: "CollectMode", Value: godbus.MakeVariant("inactive-or-failed")}}
	_ = dbusclient.SystemdManager.UseSessionWithOptions(ctx, options.collect(), func(session dbusclient.SystemSession) error {
		return session.Call(
			dbusclient.SystemdManagerIface+".SetUnitProperties",
			dbusclient.CallPolicy{},
			unit,
			true,
			properties,
		)
	})
}

func IsActive(state State) bool {
	switch state.ActiveState {
	case "active", "activating", "reloading", "deactivating":
		return true
	default:
		return false
	}
}

func isNoSuchUnitError(err error) bool {
	var dbusErr godbus.Error
	if !errors.As(err, &dbusErr) {
		return false
	}
	return dbusErr.Name == "org.freedesktop.systemd1.NoSuchUnit" || dbusErr.Name == "org.freedesktop.systemd1.LoadFailed"
}
