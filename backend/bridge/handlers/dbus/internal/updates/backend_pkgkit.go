package updates

import (
	"context"
	"fmt"
	"slices"
	"time"

	godbus "github.com/godbus/dbus/v5"
)

type pkgkitBackend struct{}

func newPkgKitBackend() Backend     { return &pkgkitBackend{} }
func (*pkgkitBackend) Name() string { return "packagekit" }
func (*pkgkitBackend) Detect() bool {
	conn, err := godbus.SystemBus()
	if err != nil {
		return false
	}
	// Check if PackageKit service exists
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return false
	}
	return slices.Contains(names, "org.freedesktop.PackageKit")
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
func (*pkgkitBackend) Apply(ctx context.Context, opt AutoUpdateOptions) error {
	_ = ctx
	_ = opt
	return fmt.Errorf("packagekit backend does not support auto-update configuration; use apt-unattended or dnf-automatic")
}

// ApplyOfflineNow schedules updates to be applied on next reboot
// This is the main purpose of the PackageKit backend
func (*pkgkitBackend) ApplyOfflineNow() error {
	const (
		pkBusName      = "org.freedesktop.PackageKit"
		pkObjPath      = "/org/freedesktop/PackageKit"
		transactionIfc = "org.freedesktop.PackageKit.Transaction"
		offlineIfc     = "org.freedesktop.PackageKit.Offline"
	)

	conn, err := godbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	// Don't close - shared system bus connection

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
		_ = err
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
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return fmt.Errorf("CreateTransaction failed: %w", err)
	}

	trans := conn.Object(busName, transPath)
	sigCh := make(chan *godbus.Signal, 20)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	_ = conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath))

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

	// First get list of updates
	var transPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return fmt.Errorf("CreateTransaction failed: %w", err)
	}

	trans := conn.Object(busName, transPath)
	sigCh := make(chan *godbus.Signal, 100)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	_ = conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath))

	// GetUpdates with filter 0 (none)
	if err := trans.Call(transIfc+".GetUpdates", 0, uint64(0)).Err; err != nil {
		return err
	}

	var packageIDs []string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Collect package IDs from Package signals
collectLoop:
	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				continue
			}
			switch sig.Name {
			case transIfc + ".Package":
				if len(sig.Body) >= 2 {
					if pkgID, ok := sig.Body[1].(string); ok {
						packageIDs = append(packageIDs, pkgID)
					}
				}
			case transIfc + ".Finished":
				break collectLoop
			case transIfc + ".ErrorCode":
				msg, _ := sig.Body[1].(string)
				return fmt.Errorf("GetUpdates error: %s", msg)
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout getting updates")
		}
	}

	if len(packageIDs) == 0 {
		return fmt.Errorf("no updates available")
	}

	// Now download them with UpdatePackages (flag 2 = ONLY_DOWNLOAD)
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return err
	}

	trans = conn.Object(busName, transPath)
	sigCh2 := make(chan *godbus.Signal, 20)
	conn.Signal(sigCh2)
	defer conn.RemoveSignal(sigCh2)

	_ = conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath))

	// UpdatePackages with ONLY_DOWNLOAD flag (2)
	if err := trans.Call(transIfc+".UpdatePackages", 0, uint64(2), packageIDs).Err; err != nil {
		return err
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel2()

	for {
		select {
		case sig := <-sigCh2:
			if sig == nil {
				continue
			}
			switch sig.Name {
			case transIfc + ".ErrorCode":
				msg, _ := sig.Body[1].(string)
				return fmt.Errorf("UpdatePackages error: %s", msg)
			case transIfc + ".Finished":
				return nil
			}
		case <-ctx2.Done():
			return fmt.Errorf("timeout downloading updates")
		}
	}
}
