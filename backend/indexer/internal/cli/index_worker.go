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
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/daemon"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

func runIndexMode(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer worker", flag.ContinueOnError)
	configPath := fs.String("config", configfile.DefaultPath(), "YAML config file path")
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		return writeUsageError(fs.Output(), "index worker does not accept arguments\n")
	}

	if err := logging.Configure("linuxio-indexer", false); err != nil {
		return writeError(os.Stderr, "linuxio-indexer: initialize logging: "+err.Error()+"\n")
	}
	slog.Debug("indexer starting", "version", version.Version, "mode", "index-worker")

	fileCfg, err := configfile.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "config_file", *configPath, "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	// The scan takes its turn with the daemon, which systemd starts for the
	// request: by then the daemon has checked the database, and set a broken
	// one aside.
	if _, err := daemon.RunScan(ctx, fileCfg, storage.DefaultDBPath, daemon.NewSocketClient()); err != nil {
		slog.Error("index failed", "error", err)
		return 1
	}
	return 0
}
