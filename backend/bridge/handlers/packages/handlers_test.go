package packages

import "testing"

// Every route here that changes the host goes through PackageKit, the package
// manager or files under /etc, which only a privileged bridge can do: a
// session without sudo must be refused before anything runs.
func TestHostChangingRoutesArePrivileged(t *testing.T) {
	want := map[string]bool{
		"updates.set_auto_updates":      true,
		"updates.apply_offline_updates": true,
		"updates.refresh_cache":         true,
		"packages.update":               true,
		"system.install_capability":     true,
	}
	for _, route := range Routes {
		if !want[route.Route] {
			continue
		}
		delete(want, route.Route)
		if !route.Privileged {
			t.Errorf("route %s is not privileged", route.Route)
		}
	}
	for route := range want {
		t.Errorf("route %s is not declared", route)
	}
}
