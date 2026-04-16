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

	"github.com/mordilloSan/LinuxIO/backend/bridge/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	linuxioTargetName              = "linuxio.target"
	linuxioWebserverServiceName    = "linuxio-webserver.service"
	linuxioAuthSocketName          = "linuxio-auth.socket"
	linuxioBridgeSocketUserService = "linuxio-bridge-socket-user.service"
)

var versionExecCommand = exec.Command

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
	case "verbose":
		runVerbose(args)
	case "version":
		showVersion(args)
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
  logs        Tail logs [webserver|bridge|auth] [lines] (default: all, 100)
  start       Start LinuxIO services
  stop        Stop LinuxIO services
  restart     Restart LinuxIO control plane [--full]
  verbose     Manage verbose logging [enable|disable|status]
  version     Show version information [--self]
  help        Show this help message

Examples:
  linuxio status
  linuxio restart
  linuxio restart --full
  linuxio logs bridge 200
  linuxio version --self`)
}

func cliVersionLine() string {
	return fmt.Sprintf("LinuxIO CLI %s", version.Version)
}

func showVersion(args []string) {
	if len(args) == 1 && args[0] == "--self" {
		fmt.Println(cliVersionLine())
		return
	}

	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println("\nInstalled components:")
	fmt.Printf("  %s\n", cliVersionLine())

	// Check linuxio-webserver
	out, err := versionExecCommand("linuxio-webserver", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-webserver: not found or error")
	}

	// Check linuxio-bridge
	out, err = versionExecCommand("linuxio-bridge", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-bridge: not found or error")
	}

	// Check linuxio-auth
	out, err = versionExecCommand("linuxio-auth", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("linuxio-auth: not found or error")
	}

}

func runStatus() {
	units, err := systemd.ListUnitsWithPrefix("linuxio")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to query systemd: %v\n", err)
		os.Exit(1)
	}

	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })

	const header = "  UNIT                                      LOAD    ACTIVE   SUB      DESCRIPTION"
	maxWidth := len(header)
	type row struct {
		dot  string
		text string
	}
	rows := make([]row, 0, len(units))
	for _, u := range units {
		var dot string
		switch u.ActiveState {
		case "active":
			dot = "\033[32m●\033[0m"
		case "failed":
			dot = "\033[31m●\033[0m"
		default:
			dot = "○"
		}
		text := fmt.Sprintf("%-44s %-8s %-8s %-8s %s", u.Name, u.LoadState, u.ActiveState, u.SubState, u.Description)
		if len(text)+2 > maxWidth {
			maxWidth = len(text) + 2
		}
		rows = append(rows, row{dot: dot, text: text})
	}

	fmt.Printf("  \033[4m%s\033[0m\n", header+strings.Repeat(" ", maxWidth-len(header)))
	for _, r := range rows {
		fmt.Printf("%s %s\n", r.dot, r.text)
	}
	fmt.Printf("\n\033[1m%d loaded units listed.\033[0m\n", len(units))
}

func runLogs(args []string) {
	mode, lines := parseLogsArgs(args)
	journalTerms := journalTermsForMode(mode)
	journalctlArgs := append(strings.Fields(strings.Join(journalTerms, " + ")), "-f", "-n", strconv.Itoa(lines), "--no-pager", "-o", "json")
	cmd := exec.Command("journalctl", journalctlArgs...)

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
		}
	}
	return mode, lines
}

func journalTermsForMode(mode string) []string {
	journalTerms := []string{
		"SYSLOG_IDENTIFIER=linuxio-webserver",
		"SYSLOG_IDENTIFIER=linuxio-bridge",
		"SYSLOG_IDENTIFIER=linuxio-auth",
		"_SYSTEMD_UNIT=linuxio.target",
		"_SYSTEMD_UNIT=linuxio-webserver.service",
		"_SYSTEMD_UNIT=linuxio-webserver.socket",
		"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service",
		"_SYSTEMD_UNIT=linuxio-auth.socket",
		"_SYSTEMD_UNIT=linuxio-auth@.service",
		"_SYSTEMD_UNIT=linuxio-issue.service",
	}

	switch mode {
	case "webserver":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-webserver",
			"_SYSTEMD_UNIT=linuxio-webserver.service",
			"_SYSTEMD_UNIT=linuxio-webserver.socket",
		}
	case "bridge":
		journalTerms = []string{"SYSLOG_IDENTIFIER=linuxio-bridge"}
	case "auth":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-auth",
			"_SYSTEMD_UNIT=linuxio-auth.socket",
			"_SYSTEMD_UNIT=linuxio-auth@.service",
		}
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

	for _, target := range targets {
		var err error
		switch action {
		case "start":
			err = systemd.StartUnit(target)
		case "stop":
			err = systemd.StopUnit(target)
		case "restart":
			err = systemd.RestartUnit(target)
		case "enable":
			err = systemd.EnableUnit(target)
		case "disable":
			err = systemd.DisableUnit(target)
		default:
			fmt.Fprintf(os.Stderr, "Unknown action: %s\n", action)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to %s %s: %v\n", action, target, err)
			os.Exit(1)
		}
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

	fmt.Println("Reloading systemd daemon...")
	if err := systemd.DaemonReload(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Restarting linuxio.target...")
	if err := systemd.RestartUnit(linuxioTargetName); err != nil {
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

	fmt.Println("Reloading systemd daemon...")
	if err := systemd.DaemonReload(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reload systemd daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Restarting linuxio.target...")
	if err := systemd.RestartUnit(linuxioTargetName); err != nil {
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
