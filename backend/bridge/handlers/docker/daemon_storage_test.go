package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// fakeDockerUnits stands in for systemd and the Docker API; startState is what
// docker.service settles on after a start job.
type fakeDockerUnits struct {
	states     map[string]string
	startState string
	stopFailed int
	calls      []string
}

func stubDockerUnits(t *testing.T, service, startState string) *fakeDockerUnits {
	t.Helper()
	f := &fakeDockerUnits{states: map[string]string{"docker.service": service, "docker.socket": "active"}, startState: startState}
	origState, origStop, origStart, origReset, origContainers, origReload, origPoll :=
		systemdActiveState, systemdStopUnit, systemdStartUnit, systemdResetFailedUnit, stopAllRunningContainers, systemdDaemonReload, dockerUnitPollInterval
	systemdActiveState = func(_ context.Context, name string) (string, error) { return f.states[name], nil }
	systemdStopUnit = func(_ context.Context, name string) error {
		f.calls = append(f.calls, "stop "+name)
		f.states[name] = "inactive"
		return nil
	}
	systemdStartUnit = func(_ context.Context, name string) error {
		f.calls = append(f.calls, "start "+name)
		f.states[name] = "active"
		if name == "docker.service" {
			f.states[name] = f.startState
		}
		return nil
	}
	systemdResetFailedUnit = func(context.Context, string) error { return nil }
	stopAllRunningContainers = func(context.Context) (apischema.DockerStoppedFailedResponse, error) {
		f.calls = append(f.calls, "stop containers")
		return apischema.DockerStoppedFailedResponse{Stopped: 2, Failed: f.stopFailed}, nil
	}
	systemdDaemonReload = func(context.Context) error { return nil }
	dockerUnitPollInterval = 0
	t.Cleanup(func() {
		systemdActiveState, systemdStopUnit, systemdStartUnit, systemdResetFailedUnit, stopAllRunningContainers, systemdDaemonReload, dockerUnitPollInterval =
			origState, origStop, origStart, origReset, origContainers, origReload, origPoll
	})
	return f
}

func storagePaths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "daemon.json"), filepath.Join(dir, "docker.service.d", "linuxio-data-root.conf")
}

func noReport(string, string) {}

func TestApplyDaemonStorageStopsDockerAroundTheSwitch(t *testing.T) {
	units := stubDockerUnits(t, "active", "active")
	path, dropIn := storagePaths(t)

	result, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{
		DataRoot: "/mnt/pool/docker/", ImageStore: "classic", StorageDriver: "overlay2", UserlandProxy: true,
	})
	require.NoError(t, err)
	require.Equal(t, "/mnt/pool/docker", result.Config.DataRoot)
	require.False(t, result.RestartRequired)
	require.Equal(t, []string{"stop containers", "stop docker.socket", "stop docker.service", "start docker.socket", "start docker.service"}, units.calls)
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

func TestApplyDaemonStorageRecoversFailedDaemonAndReportsFailedStart(t *testing.T) {
	units := stubDockerUnits(t, "failed", "failed")
	path, dropIn := storagePaths(t)

	_, err := applyDaemonStorage(context.Background(), noReport, path, dropIn, apischema.DockerDaemonConfig{StorageDriver: "overlay2", UserlandProxy: true})
	require.ErrorContains(t, err, "docker failed to start")
	require.NotContains(t, units.calls, "stop containers", "a daemon that is down has no containers to stop")
	require.Equal(t, map[string]any{"storage-driver": "overlay2"}, readJSON(t, path))
}
