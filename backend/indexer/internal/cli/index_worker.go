package cli

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/indexer/daemon"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

func runIndexMode(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer worker", flag.ContinueOnError)
	configPath := fs.String("config-file", configfile.DefaultPath(), "YAML config file path")
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		return writeError(fs.Output(), "index worker does not accept arguments\n")
	}

	if err := logging.Configure("linuxio-indexer", false); err != nil {
		return writeError(os.Stderr, "linuxio-indexer: initialize logging: "+err.Error()+"\n")
	}
	slog.Info("indexer starting", "version", version.Version, "mode", "index-worker")

	fileCfg, err := configfile.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "config_file", *configPath, "err", err)
		return 1
	}

	wire := daemon.NewWireProgress(os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	stats, err := daemon.RunIndexMode(
		ctx,
		fileCfg,
		configfile.DefaultDBPath,
		wire,
	)
	if err != nil {
		slog.Error("index failed", "err", err)
		return 1
	}
	wire.Summary(stats)
	return 0
}
