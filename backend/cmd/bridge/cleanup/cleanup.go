package cleanup

import (
	"fmt"

	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/internal/bridge"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/utils"
)

func KillLingeringBridgeStartupProcesses() {
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		logger.Errorf("❌ Failed to read /proc: %v", err)
		return
	}

	for _, entry := range procEntries {
		if !entry.IsDir() || !utils.IsNumeric(entry.Name()) {
			continue
		}

		pid := entry.Name()
		cmdlineBytes, err := os.ReadFile(fmt.Sprintf("/proc/%s/cmdline", pid))
		if err != nil || len(cmdlineBytes) == 0 {
			continue
		}

		cmdline := strings.ReplaceAll(string(cmdlineBytes), "\x00", " ")

		if strings.Contains(cmdline, "linuxio-bridge") &&
			strings.Contains(cmdline, "sudo -S env") &&
			strings.Contains(cmdline, "LINUXIO_SESSION_USER="+os.Getenv("LINUXIO_SESSION_USER")) {
			pidInt, _ := strconv.Atoi(pid)
			logger.Debugf("⚠️ Found lingering bridge process (pid=%d): %s", pidInt, cmdline)
			killParentTree(pidInt)
		}
	}
}

func killParentTree(pid int) {
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			logger.Debugf("killParentTree: could not read stat for pid %d: %v", pid, err)
			break
		}
		fields := strings.Fields(string(stat))
		if len(fields) < 4 {
			logger.Debugf("killParentTree: stat fields < 4 for pid %d", pid)
			break
		}

		ppid, _ := strconv.Atoi(fields[3])
		if ppid <= 1 || ppid == pid {
			logger.Debugf("killParentTree: hit root or self for pid %d (ppid %d)", pid, ppid)
			break
		}

		commBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", ppid))
		if err != nil {
			logger.Debugf("killParentTree: could not read comm for ppid %d: %v", ppid, err)
			break
		}

		comm := strings.TrimSpace(string(commBytes))
		logger.Debugf("killParentTree: pid=%d, ppid=%d, comm='%s'", pid, ppid, comm)
		if comm == "sudo" || comm == "env" {
			logger.Debugf("🛑 Killing sudo/env process (pid=%d, ppid=%d, comm=%s)", pid, ppid, comm)
			_ = syscall.Kill(ppid, syscall.SIGTERM)
			_ = syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(250 * time.Millisecond) // Give time for defer/logs to flush
			_ = syscall.Kill(ppid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGKILL)
			break
		}
		pid = ppid
	}
}

func killBridgeSocket(Sess *session.Session) error {
	if err := CleanupBridgeSocket(Sess); err != nil {
		logger.Warnf("Failed to remove bridge socket: %v", err)
		return err
	}
	logger.Infof("Bridge socket file removed")
	return nil
}

func FullCleanup(shutdownReason string, Sess *session.Session, socketPath string) error {
	logger.Infof("🔻 Shutdown initiated: %s", shutdownReason)
	var errs []error

	if err := killBridgeSocket(Sess); err != nil {
		logger.Warnf("killBridgeSocket failed: %v", err)
		errs = append(errs, fmt.Errorf("killBridgeSocket: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup encountered errors: %v", errs)
	}
	return nil
}

// CleanupBridgeSocket removes the bridge socket for the session.
func CleanupBridgeSocket(sess *session.Session) error {
	bridgeSock, err := bridge.BridgeSocketPath(sess)
	if err != nil {
		logger.Warnf("Could not determine bridge socket path: %v", err)
		return err
	}
	if err := os.Remove(bridgeSock); err == nil {
		logger.Infof("Removed bridge socket file %s for session %s", bridgeSock, sess.SessionID)
	} else if !os.IsNotExist(err) {
		logger.Warnf("Failed to remove bridge socket file %s: %v", bridgeSock, err)
		return err
	}
	return nil
}
