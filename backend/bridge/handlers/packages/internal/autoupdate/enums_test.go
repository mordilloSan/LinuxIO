package autoupdate

import (
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// The generated contract lists the values of AutoUpdateBackend and
// AutoUpdateNotify in apischema.StringEnums, which cannot import them from
// here. A value added to one side and not the other fails here.
func TestStringEnumsListTheAutoUpdateValues(t *testing.T) {
	backends := []AutoUpdateBackend{backendAPT, backendMint, backendDNF4, backendDNF5}
	notify := []AutoUpdateNotify{AutoUpdateNotifyNever, AutoUpdateNotifyOnFailure, AutoUpdateNotifyOnChange, AutoUpdateNotifyAlways}
	for name, want := range map[string][]string{
		"AutoUpdateBackend": toStrings(backends),
		"AutoUpdateNotify":  toStrings(notify),
	} {
		if got := apischema.StringEnums[name]; !slices.Equal(got, want) {
			t.Errorf("StringEnums[%s] = %q, want %q", name, got, want)
		}
	}
}

func toStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}
