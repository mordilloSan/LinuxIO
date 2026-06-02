package system

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// TestCapabilityRegistryCoversWireFields ensures every *_available field on
// session.CapabilitiesAvailable (the single source the wire/session/login
// structs all embed) has a matching capabilityRegistry entry (and vice versa).
// Without this check, adding a wire field but forgetting the registry entry
// would silently leave the field at its zero value, and adding a registry entry
// without the matching wire field would panic at runtime via setCapabilityField.
func TestCapabilityRegistryCoversWireFields(t *testing.T) {
	wireNames := wireAvailableFields(t)

	registryNames := make(map[string]bool, len(capabilityRegistry))
	for _, spec := range capabilityRegistry {
		if spec.Name == "" {
			t.Fatalf("registry entry with empty Name: %+v", spec)
		}
		if registryNames[spec.Name] {
			t.Fatalf("duplicate registry entry %q", spec.Name)
		}
		if spec.Detect == nil {
			t.Errorf("capability %q has nil Detect", spec.Name)
		}
		registryNames[spec.Name] = true
	}

	for name := range wireNames {
		if !registryNames[name] {
			t.Errorf("wire field %q_available has no registry entry", name)
		}
	}
	for name := range registryNames {
		if !wireNames[name] {
			t.Errorf("registry entry %q has no matching wire field %q_available", name, name)
		}
	}
}

// wireAvailableFields returns the set of wire prefixes derived from JSON tags
// shaped `<prefix>_available` on session.CapabilitiesAvailable.
func wireAvailableFields(t *testing.T) map[string]bool {
	t.Helper()
	return availableTagsOf(reflect.TypeFor[session.CapabilitiesAvailable]())
}

// availableTagsOf returns the set of wire prefixes derived from JSON tags shaped
// `<prefix>_available` on the given struct type.
func availableTagsOf(typ reflect.Type) map[string]bool {
	names := make(map[string]bool)
	for field := range typ.Fields() {
		tag := field.Tag.Get("json")
		tag, _, _ = strings.Cut(tag, ",")
		prefix, ok := strings.CutSuffix(tag, "_available")
		if !ok {
			continue
		}
		names[prefix] = true
	}
	return names
}

// TestSetCapabilityFieldRoundTrips spot-checks setCapabilityField for every
// registry entry: a unique error string written for one capability must round
// trip back through JSON serialization, and no other capability's error
// field must be touched.
func TestSetCapabilityFieldRoundTrips(t *testing.T) {
	for _, spec := range capabilityRegistry {
		t.Run(spec.Name, func(t *testing.T) {
			var out apischema.CapabilitiesResponse
			marker := "marker-for-" + spec.Name
			setCapabilityField(&out, spec.Name, true, marker)

			data, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			body := string(data)
			if !strings.Contains(body, `"`+spec.Name+`_available":true`) {
				t.Errorf("expected %s_available=true in %s", spec.Name, body)
			}
			if !strings.Contains(body, `"`+spec.Name+`_error":"`+marker+`"`) {
				t.Errorf("expected %s_error=%q in %s", spec.Name, marker, body)
			}
		})
	}
}
