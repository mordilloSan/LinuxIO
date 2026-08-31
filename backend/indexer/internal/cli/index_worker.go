package cli

import (
	"flag"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/indexer/daemon"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/version"
	"github.com/mordilloSan/LinuxIO/backend/indexer/logging"
)

func runIndexMode(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer worker", flag.ContinueOnError)
	indexFlags := registerIndexFlags(fs)
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		return writeError(fs.Output(), "index worker does not accept arguments\n")
	}

	logging.Configure("indexer-index", false)
	slog.Info("indexer starting", "version", version.String(), "mode", "index-worker")

	fileCfg, err := configfile.Load(*indexFlags.configPath)
	if err != nil {
		slog.Error("failed to load config", "config_file", *indexFlags.configPath, "err", err)
		return 1
	}
	fileCfg, err = configfile.ApplyEnvOverrides(fileCfg, os.LookupEnv)
	if err != nil {
		slog.Error("invalid environment config override", "err", err)
		return 1
	}
	fileCfg, err = applyIndexFlagOverrides(fs, fileCfg, indexFlags)
	if err != nil {
		slog.Error("invalid index worker config", "err", err)
		return 1
	}

	dbOptions, err := configfile.DBOpenOptions(fileCfg)
	if err != nil {
		slog.Error("invalid database options", "err", err)
		return 1
	}

	wire := daemon.NewWireProgress(os.Stdout)
	stats, err := daemon.RunIndexMode(
		fileCfg.IndexName,
		fileCfg.IndexPath,
		fileCfg.ExcludePaths,
		fileCfg.IncludeHidden,
		fileCfg.IncludeNetworkMounts,
		fileCfg.FreshIndex,
		fileCfg.DBPath,
		fileCfg.KeepIndexes,
		string(fileCfg.IntegrityCheck),
		dbOptions,
		wire,
	)
	if err != nil {
		slog.Error("index failed", "err", err)
		return 1
	}
	wire.Summary(stats)
	return 0
}
