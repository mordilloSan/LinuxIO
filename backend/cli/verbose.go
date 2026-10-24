package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/verbosemode"
)

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

func enableVerbose(ctx context.Context) int {
	if verbosemode.Enabled() {
		fmt.Println("Verbose mode is already enabled")
		return 0
	}
	if err := verbosemode.Enable(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to enable verbose mode: %v\n", err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		return 1
	}
	fmt.Println("✓ Verbose mode enabled")
	return reloadVerboseDaemons(ctx, "✓ Verbose logging is now active (open sessions switch at next login)\n  View debug logs with: linuxio logs")
}

func disableVerbose(ctx context.Context) int {
	if !verbosemode.Enabled() && !verbosemode.Partial() {
		fmt.Println("Verbose mode is already disabled")
		return 0
	}
	if err := verbosemode.Disable(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to disable verbose mode: %v\n", err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		return 1
	}
	fmt.Println("✓ Verbose mode disabled")
	return reloadVerboseDaemons(ctx, "✓ Verbose logging is now disabled (open sessions switch at next login)")
}

func reloadVerboseDaemons(ctx context.Context, done string) int {
	fmt.Println("Reloading LinuxIO services...")
	if err := verbosemode.ReloadDaemons(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload LinuxIO services: %v\n", err)
		return 1
	}
	fmt.Println("\n" + done)
	return 0
}

func showVerboseStatus() {
	if !verbosemode.Enabled() {
		fmt.Println("Verbose mode: \033[90mdisabled\033[0m")
		fmt.Println("\nTo enable: sudo linuxio verbose enable")
	} else {
		fmt.Println("Verbose mode: \033[32menabled\033[0m")
		fmt.Println("\nDrop-in files:")
		for _, path := range verbosemode.Paths() {
			fmt.Println("  " + path)
		}
		fmt.Println("To disable: sudo linuxio verbose disable")
	}
}
