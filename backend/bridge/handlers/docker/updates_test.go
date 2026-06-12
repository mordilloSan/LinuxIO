package docker

import (
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/watchtower"
)

func TestUpdateCheckStatuses(t *testing.T) {
	now := time.Now()
	summaries := map[string]container.Summary{
		"nginx": {ID: "c1", ImageID: "sha256:img1"},
		"redis": {ID: "c2", ImageID: "sha256:img2"},
	}
	results := []watchtower.Result{
		{Name: "nginx", Image: "nginx:latest", State: watchtower.StateStale},
		{Name: "redis", Image: "redis:7", State: watchtower.StateFresh},
		{Name: "gone", Image: "gone:1", State: watchtower.StateFailed},
	}

	statuses, result := updateCheckStatuses(results, summaries, now)

	want := apischema.DockerUpdateCheckResult{Checked: 3, Errors: 1, Updates: 1}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if len(statuses) != 3 {
		t.Fatalf("len(statuses) = %d, want 3", len(statuses))
	}
	if !statuses[0].UpdateAvailable || statuses[0].ContainerID != "c1" || statuses[0].ImageID != "sha256:img1" {
		t.Fatalf("stale status = %+v", statuses[0])
	}
	if statuses[1].UpdateAvailable || statuses[1].Err != "" {
		t.Fatalf("fresh status = %+v", statuses[1])
	}
	if statuses[2].Err == "" || statuses[2].ContainerID != "" {
		t.Fatalf("failed status without summary = %+v", statuses[2])
	}
}

func TestApplyUpdateOutcome(t *testing.T) {
	tests := []struct {
		name          string
		res           watchtower.Result
		wantReinspect bool
		wantErr       bool
		wantUpdated   bool
		wantMessage   string
	}{
		{
			name:          "updated",
			res:           watchtower.Result{State: watchtower.StateUpdated},
			wantReinspect: true,
			wantUpdated:   true,
		},
		{
			name: "fresh",
			res:  watchtower.Result{State: watchtower.StateFresh},
		},
		{
			name:          "restarted",
			res:           watchtower.Result{State: watchtower.StateRestarted},
			wantReinspect: true,
		},
		{
			name:        "skipped with reason",
			res:         watchtower.Result{State: watchtower.StateSkipped, Err: "cooldown active"},
			wantMessage: "cooldown active",
		},
		{
			name:        "skipped without reason",
			res:         watchtower.Result{State: watchtower.StateSkipped},
			wantMessage: "update skipped",
		},
		{
			name:        "stale means monitor-only",
			res:         watchtower.Result{State: watchtower.StateStale},
			wantMessage: "update available but not applied (container is monitor-only)",
		},
		{
			name:    "failed",
			res:     watchtower.Result{State: watchtower.StateFailed, Err: "pull denied"},
			wantErr: true,
		},
		{
			name:    "unexpected state",
			res:     watchtower.Result{State: watchtower.StateUnknown},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := apischema.DockerContainerUpdateResult{ContainerName: "nginx"}
			reinspect, err := applyUpdateOutcome(&result, tc.res)
			if tc.wantErr {
				if err == nil {
					t.Fatal("applyUpdateOutcome: want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyUpdateOutcome: %v", err)
			}
			if reinspect != tc.wantReinspect {
				t.Fatalf("reinspect = %v, want %v", reinspect, tc.wantReinspect)
			}
			if result.Updated != tc.wantUpdated {
				t.Fatalf("Updated = %v, want %v", result.Updated, tc.wantUpdated)
			}
			if result.Error != tc.wantMessage {
				t.Fatalf("Error = %q, want %q", result.Error, tc.wantMessage)
			}
		})
	}
}

func TestWatchtowerResultFor(t *testing.T) {
	results := []watchtower.Result{
		{Name: "nginx", State: watchtower.StateFresh},
		{Name: "redis", State: watchtower.StateUpdated},
	}
	if res, ok := watchtowerResultFor(results, "redis"); !ok || res.State != watchtower.StateUpdated {
		t.Fatalf("watchtowerResultFor(redis) = %+v, %v", res, ok)
	}
	if _, ok := watchtowerResultFor(results, "missing"); ok {
		t.Fatal("watchtowerResultFor(missing) = ok, want miss")
	}
}
