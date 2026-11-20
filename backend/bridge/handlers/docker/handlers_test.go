package docker

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// docker.list_host_ips returns addresses that docker.list_containers and
// network.get_network_info already show a session without sudo, and changes
// nothing, so it is open and safe to retry.
func TestListHostIPsIsAnOpenRetrySafeCall(t *testing.T) {
	for _, route := range Routes {
		if route.Route != "docker.list_host_ips" {
			continue
		}
		if route.Mode != bridgeipc.ModeCall || !route.RetrySafe || route.Privileged {
			t.Fatalf("route = mode %q retry-safe %v privileged %v, want an unprivileged retry-safe call", route.Mode, route.RetrySafe, route.Privileged)
		}
		return
	}
	t.Fatal("docker.list_host_ips is not registered")
}
