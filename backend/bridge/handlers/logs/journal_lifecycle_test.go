package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

const journalCommandHelperEnv = "LINUXIO_JOURNAL_COMMAND_HELPER"

type journalCommandProbe struct {
	command *exec.Cmd
}

func installJournalCommandProbe(t *testing.T) *journalCommandProbe {
	t.Helper()
	probe := &journalCommandProbe{}
	original := journalCommandContext
	journalCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		probe.command = exec.CommandContext(ctx, os.Args[0], "-test.run=TestJournalCommandHelper")
		probe.command.Env = append(os.Environ(), journalCommandHelperEnv+"=1")
		return probe.command
	}
	t.Cleanup(func() { journalCommandContext = original })
	return probe
}

func (probe *journalCommandProbe) reap() {
	if probe.command == nil || probe.command.ProcessState != nil {
		return
	}
	_ = probe.command.Process.Kill()
	_ = probe.command.Wait()
}

func TestJournalCommandHelper(t *testing.T) {
	if os.Getenv(journalCommandHelperEnv) != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, `{"MESSAGE":"`+strings.Repeat("x", flushChunkBytes)+`"}`)
	for {
		time.Sleep(time.Hour)
	}
}

func TestGeneralLogsFollowWaitsAfterWriteFailure(t *testing.T) {
	probe := installJournalCommandProbe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		cancel()
		probe.reap()
	}()

	server, client := net.Pipe()
	_ = client.Close()
	defer server.Close()

	err := streamGeneralLogsFollow(ctx, server, generalLogsRequest{}, "")
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("stream error = %v, want %v", err, io.ErrClosedPipe)
	}
	assertJournalCommandWaited(t, probe)
}

func TestServiceLogsFollowWaitsAfterWriteFailure(t *testing.T) {
	probe := installJournalCommandProbe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		cancel()
		probe.reap()
	}()

	stream := newWriteErrorConn()
	err := streamServiceLogsChannel(ctx, stream, runtime.Runtime{}, apischema.ServiceLogsFollowRequest{
		ServiceName: "probe.service",
	})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("stream error = %v, want %v", err, io.ErrClosedPipe)
	}
	assertJournalCommandWaited(t, probe)
}

func assertJournalCommandWaited(t *testing.T, probe *journalCommandProbe) {
	t.Helper()
	if probe.command == nil {
		t.Fatal("journal command was not created")
	}
	if probe.command.ProcessState == nil {
		t.Fatal("stream returned without waiting for the journal command")
	}
}

type writeErrorConn struct {
	readDone chan struct{}
	once     sync.Once
}

func newWriteErrorConn() *writeErrorConn {
	return &writeErrorConn{readDone: make(chan struct{})}
}

func (conn *writeErrorConn) Read([]byte) (int, error) {
	<-conn.readDone
	return 0, io.EOF
}

func (*writeErrorConn) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (conn *writeErrorConn) Close() error {
	conn.releaseRead()
	return nil
}
func (*writeErrorConn) LocalAddr() net.Addr                  { return nil }
func (*writeErrorConn) RemoteAddr() net.Addr                 { return nil }
func (conn *writeErrorConn) SetDeadline(time.Time) error     { conn.releaseRead(); return nil }
func (conn *writeErrorConn) SetReadDeadline(time.Time) error { conn.releaseRead(); return nil }
func (*writeErrorConn) SetWriteDeadline(time.Time) error     { return nil }

func (conn *writeErrorConn) releaseRead() {
	conn.once.Do(func() { close(conn.readDone) })
}
