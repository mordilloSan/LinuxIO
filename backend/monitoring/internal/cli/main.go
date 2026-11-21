// Package cli parses the linuxio-monitoring command line.
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

	"github.com/mordilloSan/LinuxIO/backend/common/debugserver"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/daemon"
)

// program is what the daemon answers to --help and --version.
var program = version.Program{
	Binary:    "linuxio-monitoring",
	About:     "the LinuxIO monitoring daemon",
	StartedBy: "systemd (linuxio-monitoring.service)",
	Usage:     []string{"linuxio-monitoring [--config PATH] [--verbose]"},
}

// Run starts the monitoring daemon and returns the process exit code. The
// program takes flags and no command word: systemd starts it.
func Run(args []string) int {
	if program.Answer(args[1:], os.Stdout) {
		return 0
	}
	return runDaemon(args[1:])
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("linuxio-monitoring", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath, "YAML config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	fs.Usage = func() { program.WriteHelp(fs.Output()) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(fs.Output(), "linuxio-monitoring: unknown argument: %q\n\n", fs.Arg(0))
		program.WriteHelp(fs.Output())
		return 2
	}
	if err := logging.Configure("linuxio-monitoring", *verbose); err != nil {
		return writeError(os.Stderr, fmt.Sprintf("linuxio-monitoring: initialize logging: %v\n", err))
	}
	debugserver.Start("127.0.0.1:6062")
	slog.Info("monitoring starting", "version", version.Version, "config", *configPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, *configPath); err != nil {
		slog.Error("daemon exited with error", "error", err)
		return 1
	}
	slog.Info("shutdown complete")
	return 0
}

func writeOutput(w io.Writer, value string) int {
	if _, err := io.WriteString(w, value); err != nil {
		return 1
	}
	return 0
}

func writeError(w io.Writer, value string) int {
	_ = writeOutput(w, value)
	return 1
}
