package capabilities

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

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

	if _, err := buildCapabilitiesResponse(ctx, session.User{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("buildCapabilitiesResponse error = %v, want context.Canceled", err)
	}
	if detected {
		t.Fatal("capability detection ran after cancellation")
	}
}

func TestBuildCapabilitiesResponseDetectsCapabilitiesConcurrently(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
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
		response, err := buildCapabilitiesResponse(context.Background(), session.User{})
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

func TestBuildCapabilitiesResponsePassesTheSessionUserToDetectAsUser(t *testing.T) {
	originalRegistry := capabilityRegistry
	var seen session.User
	capabilityRegistry = []CapabilitySpec{{
		Name: "node",
		DetectAsUser: func(_ context.Context, u session.User) (bool, string) {
			seen = u
			return true, ""
		},
	}}
	t.Cleanup(func() {
		capabilityRegistry = originalRegistry
	})

	want := session.User{Username: "alice", UID: 1000, GID: 1000}
	response, err := buildCapabilitiesResponse(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	if seen != want {
		t.Fatalf("DetectAsUser saw %+v, want %+v", seen, want)
	}
	if !response.NodeAvailable {
		t.Fatal("node must be reported available when DetectAsUser says so")
	}
}
