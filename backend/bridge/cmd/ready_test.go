package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
)

func socketStartupStatus(t *testing.T) (*startupStatus, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	bridge := os.NewFile(uintptr(fds[0]), "bridge-startup-status")
	launcher := os.NewFile(uintptr(fds[1]), "launcher-startup-status")
	t.Cleanup(func() {
		bridge.Close()
		launcher.Close()
	})
	return &startupStatus{enabled: true, f: bridge}, launcher
}

func readStartupByte(t *testing.T, f *os.File) byte {
	t.Helper()
	var got [1]byte
	if _, err := io.ReadFull(f, got[:]); err != nil {
		t.Fatalf("read startup byte: %v", err)
	}
	return got[0]
}

func TestStartupStatusReadyWaitsForGo(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	result := make(chan bool, 1)
	go func() { result <- s.ready() }()

	if got := readStartupByte(t, launcher); got != authipc.ProtoStartupReady {
		t.Fatalf("status byte = %#x, want %#x", got, authipc.ProtoStartupReady)
	}
	select {
	case <-result:
		t.Fatal("ready returned before launcher sent GO")
	case <-time.After(20 * time.Millisecond):
	}

	if _, err := launcher.Write([]byte{authipc.ProtoStartupGo}); err != nil {
		t.Fatalf("write GO: %v", err)
	}
	if proceed := <-result; !proceed {
		t.Fatal("ready returned false after valid GO")
	}
	if s.f != nil {
		t.Fatal("fd not released after GO")
	}

	// Later fail must be a no-op, not a write to a closed fd.
	s.fail("too late")
}

func TestStartupStatusReadyRejectsEOF(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	result := make(chan bool, 1)
	go func() { result <- s.ready() }()

	if got := readStartupByte(t, launcher); got != authipc.ProtoStartupReady {
		t.Fatalf("status byte = %#x, want %#x", got, authipc.ProtoStartupReady)
	}
	if err := launcher.Close(); err != nil {
		t.Fatalf("close launcher fd: %v", err)
	}
	if proceed := <-result; proceed {
		t.Fatal("ready returned true after launcher EOF")
	}
}

func TestStartupStatusReadyRejectsBadGo(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	result := make(chan bool, 1)
	go func() { result <- s.ready() }()

	if got := readStartupByte(t, launcher); got != authipc.ProtoStartupReady {
		t.Fatalf("status byte = %#x, want %#x", got, authipc.ProtoStartupReady)
	}
	if _, err := launcher.Write([]byte{0xff}); err != nil {
		t.Fatalf("write bad GO: %v", err)
	}
	if proceed := <-result; proceed {
		t.Fatal("ready returned true after invalid GO")
	}
}

func TestStartupStatusReadyRejectsWriteFailure(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	if err := launcher.Close(); err != nil {
		t.Fatalf("close launcher fd: %v", err)
	}
	if proceed := s.ready(); proceed {
		t.Fatal("ready returned true after write failure")
	}
}

func TestStartupStatusFailWritesTypedError(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	s.fail("boom")

	got, err := io.ReadAll(launcher)
	if err != nil {
		t.Fatalf("read status socket: %v", err)
	}
	if len(got) == 0 || got[0] != authipc.ProtoStartupError {
		t.Fatalf("status bytes = %v, want leading %#x", got, authipc.ProtoStartupError)
	}
	if string(got[1:]) != "boom" {
		t.Fatalf("message = %q, want %q", got[1:], "boom")
	}
}

func TestStartupStatusFailTruncatesLongMessage(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	s.fail(strings.Repeat("x", authipc.MaxStartupErrorLen+100))

	got, err := io.ReadAll(launcher)
	if err != nil {
		t.Fatalf("read status socket: %v", err)
	}
	if len(got) != 1+authipc.MaxStartupErrorLen {
		t.Fatalf("frame length = %d, want %d", len(got), 1+authipc.MaxStartupErrorLen)
	}
}

func TestStartupStatusDisabledIsInert(t *testing.T) {
	s := newStartupStatus(false)
	if proceed := s.ready(); !proceed {
		t.Fatal("disabled status blocked startup")
	}
	s.fail("must not write anywhere")
	if s.f != nil {
		t.Fatal("disabled status must hold no fd")
	}
}

func TestStartupStatusConcurrentTerminalCallsWriteOneFrame(t *testing.T) {
	s, launcher := socketStartupStatus(t)
	const callers = 100

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				s.ready()
				return
			}
			s.fail("boom")
		}(i)
	}
	close(start)

	first := readStartupByte(t, launcher)
	var want []byte
	switch first {
	case authipc.ProtoStartupReady:
		want = []byte{authipc.ProtoStartupReady}
		if _, err := launcher.Write([]byte{authipc.ProtoStartupGo}); err != nil {
			t.Fatalf("write GO: %v", err)
		}
	case authipc.ProtoStartupError:
		want = append([]byte{authipc.ProtoStartupError}, "boom"...)
	default:
		t.Fatalf("unexpected first status byte %#x", first)
	}

	rest, err := io.ReadAll(launcher)
	if err != nil {
		t.Fatalf("read status socket: %v", err)
	}
	wg.Wait()
	got := append([]byte{first}, rest...)
	if !bytes.Equal(got, want) {
		t.Fatalf("status bytes = %v, want exactly one frame %v", got, want)
	}
	if s.f != nil {
		t.Fatal("fd not released after terminal status")
	}
}
