package dbusclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	godbus "github.com/godbus/dbus/v5"
)

var retryDelay = 150 * time.Millisecond

type SystemBusOptions struct {
	Subsystem string
	Timeout   time.Duration

	// NoRetry disables the net.ErrClosed retry wrapper. Use it for non-idempotent
	// calls where replaying the callback could duplicate a transaction.
	NoRetry bool
}

type SystemObject struct {
	Subsystem   string
	BusName     string
	Path        godbus.ObjectPath
	Unavailable error
}

type SystemInterface struct {
	Object SystemObject
	Name   string
}

// SystemSession is a short-lived view of one open system-bus connection.
// It is valid only for the duration of the UseSession callback.
type SystemSession struct {
	ctx    context.Context
	conn   *godbus.Conn
	object SystemObject
	busObj godbus.BusObject
}

type CallPolicy struct {
	NoAutoStart                   bool
	AllowInteractiveAuthorization bool
	NoReplyExpected               bool
}

func RetryOnceIfClosed(ctx context.Context, do func() error) error {
	if do == nil {
		return fmt.Errorf("nil D-Bus retry callback")
	}
	ctx = requireContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	err := do()
	if !errors.Is(err, net.ErrClosed) {
		return err
	}
	if waitErr := waitBeforeRetry(ctx); waitErr != nil {
		return waitErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return do()
}

func UseSystemBusWithOptions(ctx context.Context, opts SystemBusOptions, fn func(context.Context, *godbus.Conn) error) error {
	if fn == nil {
		return fmt.Errorf("nil D-Bus callback")
	}

	ctx = requireContext(ctx)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	runOnce := func() error {
		return useSystemBusOnce(ctx, opts.Subsystem, fn)
	}
	run := func() error {
		if opts.NoRetry {
			return runOnce()
		}
		return RetryOnceIfClosed(ctx, func() error {
			return runOnce()
		})
	}
	return run()
}

func (o SystemObject) Interface(name string) SystemInterface {
	return SystemInterface{Object: o, Name: name}
}

func (o SystemObject) Use(ctx context.Context, fn func(context.Context, *godbus.Conn, godbus.BusObject) error) error {
	return o.UseWithOptions(ctx, SystemBusOptions{}, fn)
}

// UseSession opens the system bus for one callback and passes a small session
// object instead of leaking ctx/conn/object plumbing into callers.
func (o SystemObject) UseSession(ctx context.Context, fn func(SystemSession) error) error {
	return o.UseSessionWithOptions(ctx, SystemBusOptions{}, fn)
}

// UseSessionWithOptions is UseSession with explicit bus options.
func (o SystemObject) UseSessionWithOptions(ctx context.Context, opts SystemBusOptions, fn func(SystemSession) error) error {
	if fn == nil {
		return fmt.Errorf("nil D-Bus session callback")
	}
	return o.UseWithOptions(ctx, opts, func(ctx context.Context, conn *godbus.Conn, obj godbus.BusObject) error {
		return fn(SystemSession{
			ctx:    ctx,
			conn:   conn,
			object: o,
			busObj: obj,
		})
	})
}

func (o SystemObject) UseWithOptions(ctx context.Context, opts SystemBusOptions, fn func(context.Context, *godbus.Conn, godbus.BusObject) error) error {
	if opts.Subsystem == "" {
		opts.Subsystem = o.Subsystem
	}
	return UseSystemBusWithOptions(ctx, opts, func(ctx context.Context, conn *godbus.Conn) error {
		return fn(ctx, conn, o.BusObject(conn))
	})
}

func (o SystemObject) BusObject(conn *godbus.Conn) godbus.BusObject {
	return conn.Object(o.BusName, o.Path)
}

// Context returns the callback context used for all calls in this session.
func (s SystemSession) Context() context.Context {
	return s.ctx
}

// Object returns this session's root bus object.
func (s SystemSession) Object() godbus.BusObject {
	return s.busObj
}

// ObjectAt returns another object owned by the same bus name on this session's
// connection. It is useful for dynamic object paths returned by D-Bus calls.
func (s SystemSession) ObjectAt(path godbus.ObjectPath) godbus.BusObject {
	return s.conn.Object(s.object.BusName, path)
}

// RequireAvailable verifies that this session's well-known bus name is active
// or activatable on the same connection used by the session.
func (s SystemSession) RequireAvailable() error {
	return s.object.RequireAvailableOnConnection(s.ctx, s.conn)
}

// Call invokes a method on this session's root object.
func (s SystemSession) Call(method string, policy CallPolicy, args ...any) error {
	return s.busObj.CallWithContext(s.ctx, method, policy.Flags(), args...).Err
}

// CallStore invokes a method on this session's root object and stores its reply.
func (s SystemSession) CallStore(method string, policy CallPolicy, args []any, out ...any) error {
	return s.busObj.CallWithContext(s.ctx, method, policy.Flags(), args...).Store(out...)
}

func (i SystemInterface) Use(ctx context.Context, fn func(context.Context, *godbus.Conn, godbus.BusObject) error) error {
	return i.Object.Use(ctx, fn)
}

func (i SystemInterface) Method(member string) string {
	if member == "" {
		return i.Name
	}
	return i.Name + "." + member
}

func (i SystemInterface) Call(ctx context.Context, member string, policy CallPolicy, args ...any) error {
	return i.Use(ctx, func(ctx context.Context, _ *godbus.Conn, obj godbus.BusObject) error {
		return obj.CallWithContext(ctx, i.Method(member), policy.Flags(), args...).Err
	})
}

func (i SystemInterface) CallStore(ctx context.Context, member string, policy CallPolicy, args []any, out ...any) error {
	return i.Use(ctx, func(ctx context.Context, _ *godbus.Conn, obj godbus.BusObject) error {
		return obj.CallWithContext(ctx, i.Method(member), policy.Flags(), args...).Store(out...)
	})
}

func (p CallPolicy) Flags() godbus.Flags {
	var flags godbus.Flags
	if p.NoAutoStart {
		flags |= godbus.FlagNoAutoStart
	}
	if p.AllowInteractiveAuthorization {
		flags |= godbus.FlagAllowInteractiveAuthorization
	}
	if p.NoReplyExpected {
		flags |= godbus.FlagNoReplyExpected
	}
	return flags
}

func useSystemBusOnce(ctx context.Context, subsystem string, fn func(context.Context, *godbus.Conn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	defer closeSystemBus(conn, subsystem)

	return fn(ctx, conn)
}

func requireContext(ctx context.Context) context.Context {
	if ctx == nil {
		panic("dbusclient: nil context")
	}
	return ctx
}

func waitBeforeRetry(ctx context.Context) error {
	if retryDelay <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(retryDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func closeSystemBus(conn *godbus.Conn, subsystem string) {
	if err := conn.Close(); err != nil {
		slog.Warn("failed to close D-Bus connection", "component", "dbus", "subsystem", subsystemName(subsystem), "error", err)
	}
}

func subsystemName(subsystem string) string {
	if subsystem == "" {
		return "system"
	}
	return subsystem
}
