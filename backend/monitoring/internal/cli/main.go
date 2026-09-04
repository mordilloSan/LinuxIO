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

const usageText = `linuxio-monitoring is managed by LinuxIO and systemd.

Usage:
  linuxio-monitoring run [--config PATH] [--verbose]
  linuxio-monitoring --version
  linuxio-monitoring --help

Status, configuration, history and database maintenance are available through
the LinuxIO interface.
`

func Main(args []string) int {
	if len(args) == 0 {
		return writeOutput(os.Stdout, usageText)
	}
	switch args[0] {
	case "--version", "-v", "version":
		return writeOutput(os.Stdout, "LinuxIO Monitoring "+version.Version+"\n")
	case "--help", "-h", "help":
		return writeOutput(os.Stdout, usageText)
	case "run":
		return runDaemon(args[1:])
	}
	return writeError(os.Stderr, fmt.Sprintf("linuxio-monitoring: unknown command %q\n\n%s", args[0], usageText))
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("linuxio-monitoring run", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath, "YAML config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return writeError(fs.Output(), fmt.Sprintf("linuxio-monitoring run does not accept arguments: %s\n", fs.Arg(0)))
	}
	if err := logging.Configure("linuxio-monitoring", *verbose); err != nil {
		return writeError(os.Stderr, fmt.Sprintf("linuxio-monitoring: initialize logging: %v\n", err))
	}
	debugserver.Start("127.0.0.1:6062")
	slog.Info("monitoring starting", "version", version.Version, "config", *configPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, *configPath); err != nil {
		slog.Error("daemon exited with error", "err", err)
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
