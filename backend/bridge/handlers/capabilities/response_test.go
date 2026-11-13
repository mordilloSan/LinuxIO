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
		if docker := completed.response["docker"]; !docker.Available || docker.Error != nil {
			t.Errorf("docker status = %+v, want available without error", docker)
		}
		dockerUpdates := completed.response["docker_updates"]
		if dockerUpdates.Available {
			t.Error("docker_updates capability reported available")
		}
		if dockerUpdates.Error == nil || *dockerUpdates.Error != "not installed" {
			t.Errorf("docker_updates error = %v, want %q", dockerUpdates.Error, "not installed")
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

// TestBuildCapabilitiesResponseReportsEveryCapability runs every registry name
// through a stub detector and checks the JSON response: each name keys exactly
// its own status, an unavailable capability carries its detection message as
// error, and an available one has no error member.
func TestBuildCapabilitiesResponseReportsEveryCapability(t *testing.T) {
	originalRegistry := capabilityRegistry
	t.Cleanup(func() {
		capabilityRegistry = originalRegistry
	})

	stubs := make([]CapabilitySpec, 0, len(originalRegistry))
	for index, spec := range originalRegistry {
		available := index%2 == 0
		message := ""
		if !available {
			message = "marker-for-" + spec.Name
		}
		stubs = append(stubs, CapabilitySpec{
			Name: spec.Name,
			Detect: func(context.Context) (bool, string) {
				return available, message
			},
		})
	}
	capabilityRegistry = stubs

	response, err := buildCapabilitiesResponse(context.Background(), session.User{})
	if err != nil {
		t.Fatalf("buildCapabilitiesResponse error = %v", err)
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload) != len(stubs) {
		t.Errorf("response has %d capabilities, want %d: %s", len(payload), len(stubs), data)
	}
	for index, spec := range stubs {
		status, ok := payload[spec.Name]
		if !ok {
			t.Errorf("response has no %q: %s", spec.Name, data)
			continue
		}
		wantAvailable := index%2 == 0
		if status["available"] != wantAvailable {
			t.Errorf("%s available = %v, want %v", spec.Name, status["available"], wantAvailable)
		}
		errValue, hasError := status["error"]
		switch {
		case wantAvailable && hasError:
			t.Errorf("%s error = %v, want no error member", spec.Name, errValue)
		case !wantAvailable && errValue != "marker-for-"+spec.Name:
			t.Errorf("%s error = %v, want %q", spec.Name, errValue, "marker-for-"+spec.Name)
		}
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
	if !response["node"].Available {
		t.Fatal("node must be reported available when DetectAsUser says so")
	}
}
