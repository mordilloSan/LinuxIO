package cleanup

import (
	"fmt"

	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

// killOwnSudoParents walks our parent chain and kills lingering sudo/env parents.
// Touches only our own ancestors; safe for prod.
func KillOwnSudoParents() {
	ppid := os.Getppid()
	for ppid > 1 {
		commB, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", ppid))
		if err != nil {
			return // parent already gone
		}
		comm := strings.TrimSpace(string(commB))
		if comm != "sudo" && comm != "env" {
			return // reached a non-wrapper parent; stop
		}

		// Read the *next* parent before killing this one
		nextPPID, err := readPPID(ppid)
		if err != nil {
			return
		}

		logger.Debugf("🧹 killing lingering parent pid=%d comm=%q", ppid, comm)
		_ = syscall.Kill(ppid, syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(ppid, syscall.SIGKILL)

		ppid = nextPPID
	}
}

func readPPID(pid int) (int, error) {
	// /proc/<pid>/stat: pid (comm) state ppid ...
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 4 {
		return 0, fmt.Errorf("invalid /proc/%d/stat", pid)
	}
	return strconv.Atoi(fields[3])
}

// FullCleanup does all bridge-side cleanup for a session.
// Right now that’s just removing the socket; extend here if you add more artifacts.
func FullCleanup(shutdownReason string, sess *session.Session) error {
	logger.Debugf("Shutdown initiated: %s", shutdownReason)

	if err := cleanupBridgeSocket(sess); err != nil {
		return fmt.Errorf("cleanup bridge socket: %w", err)
	}
	return nil
}

// CleanupBridgeSocket removes the bridge socket for the session (idempotent).
func cleanupBridgeSocket(sess *session.Session) error {
	sock, err := ipc.SocketPathFor(sess)
	if err != nil {
		logger.Warnf("Could not determine bridge socket path: %v", err)
		return err
	}

	err = os.Remove(sock)
	if err == nil {
		logger.Infof("Removed bridge socket %s for session %s", sock, sess.SessionID)
		return nil
	} else if os.IsNotExist(err) {
		// Nothing to remove; not an error.
		logger.Debugf("Bridge socket %s not found (already removed) for session %s", sock, sess.SessionID)
		return nil
	}

	logger.Warnf("Failed to remove bridge socket %s: %v", sock, err)
	return err
}
