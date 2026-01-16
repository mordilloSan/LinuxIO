package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	// Build journalctl arguments
	journalctlArgs := append(strings.Fields(strings.Join(journalTerms, " + ")), "-f", "-n", strconv.Itoa(lines), "--no-pager", "-o", "json")
	cmd := exec.Command("journalctl", journalctlArgs...)

	// Get stdout pipe to read JSON
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create pipe: %v\n", err)
		os.Exit(1)
	}

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start journalctl: %v\n", err)
		os.Exit(1)
	}

	// Parse and format JSON lines
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		formatted := formatJournalEntry(line)
		if formatted != "" {
			fmt.Println(formatted)
		}
	}

	if err := cmd.Wait(); err != nil {
		// Don't exit with error for Ctrl+C
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

// formatJournalEntry parses a journalctl JSON line and formats it with colors
// PRIORITY levels: 7=DEBUG(cyan), 6,5=INFO(green), 4=WARNING(yellow), 3,2,1,0=ERROR(red)
func formatJournalEntry(jsonLine string) string {
	var entry map[string]any
	if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
		return ""
	}

	// Extract timestamp
	var timestamp string
	if ts, ok := entry["__REALTIME_TIMESTAMP"].(string); ok {
		if usec, err := strconv.ParseInt(ts, 10, 64); err == nil {
			t := time.Unix(0, usec*1000)
			timestamp = t.Format("Jan 02 15:04:05")
		}
	}
	if timestamp == "" {
		timestamp = time.Now().Format("Jan 02 15:04:05")
	}

	// Extract unit name
	unit := "unknown"
	if u, ok := entry["_SYSTEMD_UNIT"].(string); ok {
		unit = u
	} else if u, ok := entry["SYSLOG_IDENTIFIER"].(string); ok {
		unit = u
	}

	// Clean unit name (remove @.* suffix, .service, .socket)
	unit = strings.Split(unit, "@")[0]
	unit = strings.TrimSuffix(unit, ".service")
	unit = strings.TrimSuffix(unit, ".socket")

	// Extract PID
	var pid string
	if p, ok := entry["_PID"].(string); ok {
		pid = p
	} else if p, ok := entry["SYSLOG_PID"].(string); ok {
		pid = p
	}

	// Extract message
	message := ""
	if msg, ok := entry["MESSAGE"].(string); ok {
		message = msg
	}

	// Map priority to colorized level
	level := ""
	if priority, ok := entry["PRIORITY"].(string); ok {
		switch priority {
		case "7":
			level = "\033[36m[DEBUG]\033[0m"
		case "6", "5":
			level = "\033[32m[INFO]\033[0m"
		case "4":
			level = "\033[33m[WARNING]\033[0m"
		case "3", "2", "1", "0":
			level = "\033[31m[ERROR]\033[0m"
		}
	}

	// Format output
	if pid != "" {
		return fmt.Sprintf("%s  %s[%s]: %s %s", timestamp, unit, pid, level, message)
	}
	return fmt.Sprintf("%s  %s: %s %s", timestamp, unit, level, message)
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

	// Restart LinuxIO services
	fmt.Println("Restarting linuxio.target...")
	cmd = exec.Command("systemctl", "restart", "linuxio.target")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart LinuxIO services: %v\n", err)
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

	// Restart LinuxIO services
	fmt.Println("Restarting linuxio.target...")
	cmd = exec.Command("systemctl", "restart", "linuxio.target")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart LinuxIO services: %v\n", err)
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
