package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/verbosemode"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ServerConfig is the minimal runtime config passed to the server.
type ServerConfig struct {
	Port    int
	Verbose bool
}

// test seam (override in tests)
var runServerFunc = RunServer

// program is what the webserver answers to --help and --version.
var program = version.Program{
	Binary:    "linuxio-webserver",
	About:     "the LinuxIO web server",
	StartedBy: "systemd (linuxio-webserver.service)",
	Usage:     []string{"linuxio-webserver [--port N] [--verbose]"},
}

// Run starts the LinuxIO webserver and returns the process exit code. The
// program takes flags and no command word: systemd starts it, and linuxio is
// the admin tool.
func Run(args []string) int {
	if program.Answer(args[1:], os.Stdout) {
		return 0
	}

	flags := flag.NewFlagSet("linuxio-webserver", flag.ContinueOnError)

	var cfg ServerConfig
	flags.IntVar(&cfg.Port, "port", 8090, "HTTP server port (1-65535)")
	flags.BoolVar(&cfg.Verbose, "verbose", false, "enable verbose logging (default false)")
	flags.Usage = func() { program.WriteHelp(flags.Output()) }

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "linuxio-webserver: unknown argument: %q\n\n", flags.Arg(0))
		program.WriteHelp(os.Stderr)
		return 2
	}

	// Validate port (reject 0: server needs a fixed, known port for clients)
	if cfg.Port <= 0 || cfg.Port > 65535 {
		fmt.Fprintln(os.Stderr, "linuxio-webserver: invalid --port: must be between 1 and 65535 (port 0 not supported)")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	verbosemode.ReloadOnSIGHUP(ctx)
	if err := runServerFunc(ctx, cfg); err != nil {
		logging.ReportFatal(err)
		return 1
	}
	return 0
}
