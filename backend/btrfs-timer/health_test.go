package main

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func TestRunHealth(t *testing.T) {
	oldCheck, oldStart := checkBtrfsHealth, startUnit
	t.Cleanup(func() { checkBtrfsHealth, startUnit = oldCheck, oldStart })
	runs := 0
	var fail error
	var resync []string
	checkBtrfsHealth = func(context.Context) ([]string, error) {
		runs++
		return resync, fail
	}
	var started []string
	startUnit = func(_ context.Context, name string) error {
		started = append(started, name)
		return nil
	}
	for _, args := range [][]string{nil, {"bogus"}, {"run", "extra"}} {
		if code := runHealth(context.Background(), args, io.Discard); code != 2 {
			t.Errorf("runHealth(%v) = %d, want 2", args, code)
		}
	}
	if runs != 0 {
		t.Fatalf("checker ran on bad usage: %d", runs)
	}
	if code := runHealth(context.Background(), []string{"run"}, io.Discard); code != 0 || runs != 1 {
		t.Fatalf("run = %d, runs = %d", code, runs)
	}
	resync = []string{"uuid-1"}
	if code := runHealth(context.Background(), []string{"run"}, io.Discard); code != 0 || !reflect.DeepEqual(started, []string{"linuxio-btrfs-resync@uuid-1.service"}) {
		t.Fatalf("run = %d, started = %v", code, started)
	}
	fail = errors.New("boom")
	if code := runHealth(context.Background(), []string{"run"}, io.Discard); code != 1 {
		t.Fatalf("failed run = %d, want 1", code)
	}
}

func TestRunDegradedAndResync(t *testing.T) {
	oldMount, oldResync := mountDegradedPools, resyncPool
	t.Cleanup(func() { mountDegradedPools, resyncPool = oldMount, oldResync })
	var grace time.Duration
	mountDegradedPools = func(_ context.Context, g time.Duration) error {
		grace = g
		return nil
	}
	var resynced []string
	resyncPool = func(_ context.Context, uuid string) error {
		resynced = append(resynced, uuid)
		return nil
	}
	ctx := context.Background()
	for _, args := range [][]string{{"degraded"}, {"degraded", "now"}, {"resync"}, {"resync", "a", "b"}} {
		if code := runCLI(ctx, args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runCLI(%v) = %d, want 2", args, code)
		}
	}
	if code := runCLI(ctx, []string{"degraded", "run"}, io.Discard, io.Discard); code != 0 || grace != btrfs.DegradedGrace {
		t.Fatalf("degraded run = %d, grace %v", code, grace)
	}
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 0 || !reflect.DeepEqual(resynced, []string{"uuid-1"}) {
		t.Fatalf("resync = %d, resynced %v", code, resynced)
	}
	resyncPool = func(context.Context, string) error { return errors.New("still missing a disk") }
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("failed resync = %d, want 1", code)
	}
}

func TestRunCLIDispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"snapshot"}, {"health"}} {
		if code := runCLI(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runCLI(%v) = %d, want 2", args, code)
		}
	}
	if code := runCLI(context.Background(), []string{"help"}, io.Discard, io.Discard); code != 0 {
		t.Errorf("help = %d, want 0", code)
	}
}
