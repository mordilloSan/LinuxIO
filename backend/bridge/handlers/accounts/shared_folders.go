package accounts

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
)

// The shared-folder side of an account change (docs/shared-folders.md):
// Samba keeps a password of its own for each user who may sign in from
// Windows or a Mac, and a shared folder's access names users and groups.
// Vars so tests can stand in for the shares package.
var (
	syncSambaPassword   = shares.SyncSambaPassword
	removeSambaUser     = shares.RemoveSambaUser
	setSambaUserEnabled = shares.SetSambaUserEnabled
	reapplyShareAccess  = shares.ReapplyAccess
)

// shareAccessChanged applies the access of every shared folder again after
// the users or the members of a group changed. The account change is done
// by then and is not undone when this fails: the failure is logged, and the
// next change of a folder's access or of an account applies it.
func shareAccessChanged(ctx context.Context) {
	if err := reapplyShareAccess(ctx); err != nil {
		slog.Warn("failed to apply shared folder access after an account change", "error", err)
	}
}
