//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := validateBudgetEnv(); err != nil {
		return fail(err)
	}
	budget := defaultBudget()
	flag.IntVar(&budget, "cpu-budget", budget, "weighted CPU budget (defaults to GOMAXPROCS)")
	flag.Parse()
	if budget < 1 {
		return fail(fmt.Errorf("cpu budget must be positive"))
	}

	root, err := findRepoRoot()
	if err != nil {
		return fail(err)
	}
	spool, err := os.MkdirTemp("", "linuxio-check-")
	if err != nil {
		return fail(fmt.Errorf("create output spool: %w", err))
	}
	defer func() { _ = os.Remove(spool) }()
	tasks := checkTasks()
	mux := NewOutputMux(spool, os.Stdout, displayOrder(tasks)...)
	defer func() { _ = mux.Close() }()
	warningFile, err := os.CreateTemp("", "linuxio-frontend-lint-warnings-")
	if err != nil {
		return fail(fmt.Errorf("create warning file: %w", err))
	}
	warningPath := warningFile.Name()
	_ = warningFile.Close()
	defer func() { _ = os.Remove(warningPath) }()

	started := time.Now()
	if raw := os.Getenv("LINUXIO_CHECK_START_NS"); raw != "" {
		if ns, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
			started = time.Unix(0, ns)
		}
	}
	makeBin := os.Getenv("LINUXIO_CHECK_MAKE")
	if makeBin == "" {
		makeBin = os.Getenv("MAKE")
	}
	runner := MakeRunner{RepoRoot: root, Make: makeBin, Env: inheritedEnv(warningPath)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	summary := (Scheduler{Capacity: budget}).Run(ctx, tasks, runner, mux)
	summary.Duration = time.Since(started)
	if err := mux.Replay(displayOrder(tasks)); err != nil {
		return fail(err)
	}
	if data, err := os.ReadFile(warningPath); err == nil && strings.TrimSpace(string(data)) != "" {
		fmt.Printf("\n⚠️  All checks completed with %s frontend lint warning(s).\n", strings.TrimSpace(string(data)))
		fmt.Println("   Warnings are non-blocking; review the Oxlint output above or run 'make lint'.")
	}
	printSummary(summary)
	if len(summary.Failed()) > 0 {
		return 1
	}
	return 0
}

func checkTasks() []Task {
	return []Task{
		{Name: "lint", Target: "lint-only", Cost: 1},
		{Name: "golint", Target: "golint-only", Cost: 1},
		{Name: "tsc", Target: "tsc-only", Deps: []string{"lint"}, Cost: 1},
		{Name: "frontend", Target: "test-frontend-only", Deps: []string{"lint"}, Cost: 2},
		{Name: "backend", Target: "test-backend", Args: []string{"SKIP_ENSURE_GO=1"}, Deps: []string{"golint"}, Cost: 2},
		{Name: "deadcode", Target: "deadcode-only", Args: []string{"SKIP_ENSURE_GO=1"}, Deps: []string{"backend"}, Cost: 2, Informational: true},
	}
}

func displayOrder(tasks []Task) []string {
	wanted := []string{"lint", "golint", "tsc", "backend", "deadcode", "frontend"}
	known := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		known[task.Name] = true
	}
	order := make([]string, 0, len(tasks))
	for _, name := range wanted {
		if known[name] {
			order = append(order, name)
			delete(known, name)
		}
	}
	for _, task := range tasks {
		if known[task.Name] {
			order = append(order, task.Name)
			delete(known, task.Name)
		}
	}
	return order
}

func defaultBudget() int {
	if raw := os.Getenv("LINUXIO_CHECK_CPU_BUDGET"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err == nil && value > 0 {
			return value
		}
	}
	return runtime.GOMAXPROCS(0)
}

func validateBudgetEnv() error {
	raw := os.Getenv("LINUXIO_CHECK_CPU_BUDGET")
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fmt.Errorf("LINUXIO_CHECK_CPU_BUDGET must be a positive integer, got %q", raw)
	}
	return nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, "Makefile")) && fileExists(filepath.Join(dir, "backend", "go.mod")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root from %s", wd)
		}
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func printSummary(summary Summary) {
	fmt.Printf("\n%s\n", summaryLine(summary))
}

func summaryLine(summary Summary) string {
	tasks := make([]string, 0, len(summary.Results))
	for _, result := range summary.Results {
		status := ""
		if result.Err != nil {
			status = " failed"
			if result.Task.Informational {
				status = " warning"
			}
		}
		tasks = append(tasks, fmt.Sprintf("%s%s %.1fs", result.Task.Name, status, result.Duration.Seconds()))
	}
	verdict := "✅ All checks passed!"
	if len(summary.Failed()) > 0 {
		verdict = "❌ Some checks failed."
	}
	return fmt.Sprintf("%s (total %.1fs; scheduler %.1fs; budget %d, peak weight %d; tasks: %s)", verdict, summary.Duration.Seconds(), summary.SchedulerDuration.Seconds(), summary.Capacity, summary.PeakWeight, strings.Join(tasks, ", "))
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "linuxio-check:", err)
	return 1
}
