package socketactivation

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
)

const listenFDsStart = 3

// Listeners returns listeners inherited through systemd socket activation.
func Listeners() ([]net.Listener, error) {
	defer func() {
		_ = os.Unsetenv("LISTEN_PID")
		_ = os.Unsetenv("LISTEN_FDS")
		_ = os.Unsetenv("LISTEN_FDNAMES")
	}()

	pidString := os.Getenv("LISTEN_PID")
	if pidString == "" {
		return nil, nil
	}
	pid, err := strconv.Atoi(pidString)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_PID %q: %w", pidString, err)
	}
	if pid != os.Getpid() {
		return nil, nil
	}

	fdsString := os.Getenv("LISTEN_FDS")
	if fdsString == "" {
		return nil, nil
	}
	nfds, err := strconv.Atoi(fdsString)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_FDS %q: %w", fdsString, err)
	}
	if nfds <= 0 {
		return nil, nil
	}

	files := make([]*os.File, 0, nfds)
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for i := range nfds {
		fd := listenFDsStart + i
		syscall.CloseOnExec(fd)
		file := os.NewFile(uintptr(fd), fmt.Sprintf("LISTEN_FD_%d", fd))
		if file == nil {
			return nil, fmt.Errorf("invalid fd %d from systemd", fd)
		}
		files = append(files, file)
	}
	return listenersFromFiles(files)
}

func listenersFromFiles(files []*os.File) ([]net.Listener, error) {
	listeners := make([]net.Listener, 0, len(files))
	for _, file := range files {
		listener, err := net.FileListener(file)
		if err != nil {
			CloseListeners(listeners)
			return nil, fmt.Errorf("wrap fd %d: %w", file.Fd(), err)
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

func CloseListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
