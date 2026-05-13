package network

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestExecRunnerRunCanceledBeforeStart(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (ExecRunner{}).Run(ctx, "sh", "-c", "echo should-not-run")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

func TestExecRunnerRunCancelsRunningCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	started := time.Now()
	_, err := (ExecRunner{}).Run(ctx, "sleep", "5")
	if err == nil {
		t.Fatal("Run error = nil, want cancellation error")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Run took %s, command was not canceled promptly", elapsed)
	}
}
