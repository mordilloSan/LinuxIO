package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Worker supervision: full scans run in a private subprocess of this binary
// (--index-mode), so a crash or memory spike in the scan cannot take the
// daemon down. The worker's side of the protocol is in wire.go and fullscan.go.

// runIndexSubprocess spawns the current binary with --index-mode flag.
// Wire events decoded from the subprocess stdout stream to onEvent (may be
// nil); the returned stats come from the subprocess's summary event, nil when
// it never emitted one.
func (d *daemon) runIndexSubprocess(ctx context.Context, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	slog.Debug("starting index process")
	stats, err := runIndexProcess(exec.CommandContext(ctx, os.Args[0], "--index-mode", "--config", d.settings.path), onEvent)
	if err != nil {
		return nil, err
	}

	slog.Debug("index process completed successfully")
	return stats, nil
}

// runIndexProcess runs a prepared index-mode command, decoding wire events
// from its stdout as they arrive. Each event is handed to onEvent (which may
// be nil); the final summary, when the subprocess emitted one, is also
// returned as run statistics.
func runIndexProcess(cmd *exec.Cmd, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	cmd.Stderr = os.Stderr
	cancel := func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	if cmd.Cancel != nil {
		cmd.Cancel = cancel
	}
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = workerKillDelay
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("index subprocess stdout: %w", err)
	}
	if err := startAtLowPriority(cmd); err != nil {
		return nil, fmt.Errorf("index subprocess start: %w", err)
	}

	summary, decodeErr := decodeIndexProcessOutput(stdout, onEvent)
	var killTimer *time.Timer
	if decodeErr != nil {
		_ = cancel()
		// A manual Cancel does not start exec.Cmd's WaitDelay clock. Force a
		// kill after the same bound so malformed output cannot leave Wait
		// blocked on a child that ignores SIGTERM or remains stuck writing.
		killTimer = time.AfterFunc(cmd.WaitDelay, func() {
			_ = cmd.Process.Kill()
		})
	}
	waitErr := cmd.Wait()
	if killTimer != nil {
		killTimer.Stop()
	}
	if decodeErr != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("%w (wait: %v)", decodeErr, waitErr)
		}
		return nil, decodeErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("index subprocess failed: %w", waitErr)
	}
	return summary, nil
}

// ioprio_set(2) takes these; x/sys/unix names only the syscall number.
const (
	ioprioWhoProcess = 1  // IOPRIO_WHO_PROCESS: one thread, 0 for the caller
	ioprioClassIdle  = 3  // IOPRIO_CLASS_IDLE: I/O only when no one else wants the disk
	ioprioClassShift = 13 // IOPRIO_CLASS_SHIFT
	lowestNice       = 19
)

// startAtLowPriority starts cmd at the lowest CPU priority and in the idle
// I/O class, so that the scanner yields the CPU to everything else on the
// host, the daemon's own searches included. The I/O class covers what the
// scanner reads while it walks, on a disk whose scheduler honours it (BFQ,
// mq-deadline); the database pages it writes are flushed later by the kernel
// and do not carry it.
// Linux keeps both priorities per thread, and a child process and every
// thread it creates take them from the thread that started it, so one thread
// of the daemon gives up its priority and starts the child. The daemon cannot
// raise a priority again (its unit leaves it no CAP_SYS_NICE), so that thread
// is not handed back to the runtime: a goroutine that ends while locked to
// its thread takes the thread with it.
func startAtLowPriority(cmd *exec.Cmd) error {
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Setpriority(unix.PRIO_PROCESS, 0, lowestNice); err != nil {
			slog.Warn("failed to lower the scanner's CPU priority", "error", err)
		}
		if _, _, errno := unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, 0, ioprioClassIdle<<ioprioClassShift); errno != 0 {
			slog.Warn("failed to lower the scanner's I/O priority", "error", errno)
		}
		started <- cmd.Start()
	}()
	return <-started
}
