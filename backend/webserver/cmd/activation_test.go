package cmd

import (
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
)

func TestServeWithSocketActivationErrorHandled(t *testing.T) {
	t.Setenv("LISTEN_PID", "invalid")

	handled, err := serveWithSocketActivation(ServerConfig{}, nil, nil, nil)
	if !handled || err == nil {
		t.Fatalf("got handled=%v err=%v, want handled true and error", handled, err)
	}
}

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

	tcpFileListener, ok := tcpListener.(*net.TCPListener)
	if !ok {
		t.Fatalf("want *net.TCPListener, got %T", tcpListener)
	}

	file, err := tcpFileListener.File()
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	defer file.Close()

	savedFd, savedErr := syscall.Dup(listenFDsStart)
	err = syscall.Dup2(int(file.Fd()), listenFDsStart)
	if err != nil {
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

func TestListenersFromFilesClosesPartialResultsOnError(t *testing.T) {
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := tcpListener.Addr().String()

	tcpFileListener, ok := tcpListener.(*net.TCPListener)
	if !ok {
		_ = tcpListener.Close()
		t.Fatalf("want *net.TCPListener, got %T", tcpListener)
	}
	tcpFile, err := tcpFileListener.File()
	if err != nil {
		_ = tcpListener.Close()
		t.Fatalf("file: %v", err)
	}
	if closeErr := tcpListener.Close(); closeErr != nil {
		_ = tcpFile.Close()
		t.Fatalf("close source listener: %v", closeErr)
	}

	invalidFile, err := os.Open(os.DevNull)
	if err != nil {
		_ = tcpFile.Close()
		t.Fatalf("open %s: %v", os.DevNull, err)
	}

	listeners, err := listenersFromFiles([]*os.File{tcpFile, invalidFile})
	if err == nil {
		closeListeners(listeners)
		_ = tcpFile.Close()
		_ = invalidFile.Close()
		t.Fatal("expected second file to fail listener conversion")
	}
	if listeners != nil {
		t.Fatalf("want nil listeners on partial failure, got %d", len(listeners))
	}
	if closeErr := tcpFile.Close(); closeErr != nil {
		_ = invalidFile.Close()
		t.Fatalf("close TCP file: %v", closeErr)
	}
	if closeErr := invalidFile.Close(); closeErr != nil {
		t.Fatalf("close invalid file: %v", closeErr)
	}

	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("partial listener was not closed: %v", err)
	}
	_ = rebound.Close()
}
