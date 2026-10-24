package config

import "testing"

func TestLoggingRoutesArePrivileged(t *testing.T) {
	for _, name := range []string{"logging.get_debug", "logging.set_debug"} {
		found := false
		for _, spec := range Routes {
			if spec.Route == name {
				found = true
				if !spec.Privileged {
					t.Errorf("%s should be privileged", name)
				}
			}
		}
		if !found {
			t.Errorf("route %s not registered", name)
		}
	}
}
