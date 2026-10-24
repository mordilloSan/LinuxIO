package docker

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func stubReload(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := systemdReloadUnit
	systemdReloadUnit = func(_ context.Context, name string) error {
		require.Equal(t, "docker.service", name)
		calls++
		return nil
	}
	t.Cleanup(func() { systemdReloadUnit = orig })
	return &calls
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func TestSetDaemonConfigCreatesFileAndReloadsForLiveRestoreOnly(t *testing.T) {
	calls := stubReload(t)
	path := filepath.Join(t.TempDir(), "docker", "daemon.json")

	result, err := setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{LiveRestore: true, UserlandProxy: true})
	require.NoError(t, err)
	require.False(t, result.RestartRequired)
	require.Equal(t, 1, *calls)
	require.Equal(t, map[string]any{"live-restore": true}, readJSON(t, path))
}

func TestSetDaemonConfigPreservesUnknownKeysAndFlagsRestart(t *testing.T) {
	calls := stubReload(t)
	path := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"dns": ["192.168.1.66"], "log-opts": {"labels": "env"}, "default-address-pools": [{"base":"10.0.0.0/8","size":24},{"base":"fd00::/8","size":64}]}`), 0o644))

	result, err := setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{
		LiveRestore: true, LogMaxSize: "10m", LogMaxFile: "3",
		AddressPoolBase: "172.20.0.0/16", AddressPoolSize: 24, UserlandProxy: false,
	})
	require.NoError(t, err)
	require.True(t, result.RestartRequired)
	require.Equal(t, 0, *calls, "restart-only changes must not trigger a reload")
	require.Equal(t, apischema.DockerDaemonConfig{
		LiveRestore: true, LogMaxSize: "10m", LogMaxFile: "3",
		AddressPoolBase: "172.20.0.0/16", AddressPoolSize: 24, UserlandProxy: false,
	}, result.Config)

	got := readJSON(t, path)
	require.Equal(t, []any{"192.168.1.66"}, got["dns"])
	require.Equal(t, map[string]any{"labels": "env", "max-size": "10m", "max-file": "3"}, got["log-opts"])
	require.Equal(t, []any{
		map[string]any{"base": "172.20.0.0/16", "size": float64(24)},
		map[string]any{"base": "fd00::/8", "size": float64(64)},
	}, got["default-address-pools"])
	require.Equal(t, false, got["userland-proxy"])
}

func TestSetDaemonConfigDropsDefaultsAndSkipsWriteWhenUnchanged(t *testing.T) {
	calls := stubReload(t)
	path := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"live-restore": true, "userland-proxy": false, "log-opts": {"max-size": "10m"}}`), 0o644))

	result, err := setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{UserlandProxy: true})
	require.NoError(t, err)
	require.True(t, result.RestartRequired)
	require.Equal(t, map[string]any{}, readJSON(t, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	result, err = setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{UserlandProxy: true})
	require.NoError(t, err)
	require.False(t, result.RestartRequired)
	require.Equal(t, 0, *calls)
	again, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, info.ModTime(), again.ModTime(), "unchanged config must not rewrite the file")
}

func TestSetDaemonConfigRejectsStorageChanges(t *testing.T) {
	stubReload(t)
	path := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"data-root": "/mnt/pool/docker"}`), 0o644))

	_, err := setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{UserlandProxy: true})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
	require.Equal(t, map[string]any{"data-root": "/mnt/pool/docker"}, readJSON(t, path))

	_, err = setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{DataRoot: "/mnt/pool/docker", LiveRestore: true, UserlandProxy: true})
	require.NoError(t, err, "other keys still save while the storage keys stay put")

	_, err = setDaemonConfig(context.Background(), path, apischema.DockerDaemonConfig{DataRoot: "/mnt/pool/docker", ImageStore: "classic", UserlandProxy: true})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, "the image store is a storage setting too")
}

func TestValidateDaemonConfigStorage(t *testing.T) {
	require.NoError(t, validateDaemonConfig(apischema.DockerDaemonConfig{DataRoot: "/mnt/pool/docker", StorageDriver: "overlay2"}))
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{DataRoot: "pool/docker"}), "absolute path")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{DataRoot: "//"}), "absolute path")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{StorageDriver: "Overlay 2"}), "not a valid driver")

	dir := t.TempDir()
	magic, err := filesystemMagic(filepath.Join(dir, "not", "created", "yet"))
	require.NoError(t, err)
	if magic == zfsSuperMagic {
		t.Skip("temp dir is on zfs")
	}
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{DataRoot: dir, StorageDriver: "zfs"}), "on a zfs filesystem")
}

func TestImageStoreRoundTripsThroughFeatures(t *testing.T) {
	raw := applyDaemonConfig(map[string]any{"features": map[string]any{"buildkit": true}}, apischema.DockerDaemonConfig{ImageStore: "classic", UserlandProxy: true})
	require.Equal(t, map[string]any{"buildkit": true, "containerd-snapshotter": false}, raw["features"])
	require.Equal(t, "classic", daemonConfigFromMap(raw).ImageStore)

	raw = applyDaemonConfig(raw, apischema.DockerDaemonConfig{ImageStore: "containerd", UserlandProxy: true})
	require.Equal(t, "containerd", daemonConfigFromMap(raw).ImageStore)

	raw = applyDaemonConfig(map[string]any{"features": map[string]any{"containerd-snapshotter": true}}, apischema.DockerDaemonConfig{UserlandProxy: true})
	require.NotContains(t, raw, "features", "Docker's own choice leaves no empty features object")
}

func TestValidateDaemonConfigImageStore(t *testing.T) {
	require.NoError(t, validateDaemonConfig(apischema.DockerDaemonConfig{ImageStore: "classic", StorageDriver: "overlay2"}))
	require.NoError(t, validateDaemonConfig(apischema.DockerDaemonConfig{ImageStore: "containerd", StorageDriver: "overlayfs"}))
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{ImageStore: "legacy"}), "classic or containerd")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{ImageStore: "classic", StorageDriver: "overlayfs"}), "does not work with the classic image store")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{ImageStore: "containerd", StorageDriver: "vfs"}), "does not work with the containerd image store")
}

func TestValidateDaemonConfig(t *testing.T) {
	require.NoError(t, validateDaemonConfig(apischema.DockerDaemonConfig{LogMaxSize: "10m", LogMaxFile: "3", AddressPoolBase: "172.20.0.0/16", AddressPoolSize: 24}))
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{LogMaxSize: "10mb"}), "log max size")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{LogMaxFile: "0"}), "log max files")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{AddressPoolBase: "172.20.0.0"}), "CIDR")
	require.ErrorContains(t, validateDaemonConfig(apischema.DockerDaemonConfig{AddressPoolBase: "172.20.0.0/16", AddressPoolSize: 8}), "between 16 and 32")
}

func TestDaemonConfigFromMapReadsStringAndNumericLogOpts(t *testing.T) {
	cfg := daemonConfigFromMap(map[string]any{"log-opts": map[string]any{"max-size": "5m", "max-file": float64(2)}})
	require.Equal(t, "5m", cfg.LogMaxSize)
	require.Equal(t, "2", cfg.LogMaxFile)
	require.True(t, cfg.UserlandProxy, "absent userland-proxy means dockerd's default of true")
}
