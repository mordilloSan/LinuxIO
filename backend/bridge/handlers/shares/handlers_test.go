package shares

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// Every share change writes /etc/exports or smb.conf and reloads a service,
// which only a privileged bridge can do: a session without sudo must be
// refused before anything runs. Listing shares stays open.
func TestShareChangesArePrivileged(t *testing.T) {
	changes := 0
	for _, route := range Routes {
		if route.Mode == bridgeipc.ModeCall {
			continue
		}
		changes++
		if !route.Privileged {
			t.Errorf("%s is not privileged", route.Route)
		}
	}
	if changes < 8 {
		t.Fatalf("checked %d changing routes, want the NFS, Samba and rsync ones", changes)
	}
}
