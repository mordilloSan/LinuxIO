package jobs

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessAliveDetectsOwnProcess(t *testing.T) {
	pid := int32(os.Getpid())
	start, err := ProcessStart(pid)
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	if !ProcessAlive(Executor{Kind: "host", PID: pid, PIDStart: start}) {
		t.Error("own process reported dead")
	}
	if ProcessAlive(Executor{Kind: "host", PID: 0, PIDStart: start}) {
		t.Error("pid 0 reported alive")
	}
	if ProcessAlive(Executor{Kind: "host", PID: pid, PIDStart: start + 1}) {
		t.Error("mismatched start time reported alive")
	}
}

func TestProcessAliveTreatsAZombieAsDead(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start true: %v", err)
	}
	pid := int32(cmd.Process.Pid)
	start, err := ProcessStart(pid)
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	// Not waited for, the exited child stays a zombie until Wait.
	deadline := time.Now().Add(5 * time.Second)
	for state, _, _ := processStat(pid); state != "Z"; state, _, _ = processStat(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child state = %q, want Z", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ProcessAlive(Executor{Kind: "host", PID: pid, PIDStart: start}) {
		t.Error("zombie reported alive")
	}
	_ = cmd.Wait()
}
