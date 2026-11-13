package packages

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

// bindingSets is the one list of the route sets this package declares and
// registers, so a set cannot be declared without being registered. handlers.go
// reads it for both Routes and RegisterHandlers.
func bindingSets(rt runtime.Runtime) []apischema.BindingSet {
	return []apischema.BindingSet{api, packageUpdateBindings(), capabilityInstallBindings(rt)}
}
