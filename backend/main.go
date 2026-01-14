package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
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
	case "verbose":
		runVerbose(args)
	case "modules":
		runModules()
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
	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println(`
Usage: linuxio <command> [options]

Commands:
  status     Show status of all LinuxIO services
  logs       Tail logs [webserver|bridge|auth] [lines] (default: all, 100)
  start      Start LinuxIO services
  stop       Stop LinuxIO services
  restart    Restart LinuxIO services
  verbose    Manage verbose logging [enable|disable|status]
  modules    List all installed modules
  version    Show version information
  help       Show this help message`)
}

func showVersion() {
	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println("\nInstalled components:")
	fmt.Printf("  LinuxIO CLI %s\n", config.Version)

	// Check linuxio-webserver
	out, err := exec.Command("linuxio-webserver", "version").CombinedOutput()
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
	out, err = exec.Command("linuxio-auth", "version").CombinedOutput()
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

	// Print with header underlined to full width, add status dots
	for i, line := range filtered {
		if i == 0 {
			padded := line + strings.Repeat(" ", maxWidth-len(line))
			fmt.Printf("  \033[4m%s\033[0m\n", padded)
		} else {
			// Add colored status dot based on ACTIVE column
			dot := "○" // default: white circle
			if strings.Contains(line, " active ") {
				dot = "\033[32m●\033[0m" // green
			} else if strings.Contains(line, " failed ") {
				dot = "\033[31m●\033[0m" // red
			}
			fmt.Printf("%s %s\n", dot, strings.TrimLeft(line, " "))
		}
	}

	// Print summary (unit count excludes header)
	unitCount := len(filtered) - 1
	fmt.Printf("\n\033[1m%d loaded units listed.\033[0m\n", unitCount)
}

func runLogs(args []string) {
	// Defaults
	lines := 100
	mode := "all"

	// Parse args: can be [service] [lines] in any order
	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			lines = n
		} else {
			switch arg {
			case "webserver", "web", "server":
				mode = "webserver"
			case "bridge":
				mode = "bridge"
			case "auth":
				mode = "auth"
			}
		}
	}

	allUnits := []string{
		"_SYSTEMD_UNIT=linuxio.target",
		"_SYSTEMD_UNIT=linuxio-webserver.service",
		"_SYSTEMD_UNIT=linuxio-webserver.socket",
		"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service",
		"_SYSTEMD_UNIT=linuxio-auth.socket",
		"_SYSTEMD_UNIT=linuxio-auth@.service",
		"_SYSTEMD_UNIT=linuxio-issue.service",
	}
	webserverUnits := []string{
		"_SYSTEMD_UNIT=linuxio-webserver.service",
		"_SYSTEMD_UNIT=linuxio-webserver.socket",
	}
	bridgeUnits := []string{
		"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service",
	}
	authUnits := []string{
		"_SYSTEMD_UNIT=linuxio-auth.socket",
		"_SYSTEMD_UNIT=linuxio-auth@.service",
	}

	journalTerms := allUnits
	includeAuthTag := true
	switch mode {
	case "webserver":
		journalTerms = webserverUnits
		includeAuthTag = false
	case "bridge":
		journalTerms = bridgeUnits
		includeAuthTag = false
	case "auth":
		journalTerms = authUnits
		includeAuthTag = true
	}
	if includeAuthTag {
		// Include syslog-tagged auth logs (e.g., when linuxio-auth logs via syslog)
		journalTerms = append(journalTerms, "SYSLOG_IDENTIFIER=linuxio-auth")
	}

	journalMatch := strings.Join(journalTerms, " + ")

	// Use jq to parse JSON output and reconstruct the journalctl short format with colorized level prefix
	// PRIORITY levels: 7=DEBUG(cyan), 6=INFO(green), 5=NOTICE(green), 4=WARNING(yellow), 3=ERROR(red), 0-2=ERROR(red)
	jqScript := `
		(.__REALTIME_TIMESTAMP | tonumber / 1000000 | strftime("%b %d %H:%M:%S")) as $time |
		(._SYSTEMD_UNIT // .SYSLOG_IDENTIFIER // "unknown") as $unit |
		(._PID // .SYSLOG_PID // "") as $pid |
		(.MESSAGE // "") as $msg |
		(if .PRIORITY == "7" then "\u001b[36m[DEBUG]\u001b[0m"
		 elif .PRIORITY == "6" or .PRIORITY == "5" then "\u001b[32m[INFO]\u001b[0m"
		 elif .PRIORITY == "4" then "\u001b[33m[WARNING]\u001b[0m"
		 elif .PRIORITY == "3" or .PRIORITY == "2" or .PRIORITY == "1" or .PRIORITY == "0" then "\u001b[31m[ERROR]\u001b[0m"
		 else ""
		 end) as $level |
		($unit | gsub("@.*$"; "") | gsub("\\.service$"; "") | gsub("\\.socket$"; "")) as $short_unit |
		if $pid != "" then
			"\($time)  \($short_unit)[\($pid)]: \($level) \($msg)"
		else
			"\($time)  \($short_unit): \($level) \($msg)"
		end
	`

	shellCmd := fmt.Sprintf("journalctl %s -f -n %d --no-pager -o json | jq -r '%s'", journalMatch, lines, jqScript)

	cmd := exec.Command("sh", "-c", shellCmd)
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

type ModuleConfig struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Author      string `yaml:"author"`
	UI          struct {
		Route string `yaml:"route"`
	} `yaml:"ui"`
}

func runModules() {
	modulesDir := "/etc/linuxio/modules"

	// Check if modules directory exists
	if _, err := os.Stat(modulesDir); os.IsNotExist(err) {
		fmt.Println("\033[1mInstalled Modules\033[0m")
		fmt.Println()
		fmt.Println("  No modules directory found at", modulesDir)
		return
	}

	// Read all subdirectories in modules directory
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading modules directory: %v\n", err)
		os.Exit(1)
	}

	var modules []ModuleConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Read module.yaml
		yamlPath := filepath.Join(modulesDir, entry.Name(), "module.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			// Skip if no module.yaml
			continue
		}

		var module ModuleConfig
		if err := yaml.Unmarshal(data, &module); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", yamlPath, err)
			continue
		}

		modules = append(modules, module)
	}

	// Display results
	fmt.Println("\033[1mInstalled Modules\033[0m")
	fmt.Println()

	if len(modules) == 0 {
		fmt.Println("  No modules installed")
		fmt.Println("\n  To install a module:")
		fmt.Println("    make deploy-module MODULE=<name>")
		return
	}

	// Find max widths for alignment
	maxName := 0
	maxVersion := 0
	for _, m := range modules {
		if len(m.Name) > maxName {
			maxName = len(m.Name)
		}
		if len(m.Version) > maxVersion {
			maxVersion = len(m.Version)
		}
	}

	// Print modules
	for _, m := range modules {
		namePadded := m.Name + strings.Repeat(" ", maxName-len(m.Name))
		versionPadded := m.Version + strings.Repeat(" ", maxVersion-len(m.Version))

		fmt.Printf("  \033[32m●\033[0m \033[1m%s\033[0m  \033[90mv%s\033[0m  %s\n",
			namePadded, versionPadded, m.Title)

		if m.Description != "" {
			fmt.Printf("    \033[90m%s\033[0m\n", m.Description)
		}
		if m.UI.Route != "" {
			fmt.Printf("    \033[36m→ %s\033[0m\n", m.UI.Route)
		}
		fmt.Println()
	}

	fmt.Printf("\033[1m%d module(s) installed\033[0m\n", len(modules))
}

func runSystemctl(action, target string) {
	cmd := exec.Command("systemctl", action, target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to %s %s: %v\n", action, target, err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		os.Exit(1)
	}

	fmt.Printf("Successfully %sed %s\n", action, target)
}

const verboseDropinPath = "/etc/systemd/system/linuxio-webserver.service.d/verbose.conf"
const verboseDropinContent = `[Service]
ExecStart=
ExecStart=/usr/local/bin/linuxio-webserver run -verbose
`

func runVerbose(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: linuxio verbose [enable|disable|status]")
		os.Exit(1)
	}

	action := args[0]

	switch action {
	case "enable":
		enableVerbose()
	case "disable":
		disableVerbose()
	case "status":
		showVerboseStatus()
	default:
		fmt.Fprintf(os.Stderr, "Unknown verbose action: %s\n", action)
		fmt.Fprintln(os.Stderr, "Usage: linuxio verbose [enable|disable|status]")
		os.Exit(1)
	}
}

func enableVerbose() {
	// Check if already enabled
	if _, err := os.Stat(verboseDropinPath); err == nil {
		fmt.Println("Verbose mode is already enabled")
		return
	}

	// Create drop-in directory
	dropinDir := filepath.Dir(verboseDropinPath)
	if err := os.MkdirAll(dropinDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create drop-in directory: %v\n", err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		os.Exit(1)
	}

	// Write drop-in file
	if err := os.WriteFile(verboseDropinPath, []byte(verboseDropinContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write drop-in file: %v\n", err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		os.Exit(1)
	}

	fmt.Println("✓ Verbose mode enabled")

	// Reload systemd daemon
	fmt.Println("Reloading systemd daemon...")
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	// Restart webserver
	fmt.Println("Restarting linuxio-webserver.service...")
	cmd = exec.Command("systemctl", "restart", "linuxio-webserver.service")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart webserver: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ Verbose logging is now active")
	fmt.Println("  View debug logs with: linuxio logs")
}

func disableVerbose() {
	// Check if already disabled
	if _, err := os.Stat(verboseDropinPath); os.IsNotExist(err) {
		fmt.Println("Verbose mode is already disabled")
		return
	}

	// Remove drop-in file
	if err := os.Remove(verboseDropinPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to remove drop-in file: %v\n", err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		os.Exit(1)
	}

	fmt.Println("✓ Verbose mode disabled")

	// Reload systemd daemon
	fmt.Println("Reloading systemd daemon...")
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	// Restart webserver
	fmt.Println("Restarting linuxio-webserver.service...")
	cmd = exec.Command("systemctl", "restart", "linuxio-webserver.service")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart webserver: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ Verbose logging is now disabled")
}

func showVerboseStatus() {
	if _, err := os.Stat(verboseDropinPath); os.IsNotExist(err) {
		fmt.Println("Verbose mode: \033[90mdisabled\033[0m")
		fmt.Println("\nTo enable: sudo linuxio verbose enable")
	} else {
		fmt.Println("Verbose mode: \033[32menabled\033[0m")
		fmt.Println("\nDrop-in file: " + verboseDropinPath)
		fmt.Println("To disable: sudo linuxio verbose disable")
	}
}
