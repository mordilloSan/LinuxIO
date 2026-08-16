//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type MakeRunner struct {
	RepoRoot string
	Make     string
	Env      []string
	OnStart  func(int)
}

func (r MakeRunner) Run(ctx context.Context, task Task, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s cancelled before start: %w", task.Name, err)
	}
	makeBin := r.Make
	if makeBin == "" {
		makeBin = "make"
	}
	args := []string{"--no-print-directory", task.Target}
	args = append(args, task.Args...)
	cmd := exec.Command(makeBin, args...)
	cmd.Dir = r.RepoRoot
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.Env = r.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", task.Name, err)
	}
	if r.OnStart != nil {
		r.OnStart(cmd.Process.Pid)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", task.Name, err)
		}
		return nil
	case <-ctx.Done():
		// Prefer a result that is already available. This avoids signaling a
		// process group after a simultaneous natural completion.
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("%s: %w", task.Name, err)
			}
			return nil
		default:
		}
		// The command is a process-group leader. Kill its complete make/
		// shell/node/go subtree, not just the direct make process.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			cleanupErr := waitProcessGroupGone(cmd.Process.Pid)
			return cancelledTaskError(task.Name, ctx.Err(), cleanupErr)
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
			cleanupErr := waitProcessGroupGone(cmd.Process.Pid)
			return cancelledTaskError(task.Name, ctx.Err(), cleanupErr)
		}
	}
}

func cancelledTaskError(name string, cause, cleanupErr error) error {
	if cleanupErr != nil {
		return fmt.Errorf("%s cancelled: %w; cleanup: %v", name, cause, cleanupErr)
	}
	return fmt.Errorf("%s cancelled: %w", name, cause)
}

func waitProcessGroupGone(pid int) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("inspect process group %d: %w", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("process group %d still exists after SIGKILL", pid)
}

func inheritedEnv(warningFile string) []string {
	env := os.Environ()
	if warningFile != "" {
		filtered := env[:0]
		for _, entry := range env {
			if len(entry) < len("FRONTEND_LINT_WARNINGS_FILE=") || entry[:len("FRONTEND_LINT_WARNINGS_FILE=")] != "FRONTEND_LINT_WARNINGS_FILE=" {
				filtered = append(filtered, entry)
			}
		}
		env = filtered
		env = append(env, "FRONTEND_LINT_WARNINGS_FILE="+warningFile)
	}
	return env
}
