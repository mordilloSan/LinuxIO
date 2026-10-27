package jobs

import (
	"os"
	"testing"
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
