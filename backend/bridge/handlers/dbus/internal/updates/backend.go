package updates

import "context"

type AutoUpdateOptions struct {
	Enabled      bool     `json:"enabled"`
	Frequency    string   `json:"frequency"` // "hourly"|"daily"|"weekly"
	Scope        string   `json:"scope"`     // "security"|"updates"|"all"
	DownloadOnly bool     `json:"download_only"`
	RebootPolicy string   `json:"reboot_policy"` // "never"|"if_needed"|"always"|"schedule"
	ExcludePkgs  []string `json:"exclude_packages"`
}

type AutoUpdateState struct {
	Backend string            `json:"backend"`
	Options AutoUpdateOptions `json:"options"`
	Notes   []string          `json:"notes,omitempty"`
}

type Backend interface {
	Name() string
	Detect() bool
	Read() (AutoUpdateState, error)
	Apply(context.Context, AutoUpdateOptions) error
	ApplyOfflineNow() error // optional; may return not-implemented
}

func SelectBackend() Backend {
	backs := []Backend{
		newAptBackend(), // Debian/Ubuntu
		newDnfBackend(), // Fedora/RHEL
		// (We don't return pkgkit here; it's auxiliary for offline apply)
	}
	for _, b := range backs {
		if b.Detect() {
			return b
		}
	}
	return nil
}

func NewPkgKitBackendIfAvailable() Backend {
	b := newPkgKitBackend()
	if b.Detect() {
		return b
	}
	return nil
}
