package logs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
)

type runningJournalCommand struct {
	cmd    *exec.Cmd
	stderr bytes.Buffer
	stdout io.ReadCloser
}

var journalCommandContext = exec.CommandContext

func startJournalCommand(ctx context.Context, args []string) (*runningJournalCommand, error) {
	command := &runningJournalCommand{}
	command.cmd = journalCommandContext(ctx, "journalctl", args...)
	command.cmd.Stderr = &command.stderr

	stdout, err := command.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.stdout = stdout
	if err := command.cmd.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

// finish waits for journalctl and distinguishes an intentional early stop
// (our line/byte/page limit) from a genuine command failure.
func (command *runningJournalCommand) finish(ctx context.Context, readErr error, stopped bool) error {
	waitErr := command.cmd.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if waitErr == nil || stopped || isJournalctlNoEntries(waitErr, command.stderr.String()) {
		return nil
	}
	return journalctlError(waitErr, command.stderr.String())
}

func journalOutputLine(raw string) (string, bool) {
	line := strings.TrimSpace(raw)
	return line, line != "" && !strings.HasPrefix(line, "-- ")
}

func journalReadError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// readJournalOutput reads complete journal entries until EOF or until consume
// returns false. stopped reports that the consumer deliberately ended the
// command, allowing its kill-induced exit status to be ignored.
func readJournalOutput(
	ctx context.Context,
	command *runningJournalCommand,
	consume func(string) bool,
) (stopped bool, err error) {
	reader := bufio.NewReaderSize(command.stdout, 256*1024)
	for {
		if ctx.Err() != nil {
			killLogsProcess(command.cmd)
			return false, nil
		}

		raw, readErr := reader.ReadString('\n')
		if line, ok := journalOutputLine(raw); ok && !consume(line) {
			killLogsProcess(command.cmd)
			return true, nil
		}
		if readErr != nil {
			return false, journalReadError(readErr)
		}
	}
}

// isJournalctlNoEntries reports whether err is legacy journalctl's benign
// "no entries matched" exit (status 1 with a silent stderr). Modern systemd
// exits 0 on zero matches and reserves 1 for genuine failures — those write
// to stderr and must not be swallowed.
func isJournalctlNoEntries(err error, stderr string) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1 &&
		strings.TrimSpace(stderr) == ""
}

// journalctlError prefers journalctl's own stderr message over Go's generic
// "exit status N" so the failure reason reaches the UI error banner.
func journalctlError(err error, stderr string) error {
	if message := strings.TrimSpace(stderr); message != "" {
		if i := strings.IndexByte(message, '\n'); i >= 0 {
			message = message[:i]
		}
		return errors.New(message)
	}
	return err
}

func killLogsProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if killErr := cmd.Process.Kill(); killErr != nil {
		slog.Debug("failed to kill journalctl process",
			"component", "logs",
			"error", killErr)
	}
}
