package cmd

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ServerConfig is the minimal runtime config passed to the server.
type ServerConfig struct {
	Port    int
	Verbose bool
}

// test seam (override in tests)
var runServerFunc = RunServer

// Run executes the LinuxIO webserver CLI and returns the process exit code.
func Run(args []string) int {
	if len(args) < 2 {
		printGeneralUsage()
		return 0
	}

	switch args[1] {
	case "-h", "--help", "help":
		printGeneralUsage()
		return 0
	case "version", "-v", "--version":
		fmt.Printf("LinuxIO Web Server %s\n", version.Version)
		return 0
	case "run":
		runCmd := flag.NewFlagSet("run", flag.ContinueOnError)

		var cfg ServerConfig
		runCmd.IntVar(&cfg.Port, "port", 8090, "HTTP server port (1-65535)")
		runCmd.BoolVar(&cfg.Verbose, "verbose", false, "enable verbose logging (default false)")

		runCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "LinuxIO Web Server\n")
			fmt.Fprintln(os.Stderr, "\nUsage:")
			fmt.Fprintln(os.Stderr, "  linuxio run [flags]")
			fmt.Fprintln(os.Stderr, "\nFlags:")
			runCmd.PrintDefaults()
		}

		if err := runCmd.Parse(args[2:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}

		// Validate port (reject 0: server needs a fixed, known port for clients)
		if cfg.Port <= 0 || cfg.Port > 65535 {
			fmt.Fprintln(os.Stderr, "invalid -port: must be between 1 and 65535 (port 0 not supported)")
			return 2
		}

		if err := runServerFunc(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		return 0

	default:
		// Unknown subcommand → help
		fmt.Fprintf(os.Stderr, "LinuxIO Web Server\n")
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", args[1])
		printUsage()
		return 0
	}
}

func printGeneralUsage() {
	fmt.Fprintf(os.Stderr, "LinuxIO Web Server\n")
	printUsage()
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  linuxio <command> [flags]

Commands:
  run         Run the HTTP server
  version     Show version information
  help        Show this help

Examples:
  linuxio run
  linuxio run -port 8090 -verbose

Use "linuxio-webserver <command> -h" for more info about a command.
`)
}
