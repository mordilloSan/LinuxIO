package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

const (
	linuxioTargetName              = "linuxio.target"
	linuxioWebserverServiceName    = "linuxio-webserver.service"
	linuxioAuthSocketName          = "linuxio-auth.socket"
	linuxioBridgeSocketUserService = "linuxio-bridge-socket-user.service"
	monitoringUnitName             = "linuxio-monitoring.service"
	monitoringProjectName          = "linuxio-monitoring"
	monitoringComposePath          = "/etc/linuxio/docker/linuxio-monitoring/docker-compose.yml"
	monitoringGeneratedComposePath = "/run/linuxio-monitoring/docker-compose.generated.yml"
)

var execCommand = exec.Command

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
		runSystemctl("start", linuxioTargetName)
	case "stop":
		runSystemctl("stop", linuxioTargetName)
	case "restart":
		runRestart(args)
	case "monitoring":
		runMonitoring(args)
	case "verbose":
		runVerbose(args)
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
  status      Show status of all LinuxIO services
  logs        Tail logs [webserver|bridge|auth|monitoring] [lines] (default: all, 100)
  start       Start LinuxIO services
  stop        Stop LinuxIO services
  restart     Restart LinuxIO control plane [--full]
  monitoring  Manage monitoring stack [start|stop|restart|enable|disable|status]
  verbose     Manage verbose logging [enable|disable|status]
  version     Show version information
  help        Show this help message

Examples:
  linuxio status
  linuxio restart
  linuxio restart --full
  linuxio monitoring status
  linuxio logs monitoring 200`)
}

func showVersion() {
	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println("\nInstalled components:")
	fmt.Printf("  LinuxIO CLI %s\n", config.Version)

	// Check linuxio-webserver
	out, err := execCommand("linuxio-webserver", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-webserver: not found or error")
	}

	// Check linuxio-bridge
	out, err = execCommand("linuxio-bridge", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-bridge: not found or error")
	}

	// Check linuxio-auth
	out, err = execCommand("linuxio-auth", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("linuxio-auth: not found or error")
	}
}

func runStatus() {
	cmd := execCommand("systemctl", "list-units", "linuxio*", "--no-pager", "--all")
	out, err := cmd.Output()
	if err != nil {
		os.Exit(1)
	}

	// Filter out legend and footer, keep header and unit lines
	var filtered []string
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
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
	mode, lines := parseLogsArgs(args)
	journalTerms := journalTermsForMode(mode)
	journalctlArgs := append(strings.Fields(strings.Join(journalTerms, " + ")), "-f", "-n", strconv.Itoa(lines), "--no-pager", "-o", "json")
	cmd := execCommand("journalctl", journalctlArgs...)

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

	streamFormattedJournal(stdout)
	waitForJournalctl(cmd)
}

func parseLogsArgs(args []string) (string, int) {
	mode := "all"
	lines := 100
	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			lines = n
			continue
		}
		switch arg {
		case "webserver", "web", "server":
			mode = "webserver"
		case "bridge":
			mode = "bridge"
		case "auth":
			mode = "auth"
		case "monitoring":
			mode = "monitoring"
		}
	}
	return mode, lines
}

func journalTermsForMode(mode string) []string {
	journalTerms := []string{
		"_SYSTEMD_UNIT=linuxio.target",
		"_SYSTEMD_UNIT=linuxio-webserver.service",
		"_SYSTEMD_UNIT=linuxio-webserver.socket",
		"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service",
		"_SYSTEMD_UNIT=linuxio-auth.socket",
		"_SYSTEMD_UNIT=linuxio-auth@.service",
		"_SYSTEMD_UNIT=linuxio-issue.service",
		"_SYSTEMD_UNIT=" + monitoringUnitName,
	}
	includeAuthTag := true

	switch mode {
	case "webserver":
		journalTerms = []string{
			"_SYSTEMD_UNIT=linuxio-webserver.service",
			"_SYSTEMD_UNIT=linuxio-webserver.socket",
		}
		includeAuthTag = false
	case "bridge":
		journalTerms = []string{"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service"}
		includeAuthTag = false
	case "auth":
		journalTerms = []string{
			"_SYSTEMD_UNIT=linuxio-auth.socket",
			"_SYSTEMD_UNIT=linuxio-auth@.service",
		}
	case "monitoring":
		journalTerms = []string{"_SYSTEMD_UNIT=" + monitoringUnitName}
		includeAuthTag = false
	}

	if includeAuthTag {
		journalTerms = append(journalTerms, "SYSLOG_IDENTIFIER=linuxio-auth")
	}
	return journalTerms
}

func streamFormattedJournal(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		formatted := formatJournalEntry(scanner.Text())
		if formatted != "" {
			fmt.Println(formatted)
		}
	}
}

func waitForJournalctl(cmd *exec.Cmd) {
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

type journalEntry struct {
	Timestamp string `json:"__REALTIME_TIMESTAMP"`
	Unit      string `json:"_SYSTEMD_UNIT"`
	SyslogID  string `json:"SYSLOG_IDENTIFIER"`
	PID       string `json:"_PID"`
	SyslogPID string `json:"SYSLOG_PID"`
	Priority  string `json:"PRIORITY"`
	Message   string `json:"MESSAGE"`
}

// formatJournalEntry parses a journalctl JSON line and formats it with colors
// PRIORITY levels: 7=DEBUG(cyan), 6,5=INFO(green), 4=WARNING(yellow), 3,2,1,0=ERROR(red)
func formatJournalEntry(jsonLine string) string {
	var entry journalEntry
	if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
		return ""
	}

	timestamp := journalTimestamp(entry)
	unit := journalUnit(entry)
	pid := journalPID(entry)
	level := journalPriorityLevel(entry)

	if pid != "" {
		return fmt.Sprintf("%s  %s[%s]: %s %s", timestamp, unit, pid, level, entry.Message)
	}
	return fmt.Sprintf("%s  %s: %s %s", timestamp, unit, level, entry.Message)
}

func journalTimestamp(entry journalEntry) string {
	if usec, err := strconv.ParseInt(entry.Timestamp, 10, 64); err == nil {
		return time.Unix(0, usec*1000).Format("Jan 02 15:04:05")
	}
	return time.Now().Format("Jan 02 15:04:05")
}

func journalUnit(entry journalEntry) string {
	unit := "unknown"
	if entry.Unit != "" {
		unit = entry.Unit
	} else if entry.SyslogID != "" {
		unit = entry.SyslogID
	}
	if at := strings.Index(unit, "@"); at >= 0 {
		unit = unit[:at]
	}
	unit = strings.TrimSuffix(unit, ".service")
	unit = strings.TrimSuffix(unit, ".socket")
	return unit
}

func journalPID(entry journalEntry) string {
	if entry.PID != "" {
		return entry.PID
	}
	return entry.SyslogPID
}

func journalPriorityLevel(entry journalEntry) string {
	switch entry.Priority {
	case "7":
		return "\033[36m[DEBUG]\033[0m"
	case "6", "5":
		return "\033[32m[INFO]\033[0m"
	case "4":
		return "\033[33m[WARNING]\033[0m"
	case "3", "2", "1", "0":
		return "\033[31m[ERROR]\033[0m"
	default:
		return ""
	}
}

func runSystemctl(action, target string) {
	runSystemctlTargets(action, []string{target}, target)
}

func runSystemctlTargets(action string, targets []string, successLabel string) {
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "No targets provided for systemctl %s\n", action)
		os.Exit(1)
	}

	cmd := execCommand("systemctl", append([]string{action}, targets...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to %s %s: %v\n", action, strings.Join(targets, " "), err)
		fmt.Fprintln(os.Stderr, "This command requires sudo")
		os.Exit(1)
	}

	if successLabel == "" {
		successLabel = strings.Join(targets, " ")
	}
	fmt.Printf("Successfully %s %s\n", pastTense(action), successLabel)
}

func pastTense(action string) string {
	switch action {
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "restart":
		return "restarted"
	case "enable":
		return "enabled"
	case "disable":
		return "disabled"
	default:
		return action + "ed"
	}
}

func runRestart(args []string) {
	targets, successLabel, err := restartTargets(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprintln(os.Stderr, "Usage: linuxio restart [--full]")
		os.Exit(1)
	}

	runSystemctlTargets("restart", targets, successLabel)
}

func restartTargets(args []string) ([]string, string, error) {
	if len(args) == 0 {
		return []string{
			linuxioBridgeSocketUserService,
			linuxioAuthSocketName,
			linuxioWebserverServiceName,
		}, "LinuxIO control plane", nil
	}

	if len(args) == 1 {
		switch args[0] {
		case "--full", "full":
			return []string{linuxioTargetName}, linuxioTargetName, nil
		}
	}

	return nil, "", fmt.Errorf("unknown restart option: %s", strings.Join(args, " "))
}

func runMonitoring(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: linuxio monitoring [start|stop|restart|enable|disable|status]")
		os.Exit(1)
	}

	switch args[0] {
	case "start", "stop", "restart", "enable", "disable":
		runSystemctl(args[0], monitoringUnitName)
	case "status":
		showMonitoringStatus()
	default:
		fmt.Fprintf(os.Stderr, "Unknown monitoring action: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: linuxio monitoring [start|stop|restart|enable|disable|status]")
		os.Exit(1)
	}
}

func showMonitoringStatus() {
	fmt.Printf("\033[1mLinuxIO Monitoring Stack\033[0m\n")
	fmt.Printf("  Unit:        %s\n", monitoringUnitName)
	fmt.Printf("  Active:      %s\n", systemctlState("is-active", monitoringUnitName))
	fmt.Printf("  Enabled:     %s\n", systemctlState("is-enabled", monitoringUnitName))

	switch _, err := os.Stat(monitoringComposePath); {
	case err == nil:
		fmt.Printf("  Compose:     %s\n", monitoringComposePath)
	case os.IsNotExist(err):
		fmt.Printf("  Compose:     missing (%s)\n", monitoringComposePath)
	default:
		fmt.Printf("  Compose:     error: %v\n", err)
	}
	switch _, err := os.Stat(monitoringGeneratedComposePath); {
	case err == nil:
		fmt.Printf("  Override:    %s\n", monitoringGeneratedComposePath)
	case os.IsPermission(err):
		fmt.Printf("  Override:    %s (restricted)\n", monitoringGeneratedComposePath)
	}

	showMonitoringContainers()
}

type composeContainer struct {
	Name   string `json:"Name"`
	State  string `json:"State"`
	Health string `json:"Health"`
}

type dockerPSContainer struct {
	Names  string `json:"Names"`
	State  string `json:"State"`
	Status string `json:"Status"`
}

func showMonitoringContainers() {
	out, err := execCommand(
		"docker",
		"ps",
		"--all",
		"--filter", "label=com.docker.compose.project="+monitoringProjectName,
		"--format", "{{json .}}",
	).CombinedOutput()
	if err != nil {
		fmt.Printf("\n  Containers:  unable to query (%v)\n", err)
		return
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		fmt.Printf("\n  Containers:  none running\n")
		return
	}

	var containers []composeContainer
	for line := range strings.Lines(trimmed) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c dockerPSContainer
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		containers = append(containers, composeContainer{
			Name:   c.Names,
			State:  monitoringContainerState(c.State, c.Status),
			Health: monitoringContainerHealth(c.Status),
		})
	}

	if len(containers) == 0 {
		fmt.Printf("\n  Containers:  none running\n")
		return
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})

	fmt.Printf("\n    \033[4m%-28s  %-12s  %s\033[0m\n", "CONTAINER", "STATE", "HEALTH")
	for _, c := range containers {
		var dot string
		switch {
		case c.State == "running" && (c.Health == "healthy" || c.Health == ""):
			dot = "\033[32m●\033[0m"
		case c.State == "running":
			dot = "\033[33m●\033[0m"
		default:
			dot = "\033[31m●\033[0m"
		}
		health := c.Health
		if health == "" {
			health = "-"
		}
		fmt.Printf("  %s %-28s  %-12s  %s\n", dot, c.Name, c.State, health)
	}
}

func monitoringContainerState(state, status string) string {
	state = strings.TrimSpace(strings.ToLower(state))
	if state != "" {
		return state
	}

	status = strings.TrimSpace(strings.ToLower(status))
	switch {
	case strings.HasPrefix(status, "up "):
		return "running"
	case strings.HasPrefix(status, "exited"):
		return "exited"
	case strings.HasPrefix(status, "created"):
		return "created"
	case strings.HasPrefix(status, "restarting"):
		return "restarting"
	case strings.HasPrefix(status, "removing"):
		return "removing"
	case strings.HasPrefix(status, "paused"):
		return "paused"
	case strings.HasPrefix(status, "dead"):
		return "dead"
	default:
		return "-"
	}
}

func monitoringContainerHealth(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	start := strings.LastIndex(status, "(")
	end := strings.LastIndex(status, ")")
	if start == -1 || end <= start {
		return "-"
	}

	health := strings.TrimSpace(status[start+1 : end])
	health = strings.TrimPrefix(health, "health: ")
	switch health {
	case "healthy", "unhealthy", "starting":
		return health
	default:
		return "-"
	}
}

func systemctlState(args ...string) string {
	out, err := execCommand("systemctl", args...).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if state != "" {
		return state
	}
	if err != nil {
		return "unknown"
	}
	return "unknown"
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
	cmd := execCommand("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	// Restart LinuxIO services
	fmt.Println("Restarting linuxio.target...")
	cmd = execCommand("systemctl", "restart", "linuxio.target")
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
	cmd := execCommand("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	// Restart LinuxIO services
	fmt.Println("Restarting linuxio.target...")
	cmd = execCommand("systemctl", "restart", "linuxio.target")
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
