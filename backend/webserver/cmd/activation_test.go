package cmd

import (
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
)

func TestSystemdListenersNoEnv(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")

	listeners, err := systemdListeners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listeners != nil {
		t.Fatalf("want nil, got %d listeners", len(listeners))
	}
}

func TestSystemdListenersWrongPID(t *testing.T) {
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()+1))
	t.Setenv("LISTEN_FDS", "1")

	listeners, err := systemdListeners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listeners != nil {
		t.Fatalf("want nil (wrong PID), got %d listeners", len(listeners))
	}
}

func TestSystemdListenersHappyPath(t *testing.T) {
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer tcpListener.Close()

	file, err := tcpListener.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	defer file.Close()

	savedFd, savedErr := syscall.Dup(listenFDsStart)
	if err := syscall.Dup2(int(file.Fd()), listenFDsStart); err != nil {
		t.Fatalf("dup2: %v", err)
	}
	t.Cleanup(func() {
		if savedErr == nil {
			_ = syscall.Dup2(savedFd, listenFDsStart)
			_ = syscall.Close(savedFd)
		} else {
			_ = syscall.Close(listenFDsStart)
		}
	})

	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDS", "1")

	listeners, err := systemdListeners()
	if err != nil {
		t.Fatalf("systemdListeners: %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("want 1 listener, got %d", len(listeners))
	}
	for _, l := range listeners {
		_ = l.Close()
	}

	if os.Getenv("LISTEN_PID") != "" || os.Getenv("LISTEN_FDS") != "" {
		t.Fatal("env vars should be unset after systemdListeners()")
	}
}
