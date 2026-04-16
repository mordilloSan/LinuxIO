package cmd

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
)

const listenFDsStart = 3

func systemdListeners() ([]net.Listener, error) {
	defer func() {
		_ = os.Unsetenv("LISTEN_PID")
		_ = os.Unsetenv("LISTEN_FDS")
		_ = os.Unsetenv("LISTEN_FDNAMES")
	}()

	pidStr := os.Getenv("LISTEN_PID")
	if pidStr == "" {
		return nil, nil
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_PID %q: %w", pidStr, err)
	}
	if pid != os.Getpid() {
		return nil, nil
	}

	fdsStr := os.Getenv("LISTEN_FDS")
	if fdsStr == "" {
		return nil, nil
	}
	nfds, err := strconv.Atoi(fdsStr)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_FDS %q: %w", fdsStr, err)
	}
	if nfds <= 0 {
		return nil, nil
	}

	listeners := make([]net.Listener, 0, nfds)
	for i := 0; i < nfds; i++ {
		fd := listenFDsStart + i
		syscall.CloseOnExec(fd)
		file := os.NewFile(uintptr(fd), fmt.Sprintf("LISTEN_FD_%d", fd))
		if file == nil {
			return nil, fmt.Errorf("invalid fd %d from systemd", fd)
		}
		listener, err := net.FileListener(file)
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("wrap fd %d: %w", fd, err)
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}
