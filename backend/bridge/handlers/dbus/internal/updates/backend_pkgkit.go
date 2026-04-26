package updates

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/pkgkit"
)

type pkgkitBackend struct{}

func newPkgKitBackend() Backend     { return &pkgkitBackend{} }
func (*pkgkitBackend) Name() string { return "packagekit" }
func (*pkgkitBackend) Detect() bool {
	ok, err := pkgkit.Available()
	return err == nil && ok
}

// Read returns a minimal state since PackageKit doesn't manage auto-update configuration
// Auto-update config is handled by apt-unattended or dnf-automatic backends
func (*pkgkitBackend) Read() (AutoUpdateState, error) {
	return AutoUpdateState{
		Backend: "packagekit",
		Options: AutoUpdateOptions{
			Enabled:      false,
			Frequency:    "daily",
			Scope:        "security",
			DownloadOnly: false,
			RebootPolicy: "never",
			ExcludePkgs:  []string{},
		},
	}, nil
}

// Apply does nothing - PackageKit doesn't configure automatic updates
// Use apt-unattended or dnf-automatic backends for auto-update configuration
func (*pkgkitBackend) Apply(_ context.Context, _ AutoUpdateOptions) error {
	return fmt.Errorf("packagekit backend does not support auto-update configuration; use apt-unattended or dnf-automatic")
}

// ApplyOfflineNow schedules updates to be applied on next reboot
// This is the main purpose of the PackageKit backend
func (*pkgkitBackend) ApplyOfflineNow() error {
	const (
		pkBusName      = pkgkit.BusName
		pkObjPath      = pkgkit.ObjectPath
		transactionIfc = pkgkit.TransactionInterface
		offlineIfc     = pkgkit.OfflineInterface
	)

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer conn.Close()

	if err := pkgkit.RequireAvailableOnConnection(conn); err != nil {
		return err
	}

	pkObj := conn.Object(pkBusName, godbus.ObjectPath(pkObjPath))

	// Step 1: Check if updates are already prepared
	var preparedVariant godbus.Variant
	if err := pkObj.Call("org.freedesktop.DBus.Properties.Get", 0, offlineIfc, "UpdatePrepared").Store(&preparedVariant); err == nil {
		prepared, _ := preparedVariant.Value().(bool)
		if prepared {
			// Updates already prepared, just trigger
			if err := pkObj.Call(offlineIfc+".Trigger", 0, "reboot").Err; err != nil {
				return fmt.Errorf("failed to trigger offline update: %w", err)
			}
			return nil
		}
	}

	// Step 2: Refresh package cache
	if err := pkTransactionCall(conn, pkBusName, pkObjPath, transactionIfc, "RefreshCache", true); err != nil {
		return fmt.Errorf("failed to refresh cache: %w", err)
	}

	// Step 3: Download updates (UpdatePackages with ONLY_DOWNLOAD flag = 2)
	if err := pkTransactionCallWithUpdates(conn, pkBusName, pkObjPath, transactionIfc); err != nil {
		// Non-fatal - updates may already be downloaded or none available
		slog.Debug("PackageKit download step returned non-fatal error", "component", "dbus", "subsystem", "updates", "error", err)
	}

	// Step 4: Trigger offline update
	if err := pkObj.Call(offlineIfc+".Trigger", 0, "reboot").Err; err != nil {
		return fmt.Errorf("failed to trigger offline update: %w", err)
	}

	return nil
}

// pkTransactionCall creates a transaction and calls a method, waiting for completion
func pkTransactionCall(conn *godbus.Conn, busName, objPath, transIfc, method string, args ...any) error {
	obj := conn.Object(busName, godbus.ObjectPath(objPath))

	var transPath godbus.ObjectPath
	if err := pkgkit.RequireAvailableOnConnection(conn); err != nil {
		return err
	}
	if err := obj.Call(pkgkit.CreateTransactionMethod, 0).Store(&transPath); err != nil {
		return fmt.Errorf("CreateTransaction failed: %w", err)
	}

	trans := conn.Object(busName, transPath)
	sigCh := make(chan *godbus.Signal, 20)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)
	matchPath := transPath
	defer func() {
		if err := conn.RemoveMatchSignal(godbus.WithMatchObjectPath(matchPath)); err != nil {
			slog.Debug("failed to remove PackageKit signal match", "component", "dbus", "subsystem", "updates", "error", err)
		}
	}()

	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		slog.Debug("failed to add PackageKit signal match", "component", "dbus", "subsystem", "updates", "error", err)
	}

	if err := trans.Call(transIfc+"."+method, 0, args...).Err; err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				continue
			}
			switch sig.Name {
			case transIfc + ".ErrorCode":
				msg, _ := sig.Body[1].(string)
				return fmt.Errorf("PackageKit error: %s", msg)
			case transIfc + ".Finished":
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for PackageKit")
		}
	}
}

// pkTransactionCallWithUpdates gets available updates and downloads them
func pkTransactionCallWithUpdates(conn *godbus.Conn, busName, objPath, transIfc string) error {
	obj := conn.Object(busName, godbus.ObjectPath(objPath))
	packageIDs, err := collectPackageKitUpdates(conn, obj, busName, transIfc)
	if err != nil {
		return err
	}
	if len(packageIDs) == 0 {
		return fmt.Errorf("no updates available")
	}
	return downloadPackageKitUpdates(conn, obj, busName, transIfc, packageIDs)
}

func collectPackageKitUpdates(
	conn *godbus.Conn,
	obj godbus.BusObject,
	busName, transIfc string,
) ([]string, error) {
	var transPath godbus.ObjectPath
	if err := pkgkit.RequireAvailableOnConnection(conn); err != nil {
		return nil, err
	}
	if err := obj.Call(pkgkit.CreateTransactionMethod, 0).Store(&transPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction failed: %w", err)
	}

	trans := conn.Object(busName, transPath)
	sigCh := subscribePkgKitSignals(conn, transPath, 100)
	defer conn.RemoveSignal(sigCh)
	defer removePkgKitSignalMatch(conn, transPath)

	if err := trans.Call(transIfc+".GetUpdates", 0, uint64(0)).Err; err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var packageIDs []string

	for {
		select {
		case sig := <-sigCh:
			done, err := collectPackageKitUpdateSignal(sig, transIfc, &packageIDs)
			if err != nil {
				return nil, err
			}
			if done {
				return packageIDs, nil
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout getting updates")
		}
	}
}

func downloadPackageKitUpdates(
	conn *godbus.Conn,
	obj godbus.BusObject,
	busName, transIfc string,
	packageIDs []string,
) error {
	var transPath godbus.ObjectPath
	if err := pkgkit.RequireAvailableOnConnection(conn); err != nil {
		return err
	}
	if err := obj.Call(pkgkit.CreateTransactionMethod, 0).Store(&transPath); err != nil {
		return err
	}

	trans := conn.Object(busName, transPath)
	sigCh := subscribePkgKitSignals(conn, transPath, 20)
	defer conn.RemoveSignal(sigCh)
	defer removePkgKitSignalMatch(conn, transPath)

	if err := trans.Call(transIfc+".UpdatePackages", 0, uint64(2), packageIDs).Err; err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	for {
		select {
		case sig := <-sigCh:
			done, err := waitForPkgKitCompletion(sig, transIfc, "UpdatePackages")
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout downloading updates")
		}
	}
}

func subscribePkgKitSignals(conn *godbus.Conn, transPath godbus.ObjectPath, buffer int) chan *godbus.Signal {
	sigCh := make(chan *godbus.Signal, buffer)
	conn.Signal(sigCh)
	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		slog.Debug("failed to add PackageKit signal match", "component", "dbus", "subsystem", "updates", "error", err)
	}
	return sigCh
}

func removePkgKitSignalMatch(conn *godbus.Conn, transPath godbus.ObjectPath) {
	if err := conn.RemoveMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		slog.Debug("failed to remove PackageKit signal match", "component", "dbus", "subsystem", "updates", "error", err)
	}
}

func collectPackageKitUpdateSignal(sig *godbus.Signal, transIfc string, packageIDs *[]string) (bool, error) {
	if sig == nil {
		return false, nil
	}
	switch sig.Name {
	case transIfc + ".Package":
		if len(sig.Body) >= 2 {
			if pkgID, ok := sig.Body[1].(string); ok {
				*packageIDs = append(*packageIDs, pkgID)
			}
		}
	case transIfc + ".Finished":
		return true, nil
	case transIfc + ".ErrorCode":
		msg, _ := sig.Body[1].(string)
		return false, fmt.Errorf("GetUpdates error: %s", msg)
	}
	return false, nil
}

func waitForPkgKitCompletion(sig *godbus.Signal, transIfc, action string) (bool, error) {
	if sig == nil {
		return false, nil
	}
	switch sig.Name {
	case transIfc + ".ErrorCode":
		msg, _ := sig.Body[1].(string)
		return false, fmt.Errorf("%s error: %s", action, msg)
	case transIfc + ".Finished":
		return true, nil
	default:
		return false, nil
	}
}
