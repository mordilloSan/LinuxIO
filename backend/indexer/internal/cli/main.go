package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/daemon"
)

const usageText = `linuxio-indexer is managed by LinuxIO and systemd.

Configuration and operations are available through the LinuxIO interface.
`

func Main(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "--index-mode":
			return runIndexMode(args[1:])
		case "--trigger-index":
			return runIndexTrigger(args[1:])
		case "--version":
			return writeOutput(os.Stdout, "LinuxIO Indexer "+version.Version+"\n")
		case "--help", "-h":
			return writeOutput(os.Stdout, usageText)
		}
	}
	return runDaemon(args)
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer", flag.ContinueOnError)
	configPath := fs.String("config-file", configfile.DefaultPath(), "YAML config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		return writeError(fs.Output(), fmt.Sprintf("linuxio-indexer does not accept commands: %s\n", fs.Arg(0)))
	}

	if err := logging.Configure("linuxio-indexer", *verbose); err != nil {
		return writeError(os.Stderr, fmt.Sprintf("linuxio-indexer: initialize logging: %v\n", err))
	}
	slog.Info("indexer starting", "version", version.Version, "mode", "daemon")

	fileCfg, err := configfile.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "config_file", *configPath, "err", err)
		return 1
	}
	d, err := daemon.New(fileCfg, *configPath)
	if err != nil {
		slog.Error("failed to start daemon", "err", err)
		return 1
	}
	defer d.Close()

	slog.Info("daemon initialized",
		"config_file", *configPath,
		"include_network_mounts", fileCfg.IncludeNetworkMounts,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(ctx)
	}()

	select {
	case sig := <-sigCh:
		slog.Info("received signal, initiating graceful shutdown", "signal", sig)
		cancel()
		if err := <-errCh; err != nil {
			slog.Warn("daemon shutdown returned error", "err", err)
		}
	case err := <-errCh:
		if err != nil {
			slog.Error("daemon exited with error", "err", err)
			return 1
		}
	}

	slog.Info("shutdown complete")
	return 0
}

func flagParseExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 1
}

func writeOutput(w io.Writer, value string) int {
	if _, err := io.WriteString(w, value); err != nil {
		slog.Error("write output", "err", err)
		return 1
	}
	return 0
}

func writeError(w io.Writer, value string) int {
	_ = writeOutput(w, value)
	return 1
}
