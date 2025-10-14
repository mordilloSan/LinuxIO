package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ServerConfig is the minimal runtime config passed to the server.
type ServerConfig struct {
	Port             int
	BridgeBinaryPath string
	Env              string // "development" | "production"
	Verbose          bool
	ViteDevPort      int // used only for dev CORS allowance
}

// test seams (override in tests)
var (
	runServerFunc = RunServer    // used by StartLinuxIO
	execCommand   = exec.Command // used by daemonReexec
)

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

	case "version", "--version", "-version":
		fmt.Printf("linuxio %s\n", version.Version)
		return

	case "run":
		runCmd := flag.NewFlagSet("run", flag.ExitOnError)

		var cfg ServerConfig
		var detach bool

		runCmd.IntVar(&cfg.Port, "port", 8090, "HTTP server port")
		runCmd.StringVar(&cfg.BridgeBinaryPath, "bridge-binary", "", "path to linuxio-bridge (optional)")
		runCmd.StringVar(&cfg.Env, "env", "production", "environment: development|production")
		runCmd.BoolVar(&cfg.Verbose, "verbose", false, "verbose logging")
		runCmd.IntVar(&cfg.ViteDevPort, "vite-port", 3000, "vite dev server port (only used for dev CORS)")
		runCmd.BoolVar(&detach, "detach", false, "run in background (daemonize)")

		// Local usage for `run`
		runCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, `Run the LinuxIO server

Usage:
  linuxio run [flags]

Flags:
  -port <int>               HTTP server port (default: 8090)
  -bridge-binary <path>     Path to linuxio-bridge binary (optional)
  -env <development|production>  Environment (default: production)
  -verbose                  Verbose logging
  -vite-port <int>          Vite dev server port for CORS in dev (default: 3000)
  -detach                   Run as a background process
`)
		}

		_ = runCmd.Parse(os.Args[2:])

		// basic port validation
		if cfg.Port <= 0 || cfg.Port > 65535 {
			fmt.Fprintln(os.Stderr, "invalid -port: must be between 1 and 65535")
			os.Exit(2)
		}

		if detach && os.Getenv("LINUXIO_DETACHED") != "1" {
			daemonReexec()
			return
		}

		// Run the server (foreground or already-detached child)
		runServerFunc(cfg)
		return

	default:
		// Unknown subcommand → help
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", os.Args[1])
		printGeneralUsage()
		return
	}
}

func printGeneralUsage() {
	fmt.Fprintf(os.Stderr, `LinuxIO Server

Usage:
  linuxio <command> [flags]

Commands:
  run         Run the HTTP server
  version     Show version information
  help        Show this help

Examples:
  linuxio run
  linuxio run -env development -port 18090 -verbose
  linuxio run -bridge-binary /usr/local/bin/linuxio-bridge
  linuxio run -detach

Use "linuxio <command> -h" for more info about a command.
`)
}

// daemonReexec re-execs the current binary as a background process.
func daemonReexec() {
	orig := os.Args
	args := []string{"run"}

	// Keep all args after "run" except any form of -detach flag
	for i := 2; i < len(orig); i++ {
		a := orig[i]
		if a == "-detach" || a == "--detach" ||
			strings.HasPrefix(a, "-detach=") || strings.HasPrefix(a, "--detach=") {
			continue
		}
		args = append(args, a)
	}

	cmd := execCommand(orig[0], args...)
	cmd.Env = append(os.Environ(), "LINUXIO_DETACHED=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // new session
	}

	// Inherit stdout/stderr
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to detach: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("linuxio started in background (pid %d)\n", cmd.Process.Pid)
	os.Exit(0)
}
