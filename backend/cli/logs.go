package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

func isJournalGroup(name string) bool {
	return name == "systemd-journal" || name == "adm"
}

// journalAccessState returns (hasAccess, pendingGroup).
// hasAccess means this process already has journal access in its kernel group list.
// pendingGroup is set when /etc/group grants access but this session predates it.
func journalAccessState() (bool, string) {
	if os.Geteuid() == 0 {
		return true, ""
	}

	// Check actual process groups (what the kernel sees for this session).
	if pgids, err := os.Getgroups(); err == nil {
		for _, gid := range pgids {
			if g, err := user.LookupGroupId(strconv.Itoa(gid)); err == nil && isJournalGroup(g.Name) {
				return true, ""
			}
		}
	}
	// Check /etc/group to distinguish "never added" from "added but not yet active".
	if u, err := user.Current(); err == nil {
		if fgids, err := u.GroupIds(); err == nil {
			for _, gid := range fgids {
				if g, err := user.LookupGroupId(gid); err == nil && isJournalGroup(g.Name) {
					return false, g.Name
				}
			}
		}
	}
	return false, ""
}

func runLogs(args []string) int {
	hasAccess, pendingGroup := journalAccessState()
	if !hasAccess && pendingGroup == "" {
		username := "current"
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
		fmt.Fprintf(os.Stderr, "Error: user %q cannot read the system journal.\n", username)
		fmt.Fprintf(os.Stderr, "Fix: sudo usermod -aG systemd-journal $USER  (then run 'linuxio logs' again; reconnect later to refresh your shell)\n")
		return 1
	}
	mode, lines := parseLogsArgs(args)
	journalTerms := journalTermsForMode(mode)
	journalctlArgs := append(strings.Fields(strings.Join(journalTerms, " + ")), "-f", "-n", strconv.Itoa(lines), "--no-pager", "-o", "json")
	cmd := journalctlCommand(journalctlArgs, pendingGroup)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create pipe: %v\n", err)
		return 1
	}

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start journalctl: %v\n", err)
		return 1
	}

	streamFormattedJournal(stdout)
	return waitForJournalctl(cmd)
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
		case "indexer", "index":
			mode = "indexer"
		case "monitoring", "monitor":
			mode = "monitoring"
		}
	}
	return mode, lines
}

func streamFormattedJournal(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		formatted := formatJournalEntry(scanner.Text())
		if formatted != "" {
			fmt.Println(formatted)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Scanner error: %v\n", err)
	}
}

func waitForJournalctl(cmd *exec.Cmd) int {
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 130 {
			return 0
		}
		return 1
	}
	return 0
}
