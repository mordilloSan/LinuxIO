package docker

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const (
	dockerDaemonConfigPath = "/etc/docker/daemon.json"
	defaultDockerDataRoot  = "/var/lib/docker"
	zfsSuperMagic          = 0x2fc12fc1
)

var (
	systemdReloadUnit    = systemd.ReloadUnit
	systemdDaemonReload  = systemd.DaemonReload
	logMaxSizePattern    = regexp.MustCompile(`^[1-9][0-9]*[kmgKMG]?$`)
	storageDriverPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// dockerd only re-reads these keys on SIGHUP; every other key edited here
// (log-opts, default-address-pools, userland-proxy) needs a daemon restart.
var daemonReloadableKeys = map[string]bool{"live-restore": true}

// daemonStorageKeys only change through applyDaemonStorage, which stops Docker
// around the switch.
var daemonStorageKeys = []string{"data-root", "storage-driver"}

func (h dockerHandlers) handleGetDaemonConfig(_ context.Context, _ apischema.NoRequest) (apischema.DockerDaemonConfig, error) {
	raw, err := readDaemonConfigFile(dockerDaemonConfigPath)
	if err != nil {
		return apischema.DockerDaemonConfig{}, err
	}
	return daemonConfigFromMap(raw), nil
}

func (h dockerHandlers) handleSetDaemonConfig(ctx context.Context, req apischema.DockerDaemonConfig) (apischema.DockerDaemonConfigSetResult, error) {
	return setDaemonConfig(ctx, dockerDaemonConfigPath, req)
}

func setDaemonConfig(ctx context.Context, path string, req apischema.DockerDaemonConfig) (apischema.DockerDaemonConfigSetResult, error) {
	if err := validateDaemonConfig(req); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	before, err := readDaemonConfigFile(path)
	if err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	after := applyDaemonConfig(cloneMap(before), req)
	for _, key := range daemonStorageKeys {
		if !jsonEqual(before[key], after[key]) {
			return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("%w: data root and storage driver changes must go through docker.apply_daemon_storage", bridgeipc.ErrInvalidArgs)
		}
	}

	reload, restart := daemonConfigChanges(before, after)
	if reload || restart {
		if err := writeDaemonConfigFile(path, after); err != nil {
			return apischema.DockerDaemonConfigSetResult{}, err
		}
	}
	// A reload alongside a restart-only change is pointless: the restart the
	// user triggers next picks up everything.
	if reload && !restart {
		if err := systemdReloadUnit(ctx, "docker.service"); err != nil {
			return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("reload docker daemon: %w", err)
		}
	}
	return apischema.DockerDaemonConfigSetResult{Config: daemonConfigFromMap(after), RestartRequired: restart}, nil
}

func writeDaemonConfigFile(path string, raw map[string]any) error {
	data, err := json.Marshal(raw, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("encode docker daemon config: %w", err)
	}
	if err := atomicfile.Write(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write docker daemon config: %w", err)
	}
	return nil
}

func readDaemonConfigFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read docker daemon config: %w", err)
	}
	raw := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return raw, nil
	}
	if err := json.Unmarshal(data, &raw, json.MatchCaseInsensitiveNames(true)); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return raw, nil
}

func daemonConfigFromMap(raw map[string]any) apischema.DockerDaemonConfig {
	cfg := apischema.DockerDaemonConfig{
		DataRoot:      stringValue(raw["data-root"]),
		StorageDriver: stringValue(raw["storage-driver"]),
		UserlandProxy: true,
	}
	if v, ok := raw["live-restore"].(bool); ok {
		cfg.LiveRestore = v
	}
	if v, ok := raw["userland-proxy"].(bool); ok {
		cfg.UserlandProxy = v
	}
	if opts, ok := raw["log-opts"].(map[string]any); ok {
		cfg.LogMaxSize = stringValue(opts["max-size"])
		cfg.LogMaxFile = stringValue(opts["max-file"])
	}
	if pools, ok := raw["default-address-pools"].([]any); ok && len(pools) > 0 {
		if pool, ok := pools[0].(map[string]any); ok {
			cfg.AddressPoolBase = stringValue(pool["base"])
			if size, ok := pool["size"].(float64); ok {
				cfg.AddressPoolSize = int(size)
			}
		}
	}
	return cfg
}

// applyDaemonConfig writes the form fields into raw, dropping keys that are
// back at dockerd's default so the file stays as short as it was.
func applyDaemonConfig(raw map[string]any, cfg apischema.DockerDaemonConfig) map[string]any {
	setOrDelete(raw, "data-root", filepath.Clean(cfg.DataRoot), cfg.DataRoot == "")
	setOrDelete(raw, "storage-driver", cfg.StorageDriver, cfg.StorageDriver == "")
	setOrDelete(raw, "live-restore", cfg.LiveRestore, !cfg.LiveRestore)
	setOrDelete(raw, "userland-proxy", cfg.UserlandProxy, cfg.UserlandProxy)

	opts, _ := raw["log-opts"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
	}
	setOrDelete(opts, "max-size", cfg.LogMaxSize, cfg.LogMaxSize == "")
	setOrDelete(opts, "max-file", cfg.LogMaxFile, cfg.LogMaxFile == "")
	setOrDelete(raw, "log-opts", opts, len(opts) == 0)

	if cfg.AddressPoolBase == "" {
		delete(raw, "default-address-pools")
	} else {
		pools, _ := raw["default-address-pools"].([]any)
		pool := map[string]any{"base": cfg.AddressPoolBase, "size": float64(cfg.AddressPoolSize)}
		if len(pools) == 0 {
			pools = []any{pool}
		} else {
			pools[0] = pool
		}
		raw["default-address-pools"] = pools
	}
	return raw
}

// daemonConfigChanges reports whether any reloadable key changed and whether
// any key that needs a daemon restart changed.
func daemonConfigChanges(before, after map[string]any) (reload, restart bool) {
	for key := range daemonReloadableKeys {
		if !jsonEqual(before[key], after[key]) {
			reload = true
		}
	}
	for _, key := range []string{"log-opts", "default-address-pools", "userland-proxy"} {
		if !jsonEqual(before[key], after[key]) {
			restart = true
		}
	}
	return reload, restart
}

func validateDaemonConfig(cfg apischema.DockerDaemonConfig) error {
	if err := validateDaemonStorage(cfg.DataRoot, cfg.StorageDriver); err != nil {
		return err
	}
	if cfg.LogMaxSize != "" && !logMaxSizePattern.MatchString(cfg.LogMaxSize) {
		return fmt.Errorf("log max size must be a number with an optional k, m or g suffix, like 10m")
	}
	if cfg.LogMaxFile != "" {
		if n, err := strconv.Atoi(cfg.LogMaxFile); err != nil || n < 1 {
			return fmt.Errorf("log max files must be a positive whole number")
		}
	}
	if cfg.AddressPoolBase != "" {
		_, network, err := net.ParseCIDR(cfg.AddressPoolBase)
		if err != nil {
			return fmt.Errorf("address pool base must be a CIDR like 172.20.0.0/16")
		}
		prefix, bits := network.Mask.Size()
		if cfg.AddressPoolSize < prefix || cfg.AddressPoolSize > bits {
			return fmt.Errorf("address pool size must be between %d and %d for %s", prefix, bits, network)
		}
	}
	return nil
}

func validateDaemonStorage(dataRoot, driver string) error {
	if dataRoot != "" {
		root := filepath.Clean(dataRoot)
		if !filepath.IsAbs(root) || root == "/" || strings.ContainsAny(root, "\r\n") {
			return fmt.Errorf("data root must be an absolute path other than /")
		}
	}
	if driver == "" {
		return nil
	}
	if !storageDriverPattern.MatchString(driver) {
		return fmt.Errorf("storage driver %q is not a valid driver name", driver)
	}
	return checkStorageDriverFilesystem(driver, dataRoot)
}

// checkStorageDriverFilesystem rejects the btrfs and zfs drivers on any other
// filesystem, where dockerd would refuse to start.
func checkStorageDriverFilesystem(driver, dataRoot string) error {
	var want uint32
	switch driver {
	case "btrfs":
		want = unix.BTRFS_SUPER_MAGIC
	case "zfs":
		want = zfsSuperMagic
	default:
		return nil
	}
	if dataRoot == "" {
		dataRoot = defaultDockerDataRoot
	}
	got, err := filesystemMagic(filepath.Clean(dataRoot))
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the %s storage driver needs the data root on a %s filesystem", driver, driver)
	}
	return nil
}

// filesystemMagic reads the filesystem type of path, or of its nearest
// existing parent since dockerd creates a missing data root itself.
func filesystemMagic(path string) (uint32, error) {
	for {
		var st unix.Statfs_t
		err := unix.Statfs(path, &st)
		if err == nil {
			return uint32(st.Type), nil
		}
		parent := filepath.Dir(path)
		if !errors.Is(err, fs.ErrNotExist) || parent == path {
			return 0, fmt.Errorf("check filesystem of %s: %w", path, err)
		}
		path = parent
	}
}

func setOrDelete(m map[string]any, key string, value any, remove bool) {
	if remove {
		delete(m, key)
		return
	}
	m[key] = value
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func jsonEqual(a, b any) bool {
	left, _ := json.Marshal(a, json.Deterministic(true))
	right, _ := json.Marshal(b, json.Deterministic(true))
	return bytes.Equal(left, right)
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	data, _ := json.Marshal(m)
	_ = json.Unmarshal(data, &out)
	return out
}
