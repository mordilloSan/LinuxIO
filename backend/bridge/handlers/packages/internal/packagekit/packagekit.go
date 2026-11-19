package packagekit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

// RoleUpdatePackages is PK_ROLE_ENUM_UPDATE_PACKAGES, the role of an
// UpdatePackages transaction.
const RoleUpdatePackages uint32 = 22

// ExitSuccess is PK_EXIT_ENUM_SUCCESS, the exit a Finished signal carries for
// a transaction that succeeded; every higher exit is one that did not.
const ExitSuccess uint32 = 1

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
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return withOperationGate(ctx, func() error {
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

func withOperationGate(ctx context.Context, fn func() error) error {
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
	return s.WatchTransaction(path, buffer)
}

// WatchTransaction follows the signals of transaction path, one this session
// created or one another client started, such as a job host that died.
func (s ClientSession) WatchTransaction(path dbusclient.ObjectPath, buffer int) (*Transaction, error) {
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

// TransactionRunning reports whether PackageKit still runs transaction path:
// its transaction list holds the transactions it has not finished.
func (s ClientSession) TransactionRunning(path dbusclient.ObjectPath) (bool, error) {
	var running []dbusclient.ObjectPath
	if err := s.session.CallStore(dbusclient.PackageKitIface+".GetTransactionList", dbusclient.CallPolicy{}, nil, &running); err != nil {
		return false, fmt.Errorf("list PackageKit transactions: %w", err)
	}
	return slices.Contains(running, path), nil
}

// OldTransaction is PackageKit's history entry of a transaction. PackageKit
// writes the entry when the transaction starts and its outcome and duration
// when it ends, so a transaction that never ended reads not succeeded with a
// Duration of 0.
type OldTransaction struct {
	Succeeded bool
	Role      uint32
	// Duration is how long the transaction ran, in milliseconds.
	Duration uint32
}

// historyTimeout bounds the wait for PackageKit's history.
const historyTimeout = 30 * time.Second

// FindOldTransaction looks for transaction path among the newest limit entries
// of PackageKit's transaction history, which it keeps across restarts.
func (s ClientSession) FindOldTransaction(path dbusclient.ObjectPath, limit uint32) (OldTransaction, bool, error) {
	trans, err := s.CreateTransaction(int(limit) + 1)
	if err != nil {
		return OldTransaction{}, false, err
	}
	defer LogClose(s.Context(), trans)
	if err := trans.Call("GetOldTransactions", limit); err != nil {
		return OldTransaction{}, false, err
	}
	ctx, cancel := context.WithTimeout(s.Context(), historyTimeout)
	defer cancel()
	var found OldTransaction
	ok := false
	for {
		select {
		case sig, open := <-trans.Signals():
			if !open {
				return OldTransaction{}, false, ErrSignalsClosed
			}
			switch sig.Name {
			case TransactionIface + ".Transaction":
				if old, match := oldTransactionFrom(sig, path); match {
					found, ok = old, true
				}
			case TransactionIface + ".ErrorCode":
				return OldTransaction{}, false, ErrorFromSignal(sig, "transaction history")
			case TransactionIface + ".Finished":
				return found, ok, nil
			}
		case <-ctx.Done():
			return OldTransaction{}, false, fmt.Errorf("read PackageKit transaction history: %w", ctx.Err())
		}
	}
}

// oldTransactionFrom reads a Transaction signal of GetOldTransactions: object
// path, timespec, succeeded, role, duration, data, uid, cmdline.
func oldTransactionFrom(sig *dbusclient.Signal, path dbusclient.ObjectPath) (OldTransaction, bool) {
	if len(sig.Body) < 5 {
		return OldTransaction{}, false
	}
	if old, _ := sig.Body[0].(dbusclient.ObjectPath); old != path {
		return OldTransaction{}, false
	}
	succeeded, _ := sig.Body[2].(bool)
	role, _ := sig.Body[3].(uint32)
	duration, _ := sig.Body[4].(uint32)
	return OldTransaction{Succeeded: succeeded, Role: role, Duration: duration}, true
}

func (s ClientSession) UpdatePrepared() (bool, error) {
	return dbusclient.GetProperty[bool](s.session, s.session.Object(), OfflineIface, "UpdatePrepared")
}

func (s ClientSession) TriggerOffline(action string) error {
	if err := s.session.Call(OfflineIface+".Trigger", dbusclient.CallPolicy{}, action); err != nil {
		return fmt.Errorf("failed to trigger offline update: %w", err)
	}
	return nil
}

func (t *Transaction) Path() dbusclient.ObjectPath {
	return t.path
}

// Role is the transaction's PackageKit role, such as RoleUpdatePackages.
func (t *Transaction) Role() (uint32, error) {
	return dbusclient.GetProperty[uint32](t.session.session, t.object, TransactionIface, "Role")
}

// Cancel asks PackageKit to cancel the transaction, with ctx rather than the
// session's, which may already be canceled. PackageKit refuses while the
// transaction cannot be canceled, such as while dpkg runs.
func (t *Transaction) Cancel(ctx context.Context) error {
	if err := t.object.CallWithContext(ctx, TransactionIface+".Cancel", 0).Err; err != nil {
		return fmt.Errorf("cancel PackageKit transaction %s: %w", t.path, err)
	}
	return nil
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

// ErrSignalsClosed means the D-Bus subscription channel was closed (bus drop)
// before the transaction finished.
var ErrSignalsClosed = errors.New("PackageKit signal channel closed")

func CollectPackageIDs(ctx context.Context, signals <-chan *dbusclient.Signal, action string) ([]string, error) {
	var packageIDs []string
	for {
		select {
		case sig, ok := <-signals:
			if !ok {
				return nil, ErrSignalsClosed
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

// ErrorPackageNotFound is PK_ERROR_ENUM_PACKAGE_NOT_FOUND. Some backends end a
// Resolve that matches no package with it instead of an empty result.
const ErrorPackageNotFound uint32 = 8

// Error is the ErrorCode signal that ended a transaction.
type Error struct {
	Code    uint32
	Details string
	action  string
}

func (e *Error) Error() string {
	if e.action != "" {
		return fmt.Sprintf("%s error: %s", e.action, e.Details)
	}
	return fmt.Sprintf("PackageKit error code %d: %s", e.Code, e.Details)
}

func ErrorFromSignal(sig *dbusclient.Signal, action string) error {
	if sig == nil || len(sig.Body) < 2 {
		return fmt.Errorf("PackageKit error (unknown)")
	}
	code, _ := sig.Body[0].(uint32)
	details, _ := sig.Body[1].(string)
	return &Error{Code: code, Details: details, action: action}
}

func LogClose(ctx context.Context, trans *Transaction) {
	if err := trans.Close(ctx); err != nil {
		slog.Debug("failed to close PackageKit transaction subscription", "component", "dbus", "subsystem", "packagekit", "error", err)
	}
}
