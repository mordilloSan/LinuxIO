package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

func main() {
	if len(os.Args) < 2 {
		showHelp()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "status":
		runStatus()
	case "logs":
		runLogs(args)
	case "start":
		runSystemctl("start", "linuxio.target")
	case "stop":
		runSystemctl("stop", "linuxio.target")
	case "restart":
		runSystemctl("restart", "linuxio.target")
	case "version":
		showVersion()
	case "help", "-h", "--help":
		showHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		showHelp()
		os.Exit(1)
	}
}

func showHelp() {
	fmt.Println(`LinuxIO - Manage LinuxIO services

Usage: linuxio <command> [options]

Commands:
  status     Show status of all LinuxIO services
  logs       Tail logs (use -a for all units)
  start      Start LinuxIO services
  stop       Stop LinuxIO services
  restart    Restart LinuxIO services
  version    Show version information
  help       Show this help message

Examples:
  linuxio status              # Show all linuxio* units
  linuxio logs                # Tail webserver logs
  linuxio logs -a             # Tail all linuxio* logs
  linuxio restart             # Restart all services`)
}

func showVersion() {
	fmt.Printf("LinuxIO")
	fmt.Printf("Built: %s\n", config.BuildTime)

	// Also show installed component versions
	fmt.Println("\nInstalled components:")
	fmt.Printf("LinuxIO CLI %s\n", config.Version)

	// Check linuxio-webserver (uses --help, version is on first line)
	out, err := exec.Command("linuxio-webserver", "--help").CombinedOutput()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			fmt.Printf("  %s\n", lines[0])
		}
	} else {
		fmt.Println("  linuxio-webserver: not found or error")
	}

	// Check linuxio-bridge
	out, err = exec.Command("linuxio-bridge", "version").CombinedOutput()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			fmt.Printf("  %s\n", lines[0])
		}
	} else {
		fmt.Println("  linuxio-bridge: not found or error")
	}

	// Check linuxio-auth
	out, err = exec.Command("linuxio-auth", "--version").CombinedOutput()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			fmt.Printf("  %s\n", lines[0])
		}
	} else {
		fmt.Println("  linuxio-auth: not found or error")
	}
}

func runStatus() {
	cmd := exec.Command("systemctl", "list-units", "linuxio*", "--no-pager", "--all")
	out, err := cmd.Output()
	if err != nil {
		os.Exit(1)
	}

	// Filter out legend and footer, keep header and unit lines
	allLines := strings.Split(string(out), "\n")
	var filtered []string
	for _, line := range allLines {
		if line == "" || strings.HasPrefix(line, "Legend:") || strings.HasPrefix(line, "To show all") {
			break
		}
		filtered = append(filtered, line)
	}

	// Find max width for header underline
	maxWidth := 0
	for _, line := range filtered {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}

	// Print with header underlined to full width
	for i, line := range filtered {
		if i == 0 {
			padded := line + strings.Repeat(" ", maxWidth-len(line))
			fmt.Printf("\033[4m%s\033[0m\n", padded)
		} else {
			fmt.Println(line)
		}
	}

	// Print summary (unit count excludes header)
	unitCount := len(filtered) - 1
	fmt.Printf("\n\033[1m%d loaded units listed.\033[0m\n", unitCount)
}

func runLogs(args []string) {
	var cmdArgs []string

	// Check for -a flag (all units)
	allUnits := false
	for _, arg := range args {
		if arg == "-a" || arg == "--all" {
			allUnits = true
		}
	}

	if allUnits {
		// Use glob pattern for all linuxio units
		cmdArgs = []string{"-u", "linuxio*", "-f", "--no-pager"}
	} else {
		// Default: just the webserver
		cmdArgs = []string{"-u", "linuxio-webserver.service", "-f", "--no-pager"}
	}

	cmd := exec.Command("journalctl", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		// Don't exit with error for Ctrl+C
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

func runSystemctl(action, target string) {
	cmd := exec.Command("systemctl", action, target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to %s %s: %v\n", action, target, err)
		fmt.Fprintln(os.Stderr, "Hint: This command may require sudo")
		os.Exit(1)
	}

	fmt.Printf("Successfully %sed %s\n", action, target)
}
