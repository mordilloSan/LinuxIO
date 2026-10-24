package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

var verboseDropins = []struct {
	path    string
	content string
}{
	{
		path: "/etc/systemd/system/linuxio-webserver.service.d/verbose.conf",
		content: `[Service]
ExecStart=
ExecStart=` + version.BinDir + `/linuxio-webserver run -verbose
`,
	},
	{
		path: "/etc/systemd/system/linuxio-indexer.service.d/verbose.conf",
		content: `[Service]
ExecStart=
ExecStart=` + version.BinDir + `/linuxio-indexer --config-file ` + version.ConfigDir + `/indexer/config.yaml --verbose
`,
	},
	{
		path: "/etc/systemd/system/linuxio-monitoring.service.d/verbose.conf",
		content: `[Service]
ExecStart=
ExecStart=` + version.BinDir + `/linuxio-monitoring run --config ` + version.ConfigDir + `/monitoring/config.yaml --verbose
`,
	},
}

func runVerbose(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: linuxio verbose [enable|disable|status]")
		return 1
	}

	action := args[0]

	switch action {
	case "enable":
		return enableVerbose(ctx)
	case "disable":
		return disableVerbose(ctx)
	case "status":
		showVerboseStatus()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown verbose action: %s\n", action)
		fmt.Fprintln(os.Stderr, "Usage: linuxio verbose [enable|disable|status]")
		return 1
	}
}

func enableVerbose(parent context.Context) int {
	if verboseEnabled() {
		fmt.Println("Verbose mode is already enabled")
		return 0
	}

	for _, dropin := range verboseDropins {
		if err := os.MkdirAll(filepath.Dir(dropin.path), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create drop-in directory: %v\n", err)
			fmt.Fprintln(os.Stderr, "This command requires sudo")
			return 1
		}
		if err := os.WriteFile(dropin.path, []byte(dropin.content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write drop-in file: %v\n", err)
			fmt.Fprintln(os.Stderr, "This command requires sudo")
			return 1
		}
	}

	fmt.Println("✓ Verbose mode enabled")

	fmt.Println("Reloading systemd daemon...")
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := systemd.DaemonReload(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		return 1
	}

	fmt.Println("Restarting linuxio.target...")
	if err := systemd.RestartUnit(ctx, linuxioTargetName); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart LinuxIO services: %v\n", err)
		return 1
	}

	fmt.Println("\n✓ Verbose logging is now active")
	fmt.Println("  View debug logs with: linuxio logs")
	return 0
}

func disableVerbose(parent context.Context) int {
	if !verboseEnabled() && !verbosePartiallyEnabled() {
		fmt.Println("Verbose mode is already disabled")
		return 0
	}

	for _, dropin := range verboseDropins {
		if err := os.Remove(dropin.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "Failed to remove drop-in file: %v\n", err)
			fmt.Fprintln(os.Stderr, "This command requires sudo")
			return 1
		}
	}

	fmt.Println("✓ Verbose mode disabled")

	fmt.Println("Reloading systemd daemon...")
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := systemd.DaemonReload(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		return 1
	}

	fmt.Println("Restarting linuxio.target...")
	if err := systemd.RestartUnit(ctx, linuxioTargetName); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart LinuxIO services: %v\n", err)
		return 1
	}

	fmt.Println("\n✓ Verbose logging is now disabled")
	return 0
}

func showVerboseStatus() {
	if !verboseEnabled() {
		fmt.Println("Verbose mode: \033[90mdisabled\033[0m")
		fmt.Println("\nTo enable: sudo linuxio verbose enable")
	} else {
		fmt.Println("Verbose mode: \033[32menabled\033[0m")
		fmt.Println("\nDrop-in files:")
		for _, dropin := range verboseDropins {
			fmt.Println("  " + dropin.path)
		}
		fmt.Println("To disable: sudo linuxio verbose disable")
	}
}

func verboseEnabled() bool {
	for _, dropin := range verboseDropins {
		if _, err := os.Stat(dropin.path); err != nil {
			return false
		}
	}
	return true
}

func verbosePartiallyEnabled() bool {
	for _, dropin := range verboseDropins {
		if _, err := os.Stat(dropin.path); err == nil {
			return true
		}
	}
	return false
}
