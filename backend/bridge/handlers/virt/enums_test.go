package virt

import (
	"maps"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// The generated contract lists the values of VMImagePresetID and VMFirmware in
// apischema.StringEnums, which cannot import them from here. A preset added to
// the table, or a firmware added here, and not there fails here.
func TestStringEnumsListThePresetsAndFirmwares(t *testing.T) {
	var presets []string
	for id := range maps.Keys(vmImagePresets) {
		presets = append(presets, string(id))
	}
	for name, want := range map[string][]string{
		"VMImagePresetID": presets,
		"VMFirmware":      {string(vmFirmwareBIOS), string(vmFirmwareUEFI)},
	} {
		got := slices.Clone(apischema.StringEnums[name])
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("StringEnums[%s] = %q, want %q", name, got, want)
		}
	}
}
