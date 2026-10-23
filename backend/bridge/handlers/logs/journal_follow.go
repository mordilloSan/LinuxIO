package logs

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const (
	// followFlushInterval coalesces follow-mode lines into batched frames so a
	// chatty journal produces a few frames per second, not one per line.
	followFlushInterval = 150 * time.Millisecond
	// flushChunkBytes flushes a frame early once it grows past this size.
	flushChunkBytes = 64 * 1024
)

// errJournalFollowEnded reports that a follow process exited on its own.
var errJournalFollowEnded = errors.New("journalctl follow stream ended unexpectedly")

type journalDataBatch struct {
	stream net.Conn
	buf    strings.Builder
}

func (batch *journalDataBatch) append(line string) error {
	lineBytes := len(line) + 1
	if batch.buf.Len() > 0 && batch.buf.Len()+lineBytes > flushChunkBytes {
		if err := batch.flush(); err != nil {
			return err
		}
	}
	batch.buf.WriteString(line)
	batch.buf.WriteByte('\n')
	if batch.buf.Len() >= flushChunkBytes {
		return batch.flush()
	}
	return nil
}

func (batch *journalDataBatch) flush() error {
	if batch.buf.Len() == 0 {
		return nil
	}
	err := relay.WriteRelayFrame(batch.stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: []byte(batch.buf.String())})
	batch.buf.Reset()
	return err
}

func (batch *journalDataBatch) drain(lines <-chan string) error {
	for {
		select {
		case line := <-lines:
			if err := batch.append(line); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func startJournalLineReader(
	ctx context.Context,
	stdout io.Reader,
	trim func(string) string,
) (<-chan string, <-chan error) {
	lines := make(chan string, 256)
	done := make(chan error, 1)
	go func() {
		done <- readJournalLines(ctx, stdout, lines, trim)
	}()
	return lines, done
}

func readJournalLines(ctx context.Context, stdout io.Reader, lines chan<- string, trim func(string) string) error {
	reader := bufio.NewReaderSize(stdout, 256*1024)
	for {
		raw, readErr := reader.ReadString('\n')
		if line, ok := journalOutputLine(raw); ok {
			select {
			case lines <- trim(line):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if readErr != nil {
			return journalReadError(readErr)
		}
	}
}

// stopJournalFollow kills the journal process and waits for its line reader
// to exit; the reader's error is expected after the kill.
func stopJournalFollow(command *runningJournalCommand, readDone <-chan error) {
	killLogsProcess(command.cmd)
	if err := <-readDone; err != nil {
		slog.Debug("journal line reader stopped", "err", err)
	}
	_ = command.cmd.Wait()
}

func cancelJournalFollow(
	ctx context.Context,
	command *runningJournalCommand,
	readDone <-chan error,
	batch *journalDataBatch,
	lines <-chan string,
) error {
	stopJournalFollow(command, readDone)
	if err := batch.drain(lines); err != nil {
		return err
	}
	if err := batch.flush(); err != nil {
		return err
	}
	return ctx.Err()
}

func finishJournalFollow(
	ctx context.Context,
	command *runningJournalCommand,
	readErr error,
	batch *journalDataBatch,
	lines <-chan string,
) error {
	finishErr := command.finish(ctx, readErr, false)
	if err := batch.drain(lines); err != nil {
		return err
	}
	if err := batch.flush(); err != nil {
		return err
	}
	if finishErr != nil {
		return finishErr
	}
	// A follow process exiting on its own is unexpected; surface it so the
	// frontend can reconnect instead of showing a silent dead tail.
	return errJournalFollowEnded
}

// streamGeneralLogsFollow tails the journal from just after newestCursor,
// trimming each JSON entry down to the fields the UI consumes.
func streamGeneralLogsFollow(ctx context.Context, stream net.Conn, req generalLogsRequest, afterCursor string) error {
	return followJournal(ctx, stream, followArgs(req, afterCursor), func(line string) string {
		entry, _ := trimJournalLine(line)
		return entry
	})
}

// followJournal runs a journalctl follow command and relays its output lines,
// transformed by trim and coalesced into batched frames on a size/interval
// policy. It returns errJournalFollowEnded if journalctl exits on its own.
func followJournal(ctx context.Context, stream net.Conn, args []string, trim func(string) string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	command, err := startJournalCommand(ctx, args)
	if err != nil {
		return err
	}

	lines, readDone := startJournalLineReader(ctx, command.stdout, trim)
	abort := func() {
		cancel()
		stopJournalFollow(command, readDone)
	}
	ticker := time.NewTicker(followFlushInterval)
	defer ticker.Stop()
	batch := journalDataBatch{stream: stream}

	for {
		select {
		case <-ctx.Done():
			return cancelJournalFollow(ctx, command, readDone, &batch, lines)
		case line := <-lines:
			if err := batch.append(line); err != nil {
				abort()
				return err
			}
		case <-ticker.C:
			if err := batch.flush(); err != nil {
				abort()
				return err
			}
		case readErr := <-readDone:
			return finishJournalFollow(ctx, command, readErr, &batch, lines)
		}
	}
}
