package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

func TestRunHealth(t *testing.T) {
	oldCheck, oldStart := checkBtrfsHealth, startUnit
	t.Cleanup(func() { checkBtrfsHealth, startUnit = oldCheck, oldStart })
	runs := 0
	var fail error
	var actions btrfs.HealthActions
	checkBtrfsHealth = func(context.Context) (btrfs.HealthActions, error) {
		runs++
		return actions, fail
	}
	var started []string
	startUnit = func(_ context.Context, name string) error {
		started = append(started, name)
		return nil
	}
	for _, args := range [][]string{{"bogus"}, {"run"}} {
		if code := runHealth(context.Background(), args, io.Discard); code != 2 {
			t.Errorf("runHealth(%v) = %d, want 2", args, code)
		}
	}
	if runs != 0 {
		t.Fatalf("checker ran on bad usage: %d", runs)
	}
	if code := runHealth(context.Background(), nil, io.Discard); code != 0 || runs != 1 {
		t.Fatalf("run = %d, runs = %d", code, runs)
	}
	actions = btrfs.HealthActions{Resync: []string{"uuid-1"}, Rebuild: []string{"uuid-2"}}
	if code := runHealth(context.Background(), nil, io.Discard); code != 0 || !reflect.DeepEqual(started, []string{"linuxio-btrfs-resync@uuid-1.service", "linuxio-btrfs-rebuild@uuid-2.service"}) {
		t.Fatalf("run = %d, started = %v", code, started)
	}
	fail = errors.New("boom")
	if code := runHealth(context.Background(), nil, io.Discard); code != 1 {
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
	resyncPool = func(_ context.Context, uuid string, _ func(float64)) error {
		resynced = append(resynced, uuid)
		return nil
	}
	ctx := context.Background()
	for _, args := range [][]string{{"degraded", "run"}, {"degraded", "now"}, {"resync"}, {"resync", "a", "b"}} {
		if code := runCLI(ctx, args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runCLI(%v) = %d, want 2", args, code)
		}
	}
	if code := runCLI(ctx, []string{"degraded"}, io.Discard, io.Discard); code != 0 || grace != btrfs.DegradedGrace {
		t.Fatalf("degraded run = %d, grace %v", code, grace)
	}
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 0 || !reflect.DeepEqual(resynced, []string{"uuid-1"}) {
		t.Fatalf("resync = %d, resynced %v", code, resynced)
	}
	resyncPool = func(context.Context, string, func(float64)) error { return errors.New("still missing a disk") }
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("failed resync = %d, want 1", code)
	}
	// A resync that could not report its failure through the pool's alerts
	// stores no summary: the failed run and its run alert say it.
	summaries := captureSummaries(t)
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 1 || len(*summaries) != 0 {
		t.Fatalf("failed resync = %d, summaries %+v; want 1 and none", code, *summaries)
	}
	// The pool's own alerts report a failed resync, so the run completes,
	// its unit is not marked failed, and its summary says it failed.
	resyncPool = func(context.Context, string, func(float64)) error {
		return scheduledrun.Reported(errors.New("the scrub failed"))
	}
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("reported failed resync = %d, want 0", code)
	}
	if want := []scheduledrun.Summary{{Message: "Resync failed", Failures: 1}}; !reflect.DeepEqual(*summaries, want) {
		t.Fatalf("summaries = %+v, want %+v", *summaries, want)
	}
	*summaries = nil
	resyncPool = func(context.Context, string, func(float64)) error { return nil }
	if code := runCLI(ctx, []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("resync = %d, want 0", code)
	}
	if want := []scheduledrun.Summary{{Message: "Resynced"}}; !reflect.DeepEqual(*summaries, want) {
		t.Fatalf("summaries = %+v, want %+v", *summaries, want)
	}
}

// The rebuild word runs the pool's automatic rebuild: its replace's percent
// is the run's progress; a refusal or failure the pool's rebuild alert
// reports completes the run with a summary that says so; one that found
// nothing to do completes with Nothing to do; a rebuild that ends well
// starts the health check, which starts the resync at once.
func TestRunRebuild(t *testing.T) {
	oldRebuild, oldStart := rebuildPool, startUnit
	t.Cleanup(func() { rebuildPool, startUnit = oldRebuild, oldStart })
	var started []string
	startUnit = func(_ context.Context, name string) error {
		started = append(started, name)
		return nil
	}
	ctx := context.Background()
	for _, args := range [][]string{{"rebuild"}, {"rebuild", "a", "b"}, {"rebuild", ""}} {
		if code := runCLI(ctx, args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runCLI(%v) = %d, want 2", args, code)
		}
	}
	progress := stubReportProgress(t)
	summaries := captureSummaries(t)
	rebuildPool = func(_ context.Context, uuid string, report func(float64)) error {
		if uuid != "uuid-1" {
			t.Errorf("rebuild of %q", uuid)
		}
		report(37.9)
		return nil
	}
	if code := runCLI(ctx, []string{"rebuild", "uuid-1"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("rebuild = %d, want 0", code)
	}
	if want := []scheduledrun.Summary{{Message: "Rebuilt"}}; !reflect.DeepEqual(*summaries, want) {
		t.Fatalf("summaries = %+v, want %+v", *summaries, want)
	}
	if want := []string{"linuxio-btrfs-health.service"}; !reflect.DeepEqual(started, want) {
		t.Fatalf("started = %v, want %v", started, want)
	}
	if want := []string{"37 Rebuilding"}; !reflect.DeepEqual(*progress, want) {
		t.Fatalf("progress = %+v, want %+v", *progress, want)
	}

	*summaries, started = nil, nil
	rebuildPool = func(context.Context, string, func(float64)) error {
		return scheduledrun.Reported(errors.New("the target died"))
	}
	if code := runCLI(ctx, []string{"rebuild", "uuid-1"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("reported failed rebuild = %d, want 0", code)
	}
	if want := []scheduledrun.Summary{{Message: "Rebuild failed", Failures: 1}}; !reflect.DeepEqual(*summaries, want) || started != nil {
		t.Fatalf("summaries = %+v, started %v; want %+v and nothing started", *summaries, started, want)
	}

	*summaries = nil
	rebuildPool = func(context.Context, string, func(float64)) error {
		return fmt.Errorf("%w: a scrub runs on the pool", btrfs.ErrNothingToRebuild)
	}
	if code := runCLI(ctx, []string{"rebuild", "uuid-1"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("rebuild with nothing to do = %d, want 0", code)
	}
	if want := []scheduledrun.Summary{{Message: "Nothing to do", Skipped: true}}; !reflect.DeepEqual(*summaries, want) {
		t.Fatalf("summaries = %+v, want %+v", *summaries, want)
	}

	*summaries = nil
	rebuildPool = func(context.Context, string, func(float64)) error { return errors.New("pool not mounted") }
	if code := runCLI(ctx, []string{"rebuild", "uuid-1"}, io.Discard, io.Discard); code != 1 || len(*summaries) != 0 {
		t.Fatalf("unreported failure = %d, summaries %+v; want 1 and none", code, *summaries)
	}
}

// captureSummaries collects the summaries the run stores in its record.
func captureSummaries(t *testing.T) *[]scheduledrun.Summary {
	t.Helper()
	old := summarize
	t.Cleanup(func() { summarize = old })
	var stored []scheduledrun.Summary
	summarize = func(_ context.Context, summary scheduledrun.Summary) error {
		stored = append(stored, summary)
		return nil
	}
	return &stored
}

func TestRunCLIDispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"snapshot", "run"}, {"health", "run"}, {"scheduled-run"}} {
		if code := runCLI(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runCLI(%v) = %d, want 2", args, code)
		}
	}
	if code := runCLI(context.Background(), []string{"help"}, io.Discard, io.Discard); code != 0 {
		t.Errorf("help = %d, want 0", code)
	}
}

func TestResyncReportsTheScrubPercent(t *testing.T) {
	oldResync := resyncPool
	t.Cleanup(func() { resyncPool = oldResync })
	resyncPool = func(_ context.Context, _ string, report func(float64)) error {
		report(42.6)
		return nil
	}
	got := stubReportProgress(t)

	if code := runCLI(context.Background(), []string{"resync", "uuid-1"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("resync = %d, want 0", code)
	}
	if want := []string{"42 Scrubbing"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("reported %q, want %q", *got, want)
	}
}
