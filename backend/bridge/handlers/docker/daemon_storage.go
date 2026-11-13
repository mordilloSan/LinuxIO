package docker

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const dockerDataRootDropInPath = "/etc/systemd/system/docker.service.d/linuxio-data-root.conf"

var (
	systemdActiveState       = systemd.GetActiveState
	systemdInvocationID      = systemd.GetInvocationID
	systemdResetFailedUnit   = systemd.ResetFailedUnit
	systemdRestartUnit       = systemd.RestartUnit
	stopAllRunningContainers = StopAllRunning
	dockerUnitPollInterval   = time.Second
	dockerUnitWaitTimeout    = 3 * time.Minute
)

func runApplyDaemonStorageTask(ctx context.Context, task *bridgeipc.Task, req apischema.DockerDaemonConfig) (apischema.DockerDaemonConfigSetResult, error) {
	report := func(phase, message string) {
		task.ReportProgress(apischema.DockerDaemonStorageProgress{Phase: phase, Message: message})
	}
	return applyDaemonStorage(ctx, report, dockerDaemonConfigPath, dockerDataRootDropInPath, req)
}

// applyDaemonStorage stops the running containers, writes the files while
// Docker still runs and hands one restart to systemd. dockerd reads the files
// only at start, and systemd finishes the restart even if this job dies, so
// Docker cannot be left down. Containers are stopped first because live restore
// would otherwise keep them running, unseen by a daemon that now reads another
// data root or driver.
func applyDaemonStorage(ctx context.Context, report func(phase, message string), path, dropInPath string, req apischema.DockerDaemonConfig) (apischema.DockerDaemonConfigSetResult, error) {
	if err := validateDaemonConfig(req); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	before, err := readDaemonConfigFile(path)
	if err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	after := applyDaemonConfig(cloneMap(before), req)

	if err = stopRunningContainers(ctx, report); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	// The containers are down: a cancel must not stop the switch half-written.
	ctx = context.WithoutCancel(ctx)

	// The drop-in goes first so dockerd never starts on the bare mountpoint
	// of a pool that is not mounted yet.
	if dataRoot := stringValue(after["data-root"]); dataRoot != stringValue(before["data-root"]) {
		if err = syncDataRootMountOrdering(ctx, dropInPath, dataRoot); err != nil {
			return apischema.DockerDaemonConfigSetResult{}, err
		}
	}
	if err = writeDaemonConfigFile(path, after); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	if err = restartDocker(ctx, report); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}

	cfg := daemonConfigFromMap(after)
	slog.Info("changed docker storage", "component", "docker", "data_root", cfg.DataRoot, "storage_driver", cfg.StorageDriver)
	return apischema.DockerDaemonConfigSetResult{Config: cfg}, nil
}

// stopRunningContainers stops every running container and changes nothing
// when one refuses to, so the caller can leave the config.
func stopRunningContainers(ctx context.Context, report func(phase, message string)) error {
	state, err := systemdActiveState(ctx, "docker.service")
	if err != nil {
		return err
	}
	// A daemon that is down (possibly failed on a bad config) has no containers
	// to stop, which keeps this route usable for reverting a broken switch.
	if state != "active" {
		return nil
	}
	report("stop", "Stopping running containers")
	stopped, err := stopAllRunningContainers(ctx)
	if err != nil {
		return fmt.Errorf("stop running containers: %w", err)
	}
	if stopped.Failed > 0 {
		return fmt.Errorf("%d containers did not stop, storage left unchanged", stopped.Failed)
	}
	return nil
}

// restartDocker queues one systemd restart, which also starts a Docker that is
// stopped or failed, and waits for the new run. docker.socket stays up, so a
// client that connects meanwhile waits for the new daemon.
func restartDocker(ctx context.Context, report func(phase, message string)) error {
	report("restart", "Restarting Docker")
	oldInvocation, err := systemdInvocationID(ctx, "docker.service")
	if err != nil {
		return err
	}
	// Clears a failed state so the wait below only sees this restart's outcome.
	if err = systemdResetFailedUnit(ctx, "docker.service"); err != nil {
		slog.Debug("docker.service had no failed state to reset", "component", "docker", "error", err)
	}
	if err = systemdRestartUnit(ctx, "docker.service"); err != nil {
		return err
	}
	state, err := waitForDockerRestart(ctx, oldInvocation)
	if err != nil {
		return err
	}
	if state != "active" {
		return fmt.Errorf("docker failed to start with the new storage settings, revert them or check journalctl -u docker")
	}
	return nil
}

// waitForDockerRestart polls docker.service until it has failed, or is active
// on a different invocation than oldInvocation: ActiveState alone still reads
// active before the queued restart job has run. The wait is bounded by
// dockerUnitWaitTimeout.
func waitForDockerRestart(ctx context.Context, oldInvocation string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dockerUnitWaitTimeout)
	defer cancel()
	for {
		state, err := systemdActiveState(ctx, "docker.service")
		if err != nil {
			return "", fmt.Errorf("wait for docker.service restart: %w", err)
		}
		switch state {
		case "failed":
			return state, nil
		case "active":
			invocation, err := systemdInvocationID(ctx, "docker.service")
			if err != nil {
				return "", fmt.Errorf("wait for docker.service restart: %w", err)
			}
			if invocation != oldInvocation {
				return state, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for docker.service restart, still %s: %w", state, ctx.Err())
		case <-time.After(dockerUnitPollInterval):
		}
	}
}

// syncDataRootMountOrdering makes docker.service wait for the mount holding
// dataRoot, or removes that requirement when dataRoot is back to the default.
func syncDataRootMountOrdering(ctx context.Context, path, dataRoot string) error {
	if dataRoot == "" {
		return removeDockerMountOrderingDropIn(ctx, path)
	}
	content := "# Managed by LinuxIO. Do not edit by hand.\n[Unit]\nRequiresMountsFor=" + systemdUnitValue(dataRoot) + "\n"
	if err := ensureSystemdDropInDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := atomicfile.Write(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write docker service drop-in: %w", err)
	}
	return systemdDaemonReload(ctx)
}
