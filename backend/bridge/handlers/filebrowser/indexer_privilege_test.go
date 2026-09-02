package filebrowser

import "testing"

func TestIndexerRoutesArePrivileged(t *testing.T) {
	privileged := map[string]bool{}
	for _, route := range Routes {
		privileged[route.Route] = route.Privileged
	}

	for _, route := range []string{
		"filebrowser.dir_size",
		"filebrowser.index",
		"filebrowser.search",
		"filebrowser.subfolders",
	} {
		if !privileged[route] {
			t.Errorf("route %s is not privileged", route)
		}
	}
}
