//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fakeRunner struct {
	mu      sync.Mutex
	started []string
	active  atomic.Int32
	max     atomic.Int32
	delay   time.Duration
	fail    map[string]error
	order   []string
}

func (r *fakeRunner) Run(ctx context.Context, task Task, output io.Writer) error {
	r.mu.Lock()
	r.started = append(r.started, task.Name)
	r.order = append(r.order, task.Name)
	r.mu.Unlock()
	active := r.active.Add(1)
	for {
		old := r.max.Load()
		if active <= old || r.max.CompareAndSwap(old, active) {
			break
		}
	}
	defer r.active.Add(-1)
	if _, err := io.WriteString(output, task.Name+" output\n"); err != nil {
		return err
	}
	timer := time.NewTimer(r.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := r.fail[task.Name]; err != nil {
		return err
	}
	return nil
}

func testMux(t *testing.T) (*OutputMux, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	terminal := new(bytes.Buffer)
	return NewOutputMux(dir, terminal), terminal
}

func TestSchedulerWeightedCapacityAndDependencies(t *testing.T) {
	runner := &fakeRunner{delay: 15 * time.Millisecond}
	tasks := []Task{
		{Name: "lint", Cost: 1}, {Name: "golint", Cost: 1},
		{Name: "frontend", Deps: []string{"lint"}, Cost: 2},
		{Name: "backend", Deps: []string{"golint"}, Cost: 2},
		{Name: "deadcode", Deps: []string{"backend"}, Cost: 2},
	}
	mux, terminal := testMux(t)
	summary := (Scheduler{Capacity: 2}).Run(context.Background(), tasks, runner, mux)
	if got := runner.max.Load(); got > 2 {
		t.Fatalf("weighted capacity exceeded: max active %d", got)
	}
	if summary.PeakWeight != 2 || summary.Capacity != 2 {
		t.Fatalf("weighted occupancy not recorded: capacity=%d peak=%d", summary.Capacity, summary.PeakWeight)
	}
	if len(summary.Results) != len(tasks) || len(summary.Failed()) != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if err := mux.Replay([]string{"lint", "golint", "backend", "deadcode", "frontend"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "lint output") {
		t.Fatal("expected task output")
	}
	if got := strings.Count(terminal.String(), "\nlint output\n") + boolInt(strings.HasPrefix(terminal.String(), "lint output\n")); got != 1 {
		t.Fatalf("live output replayed %d times: %q", got, terminal.String())
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestSchedulerRunsDescendantAfterFailedDependency(t *testing.T) {
	runner := &fakeRunner{fail: map[string]error{"lint": errors.New("lint failed")}}
	tasks := []Task{{Name: "lint", Cost: 1}, {Name: "tsc", Deps: []string{"lint"}, Cost: 1}}
	mux, _ := testMux(t)
	summary := (Scheduler{Capacity: 1}).Run(context.Background(), tasks, runner, mux)
	if len(summary.Results) != 2 || len(summary.Failed()) != 1 {
		t.Fatalf("expected dependency and descendant to run: %+v", summary)
	}
	runner.mu.Lock()
	got := append([]string(nil), runner.started...)
	runner.mu.Unlock()
	if !reflect.DeepEqual(got, []string{"lint", "tsc"}) {
		t.Fatalf("unexpected start order: %v", got)
	}
}

func TestSchedulerMarksPendingTasksOnCancellation(t *testing.T) {
	runner := &fakeRunner{delay: 100 * time.Millisecond}
	tasks := []Task{{Name: "first", Cost: 1}, {Name: "second", Cost: 1}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	mux, _ := testMux(t)
	summary := (Scheduler{Capacity: 1}).Run(ctx, tasks, runner, mux)
	if len(summary.Results) != len(tasks) {
		t.Fatalf("cancellation left tasks unresolved: %+v", summary)
	}
	runner.mu.Lock()
	started := append([]string(nil), runner.started...)
	runner.mu.Unlock()
	if !reflect.DeepEqual(started, []string{"first"}) {
		t.Fatalf("scheduler admitted work after cancellation: %v", started)
	}
}

func TestMakeRunnerKillsProcessGroupOnCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-make")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n(sleep 30) &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := make(chan int, 1)
	var pid atomic.Int32
	runner := MakeRunner{RepoRoot: dir, Make: script, OnStart: func(value int) { started <- value }}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		value := <-started
		pid.Store(int32(value))
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	err := runner.Run(ctx, Task{Name: "process-group", Target: "ignored"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	processID := int(pid.Load())
	if processID == 0 {
		t.Fatal("runner did not report process start")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-processID, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process group %d survived cancellation", processID)
}

func TestValidateTasksRejectsCyclesAndUnknownDependencies(t *testing.T) {
	if err := validateTasks([]Task{{Name: "a", Deps: []string{"missing"}}}); err == nil {
		t.Fatal("unknown dependency accepted")
	}
	if err := validateTasks([]Task{{Name: "a", Deps: []string{"b"}}, {Name: "b", Deps: []string{"a"}}}); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestSchedulerRejectsNonPositiveCapacity(t *testing.T) {
	summary := (Scheduler{}).Run(context.Background(), []Task{{Name: "task", Cost: 1}}, &fakeRunner{}, nil)
	if len(summary.Failed()) != 1 || !strings.Contains(summary.Failed()[0].Err.Error(), "capacity must be positive") {
		t.Fatalf("invalid capacity was not rejected: %+v", summary)
	}
}

func TestOutputMuxSpoolsAndReplaysWithoutDuplication(t *testing.T) {
	mux, terminal := testMux(t)
	mux.Start("live")
	if _, err := io.WriteString(mux.Writer("live"), "live\n"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("live")
	mux.Start("buffered")
	if _, err := io.WriteString(mux.Writer("buffered"), "buffered\n"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("buffered")
	if err := mux.Replay([]string{"live", "buffered"}); err != nil {
		t.Fatal(err)
	}
	if got := terminal.String(); got != "live\nbuffered\n" {
		t.Fatalf("unexpected output %q", got)
	}
}

func TestOutputMuxPreservesEarlierBufferedTaskBeforePromotingLaterTask(t *testing.T) {
	dir := t.TempDir()
	terminal := new(bytes.Buffer)
	mux := NewOutputMux(dir, terminal, "lint", "golint", "tsc")
	mux.Start("lint")
	mux.Start("golint")
	if _, err := io.WriteString(mux.Writer("golint"), "golint\n"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("golint")
	if _, err := io.WriteString(mux.Writer("lint"), "lint\n"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("lint")
	mux.Start("tsc")
	if _, err := io.WriteString(mux.Writer("tsc"), "tsc\n"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("tsc")
	if err := mux.Replay([]string{"lint", "golint", "tsc"}); err != nil {
		t.Fatal(err)
	}
	if got := terminal.String(); got != "lint\ngolint\ntsc\n" {
		t.Fatalf("output order or duplication incorrect: %q", got)
	}
}

func TestOutputMuxPromotionDrainsActiveBacklogOnce(t *testing.T) {
	dir := t.TempDir()
	terminal := new(bytes.Buffer)
	mux := NewOutputMux(dir, terminal, "first", "second")
	mux.Start("first")
	mux.Start("second")
	if _, err := io.WriteString(mux.Writer("second"), "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(mux.Writer("first"), "first\n"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("first")
	if _, err := io.WriteString(mux.Writer("second"), "B"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("second")
	if err := mux.Replay([]string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	if got := terminal.String(); got != "first\nAB" {
		t.Fatalf("promotion lost or duplicated backlog: %q", got)
	}
}

func TestOutputMuxCloseRemovesSpoolDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mux := NewOutputMux(dir, io.Discard, "task")
	mux.Start("task")
	if _, err := io.WriteString(mux.Writer("task"), "output"); err != nil {
		t.Fatal(err)
	}
	mux.Finish("task")
	if err := mux.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("spool directory still exists: %v", err)
	}
}

func TestCheckTasksMatchesMakeGraphAndOrder(t *testing.T) {
	tasks := checkTasks()
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	if !reflect.DeepEqual(names, []string{"lint", "golint", "tsc", "frontend", "backend", "deadcode"}) {
		t.Fatalf("unexpected task declaration order: %v", names)
	}
	if !reflect.DeepEqual(tasks[2].Deps, []string{"lint"}) || !reflect.DeepEqual(tasks[3].Deps, []string{"lint"}) {
		t.Fatal("frontend dependencies changed")
	}
	if !reflect.DeepEqual(tasks[4].Deps, []string{"golint"}) || !reflect.DeepEqual(tasks[5].Deps, []string{"backend"}) {
		t.Fatal("backend dependencies changed")
	}
	if !tasks[5].Informational || !reflect.DeepEqual(tasks[4].Args, []string{"SKIP_ENSURE_GO=1"}) {
		t.Fatal("backend/deadcode make semantics changed")
	}
}

func TestFindRepoRootFromBackend(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Makefile")); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryFailedExcludesInformationalTasks(t *testing.T) {
	summary := Summary{Results: []TaskResult{{Task: Task{Name: "deadcode", Informational: true}, Err: errors.New("warning")}}}
	if failed := summary.Failed(); len(failed) != 0 {
		t.Fatalf("informational task treated as fatal: %v", failed)
	}
}

func TestSummaryLineIsCondensed(t *testing.T) {
	summary := Summary{
		Results: []TaskResult{
			{Task: Task{Name: "lint"}, Duration: 6400 * time.Millisecond},
			{Task: Task{Name: "golint"}, Duration: 11700 * time.Millisecond},
		},
		Duration:          37400 * time.Millisecond,
		SchedulerDuration: 33900 * time.Millisecond,
		Capacity:          16,
		PeakWeight:        4,
	}
	want := "✅ All checks passed! (total 37.4s; scheduler 33.9s; budget 16, peak weight 4; tasks: lint 6.4s, golint 11.7s)"
	if got := summaryLine(summary); got != want {
		t.Fatalf("unexpected summary line:\n got: %q\nwant: %q", got, want)
	}
}

func TestDisplayOrder(t *testing.T) {
	order := displayOrder(checkTasks())
	want := []string{"lint", "golint", "tsc", "backend", "deadcode", "frontend"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("unexpected display order: %v", order)
	}
}
