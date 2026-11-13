package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// fakeDockerUnits stands in for systemd and the Docker API. After a restart
// request docker.service reports its old state and invocation for stalePolls
// polls, then settles on restartState with a new invocation.
type fakeDockerUnits struct {
	state        string
	restartState string
	stalePolls   int
	stopFailed   int
	restarted    bool
	polls        int
	runs         int
	prevRuns     int
	calls        []string
	// onContainersStopped runs once the containers are stopped.
	onContainersStopped func()
	path                string
}

func (f *fakeDockerUnits) settled() bool { return f.restarted && f.polls > f.stalePolls }

func (f *fakeDockerUnits) activeState(ctx context.Context, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.restarted {
		f.polls++
	}
	if f.settled() {
		return f.restartState, nil
	}
	return f.state, nil
}

func (f *fakeDockerUnits) invocationID(ctx context.Context, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.settled() {
		return fmt.Sprint("run", f.runs), nil
	}
	return fmt.Sprint("run", f.prevRuns), nil
}

func (f *fakeDockerUnits) resetFailed(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(f.path); err == nil {
		f.calls = append(f.calls, "write daemon.json")
	}
	f.calls = append(f.calls, "reset-failed")
	if f.state == "failed" {
		f.state = "inactive"
	}
	return nil
}

func (f *fakeDockerUnits) restart(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.calls = append(f.calls, "restart "+name)
	f.restarted = true
	f.polls = 0
	f.prevRuns = f.runs
	f.runs++
	return nil
}

func (f *fakeDockerUnits) stopContainers(context.Context) (apischema.DockerStoppedFailedResponse, error) {
	f.calls = append(f.calls, "stop containers")
	if f.onContainersStopped != nil {
		f.onContainersStopped()
	}
	return apischema.DockerStoppedFailedResponse{Stopped: 2, Failed: f.stopFailed}, nil
}

func (f *fakeDockerUnits) daemonReload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.calls = append(f.calls, "write drop-in")
	return nil
}

func stubDockerUnits(t *testing.T, service, restartState string) *fakeDockerUnits {
	t.Helper()
	f := &fakeDockerUnits{state: service, restartState: restartState}
	origState, origInvocation, origReset, origRestart, origContainers, origReload, origPoll, origTimeout :=
		systemdActiveState, systemdInvocationID, systemdResetFailedUnit, systemdRestartUnit, stopAllRunningContainers, systemdDaemonReload, dockerUnitPollInterval, dockerUnitWaitTimeout
	systemdActiveState, systemdInvocationID, systemdResetFailedUnit, systemdRestartUnit = f.activeState, f.invocationID, f.resetFailed, f.restart
	stopAllRunningContainers, systemdDaemonReload, dockerUnitPollInterval = f.stopContainers, f.daemonReload, 0
	t.Cleanup(func() {
		systemdActiveState, systemdInvocationID, systemdResetFailedUnit, systemdRestartUnit, stopAllRunningContainers, systemdDaemonReload, dockerUnitPollInterval, dockerUnitWaitTimeout =
			origState, origInvocation, origReset, origRestart, origContainers, origReload, origPoll, origTimeout
	})
	return f
}

func storagePaths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "daemon.json"), filepath.Join(dir, "docker.service.d", "linuxio-data-root.conf")
}

func noReport(string, string) {}

func TestApplyDaemonStorageRestartsDockerOnce(t *testing.T) {
	units := stubDockerUnits(t, "active", "active")
	path, dropIn := storagePaths(t)
	units.path = path

	result, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{
		DataRoot: "/mnt/pool/docker/", ImageStore: "classic", StorageDriver: "overlay2", UserlandProxy: true,
	})
	require.NoError(t, err)
	require.Equal(t, "/mnt/pool/docker", result.Config.DataRoot)
	require.False(t, result.RestartRequired)
	require.Equal(t, []string{"stop containers", "write drop-in", "write daemon.json", "reset-failed", "restart docker.service"}, units.calls)
	require.Equal(t, map[string]any{
		"data-root": "/mnt/pool/docker", "features": map[string]any{"containerd-snapshotter": false}, "storage-driver": "overlay2",
	}, readJSON(t, path))
	content, err := os.ReadFile(dropIn)
	require.NoError(t, err)
	require.Contains(t, string(content), "RequiresMountsFor=/mnt/pool/docker\n")

	_, err = applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{UserlandProxy: true})
	require.NoError(t, err)
	require.NoFileExists(t, dropIn, "back on the default data root nothing needs mounting")
}

func TestApplyDaemonStorageLeavesConfigWhenContainersDoNotStop(t *testing.T) {
	units := stubDockerUnits(t, "active", "active")
	units.stopFailed = 1
	path, dropIn := storagePaths(t)

	_, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{DataRoot: "/mnt/pool/docker", UserlandProxy: true})
	require.ErrorContains(t, err, "1 containers did not stop")
	require.Equal(t, []string{"stop containers"}, units.calls)
	require.NoFileExists(t, path)
}

func TestApplyDaemonStorageWaitsForANewInvocation(t *testing.T) {
	units := stubDockerUnits(t, "active", "active")
	units.stalePolls = 2
	path, dropIn := storagePaths(t)

	_, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{StorageDriver: "overlay2", UserlandProxy: true})
	require.NoError(t, err)
	require.Equal(t, 3, units.polls, "active on the old invocation does not end the wait")
}

func TestApplyDaemonStorageWaitIsBounded(t *testing.T) {
	units := stubDockerUnits(t, "active", "active")
	units.stalePolls = 1 << 30
	dockerUnitWaitTimeout = 20 * time.Millisecond
	dockerUnitPollInterval = time.Millisecond
	path, dropIn := storagePaths(t)

	_, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{StorageDriver: "overlay2", UserlandProxy: true})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "wait for docker.service restart")
}

func TestApplyDaemonStorageReportsAFailedStart(t *testing.T) {
	units := stubDockerUnits(t, "failed", "failed")
	path, dropIn := storagePaths(t)

	_, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{StorageDriver: "overlay2", UserlandProxy: true})
	require.ErrorContains(t, err, "docker failed to start")
	require.NotContains(t, units.calls, "stop containers", "a daemon that is down has no containers to stop")
	require.Contains(t, units.calls, "restart docker.service", "the restart also starts a failed daemon")
	require.Equal(t, map[string]any{"storage-driver": "overlay2"}, readJSON(t, path))
}

func TestApplyDaemonStorageFinishesAfterCancel(t *testing.T) {
	units := stubDockerUnits(t, "active", "active")
	path, dropIn := storagePaths(t)
	units.path = path
	ctx, cancel := context.WithCancel(context.Background())
	units.onContainersStopped = cancel

	_, err := applyDaemonStorage(ctx, noReport, path, dropIn, apischema.DockerDaemonConfig{DataRoot: "/mnt/pool/docker", UserlandProxy: true})
	require.NoError(t, err)
	require.Equal(t, []string{"stop containers", "write drop-in", "write daemon.json", "reset-failed", "restart docker.service"}, units.calls)
	require.FileExists(t, dropIn)
}

func TestApplyDaemonStorageStartsAStoppedDocker(t *testing.T) {
	units := stubDockerUnits(t, "inactive", "active")
	path, dropIn := storagePaths(t)

	_, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{StorageDriver: "overlay2", UserlandProxy: true})
	require.NoError(t, err)
	require.NotContains(t, units.calls, "stop containers")
	require.Contains(t, units.calls, "restart docker.service")
}
