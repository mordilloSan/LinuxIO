// Package daemon runs linuxio-monitoring: config, listeners, and the app.
package daemon

import (
	"context"
	"fmt"
	"log/slog"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/app"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
)

const DataDir = "/var/lib/linuxio/monitoring"

// Listeners returns the two fixed LinuxIO sockets followed by the configured
// read-only listeners. Commands are never served off the control socket.
func Listeners(cfg config.Config) []app.ListenerOptions {
	out := []app.ListenerOptions{
		{Name: "api", Address: "unix:" + monitoringapi.APISocketPath, APIs: []string{"metrics"}, Mode: 0o666},
		{Name: "control", Address: "unix:" + monitoringapi.ControlSocketPath, APIs: []string{"metrics", "commands"}, Mode: 0o600, RootOnly: true},
	}
	for _, listener := range cfg.Listeners {
		out = append(out, app.ListenerOptions{
			Name:    listener.Name,
			Address: app.GetAddress(listener.Address),
			APIs:    []string{"metrics"},
			// An empty plugin list stays nil, which the server reads as
			// "all metrics plugins".
			Plugins: append([]string(nil), listener.Plugins...),
			// A configured address that cannot bind must not take the fixed
			// sockets down with it: they are the only way to fix the address.
			BestEffort: true,
		})
	}
	return out
}

// historyString renders the configured history plugins for the store parser.
// An empty list means "record nothing"; the parser reads an empty string as
// "unset" and would fall back to the default plugin set.
func historyString(cfg config.Config) string {
	if len(cfg.History.Plugins) == 0 {
		return "none"
	}
	return cfg.HistoryString()
}

func runOptions(cfg config.Config, source string) app.ReloadOptions {
	return app.ReloadOptions{
		CollectorInterval:    cfg.Collector.Interval.Duration(),
		SmartRefreshInterval: cfg.Collector.SmartRefreshInterval.Duration(),
		DiskUsageCache:       cfg.Collector.DiskUsageCache.Duration(),
		HistoryRetention:     cfg.History.Retention.Duration(),
		History:              historyString(cfg),
		HistorySet:           true,
		HistoryIntervals:     cfg.HistoryIntervalDurations(),
		ConfigSource:         source,
		ConfigVersion:        cfg.Version,
	}
}

// Run loads the config, writes it when absent, and blocks until ctx ends.
func Run(ctx context.Context, configPath string) error {
	cfg, loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	source := "loaded"
	if !loaded {
		source = "defaults"
		if created, saveErr := config.SaveIfMissing(configPath, cfg); saveErr != nil {
			slog.Warn("could not write default config", "path", configPath, "err", saveErr)
		} else if created {
			source = "created"
		}
	}

	a, err := app.New(ctx, DataDir)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	reload := runOptions(cfg, source)
	executor := NewCommandExecutor(a, configPath)
	return a.StartContext(ctx, app.RunOptions{
		Listeners:            Listeners(cfg),
		CollectorInterval:    reload.CollectorInterval,
		SmartRefreshInterval: reload.SmartRefreshInterval,
		DiskUsageCache:       reload.DiskUsageCache,
		HistoryRetention:     reload.HistoryRetention,
		History:              reload.History,
		HistorySet:           true,
		HistoryIntervals:     reload.HistoryIntervals,
		ConfigPath:           configPath,
		ConfigSource:         source,
		ConfigVersion:        cfg.Version,
		CommandExecutor:      executor,
		ReloadConfig:         executor.reloadFromFile,
	})
}
