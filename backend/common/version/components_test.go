package version

import "testing"

func TestComponentsAreUniqueAndIncludeMonitoring(t *testing.T) {
	names := map[string]bool{}
	binaries := map[string]bool{}
	for _, c := range Components {
		if c.Name == "" || c.Binary == "" || len(c.Args) == 0 {
			t.Errorf("incomplete component %+v", c)
		}
		if names[c.Name] || binaries[c.Binary] {
			t.Errorf("duplicate component %+v", c)
		}
		names[c.Name], binaries[c.Binary] = true, true
	}
	if !binaries["linuxio-monitoring"] {
		t.Error("Components leaves out linuxio-monitoring")
	}
}
