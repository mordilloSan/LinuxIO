package control

import "testing"

// control.version is a read any session may make and repeat; reboot and
// power-off need root and are never retried.
func TestRouteBindings(t *testing.T) {
	want := map[string]struct{ privileged, retrySafe bool }{
		"control.version":   {privileged: false, retrySafe: true},
		"control.reboot":    {privileged: true, retrySafe: false},
		"control.power_off": {privileged: true, retrySafe: false},
	}
	if len(Routes) != len(want) {
		t.Fatalf("routes = %d, want %d", len(Routes), len(want))
	}
	for _, route := range Routes {
		w, ok := want[route.Route]
		if !ok {
			t.Fatalf("unexpected route %s", route.Route)
		}
		if route.Privileged != w.privileged || route.RetrySafe != w.retrySafe {
			t.Fatalf("%s: privileged=%t retrySafe=%t, want %t %t", route.Route, route.Privileged, route.RetrySafe, w.privileged, w.retrySafe)
		}
	}
}
