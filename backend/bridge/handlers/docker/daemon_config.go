package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const dockerDaemonConfigPath = "/etc/docker/daemon.json"

var (
	systemdReloadUnit = systemd.ReloadUnit
	logMaxSizePattern = regexp.MustCompile(`^[1-9][0-9]*[kmgKMG]?$`)
)

// dockerd only re-reads these keys on SIGHUP; every other key edited here
// (log-opts, default-address-pools, userland-proxy) needs a daemon restart.
var daemonReloadableKeys = map[string]bool{"live-restore": true}

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
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	before, err := readDaemonConfigFile(path)
	if err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	after := applyDaemonConfig(cloneMap(before), req)

	reload, restart := daemonConfigChanges(before, after)
	if reload || restart {
		data, err := json.MarshalIndent(after, "", "  ")
		if err != nil {
			return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("encode docker daemon config: %w", err)
		}
		if err := atomicfile.Write(path, append(data, '\n'), 0o644); err != nil {
			return apischema.DockerDaemonConfigSetResult{}, fmt.Errorf("write docker daemon config: %w", err)
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
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return raw, nil
}

func daemonConfigFromMap(raw map[string]any) apischema.DockerDaemonConfig {
	cfg := apischema.DockerDaemonConfig{UserlandProxy: true}
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
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	data, _ := json.Marshal(m)
	_ = json.Unmarshal(data, &out)
	return out
}
