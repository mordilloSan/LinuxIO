// bridge/handlers/network/log_unit.go
package network

import (
	"os"
	"path/filepath"
)

// A journal view of an interface tails the unit that owns its configuration,
// so the candidates follow the detected config backend. netplan renders to
// either stack, so both are tried in the order netplan itself defaults to.
var networkLogUnitCandidates = map[string][]string{
	"ifcfg":            {"NetworkManager.service"},
	"ifupdown":         {"networking.service"},
	"netplan":          {"systemd-networkd.service", "NetworkManager.service"},
	"nmconnection":     {"NetworkManager.service"},
	"systemd-networkd": {"systemd-networkd.service"},
}

// Tried when no backend claims the interface, and after a backend's own
// candidates when none of those units are installed.
var networkLogUnitFallbacks = []string{
	"NetworkManager.service",
	"systemd-networkd.service",
	"networking.service",
}

var systemdUnitDirs = []string{
	"/etc/systemd/system",
	"/run/systemd/system",
	"/usr/lib/systemd/system",
	"/lib/systemd/system",
}

var (
	// Memoized per config backend: GetNetworkInfo polls every second, and the
	// answer only changes when units are installed or removed.
	networkLogUnitCache = map[string]string{}
	// Test seam.
	systemdUnitInstalled = defaultSystemdUnitInstalled
)

// resolveNetworkLogUnit returns the installed unit whose journal covers the
// interface's stack, or "" when none of the candidates exist — the signal for
// callers to offer no log view rather than tail a unit that cannot log.
// Callers must hold networkStatsMu.
func resolveNetworkLogUnit(configBackend string) string {
	if unit, cached := networkLogUnitCache[configBackend]; cached {
		return unit
	}

	backendCandidates := networkLogUnitCandidates[configBackend]
	candidates := make([]string, 0, len(backendCandidates)+len(networkLogUnitFallbacks))
	candidates = append(candidates, backendCandidates...)
	candidates = append(candidates, networkLogUnitFallbacks...)

	unit := ""
	for _, candidate := range candidates {
		if systemdUnitInstalled(candidate) {
			unit = candidate
			break
		}
	}
	networkLogUnitCache[configBackend] = unit
	return unit
}

func defaultSystemdUnitInstalled(unit string) bool {
	for _, dir := range systemdUnitDirs {
		if _, err := os.Stat(filepath.Join(dir, unit)); err == nil {
			return true
		}
	}
	return false
}
