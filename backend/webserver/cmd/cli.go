package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

// ServerConfig is the minimal runtime config passed to the server.
type ServerConfig struct {
	Port    int
	Verbose bool
}

// test seam (override in tests)
var runServerFunc = RunServer

// StartLinuxIO is the CLI entrypoint (called from main.go).
func StartLinuxIO() {
	if len(os.Args) < 2 {
		printGeneralUsage()
		return
	}

	switch os.Args[1] {
	case "-h", "--help", "help":
		printGeneralUsage()
		return
	case "run":
		runCmd := flag.NewFlagSet("run", flag.ExitOnError)

		var cfg ServerConfig
		runCmd.IntVar(&cfg.Port, "port", 8090, "HTTP server port (1-65535)")
		runCmd.BoolVar(&cfg.Verbose, "verbose", false, "enable verbose logging (default false)")

		runCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "LinuxIO Web Server %s\n", config.Version)
			fmt.Fprintln(os.Stderr, "\nUsage:")
			fmt.Fprintln(os.Stderr, "  linuxio run [flags]")
			fmt.Fprintln(os.Stderr, "\nFlags:")
			runCmd.PrintDefaults()
		}

		if err := runCmd.Parse(os.Args[2:]); err != nil {
			// flag.ExitOnError handles most errors; ErrHelp means -h was used
			return
		}

		// Validate port (reject 0: server needs a fixed, known port for clients)
		if cfg.Port <= 0 || cfg.Port > 65535 {
			fmt.Fprintln(os.Stderr, "invalid -port: must be between 1 and 65535 (port 0 not supported)")
			os.Exit(2)
		}

		runServerFunc(cfg)

	default:
		// Unknown subcommand → help
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", os.Args[1])
		printGeneralUsage()
		return
	}
}

func printGeneralUsage() {
	fmt.Fprintf(os.Stderr, `LinuxIO Web Server %s

Usage:
  linuxio <command> [flags]

Commands:
  run         Run the HTTP server
  help        Show this help

Examples:
  linuxio run
  linuxio run -port 8090 -verbose

Use "linuxio <command> -h" for more info about a command.
`, config.Version)
}
