package assistant

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// The plugin routes run the agent's CLI as the session user and write only
// under that user's home, so a session without sudo may use all of them. The
// two reads are Calls a lost reply may repeat; every write is a Mutation.
func TestPluginRoutesAreUnprivilegedAndWritesAreMutations(t *testing.T) {
	want := map[string]bridgeipc.Mode{
		"assistant.list_plugins":       bridgeipc.ModeCall,
		"assistant.search_plugins":     bridgeipc.ModeCall,
		"assistant.install_plugin":     bridgeipc.ModeMutation,
		"assistant.uninstall_plugin":   bridgeipc.ModeMutation,
		"assistant.set_plugin_enabled": bridgeipc.ModeMutation,
		"assistant.add_marketplace":    bridgeipc.ModeMutation,
		"assistant.remove_marketplace": bridgeipc.ModeMutation,
		"assistant.update_marketplace": bridgeipc.ModeMutation,
	}
	for _, spec := range Routes {
		mode, ok := want[spec.Route]
		if !ok {
			if strings.Contains(spec.Route, "plugin") || strings.Contains(spec.Route, "marketplace") {
				t.Errorf("%s is not covered by this test", spec.Route)
			}
			continue
		}
		delete(want, spec.Route)
		if spec.Mode != mode {
			t.Errorf("%s: mode = %v, want %v", spec.Route, spec.Mode, mode)
		}
		if spec.Privileged {
			t.Errorf("%s is privileged", spec.Route)
		}
		if spec.RetrySafe != (mode == bridgeipc.ModeCall) {
			t.Errorf("%s: retry safe = %v", spec.Route, spec.RetrySafe)
		}
	}
	for route := range want {
		t.Errorf("%s is not declared", route)
	}
}
