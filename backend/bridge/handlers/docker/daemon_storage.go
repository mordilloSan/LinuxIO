package docker

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const dockerDataRootDropInPath = "/etc/systemd/system/docker.service.d/linuxio-data-root.conf"

var (
	systemdActiveState       = systemd.GetActiveState
	systemdStopUnit          = systemd.StopUnit
	systemdStartUnit         = systemd.StartUnit
	systemdResetFailedUnit   = systemd.ResetFailedUnit
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

// applyDaemonStorage saves the whole form with Docker fully stopped. Running
// containers are stopped first because live restore would otherwise keep them
// running, unseen by a daemon that now reads another data root or driver.
func applyDaemonStorage(ctx context.Context, report func(phase, message string), path, dropInPath string, req apischema.DockerDaemonConfig) (apischema.DockerDaemonConfigSetResult, error) {
	if err := validateDaemonConfig(req); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	before, err := readDaemonConfigFile(path)
	if err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	after := applyDaemonConfig(cloneMap(before), req)

	socketActive, err := stopDocker(ctx, report)
	if err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	// Docker is down: a cancelled caller must not leave it that way.
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
	if err = startDocker(ctx, report, socketActive); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}

	cfg := daemonConfigFromMap(after)
	slog.Info("changed docker storage", "component", "docker", "data_root", cfg.DataRoot, "storage_driver", cfg.StorageDriver)
	return apischema.DockerDaemonConfigSetResult{Config: cfg}, nil
}

// stopDocker stops every running container, then docker.socket and
// docker.service, and reports whether the socket was active. Nothing is
// stopped when a container refuses to, so the caller can leave the config.
func stopDocker(ctx context.Context, report func(phase, message string)) (bool, error) {
	serviceState, err := systemdActiveState(ctx, "docker.service")
	if err != nil {
		return false, err
	}
	socketState, err := systemdActiveState(ctx, "docker.socket")
	if err != nil {
		return false, err
	}
	// A daemon that is down (possibly failed on a bad config) has nothing to
	// stop, which keeps this route usable for reverting a broken switch.
	if serviceState == "active" {
		report("stop", "Stopping running containers")
		var stopped apischema.DockerStoppedFailedResponse
		if stopped, err = stopAllRunningContainers(ctx); err != nil {
			return false, fmt.Errorf("stop running containers: %w", err)
		}
		if stopped.Failed > 0 {
			return false, fmt.Errorf("%d containers did not stop, storage left unchanged", stopped.Failed)
		}
	}

	report("stop", "Stopping Docker")
	socketActive := socketState == "active"
	// The socket goes first so a client cannot re-activate the old daemon.
	if socketActive {
		if err = systemdStopUnit(ctx, "docker.socket"); err != nil {
			return false, err
		}
	}
	if err = systemdStopUnit(ctx, "docker.service"); err != nil {
		return false, err
	}
	_, err = waitForDockerUnit(ctx, "inactive", "failed")
	return socketActive, err
}

func startDocker(ctx context.Context, report func(phase, message string), socketActive bool) error {
	report("start", "Starting Docker")
	// Clears a failed state so the wait below only sees this start's outcome.
	if err := systemdResetFailedUnit(ctx, "docker.service"); err != nil {
		slog.Debug("docker.service had no failed state to reset", "component", "docker", "error", err)
	}
	if socketActive {
		if err := systemdStartUnit(ctx, "docker.socket"); err != nil {
			return err
		}
	}
	if err := systemdStartUnit(ctx, "docker.service"); err != nil {
		return err
	}
	state, err := waitForDockerUnit(ctx, "active", "failed")
	if err != nil {
		return err
	}
	if state != "active" {
		return fmt.Errorf("docker failed to start with the new storage settings, revert them or check journalctl -u docker")
	}
	return nil
}

// waitForDockerUnit polls docker.service until it reaches one of states;
// systemd runs start and stop jobs asynchronously.
func waitForDockerUnit(ctx context.Context, states ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dockerUnitWaitTimeout)
	defer cancel()
	for {
		state, err := systemdActiveState(ctx, "docker.service")
		if err != nil {
			return "", err
		}
		if slices.Contains(states, state) {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for docker.service, still %s: %w", state, ctx.Err())
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
