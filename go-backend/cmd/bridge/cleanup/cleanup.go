package cleanup

import (
	"context"
	"fmt"
	"go-backend/internal/bridge"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"go-backend/internal/terminal"
	"go-backend/internal/utils"

	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
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

	if shutdownReason != "logout" {
		if err := killFilebrowserContainer(); err != nil {
			logger.Warnf("killFilebrowserContainer failed: %v", err)
			errs = append(errs, fmt.Errorf("killFilebrowserContainer: %w", err))
		}
	}

	if err := terminal.Close(Sess.SessionID); err != nil {
		if strings.Contains(err.Error(), "no terminal found") {
			logger.Infof("No terminal found for session %s during cleanup", Sess.SessionID)
		} else {
			logger.Warnf("Terminal cleanup failed for session %s: %v", Sess.SessionID, err)
		}
		errs = append(errs, fmt.Errorf("terminal.Close: %w", err))
	} else {
		logger.Infof("Terminal cleanup complete for session %s", Sess.SessionID)
	}

	if err := killBridgeSocket(Sess); err != nil {
		logger.Warnf("killBridgeSocket failed: %v", err)
		errs = append(errs, fmt.Errorf("killBridgeSocket: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup encountered errors: %v", errs)
	}
	return nil
}

func killFilebrowserContainer() error {
	err := cleanupFilebrowserContainer()
	if err != nil {
		logger.Infof("CleanupFilebrowserContainer failed: %v", err)
		return err
	}
	logger.Infof("CleanupFilebrowserContainer finished OK")
	return nil
}

func cleanupFilebrowserContainer() error {
	containerName := "/filebrowser-linuxio"
	timeout := 0 // seconds

	var errors []error

	logger.Infof("Stopping FileBrowser container: %s", containerName)
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Warnf("Failed to create Docker client: %v", err)
		return err
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStop(context.Background(), containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s was not running.", containerName)
		} else {
			logger.Warnf("Failed to stop container %s: %v", containerName, err)
			errors = append(errors, fmt.Errorf("stop: %w", err))
		}
	} else {
		logger.Infof("Stopped FileBrowser container: %s", containerName)
	}

	logger.Infof("Removing FileBrowser container: %s", containerName)
	if err := cli.ContainerRemove(context.Background(), containerName, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s already removed.", containerName)
		} else {
			logger.Warnf("Failed to remove container %s: %v", containerName, err)
			errors = append(errors, fmt.Errorf("remove: %w", err))
		}
	} else {
		logger.Infof("Removed FileBrowser container: %s", containerName)
	}

	if len(errors) > 0 {
		return fmt.Errorf("CleanupFilebrowserContainer encountered errors: %v", errors)
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
