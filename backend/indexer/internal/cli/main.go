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

// program is what the indexer answers to --help and --version.
var program = version.Program{
	Binary:    "linuxio-indexer",
	About:     "the LinuxIO file index daemon",
	StartedBy: "systemd (linuxio-indexer.service, and linuxio-indexer-index.service for the timer)",
	Usage: []string{
		"linuxio-indexer [--config PATH] [--verbose]   the daemon",
		"linuxio-indexer --index-mode [--config PATH]  the scanner, which the index timer starts",
	},
}

// Run executes the linuxio-indexer CLI and returns the process exit code.
func Run(args []string) int {
	if program.Answer(args[1:], os.Stdout) {
		return 0
	}
	if len(args) > 1 {
		if args[1] == "--index-mode" {
			return runIndexMode(args[2:])
		}
	}
	return runDaemon(args[1:])
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("linuxio-indexer", flag.ContinueOnError)
	configPath := fs.String("config", configfile.DefaultPath(), "YAML config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	fs.Usage = func() { program.WriteHelp(fs.Output()) }
	if err := fs.Parse(args); err != nil {
		return flagParseExitCode(err)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(fs.Output(), "linuxio-indexer: unknown argument: %q\n\n", fs.Arg(0))
		program.WriteHelp(fs.Output())
		return 2
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

func writeError(w io.Writer, value string) int {
	writeOutput(w, value)
	return 1
}

// writeUsageError reports a command-line usage error with flag's exit code 2.
func writeUsageError(w io.Writer, value string) int {
	writeOutput(w, value)
	return 2
}

// writeOutput writes value to w; a failed write is logged, since the exit
// status already says what happened.
func writeOutput(w io.Writer, value string) {
	if _, err := io.WriteString(w, value); err != nil {
		slog.Error("write output", "error", err)
	}
}
