package socketactivation

import (
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
)

func TestListenersNoEnv(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")

	listeners, err := Listeners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listeners != nil {
		t.Fatalf("want nil, got %d listeners", len(listeners))
	}
}

func TestListenersWrongPID(t *testing.T) {
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()+1))
	t.Setenv("LISTEN_FDS", "1")

	listeners, err := Listeners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listeners != nil {
		t.Fatalf("want nil (wrong PID), got %d listeners", len(listeners))
	}
}

func TestListenersHappyPath(t *testing.T) {
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

	savedFD, savedErr := syscall.Dup(listenFDsStart)
	dupErr := syscall.Dup2(int(file.Fd()), listenFDsStart)
	if dupErr != nil {
		t.Fatalf("dup2: %v", dupErr)
	}
	t.Cleanup(func() {
		if savedErr == nil {
			_ = syscall.Dup2(savedFD, listenFDsStart)
			_ = syscall.Close(savedFD)
			return
		}
		_ = syscall.Close(listenFDsStart)
	})

	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDS", "1")
	listeners, err := Listeners()
	if err != nil {
		t.Fatalf("Listeners: %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("want 1 listener, got %d", len(listeners))
	}
	CloseListeners(listeners)
	if os.Getenv("LISTEN_PID") != "" || os.Getenv("LISTEN_FDS") != "" {
		t.Fatal("environment should be unset after Listeners")
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
		t.Fatalf("want *net.TCPListener, got %T", tcpListener)
	}
	tcpFile, err := tcpFileListener.File()
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	closeErr := tcpListener.Close()
	if closeErr != nil {
		t.Fatalf("close source listener: %v", closeErr)
	}
	invalidFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer invalidFile.Close()
	defer tcpFile.Close()

	listeners, err := listenersFromFiles([]*os.File{tcpFile, invalidFile})
	if err == nil {
		CloseListeners(listeners)
		t.Fatal("expected second file to fail listener conversion")
	}
	if listeners != nil {
		t.Fatalf("want nil listeners on partial failure, got %d", len(listeners))
	}
	closeErr = tcpFile.Close()
	if closeErr != nil {
		t.Fatalf("close source file: %v", closeErr)
	}
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("partial listener was not closed: %v", err)
	}
	_ = rebound.Close()
}
