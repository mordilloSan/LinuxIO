package control

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// linuxioPackage is the name LinuxIO's DEB and RPM are installed under.
const linuxioPackage = "linuxio"

// updateLookupTimeout bounds the PackageKit read, so a slow or busy
// PackageKit costs the answer its update version, not the footer its version.
const updateLookupTimeout = 20 * time.Second

// findUpdateByName is a test seam.
var findUpdateByName = packages.FindUpdateByName

// handleVersion answers with the running build and the update PackageKit
// lists for the linuxio package. PackageKit is optional: a host without it,
// without the package, or where it cannot answer gets no update version.
func handleVersion(ctx context.Context, _ apischema.NoRequest) (apischema.VersionResponse, error) {
	resp := apischema.VersionResponse{CurrentVersion: version.Version}

	lookupCtx, cancel := context.WithTimeout(ctx, updateLookupTimeout)
	defer cancel()
	update, err := findUpdateByName(lookupCtx, linuxioPackage)
	switch {
	case err == nil:
		resp.UpdateVersion = update
	case errors.Is(err, dbusclient.ErrPackageKitUnavailable) || ctx.Err() != nil:
		slog.Debug("linuxio update lookup skipped", "package", linuxioPackage, "error", err)
	default:
		slog.Warn("linuxio update lookup failed", "package", linuxioPackage, "error", err)
	}
	return resp, nil
}
