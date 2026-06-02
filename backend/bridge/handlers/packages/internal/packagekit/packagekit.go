package packagekit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const (
	TransactionIface = dbusclient.PackageKitTransactionIface
	OfflineIface     = dbusclient.PackageKitOfflineIface
)

var operationGate = make(chan struct{}, 1)

func init() {
	operationGate <- struct{}{}
}

type OperationOptions struct {
	Timeout time.Duration
	NoRetry bool
}

type ClientSession struct {
	session dbusclient.SystemSession
}

type Transaction struct {
	session ClientSession
	object  dbusclient.BusObject
	path    dbusclient.ObjectPath
	sub     *dbusclient.SignalSubscription
}

func Run(ctx context.Context, opts OperationOptions, fn func(ClientSession) error) error {
	if fn == nil {
		return fmt.Errorf("nil PackageKit callback")
	}
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return WithGate(ctx, func() error {
		return dbusclient.PackageKit.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{
			Subsystem: "packagekit",
			NoRetry:   opts.NoRetry,
		}, func(session dbusclient.SystemSession) error {
			if err := session.RequireAvailable(); err != nil {
				return err
			}
			return fn(ClientSession{session: session})
		})
	})
}

func WithGate(ctx context.Context, fn func() error) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if fn == nil {
		return fmt.Errorf("nil PackageKit gate callback")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-operationGate:
		defer func() { operationGate <- struct{}{} }()
		return fn()
	}
}

func (s ClientSession) Context() context.Context {
	return s.session.Context()
}

func (s ClientSession) CreateTransaction(buffer int) (*Transaction, error) {
	var path dbusclient.ObjectPath
	if err := s.session.CallStore(dbusclient.PackageKitCreateTransaction, dbusclient.CallPolicy{}, nil, &path); err != nil {
		return nil, fmt.Errorf("CreateTransaction failed: %w", err)
	}

	sub, err := dbusclient.WatchObjectSignals(s.Context(), path, buffer, "", "")
	if err != nil {
		return nil, fmt.Errorf("watch PackageKit transaction signals: %w", err)
	}

	return &Transaction{
		session: s,
		object:  s.session.ObjectAt(path),
		path:    path,
		sub:     sub,
	}, nil
}

func (s ClientSession) UpdatePrepared() (bool, error) {
	return dbusclient.GetProperty[bool](s.Context(), s.session.Object(), OfflineIface, "UpdatePrepared")
}

func (s ClientSession) TriggerOffline(action string) error {
	if err := s.session.Call(OfflineIface+".Trigger", dbusclient.CallPolicy{}, action); err != nil {
		return fmt.Errorf("failed to trigger offline update: %w", err)
	}
	return nil
}

func (t *Transaction) Path() dbusclient.ObjectPath {
	if t == nil {
		return ""
	}
	return t.path
}

func (t *Transaction) Signals() <-chan *dbusclient.Signal {
	if t == nil || t.sub == nil {
		return nil
	}
	return t.sub.Chan()
}

func (t *Transaction) Close(ctx context.Context) error {
	if t == nil || t.sub == nil {
		return nil
	}
	return t.sub.Close(ctx)
}

func (t *Transaction) Call(method string, args ...any) error {
	if t == nil {
		return fmt.Errorf("nil PackageKit transaction")
	}
	if err := t.object.CallWithContext(t.session.Context(), TransactionIface+"."+method, 0, args...).Err; err != nil {
		return fmt.Errorf("%s failed: %w", method, err)
	}
	return nil
}

func (t *Transaction) AwaitFinished(ctx context.Context, action string) error {
	return AwaitFinished(ctx, t.Signals(), action)
}

func AwaitFinished(ctx context.Context, signals <-chan *dbusclient.Signal, action string) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	for {
		select {
		case sig := <-signals:
			if sig == nil {
				return fmt.Errorf("nil signal from D-Bus")
			}
			switch sig.Name {
			case TransactionIface + ".ErrorCode":
				return ErrorFromSignal(sig, action)
			case TransactionIface + ".Finished":
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func CollectPackageIDs(ctx context.Context, signals <-chan *dbusclient.Signal, action string) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	var packageIDs []string
	for {
		select {
		case sig := <-signals:
			if sig == nil {
				continue
			}
			switch sig.Name {
			case TransactionIface + ".Package":
				if len(sig.Body) >= 2 {
					if pkgID, ok := sig.Body[1].(string); ok {
						packageIDs = append(packageIDs, pkgID)
					}
				}
			case TransactionIface + ".ErrorCode":
				return nil, ErrorFromSignal(sig, action)
			case TransactionIface + ".Finished":
				return packageIDs, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func ErrorFromSignal(sig *dbusclient.Signal, action string) error {
	if sig == nil || len(sig.Body) < 2 {
		return fmt.Errorf("PackageKit error (unknown)")
	}
	code, _ := sig.Body[0].(uint32)
	details, _ := sig.Body[1].(string)
	if action != "" {
		return fmt.Errorf("%s error: %s", action, details)
	}
	return fmt.Errorf("PackageKit error code %d: %s", code, details)
}

func LogClose(ctx context.Context, trans *Transaction) {
	if err := trans.Close(ctx); err != nil {
		slog.Debug("failed to close PackageKit transaction subscription", "component", "dbus", "subsystem", "packagekit", "error", err)
	}
}
