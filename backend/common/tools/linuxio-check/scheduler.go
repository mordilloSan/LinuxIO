//go:build linux

package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Task is one independently runnable check. Dependencies are ordering
// constraints only: a task still runs when a dependency reports an error,
// matching make's existing two-lane behaviour.
type Task struct {
	Name          string
	Target        string
	Args          []string
	Deps          []string
	Cost          int
	Informational bool
}

type Runner interface {
	Run(context.Context, Task, io.Writer) error
}

type TaskResult struct {
	Task     Task
	Err      error
	Duration time.Duration
}

type Summary struct {
	Results           []TaskResult
	Duration          time.Duration
	SchedulerDuration time.Duration
	Capacity          int
	PeakWeight        int
}

func (s Summary) Failed() []TaskResult {
	failed := make([]TaskResult, 0)
	for _, result := range s.Results {
		if result.Err != nil && !result.Task.Informational {
			failed = append(failed, result)
		}
	}
	return failed
}

// Scheduler is deliberately a small central dispatcher. Keeping the budget
// accounting here (rather than having workers race on a semaphore) gives
// ready tasks deterministic admission and makes weighted scheduling testable.
type Scheduler struct {
	Capacity int
}

func (s Scheduler) Run(ctx context.Context, tasks []Task, runner Runner, mux *OutputMux) Summary {
	started := time.Now()
	if s.Capacity < 1 {
		return failedSummary(started, s.Capacity, fmt.Errorf("capacity must be positive"))
	}
	if err := validateTasks(tasks); err != nil {
		return failedSummary(started, s.Capacity, err)
	}
	state := newSchedulerState(tasks, s.Capacity, runner, mux)
	state.run(ctx)
	state.workers.Wait()
	return state.summary(time.Since(started))
}

func failedSummary(started time.Time, capacity int, err error) Summary {
	elapsed := time.Since(started)
	return Summary{
		Results:           []TaskResult{{Task: Task{Name: "scheduler"}, Err: err}},
		Duration:          elapsed,
		SchedulerDuration: elapsed,
		Capacity:          capacity,
	}
}

type schedulerState struct {
	tasks     []Task
	runner    Runner
	mux       *OutputMux
	capacity  int
	order     map[string]int
	started   map[string]bool
	finished  map[string]bool
	resultCh  chan TaskResult
	workers   sync.WaitGroup
	used      int
	peak      int
	completed int
	results   []TaskResult
}

func newSchedulerState(tasks []Task, capacity int, runner Runner, mux *OutputMux) *schedulerState {
	normalized := make([]Task, len(tasks))
	order := make(map[string]int, len(tasks))
	for i, task := range tasks {
		order[task.Name] = i
		if task.Cost < 1 {
			task.Cost = 1
		}
		if task.Cost > capacity {
			task.Cost = capacity
		}
		normalized[i] = task
	}
	return &schedulerState{
		tasks:    normalized,
		runner:   runner,
		mux:      mux,
		capacity: capacity,
		order:    order,
		started:  make(map[string]bool, len(tasks)),
		finished: make(map[string]bool, len(tasks)),
		resultCh: make(chan TaskResult, len(tasks)),
		results:  make([]TaskResult, 0, len(tasks)),
	}
}

func (s *schedulerState) run(ctx context.Context) {
	for s.completed < len(s.tasks) {
		if ctx.Err() != nil {
			s.cancelPendingAndDrain(ctx.Err())
			return
		}
		launched := s.launchReady(ctx)
		if s.completed == len(s.tasks) {
			return
		}
		if !launched && s.used == 0 {
			s.results = append(s.results, TaskResult{Task: Task{Name: "scheduler"}, Err: fmt.Errorf("scheduler made no progress")})
			return
		}
		select {
		case result := <-s.resultCh:
			s.record(result)
		case <-ctx.Done():
			s.cancelPendingAndDrain(ctx.Err())
			return
		}
	}
}

func (s *schedulerState) launchReady(ctx context.Context) bool {
	launched := false
	for _, task := range s.tasks {
		if ctx.Err() != nil {
			return launched
		}
		if s.started[task.Name] || !s.dependenciesFinished(task) || s.used+task.Cost > s.capacity {
			continue
		}
		s.launch(ctx, task)
		launched = true
	}
	return launched
}

func (s *schedulerState) dependenciesFinished(task Task) bool {
	for _, dependency := range task.Deps {
		if !s.finished[dependency] {
			return false
		}
	}
	return true
}

func (s *schedulerState) launch(ctx context.Context, task Task) {
	s.started[task.Name] = true
	s.used += task.Cost
	s.peak = max(s.peak, s.used)
	if s.mux != nil {
		s.mux.Start(task.Name)
	}
	s.workers.Go(func() {
		started := time.Now()
		output := io.Discard
		if s.mux != nil {
			output = s.mux.Writer(task.Name)
		}
		err := s.runner.Run(ctx, task, output)
		if s.mux != nil {
			s.mux.Finish(task.Name)
		}
		s.resultCh <- TaskResult{Task: task, Err: err, Duration: time.Since(started)}
	})
}

func (s *schedulerState) record(result TaskResult) {
	s.completed++
	s.used -= result.Task.Cost
	s.finished[result.Task.Name] = true
	s.results = append(s.results, result)
}

func (s *schedulerState) cancelPendingAndDrain(cause error) {
	for _, task := range s.tasks {
		if s.started[task.Name] {
			continue
		}
		s.started[task.Name] = true
		s.finished[task.Name] = true
		s.results = append(s.results, TaskResult{Task: task, Err: cause})
		s.completed++
	}
	for s.completed < len(s.tasks) {
		s.record(<-s.resultCh)
	}
}

func (s *schedulerState) summary(duration time.Duration) Summary {
	rank := func(name string) int {
		if value, ok := s.order[name]; ok {
			return value
		}
		return len(s.order)
	}
	sort.SliceStable(s.results, func(i, j int) bool {
		return rank(s.results[i].Task.Name) < rank(s.results[j].Task.Name)
	})
	return Summary{
		Results:           s.results,
		Duration:          duration,
		SchedulerDuration: duration,
		Capacity:          s.capacity,
		PeakWeight:        s.peak,
	}
}

func validateTasks(tasks []Task) error {
	indexed, err := indexTasks(tasks)
	if err != nil {
		return err
	}
	if err := validateDependencies(tasks, indexed); err != nil {
		return err
	}
	return validateAcyclic(tasks, indexed)
}

func indexTasks(tasks []Task) (map[string]Task, error) {
	indexed := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		if task.Name == "" {
			return nil, fmt.Errorf("task name must not be empty")
		}
		if _, exists := indexed[task.Name]; exists {
			return nil, fmt.Errorf("duplicate task name %q", task.Name)
		}
		indexed[task.Name] = task
	}
	return indexed, nil
}

func validateDependencies(tasks []Task, indexed map[string]Task) error {
	for _, task := range tasks {
		for _, dependency := range task.Deps {
			if _, exists := indexed[dependency]; !exists {
				return fmt.Errorf("task %q depends on unknown task %q", task.Name, dependency)
			}
		}
	}
	return nil
}

func validateAcyclic(tasks []Task, indexed map[string]Task) error {
	const (
		visiting = iota + 1
		visited
	)
	state := make(map[string]int, len(tasks))
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == visiting {
			return fmt.Errorf("dependency cycle includes %q", name)
		}
		if state[name] == visited {
			return nil
		}
		state[name] = visiting
		for _, dependency := range indexed[name].Deps {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = visited
		return nil
	}
	for _, task := range tasks {
		if err := visit(task.Name); err != nil {
			return err
		}
	}
	return nil
}
