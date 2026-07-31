package system

import (
	"context"
	"encoding/json"
	"errors"
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

func TestBuildCapabilitiesResponseStopsBeforeDetectionWhenCanceled(t *testing.T) {
	originalRegistry := capabilityRegistry
	detected := false
	capabilityRegistry = []CapabilitySpec{{
		Name: "docker",
		Detect: func(context.Context) (bool, string) {
			detected = true
			return true, ""
		},
	}}
	t.Cleanup(func() {
		capabilityRegistry = originalRegistry
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := buildCapabilitiesResponse(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("buildCapabilitiesResponse error = %v, want context.Canceled", err)
	}
	if detected {
		t.Fatal("capability detection ran after cancellation")
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
			assertTopLevelCapabilityJSON(t, data, spec.Name, marker)
		})
	}
}

func assertTopLevelCapabilityJSON(t *testing.T, data []byte, name, marker string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	available, ok := payload[name+"_available"].(bool)
	if !ok || !available {
		t.Errorf("expected top-level %s_available=true in %s", name, data)
	}
	errValue, ok := payload[name+"_error"].(string)
	if !ok || errValue != marker {
		t.Errorf("expected top-level %s_error=%q in %s", name, marker, data)
	}
}

func TestCheckMonitoringAvailabilityRunsHealth(t *testing.T) {
	withMonitoringCLI(t,
		func(name string) (string, error) {
			if name != "go-monitoring" {
				t.Fatalf("look path name = %q", name)
			}
			return "/usr/bin/go-monitoring", nil
		},
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "go-monitoring" {
				t.Fatalf("command name = %q", name)
			}
			if len(args) != 1 || args[0] != "health" {
				t.Fatalf("command args = %v, want [health]", args)
			}
			return []byte("ok\n"), nil
		},
	)

	ok, err := checkMonitoringAvailability(context.Background())
	if err != nil {
		t.Fatalf("checkMonitoringAvailability: %v", err)
	}
	if !ok {
		t.Fatal("checkMonitoringAvailability returned false")
	}
}

func TestMonitoringCapabilityInstallSpec(t *testing.T) {
	spec, ok := CapabilitySpecByName("monitoring")
	if !ok {
		t.Fatal("monitoring capability not registered")
	}
	if spec.Install == nil {
		t.Fatal("monitoring capability is not installable")
	}
	if spec.Install.OptionalComponent != OptionalComponentMonitoring {
		t.Fatalf("optional component = %q, want %q", spec.Install.OptionalComponent, OptionalComponentMonitoring)
	}
	if spec.Install.ServiceDebian != "go-monitoring.service" || spec.Install.ServiceRHEL != "go-monitoring.service" {
		t.Fatalf("service names = %q/%q, want go-monitoring.service", spec.Install.ServiceDebian, spec.Install.ServiceRHEL)
	}
	if !spec.Install.EnableService {
		t.Fatal("monitoring installer should enable the service")
	}
}

func TestCheckMonitoringAvailabilityReportsHealthOutput(t *testing.T) {
	withMonitoringCLI(t,
		func(string) (string, error) {
			return "/usr/bin/go-monitoring", nil
		},
		func(context.Context, string, ...string) ([]byte, error) {
			return []byte("latest tick is stale\n"), errors.New("exit status 1")
		},
	)

	ok, err := checkMonitoringAvailability(context.Background())
	if err == nil || !strings.Contains(err.Error(), "latest tick is stale") {
		t.Fatalf("error = %v, want health output", err)
	}
	if ok {
		t.Fatal("checkMonitoringAvailability returned true")
	}
}

func TestCheckMonitoringAvailabilityReportsMissingBinary(t *testing.T) {
	withMonitoringCLI(t,
		func(string) (string, error) {
			return "", errors.New("missing")
		},
		func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("health command should not run when binary is missing")
			return nil, nil
		},
	)

	ok, err := checkMonitoringAvailability(context.Background())
	if err == nil || !strings.Contains(err.Error(), "go-monitoring not found") {
		t.Fatalf("error = %v, want missing binary error", err)
	}
	if ok {
		t.Fatal("checkMonitoringAvailability returned true")
	}
}

func withMonitoringCLI(
	t *testing.T,
	lookPath func(string) (string, error),
	output func(context.Context, string, ...string) ([]byte, error),
) {
	t.Helper()

	origLookPath := monitoringCLILookPath
	origOutput := monitoringCLIOutput
	monitoringCLILookPath = lookPath
	monitoringCLIOutput = output
	t.Cleanup(func() {
		monitoringCLILookPath = origLookPath
		monitoringCLIOutput = origOutput
	})
}
