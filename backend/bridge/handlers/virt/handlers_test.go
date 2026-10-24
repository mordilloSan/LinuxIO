package virt

import (
	"testing"
)

func TestRoutesArePrivileged(t *testing.T) {
	for _, route := range Routes {
		if !route.Privileged {
			t.Fatalf("route %s is not privileged", route.Route)
		}
	}
}
