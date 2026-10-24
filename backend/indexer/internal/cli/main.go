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
	"github.com/mordilloSan/LinuxIO/backend/common/verbosemode"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/daemon"
)

const usageText = `linuxio-indexer is managed by LinuxIO and systemd.

Configuration and operations are available through the LinuxIO interface.
`

// Run executes the linuxio-indexer CLI and returns the process exit code.
func Run(args []string) int {
	if len(args) > 1 {
		switch args[1] {
		case "--index-mode":
			return runIndexMode(args[2:])
		case "--trigger-index":
			return runIndexTrigger(args[2:])
		case "--version":
			return writeOutput(os.Stdout, "LinuxIO Indexer "+version.Version+"\n")
		case "--help", "-h":
			return writeOutput(os.Stdout, usageText)
		}
	}
	return runDaemon(args[1:])
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer", flag.ContinueOnError)
	configPath := fs.String("config-file", configfile.DefaultPath(), "YAML config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		return writeUsageError(fs.Output(), fmt.Sprintf("linuxio-indexer does not accept commands: %s\n", fs.Arg(0)))
	}

	if err := logging.Configure("linuxio-indexer", *verbose); err != nil {
		return writeError(os.Stderr, fmt.Sprintf("linuxio-indexer: initialize logging: %v\n", err))
	}
	// SIGHUP is handled before the unbounded store recovery in daemon.New so an
	// ExecReload during it does not take the default (exit) action. SIGTERM must
	// keep its default action there, so the stop context starts after daemon.New.
	hupCtx, stopHUP := context.WithCancel(context.Background())
	defer stopHUP()
	verbosemode.ReloadOnSIGHUP(hupCtx)
	slog.Info("indexer starting", "version", version.Version, "mode", "daemon")

	fileCfg, err := configfile.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "config_file", *configPath, "error", err)
		return 1
	}
	d, err := daemon.New(fileCfg, *configPath)
	if err != nil {
		slog.Error("failed to start daemon", "error", err)
		return 1
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Debug("daemon initialized",
		"config_file", *configPath,
		"include_network_mounts", fileCfg.IncludeNetworkMounts,
	)

	if err := d.Run(ctx); err != nil {
		slog.Error("daemon exited with error", "error", err)
		return 1
	}
	slog.Debug("shutdown complete")
	return 0
}

func flagParseExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func writeOutput(w io.Writer, value string) int {
	if _, err := io.WriteString(w, value); err != nil {
		slog.Error("write output", "error", err)
		return 1
	}
	return 0
}

func writeError(w io.Writer, value string) int {
	_ = writeOutput(w, value)
	return 1
}

// writeUsageError reports a command-line usage error with flag's exit code 2.
func writeUsageError(w io.Writer, value string) int {
	_ = writeOutput(w, value)
	return 2
}
