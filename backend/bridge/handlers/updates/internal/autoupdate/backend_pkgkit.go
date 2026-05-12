package autoupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/updates/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

type pkgkitBackend struct{}

func newPkgKitBackend() Backend     { return &pkgkitBackend{} }
func (*pkgkitBackend) Name() string { return "packagekit" }
func (*pkgkitBackend) Detect() bool {
	ok, err := dbusclient.PackageKit.Available(context.Background())
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
func (*pkgkitBackend) ApplyOfflineNow(ctx context.Context) error {
	return packagekit.Run(ctx, packagekit.OperationOptions{NoRetry: true}, func(session packagekit.Session) error {
		// Step 1: Check if updates are already prepared
		if prepared, err := session.UpdatePrepared(); err == nil {
			if prepared {
				return session.TriggerOffline("reboot")
			}
		}

		// Step 2: Refresh package cache
		if err := pkTransactionCall(ctx, session, "RefreshCache", true); err != nil {
			return fmt.Errorf("failed to refresh cache: %w", err)
		}

		// Step 3: Download updates (UpdatePackages with ONLY_DOWNLOAD flag = 2)
		if err := pkTransactionCallWithUpdates(ctx, session); err != nil {
			// Non-fatal - updates may already be downloaded or none available
			slog.Debug("PackageKit download step returned non-fatal error", "component", "dbus", "subsystem", "updates", "error", err)
		}

		// Step 4: Trigger offline update
		return session.TriggerOffline("reboot")
	})
}

// pkTransactionCall creates a transaction and calls a method, waiting for completion
func pkTransactionCall(ctx context.Context, session packagekit.Session, method string, args ...any) error {
	trans, err := session.CreateTransaction(20)
	if err != nil {
		return err
	}
	defer packagekit.LogClose(ctx, trans)

	if err := trans.Call(method, args...); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := trans.AwaitFinished(waitCtx, "PackageKit"); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timeout waiting for PackageKit")
		}
		return err
	}
	return nil
}

// pkTransactionCallWithUpdates gets available updates and downloads them
func pkTransactionCallWithUpdates(ctx context.Context, session packagekit.Session) error {
	packageIDs, err := collectPackageKitUpdates(ctx, session)
	if err != nil {
		return err
	}
	if len(packageIDs) == 0 {
		return fmt.Errorf("no updates available")
	}
	return downloadPackageKitUpdates(ctx, session, packageIDs)
}

func collectPackageKitUpdates(ctx context.Context, session packagekit.Session) ([]string, error) {
	trans, err := session.CreateTransaction(100)
	if err != nil {
		return nil, err
	}
	defer packagekit.LogClose(ctx, trans)

	if err = trans.Call("GetUpdates", uint64(0)); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	packageIDs, err := packagekit.CollectPackageIDs(waitCtx, trans.Signals(), "GetUpdates")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("timeout getting updates")
		}
		return nil, err
	}
	return packageIDs, nil
}

func downloadPackageKitUpdates(ctx context.Context, session packagekit.Session, packageIDs []string) error {
	trans, err := session.CreateTransaction(20)
	if err != nil {
		return err
	}
	defer packagekit.LogClose(ctx, trans)

	if err := trans.Call("UpdatePackages", uint64(2), packageIDs); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	if err := trans.AwaitFinished(waitCtx, "UpdatePackages"); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timeout downloading updates")
		}
		return err
	}
	return nil
}
