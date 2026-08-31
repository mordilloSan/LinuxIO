package daemon

import (
	"context"
	"net"
	"net/http"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

type connectionKind string

const (
	connectionKindUnix connectionKind = "unix"
	connectionKindTCP  connectionKind = "tcp"
)

type connectionKindContextKey struct{}
type peerCredContextKey struct{}

type peerCred struct {
	uid uint32
	gid uint32
}

func unixConnContext(ctx context.Context, c net.Conn) context.Context {
	ctx = withConnectionKind(ctx, connectionKindUnix)
	if uc, ok := c.(*net.UnixConn); ok {
		if cred, err := readUnixPeerCred(uc); err == nil {
			ctx = withPeerCred(ctx, cred)
		}
	}
	return ctx
}

func tcpConnContext(ctx context.Context, _ net.Conn) context.Context {
	return withConnectionKind(ctx, connectionKindTCP)
}

func withConnectionKind(ctx context.Context, kind connectionKind) context.Context {
	return context.WithValue(ctx, connectionKindContextKey{}, kind)
}

func withPeerCred(ctx context.Context, cred peerCred) context.Context {
	return context.WithValue(ctx, peerCredContextKey{}, cred)
}

func requestFromUnixSocket(r *http.Request) bool {
	kind, _ := r.Context().Value(connectionKindContextKey{}).(connectionKind)
	return kind == connectionKindUnix
}

func peerUIDFromRequest(r *http.Request) (uint32, bool) {
	cred, ok := r.Context().Value(peerCredContextKey{}).(peerCred)
	if !ok {
		return 0, false
	}
	return cred.uid, true
}

func daemonConfigToFileConfig(cfg DaemonConfig) (configfile.Config, error) {
	if cfg.DBOptions == (storage.OpenOptions{}) {
		cfg.DBOptions = storage.DefaultOpenOptions()
	}
	dbOptions, err := storage.NormalizeOpenOptions(cfg.DBOptions)
	if err != nil {
		return configfile.Config{}, err
	}
	return configfile.Normalize(configfile.Config{
		IndexPath:            cfg.IndexPath,
		IndexName:            cfg.IndexName,
		ExcludePaths:         append([]string(nil), cfg.ExcludePaths...),
		IncludeHidden:        cfg.IncludeHidden,
		IncludeNetworkMounts: cfg.IncludeNetworkMounts,
		FreshIndex:           cfg.FreshIndex,
		FTSSearch:            !dbOptions.DisableFTS,
		KeepIndexes:          cfg.KeepIndexes,
		IntegrityCheck:       api.IndexerIntegrityCheck(cfg.IntegrityCheck),
		DBPath:               cfg.DBPath,
		DBBusyTimeout:        dbOptions.BusyTimeout.String(),
		DBJournalMode:        dbOptions.JournalMode,
		DBSynchronous:        dbOptions.Synchronous,
		DBAutoVacuum:         dbOptions.AutoVacuum,
		DBMaxOpenConns:       dbOptions.MaxOpenConns,
		DBMaxIdleConns:       dbOptions.MaxIdleConns,
		DBConnMaxIdleTime:    dbOptions.ConnMaxIdleTime.String(),
		DBStmtCacheSize:      dbOptions.StmtCacheSize,
		SearchDefaultLimit:   cfg.SearchDefaultLimit,
		SearchMaxLimit:       cfg.SearchMaxLimit,
		EntriesDefaultLimit:  cfg.EntriesDefaultLimit,
		EntriesMaxLimit:      cfg.EntriesMaxLimit,
		IdleTimeout:          cfg.IdleTimeout.String(),
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
	if saved.IndexPath != "" {
		return saved
	}
	cfg, err := daemonConfigToFileConfig(active)
	if err != nil {
		return configfile.Defaults()
	}
	return cfg
}

func (d *daemon) applySavedConfig(saved configfile.Config) (bool, error) {
	next, err := DaemonConfigFromConfig(saved, d.configSnapshot().ConfigPath)
	if err != nil {
		return false, err
	}

	d.cfgMu.Lock()
	old := d.cfg
	restartRequired := false

	d.savedConfig = saved
	d.cfg.IndexName = next.IndexName
	d.cfg.IndexPath = next.IndexPath
	d.cfg.ExcludePaths = append([]string(nil), next.ExcludePaths...)
	d.cfg.IncludeHidden = next.IncludeHidden
	d.cfg.IncludeNetworkMounts = next.IncludeNetworkMounts
	d.cfg.FreshIndex = next.FreshIndex
	d.cfg.KeepIndexes = next.KeepIndexes
	d.cfg.IntegrityCheck = next.IntegrityCheck
	d.cfg.SearchDefaultLimit = next.SearchDefaultLimit
	d.cfg.SearchMaxLimit = next.SearchMaxLimit
	d.cfg.EntriesDefaultLimit = next.EntriesDefaultLimit
	d.cfg.EntriesMaxLimit = next.EntriesMaxLimit
	d.cfg.IdleTimeout = next.IdleTimeout

	if old.DBOptions == next.DBOptions {
		d.cfg.DBOptions = next.DBOptions
	} else {
		// DisableFTS is not a connection-pool property: it takes effect when
		// the next scan subprocess opens the database. Apply it at runtime so
		// a UI toggle of fts_search works without a daemon restart; only the
		// real connection options force one.
		oldConn, nextConn := old.DBOptions, next.DBOptions
		oldConn.DisableFTS, nextConn.DisableFTS = false, false
		if oldConn == nextConn {
			d.cfg.DBOptions = next.DBOptions
		} else {
			restartRequired = true
		}
	}
	if old.DBPath == next.DBPath {
		d.cfg.DBPath = next.DBPath
	} else {
		restartRequired = true
	}
	d.cfg.ConfigPath = old.ConfigPath
	d.cfgMu.Unlock()

	return restartRequired, nil
}

func appendDBOptionArgs(args []string, opts storage.OpenOptions) []string {
	opts, err := storage.NormalizeOpenOptions(opts)
	if err != nil {
		opts = storage.DefaultOpenOptions()
	}
	return append(args,
		"--db-busy-timeout", opts.BusyTimeout.String(),
		"--db-journal-mode", opts.JournalMode,
		"--db-synchronous", opts.Synchronous,
		"--db-auto-vacuum", opts.AutoVacuum,
		"--db-max-open-conns", strconv.Itoa(opts.MaxOpenConns),
		"--db-max-idle-conns", strconv.Itoa(opts.MaxIdleConns),
		"--db-conn-max-idle-time", opts.ConnMaxIdleTime.String(),
		"--db-stmt-cache-size", strconv.Itoa(opts.StmtCacheSize),
	)
}
