package daemon

import (
	"context"
	"net"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

type connectionKindContextKey struct{}

func unixConnContext(ctx context.Context, c net.Conn) context.Context {
	ctx = context.WithValue(ctx, connectionKindContextKey{}, true)
	return peercred.ConnContext(ctx, c)
}

func requestFromUnixSocket(r *http.Request) bool {
	unix, _ := r.Context().Value(connectionKindContextKey{}).(bool)
	return unix
}

func daemonConfigToFileConfig(cfg DaemonConfig) (configfile.Config, error) {
	return configfile.Normalize(configfile.Config{
		ExcludePaths:         append([]string(nil), cfg.ExcludePaths...),
		IncludeNetworkMounts: cfg.IncludeNetworkMounts,
	})
}

func (d *daemon) configSnapshot() DaemonConfig {
	d.cfgMu.RLock()
	defer d.cfgMu.RUnlock()
	return d.cfg
}

func (d *daemon) savedConfigSnapshot() configfile.Config {
	d.cfgMu.RLock()
	saved := d.savedConfig
	active := d.cfg
	d.cfgMu.RUnlock()
	if saved.ExcludePaths != nil || saved.IncludeNetworkMounts {
		return saved
	}
	cfg, err := daemonConfigToFileConfig(active)
	if err != nil {
		return configfile.Defaults()
	}
	return cfg
}

func (d *daemon) applySavedConfig(saved configfile.Config) error {
	next, err := DaemonConfigFromConfig(saved, d.configSnapshot().ConfigPath)
	if err != nil {
		return err
	}
	d.cfgMu.Lock()
	d.savedConfig = saved
	d.cfg.ExcludePaths = append([]string(nil), next.ExcludePaths...)
	d.cfg.IncludeNetworkMounts = next.IncludeNetworkMounts
	d.cfgMu.Unlock()
	return nil
}
