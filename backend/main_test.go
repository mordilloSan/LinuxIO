package main

import "testing"

func TestRestartTargetsDefaultsToControlPlane(t *testing.T) {
	t.Parallel()

	targets, label, err := restartTargets(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	wantTargets := []string{
		linuxioBridgeSocketUserService,
		linuxioAuthSocketName,
		linuxioWebserverServiceName,
	}
	if len(targets) != len(wantTargets) {
		t.Fatalf("expected %d targets, got %d", len(wantTargets), len(targets))
	}
	for i, want := range wantTargets {
		if targets[i] != want {
			t.Fatalf("expected target %d to be %q, got %q", i, want, targets[i])
		}
	}
	if label != "LinuxIO control plane" {
		t.Fatalf("expected control plane label, got %q", label)
	}
}

func TestRestartTargetsSupportsFullRestart(t *testing.T) {
	t.Parallel()

	targets, label, err := restartTargets([]string{"--full"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(targets) != 1 || targets[0] != linuxioTargetName {
		t.Fatalf("expected full restart target %q, got %v", linuxioTargetName, targets)
	}
	if label != linuxioTargetName {
		t.Fatalf("expected full restart label %q, got %q", linuxioTargetName, label)
	}
}

func TestRestartTargetsRejectsUnknownOption(t *testing.T) {
	t.Parallel()

	if _, _, err := restartTargets([]string{"--monitoring"}); err == nil {
		t.Fatal("expected an error for an unknown restart option")
	}
}

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
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			health := monitoringContainerHealth(tt.status)
			if health != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, health)
			}
		})
	}
}
