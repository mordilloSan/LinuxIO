// Command linuxio-docker-update runs the short-lived Docker update jobs.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
)

// runUpdates keeps argument and dispatch behavior testable without contacting Docker.
var runUpdates = docker.RunScheduledContainerUpdates
var runOperation = docker.RunDurableDockerUpdate
var configureLogging = logging.Configure

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runWorker(os.Args, ctx, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func runWorker(args []string, ctx context.Context, stdout, stderr io.Writer) int {
	if err := configureLogging("linuxio-docker-update", false); err != nil {
		fmt.Fprintf(stderr, "linuxio-docker-update: initialize logging: %v\n", err)
		return 1
	}
	return runCLI(args, ctx, stdout, stderr)
}

func runCLI(args []string, ctx context.Context, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		writeHelp(stdout)
		return 0
	}

	switch args[1] {
	case "run":
		configPath, ok := parseRunArgs(args[2:], stderr)
		if !ok {
			return 2
		}
		if err := runUpdates(ctx, configPath); err != nil {
			fmt.Fprintf(stderr, "linuxio-docker-update: run failed: %v\n", err)
			return 1
		}
		return 0
	case "run-operation":
		operationID, ok := parseOperationArgs(args[2:], stderr)
		if !ok {
			return 2
		}
		if err := runOperation(ctx, operationID); err != nil {
			fmt.Fprintf(stderr, "linuxio-docker-update: run-operation failed: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		writeHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "linuxio-docker-update: unknown command %q\n", args[1])
		writeUsage(stderr)
		return 2
	}
}

func parseOperationArgs(args []string, stderr io.Writer) (string, bool) {
	operationID := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--id" {
			fmt.Fprintf(stderr, "linuxio-docker-update: run-operation: unknown argument %q\n", args[i])
			return "", false
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			fmt.Fprintln(stderr, "linuxio-docker-update: run-operation: --id requires an operation ID")
			return "", false
		}
		if operationID != "" {
			fmt.Fprintln(stderr, "linuxio-docker-update: run-operation: --id may only be provided once")
			return "", false
		}
		operationID = strings.TrimSpace(args[i+1])
		i++
	}
	if operationID == "" {
		fmt.Fprintln(stderr, "linuxio-docker-update: run-operation: --id requires an operation ID")
		return "", false
	}
	return operationID, true
}

func parseRunArgs(args []string, stderr io.Writer) (string, bool) {
	configPath := docker.DockerUpdateConfigPath
	for i := 0; i < len(args); i++ {
		if args[i] != "--config" {
			fmt.Fprintf(stderr, "linuxio-docker-update: run: unknown argument %q\n", args[i])
			return "", false
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			fmt.Fprintln(stderr, "linuxio-docker-update: run: --config requires a path")
			return "", false
		}
		configPath = args[i+1]
		i++
	}
	return configPath, true
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: linuxio-docker-update run [options]")
	fmt.Fprintln(w, "       linuxio-docker-update run-operation --id OPERATION_ID")
}

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "LinuxIO Docker update command")
	fmt.Fprintln(w)
	writeUsage(w)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  run  Run one configured Docker update pass")
	fmt.Fprintln(w, "  run-operation  Run one persisted Docker update operation")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options for run:")
	fmt.Fprintln(w, "  --config PATH  Use an alternate update configuration file")
	fmt.Fprintln(w, "Options for run-operation:")
	fmt.Fprintln(w, "  --id ID  Persisted operation identifier")
}
