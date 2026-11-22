package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(runCLI(os.Args))
}

func runCLI(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) < 2 {
		showHelp(os.Stdout)
		return 0
	}

	cmd := args[1]
	cmdArgs := args[2:]

	switch cmd {
	case "status":
		return runStatus(ctx)
	case "logs":
		return runLogs(cmdArgs)
	case "start":
		return runSystemctl(ctx, "start", []string{linuxioTargetName}, linuxioTargetName)
	case "stop":
		return runSystemctl(ctx, "stop", []string{linuxioTargetName}, linuxioTargetName)
	case "restart":
		return runRestart(ctx, cmdArgs)
	case "verbose":
		return runVerbose(ctx, cmdArgs)
	case "version", "--version":
		showVersion(os.Stdout)
		return 0
	case "help", "-h", "--help":
		showHelp(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		showHelp(os.Stderr)
		return 2
	}
}

// helpText is what linuxio with no command, help, -h and --help print.
const helpText = `LinuxIO system administration

linuxio status
linuxio logs [component] [lines]
linuxio start
linuxio stop
linuxio restart [--full]
linuxio verbose enable|disable|status
linuxio version
`

func showHelp(w io.Writer) {
	fmt.Fprint(w, helpText)
}
