package packages

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

// refreshingStatus is the progress frame InstallByNameWithProgress sends
// before it refreshes the package lists.
const refreshingStatus = "Refreshing the package lists"

// TestInstallByNameRefreshesAColdCacheIntegration installs the package
// LINUXIO_PACKAGEKIT_TEST_PACKAGE by name through the real PackageKit while
// its cache is cold, as a capability install meets it on a host where
// nothing refreshed it: the first Resolve finds nothing, so the install
// refreshes the package lists once and installs the package. It logs which
// way the backend says it found nothing: an empty result or PackageKit's
// package-not-found error. It needs root and installs a package, so it runs
// only in the disposable test VM, where the rig removes the package and
// makes the cache cold first and removes the package again after:
// `make test-vm-backend SUITES=packagekit`.
func TestInstallByNameRefreshesAColdCacheIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_PACKAGEKIT_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_PACKAGEKIT_INTEGRATION=1 and run as root in the test VM")
	}
	name := os.Getenv("LINUXIO_PACKAGEKIT_TEST_PACKAGE")
	if os.Geteuid() != 0 || name == "" {
		t.Skip("needs root and LINUXIO_PACKAGEKIT_TEST_PACKAGE")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	if state, err := dbusclient.PackageKit.BusNameState(ctx); err != nil || !state.Available() {
		t.Skipf("PackageKit is not on the bus: %+v, %v", state, err)
	}
	if installed, err := resolvePackageIDs(ctx, name, pkgkitFilterInstalled); err != nil || len(installed) > 0 {
		t.Fatalf("%s installed = %v, %v; want it removed before the test", name, installed, err)
	}
	ids, err := rawResolve(ctx, name)
	pkErr, isPkErr := errors.AsType[*pkgkit.Error](err)
	switch {
	case isPkErr && pkErr.Code == pkgkit.ErrorPackageNotFound:
		t.Logf("with the cache cold, Resolve %s ended with PackageKit's package-not-found error: %v", name, err)
	case err == nil && len(ids) == 0:
		t.Logf("with the cache cold, Resolve %s returned an empty result", name)
	default:
		t.Fatalf("Resolve %s = %v, %v; want nothing found while the cache is cold", name, ids, err)
	}

	var statuses []string
	if err := InstallByNameWithProgress(ctx, name, func(p *PkgUpdateProgress) error {
		if p.Type == "status" {
			statuses = append(statuses, p.Status)
		}
		return nil
	}); err != nil {
		t.Fatalf("install %s with the cache cold: %v", name, err)
	}
	refreshes := 0
	for _, status := range statuses {
		if status == refreshingStatus {
			refreshes++
		}
	}
	t.Logf("progress: %v", slices.Compact(statuses))
	if refreshes != 1 {
		t.Errorf("%q sent %d times, want once", refreshingStatus, refreshes)
	}
	if installed, err := resolvePackageIDs(ctx, name, pkgkitFilterInstalled); err != nil || len(installed) == 0 {
		t.Errorf("%s installed = %v, %v; want it installed", name, installed, err)
	}
}

// rawResolve resolves name among the packages not installed, returning
// PackageKit's error as it is, where resolvePackageIDs takes a
// package-not-found error for an empty result.
func rawResolve(ctx context.Context, name string) ([]string, error) {
	var ids []string
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)
		if callErr := trans.Call("Resolve", pkgkitFilterNotInstalled|pkgkitFilterNewest, []string{name}); callErr != nil {
			return callErr
		}
		ids, err = pkgkit.CollectPackageIDs(session.Context(), trans.Signals(), "resolve "+name)
		return err
	})
	return ids, err
}
