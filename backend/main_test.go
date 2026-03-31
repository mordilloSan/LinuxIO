package main

import "testing"

func TestMonitoringContainerStateUsesExplicitState(t *testing.T) {
	t.Parallel()

	state := monitoringContainerState("running", "Exited (0) 2 seconds ago")
	if state != "running" {
		t.Fatalf("expected explicit state to win, got %q", state)
	}
}

func TestMonitoringContainerStateFallsBackToStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   string
	}{
		{status: "Up 10 seconds", want: "running"},
		{status: "Exited (0) 2 minutes ago", want: "exited"},
		{status: "Restarting (1) 3 seconds ago", want: "restarting"},
		{status: "Paused", want: "paused"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			state := monitoringContainerState("", tt.status)
			if state != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, state)
			}
		})
	}
}

func TestMonitoringContainerHealthParsesHealthStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   string
	}{
		{status: "Up 15 seconds (healthy)", want: "healthy"},
		{status: "Up 15 seconds (unhealthy)", want: "unhealthy"},
		{status: "Up 15 seconds (health: starting)", want: "starting"},
		{status: "Exited (0) 2 minutes ago", want: "-"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			health := monitoringContainerHealth(tt.status)
			if health != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, health)
			}
		})
	}
}
