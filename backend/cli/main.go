package main

import (
	"context"
	"fmt"
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
		showHelp()
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
		return runSystemctl(ctx, "start", linuxioTargetName)
	case "stop":
		return runSystemctl(ctx, "stop", linuxioTargetName)
	case "restart":
		return runRestart(ctx, cmdArgs)
	case "verbose":
		return runVerbose(ctx, cmdArgs)
	case "alert":
		return runAlert(ctx, cmdArgs)
	case "btrfs-snapshot", "btrfs-health": // hidden; see btrfs_snapshot.go
		return runBtrfsCompat(cmd, cmdArgs)
	case "version":
		showVersion(cmdArgs)
		return 0
	case "help", "-h", "--help":
		showHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		showHelp()
		return 2
	}
}

func showHelp() {
	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println(`
Usage: linuxio <command> [options]

Commands:
  status      Show status of all LinuxIO services
  logs        Tail logs [webserver|bridge|auth|indexer|monitoring] [lines] (default: all, 100)
  start       Start LinuxIO services
  stop        Stop LinuxIO services
  restart     Restart LinuxIO control plane [--full]
  verbose     Manage verbose logging [enable|disable|status]
  alert raise|resolve|auto-update   Record or clear an alert (used by systemd hooks; needs sudo)
  version     Show version information [--self]
  help        Show this help message

Examples:
  linuxio status
  linuxio restart
  linuxio restart --full
  linuxio logs bridge 200
  linuxio version --self`)
}
