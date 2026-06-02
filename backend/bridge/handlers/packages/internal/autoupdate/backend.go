package autoupdate

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type (
	AutoUpdateFrequency    = apischema.AutoUpdateFrequency
	AutoUpdateOptions      = apischema.AutoUpdateOptions
	AutoUpdateRebootPolicy = apischema.AutoUpdateRebootPolicy
	AutoUpdateScope        = apischema.AutoUpdateScope
	AutoUpdateState        = apischema.AutoUpdateState
)

type UpdateBackend interface {
	Name() string
	Detect(context.Context) bool
	Read() (AutoUpdateState, error)
	Apply(context.Context, AutoUpdateOptions) error
	ApplyOfflineNow(context.Context) error // optional; may return not-implemented
}

func SelectBackend(ctx context.Context) UpdateBackend {
	backs := []UpdateBackend{
		newAptBackend(), // Debian/Ubuntu
		newDnfBackend(), // Fedora/RHEL
	}
	for _, b := range backs {
		if b.Detect(ctx) {
			return b
		}
	}
	return nil
}

func NewPkgKitBackendIfAvailable(ctx context.Context) UpdateBackend {
	b := newPkgKitBackend()
	if b.Detect(ctx) {
		return b
	}
	return nil
}
