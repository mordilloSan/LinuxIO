package daemon

import (
	"context"
	"net"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

type connectionKindContextKey struct{}

type peerCredContextKey struct{}

type peerCred struct {
	uid uint32
	gid uint32
}

func unixConnContext(ctx context.Context, c net.Conn) context.Context {
	ctx = context.WithValue(ctx, connectionKindContextKey{}, true)
	if uc, ok := c.(*net.UnixConn); ok {
		if cred, err := readUnixPeerCred(uc); err == nil {
			ctx = withPeerCred(ctx, cred)
		}
	}
	return ctx
}

func withPeerCred(ctx context.Context, cred peerCred) context.Context {
	return context.WithValue(ctx, peerCredContextKey{}, cred)
}

func requestFromUnixSocket(r *http.Request) bool {
	unix, _ := r.Context().Value(connectionKindContextKey{}).(bool)
	return unix
}

func peerUIDFromRequest(r *http.Request) (uint32, bool) {
	cred, ok := r.Context().Value(peerCredContextKey{}).(peerCred)
	if !ok {
		return 0, false
	}
	return cred.uid, true
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
