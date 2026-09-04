package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// TestCapabilityRegistryCoversWireFields ensures every *_available field on
// session.CapabilitiesAvailable (embedded in the wire response) has a matching
// capabilityRegistry entry (and vice versa).
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

func TestBuildCapabilitiesResponseDetectsCapabilitiesConcurrently(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	originalRegistry := capabilityRegistry
	t.Cleanup(func() {
		capabilityRegistry = originalRegistry
	})

	started := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseDetectors := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(releaseDetectors)

	capabilityRegistry = []CapabilitySpec{
		{
			Name: "docker",
			Detect: func(context.Context) (bool, string) {
				started <- "docker"
				<-release
				return true, ""
			},
		},
		{
			Name: "docker_updates",
			Detect: func(context.Context) (bool, string) {
				started <- "docker_updates"
				<-release
				return false, "not installed"
			},
		},
	}

	type responseResult struct {
		response apischema.CapabilitiesResponse
		err      error
	}
	result := make(chan responseResult, 1)
	go func() {
		response, err := buildCapabilitiesResponse(context.Background())
		result <- responseResult{response: response, err: err}
	}()

	seen := make(map[string]bool, 2)
	startTimeout := time.NewTimer(time.Second)
	defer startTimeout.Stop()
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-startTimeout.C:
			releaseDetectors()
			select {
			case <-result:
			case <-time.After(time.Second):
			}
			t.Fatalf("capability detections did not overlap; started=%v", seen)
		}
	}
	releaseDetectors()

	select {
	case completed := <-result:
		if completed.err != nil {
			t.Fatalf("buildCapabilitiesResponse error = %v", completed.err)
		}
		if !completed.response.DockerAvailable {
			t.Error("docker capability reported unavailable")
		}
		if completed.response.DockerUpdatesAvailable {
			t.Error("docker_updates capability reported available")
		}
		if completed.response.DockerUpdatesError == nil || *completed.response.DockerUpdatesError != "not installed" {
			t.Errorf("docker_updates error = %v, want %q", completed.response.DockerUpdatesError, "not installed")
		}
	case <-time.After(time.Second):
		t.Fatal("capability detections did not complete after release")
	}

	assertCapabilityTimingEvent(t, logs.String(),
		"capabilities_us",
		"capabilities_docker_us",
		"capabilities_docker_updates_us",
	)
}

func assertCapabilityTimingEvent(t *testing.T, logs string, fields ...string) {
	t.Helper()

	var timingEvent map[string]any
	timingEventCount := 0
	for line := range strings.Lines(logs) {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode log event: %v", err)
		}
		if event["msg"] == "capabilities timing" {
			timingEvent = event
			timingEventCount++
		}
	}
	if timingEventCount != 1 {
		t.Fatalf("capabilities timing event count = %d, want 1", timingEventCount)
	}
	for _, field := range fields {
		value, ok := timingEvent[field].(float64)
		if !ok || value < 0 {
			t.Errorf("%s = %v, want a non-negative number", field, timingEvent[field])
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func withMonitoringHealth(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := monitoringHealthClient
	monitoringHealthClient = &http.Client{Transport: fn}
	t.Cleanup(func() { monitoringHealthClient = orig })
}

func TestCheckMonitoringAvailabilityProbesHealthz(t *testing.T) {
	withMonitoringHealth(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/healthz" {
			t.Fatalf("path = %s, want /healthz", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
		}, nil
	})

	ok, err := checkMonitoringAvailability(context.Background())
	if err != nil {
		t.Fatalf("checkMonitoringAvailability: %v", err)
	}
	if !ok {
		t.Fatal("checkMonitoringAvailability returned false")
	}
}

func TestCheckMonitoringAvailabilityReportsUnhealthyBody(t *testing.T) {
	withMonitoringHealth(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("latest tick is stale\n")),
		}, nil
	})

	ok, err := checkMonitoringAvailability(context.Background())
	if err == nil || !strings.Contains(err.Error(), "latest tick is stale") {
		t.Fatalf("error = %v, want health body", err)
	}
	if ok {
		t.Fatal("checkMonitoringAvailability returned true")
	}
}

func TestMonitoringCapabilityIsNotInstallable(t *testing.T) {
	spec, ok := CapabilitySpecByName("monitoring")
	if !ok {
		t.Fatal("monitoring capability not registered")
	}
	if spec.Install != nil {
		t.Fatalf("install spec = %#v, want nil: the daemon ships with LinuxIO", spec.Install)
	}
}

func TestCapabilityInstallPackageSelection(t *testing.T) {
	tests := []struct {
		name             string
		wantDebian       string
		wantRHEL         string
		wantOptionalRHEL string
		wantWarning      string
	}{
		{name: "lm_sensors", wantDebian: "lm-sensors", wantRHEL: "lm_sensors"},
		{
			name:             "avahi",
			wantDebian:       "avahi-daemon libnss-mdns",
			wantRHEL:         "avahi",
			wantOptionalRHEL: "nss-mdns",
			wantWarning:      "nss-mdns was not installed. Avahi is running, but this host may need EPEL for .local name resolution.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, ok := CapabilitySpecByName(test.name)
			if !ok || spec.Install == nil {
				t.Fatalf("capability %q is not installable", test.name)
			}
			if got := spec.Install.PackageDebian; got != test.wantDebian {
				t.Errorf("debian package = %q, want %q", got, test.wantDebian)
			}
			if got := spec.Install.PackageRHEL; got != test.wantRHEL {
				t.Errorf("rhel package = %q, want %q", got, test.wantRHEL)
			}
			if got := spec.Install.OptionalPackageRHEL; got != test.wantOptionalRHEL {
				t.Errorf("optional rhel package = %q, want %q", got, test.wantOptionalRHEL)
			}
			if got := spec.Install.OptionalPackageRHELFailureWarning; got != test.wantWarning {
				t.Errorf("optional rhel package warning = %q, want %q", got, test.wantWarning)
			}
		})
	}
}

func TestLMSensorsInstallPostInstallCommand(t *testing.T) {
	spec, ok := CapabilitySpecByName("lm_sensors")
	if !ok || spec.Install == nil || spec.Install.PostInstall == nil {
		t.Fatal("lm_sensors post-install command is not registered")
	}
	if spec.Install.PostInstall.Name != "sensors-detect" {
		t.Fatalf("post-install command = %q, want sensors-detect", spec.Install.PostInstall.Name)
	}
	if got, want := spec.Install.PostInstall.Args, []string{"--auto"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("post-install args = %v, want %v", got, want)
	}
}
